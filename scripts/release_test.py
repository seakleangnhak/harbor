"""Real Git archives and synthetic unsafe-source checks; no Docker daemon needed."""
import gzip
import hashlib
import io
import json
from pathlib import Path
import subprocess
import tarfile
import tempfile
import sys
import contextlib
from unittest.mock import patch
import unittest

import release


class ReleaseTest(unittest.TestCase):
    def test_archive_is_reproducible_and_excludes_private_untracked_state(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            subprocess.run(['git', 'init', '-q', str(root)], check=True)
            for name in ('Dockerfile', 'compose.yaml', 'go.mod', 'go.sum', 'web/package-lock.json'):
                path = root / name
                path.parent.mkdir(exist_ok=True)
                path.write_text(('FROM example@sha256:' + 'a' * 64 + '\n') * 3 if name == 'Dockerfile' else '{}')
            script = root / 'check.sh'
            script.write_text('#!/bin/sh\nexit 0\n')
            script.chmod(0o755)
            subprocess.run(['git', '-C', str(root), 'add', '.'], check=True)
            subprocess.run(['git', '-C', str(root), '-c', 'user.name=Fixture', '-c',
                            'user.email=fixture@example.invalid', 'commit', '-qm', 'fixture'], check=True)
            commit = release.run('git', 'rev-parse', 'HEAD', cwd=root)
            (root / '.env').write_text('SYNTHETIC_SECRET=must-not-export')
            (root / 'go.mod').write_text('dirty-worktree-must-not-export')
            for number in range(2):
                context = root / str(number) / 'context'
                context.mkdir(parents=True)
                release.source_archive(root, commit, root / (str(number) + '.gz'), context)
                self.assertEqual((context / 'go.mod').read_text(), '{}')
                self.assertFalse((context / '.env').exists())
                self.assertEqual((context / 'go.mod').stat().st_mode & 0o7777, 0o644)
                self.assertEqual((context / 'check.sh').stat().st_mode & 0o7777, 0o755)
            self.assertEqual((root / '0.gz').read_bytes(), (root / '1.gz').read_bytes())
            self.assertNotIn(b'must-not-export', gzip.decompress((root / '0.gz').read_bytes()))

    def test_unsafe_archive_members_are_rejected(self):
        for name, kind in (('../outside', tarfile.REGTYPE), ('/outside', tarfile.REGTYPE),
                           ('folder/../../outside', tarfile.REGTYPE), ('.env', tarfile.REGTYPE),
                           ('data/drive.db', tarfile.REGTYPE), ('content.key', tarfile.REGTYPE),
                           ('output/private.txt', tarfile.REGTYPE), ('web/node_modules/x', tarfile.REGTYPE),
                           ('nested/data-custom/file', tarfile.REGTYPE), ('nested/private.session.json', tarfile.REGTYPE),
                           ('nested/state.sqlite3-wal', tarfile.REGTYPE), ('nested/recovery-password', tarfile.REGTYPE),
                           ('nested/test-secrets/token', tarfile.REGTYPE), ('nested/secret.harbor.part', tarfile.REGTYPE),
                           ('link', tarfile.SYMTYPE), ('hardlink', tarfile.LNKTYPE)):
            with self.subTest(name=name), tarfile.open(fileobj=io.BytesIO(), mode='w') as archive:
                member = tarfile.TarInfo(name)
                member.type = kind
                archive.addfile(member)
                with self.assertRaises(ValueError):
                    release.safe_members(archive)

    def test_source_archive_rejects_populated_or_symlink_destinations_before_export(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            outside = root / 'outside'
            outside.mkdir()
            sentinel = outside / 'keep'
            sentinel.write_text('must-not-change')
            linked = root / 'linked'
            linked.symlink_to(outside, target_is_directory=True)
            populated = root / 'populated'
            populated.mkdir()
            (populated / 'nested').symlink_to(outside, target_is_directory=True)
            for context in (outside, linked, populated):
                with self.subTest(context=context), patch.object(release, 'run') as export, self.assertRaises(ValueError):
                    release.source_archive(root, 'trusted-commit', root / 'archive.gz', context)
                export.assert_not_called()
            self.assertEqual(sentinel.read_text(), 'must-not-change')

    def test_base_images_must_all_be_pinned(self):
        pinned = ('FROM example@sha256:' + 'a' * 64 + '\n') * 3
        self.assertEqual(len(release.base_images(pinned)), 3)
        self.assertEqual(len(release.base_images(pinned.replace('FROM ', 'FROM --platform=$BUILDPLATFORM '))), 3)
        for value in ('FROM node:24\n', pinned.replace('@sha256:', '@sha256:wrong', 1)):
            with self.assertRaises(ValueError):
                release.base_images(value)

    def test_base_images_allow_only_previously_declared_stage_aliases(self):
        pinned = 'example@sha256:' + 'a' * 64
        source = ('FROM --platform=$BUILDPLATFORM ' + pinned + ' AS source\n'
                  'FROM source AS checks\nFROM SOURCE AS server\n'
                  'FROM ' + pinned + ' AS web\nFROM ' + pinned + '\n')
        self.assertEqual(release.base_images(source), [pinned] * 3)
        self.assertEqual(len(release.base_images((release.ROOT / 'Dockerfile').read_text())), 3)
        for invalid in (source.replace('FROM source AS checks', 'FROM unknown AS checks'),
                        'FROM source AS premature\n' + source,
                        source.replace(pinned, 'example:latest', 1)):
            with self.subTest(source=invalid), self.assertRaises(ValueError):
                release.base_images(invalid)

    def test_untrusted_versions_fail_before_build_or_git_access(self):
        for version in ('v1.0.1', '01.0.1', '1.0.1-rc.1', '1.0.1;command'):
            with self.subTest(version=version), patch.object(sys, 'argv', ['release.py', '--ref', 'HEAD', '--version', version]), patch.object(release, 'run') as run, contextlib.redirect_stderr(io.StringIO()):
                with self.assertRaises(SystemExit):
                    release.main()
                run.assert_not_called()

    def test_existing_outputs_and_symlinks_are_not_replaced(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            file = root / 'existing'
            file.write_text('keep')
            link = root / 'link'
            link.symlink_to(root / 'absent')
            for path in (root, file, link):
                with self.subTest(path=path), self.assertRaises(ValueError):
                    release.fresh_output(path)
            self.assertEqual(file.read_text(), 'keep')
            self.assertTrue(link.is_symlink())

    def test_exported_manifest_digest_is_verified(self):
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / 'image.tar'
            digest = 'sha256:' + hashlib.sha256(b'fixture manifest').hexdigest()
            for content in (b'fixture manifest', b'changed manifest'):
                with tarfile.open(path, 'w') as archive:
                    for name, data in (('index.json', json.dumps({'manifests': [{'digest': digest}]}).encode()),
                                       ('blobs/sha256/' + digest.removeprefix('sha256:'), content)):
                        member = tarfile.TarInfo(name)
                        member.size = len(data)
                        archive.addfile(member, io.BytesIO(data))
                if content == b'fixture manifest':
                    self.assertEqual(release.image_manifest_digest(path), digest)
                else:
                    with self.assertRaises(ValueError):
                        release.image_manifest_digest(path)


if __name__ == '__main__':
    unittest.main()
