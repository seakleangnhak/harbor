from pathlib import Path
import subprocess
import tempfile
import unittest

from release_gate import qualify


class ReleaseGateTest(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        self.git('init', '--initial-branch=main', '--quiet')
        self.git('config', 'user.name', 'Harbor gate test')
        self.git('config', 'user.email', 'test@example.invalid')
        for name in ('.github/workflows/ci.yml', '.github/workflows/docker-publish.yml',
                     'scripts/release_gate.py', 'LICENSE'):
            path = self.root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text('synthetic fixture\n')
        self.git('add', '.')
        self.git('commit', '-qm', 'fixture')
        self.sha = self.git('rev-parse', 'HEAD')
        self.git('update-ref', 'refs/remotes/origin/main', self.sha)
        self.git('tag', '-a', 'v1.0.1', '-m', 'release fixture')

    def git(self, *args):
        return subprocess.check_output(['git', *args], cwd=self.root, text=True,
                                       stderr=subprocess.DEVNULL).strip()

    def qualify(self, **changes):
        values = {'event': 'push', 'ref': 'refs/tags/v1.0.1', 'sha': self.sha,
                  'repository': 'Example-Owner/harbor', 'requested': ''}
        values.update(changes)
        return qualify(self.root, **values)

    def test_stable_push_and_manual_current_tag(self):
        expected = {'tag': 'v1.0.1', 'version': '1.0.1', 'minor': '1.0',
                    'commit': self.sha, 'image': 'ghcr.io/example-owner/harbor'}
        self.assertEqual(self.qualify(), expected)
        self.assertEqual(self.qualify(event='workflow_dispatch', requested='v1.0.1'), expected)

    def test_nonrelease_events_and_refs_rejected(self):
        for event, ref in (('pull_request', 'refs/tags/v1.0.1'),
                           ('push', 'refs/heads/main'),
                           ('workflow_dispatch', 'refs/heads/main'),
                           ('push', 'refs/tags/v1.0.2-rc.1'),
                           ('push', 'refs/tags/v01.0.1'),
                           ('push', 'refs/tags/v1.0.0')):
            with self.subTest(event=event, ref=ref), self.assertRaises(ValueError):
                self.qualify(event=event, ref=ref)

    def test_arbitrary_manual_tag_or_image_rejected(self):
        with self.assertRaises(ValueError):
            self.qualify(event='workflow_dispatch', requested='v1.0.2')
        for repository in ('owner/harbor:latest', 'owner/harbor\ninjected', '../harbor'):
            with self.subTest(repository=repository), self.assertRaises(ValueError):
                self.qualify(repository=repository)

    def test_current_commit_and_annotated_tag_required(self):
        with self.assertRaises(ValueError):
            self.qualify(sha='0' * 40)
        self.git('tag', 'v1.0.2')
        with self.assertRaises(ValueError):
            self.qualify(ref='refs/tags/v1.0.2')

    def test_main_ancestry_required(self):
        self.git('checkout', '--orphan', 'untrusted')
        self.git('commit', '-qm', 'unrelated')
        sha = self.git('rev-parse', 'HEAD')
        self.git('tag', '-a', 'v1.0.2', '-m', 'unrelated fixture')
        with self.assertRaises(subprocess.CalledProcessError):
            self.qualify(ref='refs/tags/v1.0.2', sha=sha)

    def test_latest_cannot_move_backwards(self):
        self.git('tag', '-a', 'v1.1.0', '-m', 'newer fixture')
        with self.assertRaises(ValueError):
            self.qualify()

    def test_required_tree_files(self):
        (self.root / 'LICENSE').unlink()
        with self.assertRaises(ValueError):
            self.qualify()


if __name__ == '__main__':
    unittest.main()
