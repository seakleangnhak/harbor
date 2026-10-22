"""Synthetic safety checks; no Docker or private inputs."""
import argparse
import ast
import json
from pathlib import Path
import threading
import unittest

import milestone_c_live_check as live


class LiveSafetyTests(unittest.TestCase):
    def test_archive_and_budget_self_check(self):
        live.self_check()

    def test_config_archive_ownership(self):
        import io
        import tarfile
        with tarfile.open(fileobj=io.BytesIO(live.session_archive(b'synthetic-safe-fixture', 'config.env'))) as archive:
            member = archive.next()
            self.assertEqual((member.name, member.uid, member.gid, member.mode), ('config.env', 65532, 65532, 0o600))
        with self.assertRaises(RuntimeError):
            live.session_archive(b'fixture', '../escape')

    def test_synchronized_budget(self):
        budget = live.TrafficBudget()
        threads = [threading.Thread(target=lambda: [budget.reserve(upload=1024, download=2048) for _ in range(1000)]) for _ in range(4)]
        for thread in threads:
            thread.start()
        for thread in threads:
            thread.join()
        self.assertEqual((budget.upload, budget.download), (4096000, 8192000))
        before = budget.upload, budget.download
        with self.assertRaises(RuntimeError):
            budget.reserve(upload=1536 * live.MIB)
        self.assertEqual((budget.upload, budget.download), before)

    def test_rejects_unsafe_inputs_before_config_or_docker(self):
        image = 'sha256:' + 'a' * 64
        args = argparse.Namespace(duration_seconds=899, image=image, helper_image=image,
            previous_image=None, output=Path('/not-an-approved-output'))
        with self.assertRaises(RuntimeError):
            live.run(args)
        args.duration_seconds = 900
        args.image = 'mutable:latest'
        with self.assertRaises(ValueError):
            live.run(args)
        args.image = image
        with self.assertRaises(ValueError):
            live.run(args)

    def test_network_metric_validation(self):
        self.assertEqual(live.network_bytes('1.25GiB'), 1342177280)
        self.assertEqual(live.network_bytes('2.5kB'), 2500)
        for value in ('?', '-1MB', '1024', '1PB'):
            with self.assertRaises(ValueError):
                live.network_bytes(value)

    def test_archive_only_create_removes_source_mount_and_checks_actual_mounts(self):
        source = ast.parse(Path(live.__file__).read_text())
        create = next(node for node in ast.walk(source) if isinstance(node, ast.FunctionDef) and node.name == 'create')
        sandbox = ['--read-only', '--volume', 'fixture-source:/live', '--volume', 'fixture-restored:/restore']
        mounts = [{'Name': 'fixture-restored', 'Destination': '/restore'}]
        calls = []

        def docker(*parts):
            calls.append(parts)
            return json.dumps(mounts).encode() if parts[0] == 'inspect' else b''

        scope = dict(sandbox=sandbox, runtime={'DRIVE_DATA_DIR': '/restore/fresh'},
                     volume='fixture-source', restore_volume='fixture-restored', container='fixture-app',
                     current_image='fixture-image', check=live.check, docker=docker, json=json, report={})
        exec(compile(ast.Module(body=[create], type_ignores=[]), '<actual create closure>', 'exec'), scope)
        scope['create']()
        self.assertNotIn('fixture-source:/live', calls[0])
        self.assertIn('fixture-restored:/restore', calls[0])
        self.assertIn('fixture-source:/live', sandbox, 'Shared recovery-one-shot mounts must remain intact')
        self.assertEqual(scope['report']['archive_only_mount_proofs'], [{
            'data_dir': '/restore/fresh', 'original_live_mount_absent': True,
            'original_volume_absent': True, 'restore_mount_present': True}])
        for unexpected in ([{'Name': 'fixture-source', 'Destination': '/unexpected'}, *mounts],
                           [{'Name': 'unrelated', 'Destination': '/live'}, *mounts], []):
            with self.subTest(mounts=unexpected), self.assertRaises(RuntimeError):
                mounts[:] = unexpected
                scope['create']()
        scope['runtime']['DRIVE_DATA_DIR'] = '/live'
        calls.clear()
        scope['create']()
        self.assertIn('fixture-source:/live', calls[0])
        self.assertEqual(len(calls), 1, 'Normal startup must not claim archive-only restore proof')

    def test_live_pacing_uses_180_seconds_for_supported_intervals(self):
        source = ast.parse(Path(live.__file__).read_text())
        report = next(node.value for node in ast.walk(source) if isinstance(node, ast.Assign)
                      and any(isinstance(target, ast.Name) and target.id == 'report' for target in node.targets))
        expression = next(value for key, value in zip(report.keys, report.values)
                          if isinstance(key, ast.Constant) and key.value == 'paced_cycle_seconds')
        code = compile(ast.Expression(expression), '<actual live pacing>', 'eval')
        for seconds in (900, 1800):
            with self.subTest(duration_seconds=seconds):
                self.assertEqual(eval(code, {'args': argparse.Namespace(duration_seconds=seconds)}), 180)


if __name__ == '__main__':
    unittest.main()
