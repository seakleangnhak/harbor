"""Synthetic control-harness checks; no credentials, Docker calls or network."""
import fcntl
import io
import json
import os
from pathlib import Path
import signal
import subprocess
import tarfile
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

import transfer_live_check as live


class TransferLiveCheckTest(unittest.TestCase):
    def setUp(self):
        self.original_umask = os.umask(0o077)
        self.temporary = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary.name)
        self.data = self.root / 'source-data'
        self.data.mkdir(mode=0o700)
        (self.data / '.lock').touch(mode=0o600)
        self.authorization = b'synthetic invalid authorization fixture'
        (self.data / 'telegram.session').write_bytes(self.authorization)
        self.args = SimpleNamespace(image='sha256:' + 'a' * 64, binary_volume='synthetic-binary',
                                    context='synthetic', source_env=self.root / 'config.env',
                                    output=self.root / 'output' / 'live')
        self.settings = {'DRIVE_DATA_DIR': str(self.data), 'TELEGRAM_APP_ID': '1',
                         'TELEGRAM_APP_HASH': '0' * 32}
        self.target_exists = False
        self.authorization_removed = False
        self.cleanup_attempts = 0
        self.fail_copy = self.fail_cleanup_once = False
        self.check_cleanup_signals = False
        self.operations = []

    def tearDown(self):
        self.temporary.cleanup()
        os.umask(self.original_umask)

    def assert_original_locked(self):
        with (self.data / '.lock').open('r+b') as lock:
            with self.assertRaises(BlockingIOError):
                fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)

    def docker(self, command, **kwargs):
        self.assertEqual(command[:3], ['docker', '--context', 'synthetic'])
        parts = command[3:]
        self.operations.append(parts[0])
        self.assert_original_locked()
        result, output = 0, b''
        if parts[0] == 'create':
            self.target_exists = True
            runtime = Path(parts[parts.index('--env-file') + 1])
            self.assertEqual(runtime.stat().st_mode & 0o777, 0o600)
            self.assertIn('TELEGRAM_SESSION_FILE=/live/telegram.session\n', runtime.read_text())
        elif parts[0] == 'cp':
            if self.fail_copy:
                result, output = 1, b'synthetic-private-output'
            else:
                with tarfile.open(fileobj=io.BytesIO(kwargs['input'])) as archive:
                    self.assertEqual(archive.getnames(), ['telegram.session'])
                    member = archive.getmember('telegram.session')
                    self.assertTrue(member.isfile())
                    self.assertEqual((member.uid, member.gid, member.mode), (65532, 65532, 0o600))
                    self.assertEqual(archive.extractfile(member).read(), self.authorization)
        elif parts[0] == 'inspect':
            output = json.dumps({'Running': False, 'ExitCode': 0, 'OOMKilled': False}).encode()
        elif parts[0] == 'logs':
            output = b'PASS synthetic fixture\n'
        elif parts[0] == 'ps':
            output = b'synthetic-container\n' if self.target_exists else b''
        elif parts[0] == 'rm':
            if self.check_cleanup_signals:
                for kind in (signal.SIGINT, signal.SIGTERM):
                    self.assertEqual(signal.getsignal(kind), signal.SIG_IGN)
                    signal.raise_signal(kind)
            self.target_exists = False
        elif parts[0] == 'run' and parts[-1] == 'rm -f /live/telegram.session /live/.telegram-session-*':
            self.assertFalse(self.target_exists, 'Remove target before copied authorization cleanup')
            self.cleanup_attempts += 1
            if self.fail_cleanup_once and self.cleanup_attempts == 1:
                result, output = 1, b'synthetic-private-output'
            else:
                self.authorization_removed = True
        return subprocess.CompletedProcess(command, result, output, b'')

    def invoke(self):
        with patch.object(live, '__file__', str(self.root / 'scripts' / 'transfer_live_check.py')), \
                patch.object(live, 'config', return_value=self.settings), \
                patch.object(live.subprocess, 'run', side_effect=self.docker), \
                patch.object(live.time, 'sleep'):
            live.run(self.args)

    def test_normal_handoff_holds_original_lock_and_removes_only_copied_authorization(self):
        self.invoke()
        self.assertFalse(self.target_exists)
        self.assertTrue(self.authorization_removed)
        self.assertEqual((self.data / 'telegram.session').read_bytes(), self.authorization)
        result = json.loads((self.args.output / 'result.json').read_text())
        self.assertEqual(result['result'], 'PASS')
        self.assertTrue(result['original_session_unchanged'])
        self.assertTrue(result['copied_authorization_removed'])
        self.assertLess(self.operations.index('rm'), len(self.operations) - 1)
        self.assertNotIn('0' * 32, (self.args.output / 'result.json').read_text())

    def test_failed_import_stops_target_and_hides_private_docker_output(self):
        self.fail_copy = True
        with self.assertRaisesRegex(RuntimeError, '^Docker operation failed: cp; private output withheld$'):
            self.invoke()
        self.assertFalse(self.target_exists)
        self.assertTrue(self.authorization_removed)
        self.assertNotIn('start', self.operations)
        self.assertFalse((self.args.output / 'result.json').exists())

    def test_transient_authorization_cleanup_failure_retries_before_unlocking(self):
        self.fail_cleanup_once = True
        self.invoke()
        self.assertEqual(self.cleanup_attempts, 2)
        self.assertTrue(self.authorization_removed)

    def test_cleanup_defers_repeated_termination_and_restores_signal_handlers(self):
        self.check_cleanup_signals = True
        handlers = {kind: signal.getsignal(kind) for kind in (signal.SIGINT, signal.SIGTERM)}
        self.invoke()
        self.assertTrue(self.authorization_removed)
        for kind, handler in handlers.items():
            self.assertEqual(signal.getsignal(kind), handler)

    def test_interrupted_temporary_cleanup_retries_secrets_removal_before_unlocking(self):
        handlers = {kind: signal.getsignal(kind) for kind in (signal.SIGINT, signal.SIGTERM)}
        private = []
        removed = []
        rmtree = live.shutil.rmtree

        def interrupted_exit(directory, *_):
            private.append(Path(directory.name))
            self.assertTrue((private[0] / 'runtime.env').is_file())
            self.assert_original_locked()
            # Model interruption after TemporaryDirectory detaches its finalizer.
            directory._finalizer.detach()
            raise KeyboardInterrupt

        def remove_private(path, *args, **kwargs):
            self.assert_original_locked()
            self.assertFalse(self.target_exists)
            self.assertTrue(self.authorization_removed)
            for kind in handlers:
                self.assertEqual(signal.getsignal(kind), signal.SIG_IGN)
                signal.raise_signal(kind)
            removed.append(Path(path))
            if len(removed) == 1:
                raise PermissionError('injected temporary secret cleanup failure')
            return rmtree(path, *args, **kwargs)

        with patch.object(live.tempfile.TemporaryDirectory, '__exit__', autospec=True,
                          side_effect=interrupted_exit), \
                patch.object(live.shutil, 'rmtree', side_effect=remove_private):
            with self.assertRaises(KeyboardInterrupt):
                self.invoke()
        self.assertEqual(removed, private * 2)
        self.assertFalse(private[0].exists())
        self.assertEqual(self.cleanup_attempts, 2)
        self.assertEqual((self.data / 'telegram.session').read_bytes(), self.authorization)
        for kind, handler in handlers.items():
            self.assertEqual(signal.getsignal(kind), handler)

    def test_mutable_image_fails_before_private_configuration_or_docker_access(self):
        self.args.image = 'harbor:latest'
        with patch.object(live, 'config') as config, patch.object(live.subprocess, 'run') as docker:
            with self.assertRaisesRegex(ValueError, 'immutable local tooling image ID'):
                live.run(self.args)
            config.assert_not_called()
            docker.assert_not_called()


if __name__ == '__main__':
    unittest.main()
