"""Synthetic qualification boundaries; no Docker, credentials or network."""
import io
import tarfile
import unittest
import json
from unittest.mock import patch
from types import SimpleNamespace
from pathlib import Path

import phase6_live_check as live


class LiveCheckTest(unittest.TestCase):
    def test_docker_resource_units_need_no_space(self):
        sample = live.Samples('synthetic')
        def stats(*args, **kwargs):
            sample.stop.set()
            return json.dumps({'MemUsage': '17.5MiB / 768MiB', 'CPUPerc': '2.5%'}).encode()
        with patch.object(live, 'docker', stats):
            sample.sample()
        self.assertEqual(sample.values, [{'memory_bytes': int(17.5 * (1 << 20)), 'cpu_percent': 2.5}])

    def test_session_import_preserves_nonroot_private_ownership_and_exact_bytes(self):
        expected = {'telegram.session': b'synthetic session', 'archive-password': b'synthetic password'}
        with tarfile.open(fileobj=io.BytesIO(live.private_tar(expected))) as archive:
            self.assertEqual(set(archive.getnames()), set(expected))
            for member in archive:
                self.assertTrue(member.isfile())
                self.assertEqual((member.uid, member.gid, member.mode), (65532, 65532, 0o600))
                self.assertEqual(archive.extractfile(member).read(), expected[member.name])

    def test_mutable_image_and_nonisolated_output_fail_before_any_secret_or_docker_access(self):
        for image, output in [('harbor:latest', live.ROOT / 'output/rejected'),
                              ('sha256:' + 'a' * 64, live.ROOT / 'data/rejected')]:
            with self.subTest(image=image), patch.object(live.recovery, 'config') as config, patch.object(live, 'docker') as docker:
                with self.assertRaises(live.q.QualificationError):
                    live.run(SimpleNamespace(image=image, port=8093, output=output, source_env=Path('must-not-read')))
                config.assert_not_called()
                docker.assert_not_called()
