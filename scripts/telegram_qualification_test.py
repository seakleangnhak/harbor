#!/usr/bin/env python3
"""Synthetic checks only; no Telegram or owner credentials needed."""
import argparse
import json
from pathlib import Path
import struct
import tempfile
import unittest
import zlib

import telegram_qualification as qualification


class QualificationTests(unittest.TestCase):
    def test_generated_fixtures_are_valid_and_deterministic(self):
        for name in ('small.txt', 'small.png', 'small.pdf', 'multipart.bin'):
            first, _ = qualification.fixture(name, 4 << 20)
            self.assertEqual(first, qualification.fixture(name, 4 << 20)[0])
        png, _ = qualification.fixture('small.png', 4 << 20)
        self.assertEqual(png[:8], b'\x89PNG\r\n\x1a\n')
        offset, image = 8, b''
        while offset < len(png):
            length = struct.unpack('!I', png[offset:offset + 4])[0]
            kind, content = png[offset + 4:offset + 8], png[offset + 8:offset + 8 + length]
            self.assertEqual(zlib.crc32(kind + content), struct.unpack('!I', png[offset + 8 + length:offset + 12 + length])[0])
            if kind == b'IDAT':
                image += content
            offset += length + 12
        self.assertEqual(len(zlib.decompress(image)), 16 * (1 + 16 * 3))
        pdf, _ = qualification.fixture('small.pdf', 4 << 20)
        xref = int(pdf.split(b'startxref\n')[1].splitlines()[0])
        self.assertTrue(pdf[xref:].startswith(b'xref\n0 6\n'))
        rows = pdf[xref:].splitlines()
        for index in range(1, 6):
            offset = int(rows[index + 2].split()[0])
            self.assertTrue(pdf[offset:].startswith(f'{index} 0 obj\n'.encode()))
        multipart, _ = qualification.fixture('multipart.bin', 4 << 20)
        self.assertGreater(len(multipart), 4 << 20)
        self.assertLess(len(multipart), 5 << 20)

    def test_rejects_external_origins_credentials_and_redirects(self):
        for url in ('https://example.com', 'http://127.0.0.1@evil.invalid', 'http://127.0.0.1/?token=x',
                    'http://owner:secret@localhost', 'http://127.0.0.1/path', 'file:///secret'):
            with self.assertRaises(argparse.ArgumentTypeError):
                qualification.validate_url(url)
        self.assertEqual(qualification.validate_url('http://127.0.0.1:8080/'), 'http://127.0.0.1:8080')
        self.assertEqual(qualification.validate_url('http://[::1]:8080'), 'http://[::1]:8080')
        with self.assertRaises(qualification.QualificationError):
            qualification.NoRedirect().redirect_request(None, None, 302, '', {}, 'https://external.invalid')

    def test_state_permissions_validation_and_diagnostics_allowlist(self):
        state = {'version': 1, 'run_id': '123456789abc', 'folder_id': 'a' * 32,
                 'chunk_size': 4 << 20, 'files': {}}
        for name in ('small.txt', 'small.png', 'small.pdf', 'multipart.bin'):
            content, mime = qualification.fixture(name, state['chunk_size'])
            state['files'][name] = {'size': len(content), 'sha256': qualification.digest(content), 'mime_type': mime}
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'state.json'
            qualification.save_state(path, state)
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            self.assertEqual(qualification.load_state(path), state)
            state['files']['small.txt']['upload_id'] = '../unrelated'
            path.write_text(json.dumps(state))
            with self.assertRaises(qualification.QualificationError):
                qualification.load_state(path)
        diagnostic = {'storage_connection': {'state': 'connected', 'retry_count': 2,
                      'session': 'must-never-be-recorded', 'channel': 'must-never-be-recorded'}}
        self.assertEqual(qualification.connection_snapshot(diagnostic), {'state': 'connected', 'retry_count': 2})


if __name__ == '__main__':
    unittest.main()
