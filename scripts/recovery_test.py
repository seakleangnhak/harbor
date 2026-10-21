"""Synthetic subprocess coverage for the encrypted recovery wrapper."""
import contextlib
import hashlib
import io
import json
import os
from pathlib import Path
import sqlite3
import subprocess
import tempfile
import unittest
from unittest.mock import patch

from recovery import ROOT, backup, restore


class RecoveryTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.build = tempfile.TemporaryDirectory(prefix='harbor-recovery-test-')
        cls.binary = Path(cls.build.name) / 'harbor-recovery'
        subprocess.run(['go', 'build', '-o', str(cls.binary), './cmd/harbor-recovery'], cwd=ROOT, check=True)

    @classmethod
    def tearDownClass(cls):
        cls.build.cleanup()

    def setUp(self):
        self.scratch = tempfile.TemporaryDirectory()
        self.addCleanup(self.scratch.cleanup)
        self.root = Path(self.scratch.name)
        self.data = self.root / 'source'
        self.data.mkdir(mode=0o700)
        self.key = self.data / 'content.key'
        self.key.write_bytes(bytes(range(32)))
        self.key.chmod(0o600)
        binding = {'adapter': 'local', 'key_sha256': hashlib.sha256(self.key.read_bytes()).hexdigest()}
        (self.data / 'storage.json').write_text(json.dumps(binding))
        (self.data / 'storage.json').chmod(0o600)
        (self.data / 'blobs').mkdir(mode=0o700)
        with contextlib.closing(sqlite3.connect(self.data / 'drive.db')) as db, db:
            db.executescript("CREATE TABLE owner(id INTEGER PRIMARY KEY,password_hash BLOB);"
                             "INSERT INTO owner VALUES(1,'$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy');"
                             "CREATE TABLE sessions(token_hash TEXT);INSERT INTO sessions VALUES('old');"
                             "CREATE TABLE nodes(name TEXT);INSERT INTO nodes VALUES('example.txt');"
                             "CREATE TABLE storage_uploads(id TEXT);"
                             "CREATE TABLE storage_chunks(upload_id TEXT,idx INTEGER,size INTEGER,sha256 TEXT,ref TEXT);")
        (self.data / 'drive.db').chmod(0o600)
        self.password = self.root / 'backup-password'
        self.password.write_text('synthetic-backup-password-12345\n')
        self.password.chmod(0o600)
        self.archive = self.root / 'backup.harbor'
        self.environment = patch.dict(os.environ, {'HARBOR_RECOVERY_BINARY': str(self.binary)})
        self.environment.start()
        self.addCleanup(self.environment.stop)

    def test_complete_encrypted_roundtrip_and_private_recovery_state(self):
        backup(self.data, self.archive, self.password)
        self.assertEqual(self.archive.read_bytes()[:8], b'HBRBK002')
        self.assertNotIn(b'example.txt', self.archive.read_bytes())
        destination = self.root / 'restored'
        restore(self.archive, destination, self.password)
        self.assertEqual((destination / 'content.key').read_bytes(), self.key.read_bytes())
        self.assertEqual((destination / 'content.key').stat().st_mode & 0o777, 0o600)
        self.assertEqual(destination.stat().st_mode & 0o777, 0o700)
        with contextlib.closing(sqlite3.connect(destination / 'drive.db')) as db:
            self.assertEqual(db.execute('SELECT COUNT(*) FROM sessions').fetchone()[0], 0)
            self.assertEqual(db.execute('SELECT name FROM nodes').fetchone()[0], 'example.txt')

    def test_password_file_is_required_for_noninteractive_runs(self):
        with subprocess.Popen([str(self.binary), 'backup', '--data-dir', str(self.data), '--output', str(self.archive)],
                              stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE) as process:
            _, error = process.communicate()
        self.assertNotEqual(process.returncode, 0)
        self.assertIn(b'--password-file', error)
        self.assertFalse(self.archive.exists())

    def test_plaintext_legacy_archive_and_wrong_password_are_rejected(self):
        backup(self.data, self.archive, self.password)
        self.password.write_text('incorrect-backup-password-12345\n')
        with self.assertRaises(subprocess.CalledProcessError):
            restore(self.archive, self.root / 'wrong', self.password)
        self.assertFalse((self.root / 'wrong').exists())
        legacy = self.root / 'legacy.tar.gz'
        legacy.write_bytes(b'old plaintext archive')
        with self.assertRaises(subprocess.CalledProcessError):
            restore(legacy, self.root / 'legacy', self.password)
        self.assertFalse((self.root / 'legacy').exists())


if __name__ == '__main__':
    unittest.main()
