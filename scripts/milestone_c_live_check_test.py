"""Synthetic safety checks; no Docker or private inputs."""
import argparse
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


if __name__ == '__main__':
    unittest.main()
