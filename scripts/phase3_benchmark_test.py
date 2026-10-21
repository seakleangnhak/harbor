import argparse
import copy
import hashlib
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import phase3_benchmark as benchmark
from phase3_benchmark import BLOCK, MIB, checksum, content


class BenchmarkFixtureTest(unittest.TestCase):
    def test_offsets_and_streaming_digest(self):
        data = BLOCK * 3 + BLOCK[:37]
        self.assertEqual(content(MIB - 17, 50), data[MIB - 17:MIB + 33])
        self.assertEqual(content(2 * MIB + 20, 17), data[2 * MIB + 20:2 * MIB + 37])
        self.assertEqual(checksum(len(data)), hashlib.sha256(data).hexdigest())
        self.assertEqual(content(0, 0), b'')

    def test_own_state_validation_and_recheck_preserves_measurements(self):
        state = {'mode': 'live', 'phase': 'baseline', 'folder_id': 'a' * 32, 'status': 'PASS',
                 'elapsed_seconds': 12.5, 'process': {'rss_max_bytes': 123456},
                 'connection_before': {'state': 'connected', 'retry_count': 1},
                 'connection_after': {'state': 'connected', 'retry_count': 2},
                 'files': [{'id': 'b' * 32, 'size': MIB, 'sha256': checksum(MIB), 'chunk_size': 4 * MIB,
                            'chunk_seconds': [1.25], 'completed': True, 'upload_seconds': 5.0,
                            'download': {'seconds': 2.0, 'sha256_equal': True}}]}
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'benchmark.json'
            path.write_text(json.dumps(state))
            self.assertEqual(benchmark.load_state(path, 'live'), state)
            with self.assertRaises(benchmark.q.QualificationError):
                benchmark.load_state(path, 'local')
            for field, wrong in (('id', '../unrelated'), ('size', 0), ('size', 513 * MIB), ('size', True),
                                 ('chunk_size', 0), ('chunk_size', 9 * MIB), ('sha256', '0' * 64),
                                 ('completed', False), ('chunk_seconds', [])):
                bad = copy.deepcopy(state)
                bad['files'][0][field] = wrong
                path.write_text(json.dumps(bad))
                with self.subTest(field=field, wrong=wrong), self.assertRaises(benchmark.q.QualificationError):
                    benchmark.load_state(path, 'live')
            bad = copy.deepcopy(state)
            bad['files'].append(copy.deepcopy(bad['files'][0]))
            path.write_text(json.dumps(bad))
            with self.assertRaises(benchmark.q.QualificationError):
                benchmark.load_state(path, 'live')
            path.write_text(json.dumps(state))
            args = argparse.Namespace(state_file=str(path), recheck=True, live=True, base_url='http://127.0.0.1:8084',
                                      password_file='synthetic-not-read', server_pid=None, label='after-recreation')
            with patch.object(benchmark.q, 'Client') as client, patch.object(benchmark, 'download') as download:
                client.return_value.request.return_value = ({'adapter': 'telegram', 'storage_connection': {'state': 'connected', 'retry_count': 3}}, {})
                download.return_value = {'seconds': 3.0, 'mib_per_second': 1 / 3, 'sha256_equal': True}
                benchmark.run(args)
                self.assertEqual(client.return_value.request.call_args.args, ('POST', '/auth/logout', {}))
            checked = json.loads(path.read_text())
            rechecks = checked.pop('rechecks')
            self.assertEqual(checked, state)
            self.assertEqual(len(rechecks), 1)
            self.assertEqual(rechecks[0]['label'], 'after-recreation')
            self.assertEqual(rechecks[0]['status'], 'PASS')
            self.assertEqual(rechecks[0]['files'][0]['download']['seconds'], 3.0)


if __name__ == '__main__':
    unittest.main()
