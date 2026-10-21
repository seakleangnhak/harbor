import argparse
import io
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest import mock

import ci_check as check


class CICheckTest(unittest.TestCase):
    def test_workflow_policy_rejects_credential_and_gate_regressions(self):
        ci = (check.ROOT / '.github/workflows/ci.yml').read_text()
        publish = (check.ROOT / '.github/workflows/docker-publish.yml').read_text()
        check.workflow_policy(ci, publish)
        for modified in (ci.replace('contents: read', 'contents: write'),
                         ci.replace('  pull_request:', '  pull_request_target:'),
                         ci + '\n# ${{secrets.GHCR_TOKEN}}',
                         ci.replace('push: false', 'push: true'),
                         check.re.sub(r'actions/checkout@[0-9a-f]{40}', 'actions/checkout@v7', ci)):
            with self.subTest(source='ci'), self.assertRaises(check.QualificationError):
                check.workflow_policy(modified, publish)
        for modified in (publish.replace('needs: [gate, verify]', 'needs: gate'),
                         publish.replace('environment: ghcr-release', 'environment: unprotected'),
                         publish.replace('python3 scripts/release_gate.py', 'true', 1),
                         publish + '\n  pull_request:\n',
                         publish + '\n# id-token: write\n',
                         publish + '\n# packages: write\n'):
            with self.subTest(source='publish'), self.assertRaises(check.QualificationError):
                check.workflow_policy(ci, modified)

    def test_stable_versions_only(self):
        self.assertEqual(check.version('1.0.1'), '1.0.1')
        for value in ('v1.0.1', '01.0.1', '1.0.1-rc.1', '1.0.1;echo secret', '1.0.1\n'):
            with self.subTest(value=value), self.assertRaises(argparse.ArgumentTypeError):
                check.version(value)

    def test_image_runtime_contract(self):
        value = {'Os': 'linux', 'Architecture': 'arm64', 'Config': {'User': '65532',
                 'Entrypoint': ['/app/drive'], 'Healthcheck': {'Test': ['CMD', '/app/drive', 'health']},
                 'Labels': {'org.opencontainers.image.version': '1.0.1'}}}
        check.validate_image(value, 'linux/arm64', '1.0.1')
        for field, wrong in (('User', '0'), ('Entrypoint', ['/bin/sh']), ('Labels', {}), ('Healthcheck', {})):
            modified = {**value, 'Config': {**value['Config'], field: wrong}}
            with self.subTest(field=field), self.assertRaises(check.QualificationError):
                check.validate_image(modified, 'linux/arm64', '1.0.1')
        with self.assertRaises(check.QualificationError):
            check.validate_image(value, 'linux/amd64', '1.0.1')

    def test_private_archive_modes_and_missing_key(self):
        def archive(mode=0o600, missing=False):
            data = io.BytesIO()
            with tarfile.open(fileobj=data, mode='w') as output:
                for name in ('content.key', 'owner-password', 'drive.db', 'storage.json'):
                    if missing and name == 'content.key':
                        continue
                    member = tarfile.TarInfo('./' + name)
                    member.mode = mode
                    output.addfile(member)
            return data.getvalue()
        check.validate_private_modes(archive())
        for content in (archive(mode=0o644), archive(missing=True)):
            with self.assertRaises(check.QualificationError):
                check.validate_private_modes(content)

    def test_reproducible_build_compares_both_independent_outputs(self):
        caches = []
        def build(*args, **kwargs):
            path = Path(args[args.index('-o') + 1])
            path.write_bytes(b'executable-' + path.name.encode())
            self.assertIn('-trimpath', args)
            self.assertIn('-buildvcs=false', args)
            caches.append(kwargs['env']['GOCACHE'])
            self.assertEqual(Path(caches[-1]).parent, path.parent)
        with mock.patch.object(check, 'run', side_effect=build):
            self.assertEqual(set(check.reproducible('1.0.1')['reproducible']), {'drive', 'harbor-recovery'})
        self.assertEqual(caches[0], caches[1])
        self.assertEqual(caches[2], caches[3])
        self.assertNotEqual(caches[0], caches[2])
        def different(*args, **kwargs):
            path = Path(args[args.index('-o') + 1])
            path.write_bytes(path.parent.name.encode())
        with mock.patch.object(check, 'run', side_effect=different), self.assertRaises(check.QualificationError):
            check.reproducible('1.0.1')

    def test_readme_links_and_safe_original_svg(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            assets = root / 'docs/assets'
            assets.mkdir(parents=True)
            logo = assets / 'logo.svg'
            logo.write_text('<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0"/></svg>')
            readme = root / 'README.md'
            readme.write_text('## Get started\n[Start](#get-started) ![Logo](docs/assets/logo.svg)')
            self.assertEqual(check.check_readme(root)['safe_svg_assets'], 1)
            for text in ('[Missing](missing.md)', '[Invalid](#missing)', '[Outside](../../outside.md)'):
                readme.write_text(text)
                with self.assertRaises(check.QualificationError):
                    check.check_readme(root)
            readme.write_text('## Get started\n')
            for content in ('<svg><script/></svg>', '<svg onload="alert(1)"/>',
                            '<svg><a href="https://example.invalid"/></svg>',
                            '<svg style="fill:url(https://example.invalid)"/>'):
                logo.write_text(content)
                with self.assertRaises(check.QualificationError):
                    check.check_readme(root)


if __name__ == '__main__':
    unittest.main()
