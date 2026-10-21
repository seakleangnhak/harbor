import argparse
import contextlib
import hashlib
import io
import json
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import textwrap
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
                         publish.replace('github.actor == github.repository_owner', 'true', 1),
                         publish.replace('github.triggering_actor == github.repository_owner', 'true'),
                         publish.replace('  publish:\n    if:', '  publish:\n    # if:'),
                         publish.replace('python3 scripts/release_gate.py', 'true', 1),
                         publish.replace('python3 scripts/ci_check.py package-check\n', 'true\n'),
                         publish.replace('python3 scripts/ci_check.py package-check --required', 'true'),
                         publish.replace('docker pull --platform', 'echo'),
                         publish.replace('python3 scripts/ci_check.py docker-smoke', 'true'),
                         publish + '\n  pull_request:\n',
                         publish + '\n# id-token: write\n',
                         publish + '\n# packages: write\n'):
            with self.subTest(source='publish'), self.assertRaises(check.QualificationError):
                check.workflow_policy(ci, modified)

    def test_package_privacy_and_repository_linkage_fail_closed(self):
        def response(status, body, code=0):
            return subprocess.CompletedProcess([], code, 'HTTP/2.0 ' + str(status) + '\nHeader: fixture\n\n' + json.dumps(body), '')
        private = {'visibility': 'private', 'repository': {'full_name': 'Example-Owner/harbor'}}
        environment = {'GITHUB_REPOSITORY_OWNER': 'Example-Owner', 'GITHUB_REPOSITORY': 'Example-Owner/harbor',
                       'GH_TOKEN': 'synthetic-workflow-token'}
        with mock.patch.dict(os.environ, environment):
            with mock.patch.object(check.subprocess, 'run', return_value=response(200, private)) as request:
                self.assertEqual(check.package_check(True)['visibility'], 'private')
                self.assertIn('users/Example-Owner/packages/container/harbor', request.call_args.args[0])
                self.assertNotIn(environment['GH_TOKEN'], request.call_args.args[0])
            with mock.patch.object(check.subprocess, 'run', return_value=response(404, {}, 1)):
                self.assertEqual(check.package_check(False)['state'], 'not_found')
                with self.assertRaises(check.QualificationError):
                    check.package_check(True)
            for status, body, code in ((401, {}, 1), (403, {}, 1), (500, {}, 1), (200, private, 1),
                                       (200, {**private, 'visibility': 'public'}, 0),
                                       (200, {**private, 'repository': {'full_name': 'Other/harbor'}}, 0),
                                       (200, {**private, 'repository': {'full_name': None}}, 0),
                                       (200, {**private, 'repository': None}, 0), (200, [], 0)):
                with self.subTest(status=status, body=body), mock.patch.object(check.subprocess, 'run', return_value=response(status, body, code)):
                    with self.assertRaises(check.QualificationError):
                        check.package_check(False)
            with mock.patch.object(check.subprocess, 'run', return_value=subprocess.CompletedProcess([], 1, '', 'sensitive error')):
                with self.assertRaisesRegex(check.QualificationError, 'invalid response'):
                    check.package_check(False)
        with mock.patch.dict(os.environ, {**environment, 'GITHUB_REPOSITORY_OWNER': '../Other'}):
            with mock.patch.object(check.subprocess, 'run') as request, self.assertRaises(check.QualificationError):
                check.package_check(False)
            request.assert_not_called()

    def test_published_index_requires_unique_platform_children(self):
        publish = (check.ROOT / '.github/workflows/docker-publish.yml').read_text()
        source = textwrap.dedent(publish.split("          python3 - <<'PY'\n", 1)[1].split('          PY\n', 1)[0])
        children = [{'platform': {'os': 'linux', 'architecture': arch}, 'digest': 'sha256:' + digit * 64}
                    for arch, digit in (('amd64', 'a'), ('arm64', 'b'))]
        attestations = [{'platform': {'os': 'unknown', 'architecture': 'unknown'}, 'digest': 'sha256:' + digit * 64,
                         'annotations': {'vnd.docker.reference.digest': child['digest'],
                                         'vnd.docker.reference.type': 'attestation-manifest'}}
                        for child, digit in zip(children, ('c', 'd'))]
        spdx = {'spdxVersion': 'SPDX-2.3', 'SPDXID': 'SPDXRef-DOCUMENT', 'packages': [{'name': 'fixture'}]}
        slsa = {'buildType': 'https://mobyproject.org/buildkit@v1', 'materials': [{'uri': 'fixture'}]}
        def inspect(manifests, spdx=spdx, slsa=slsa, digest=None):
            raw = json.dumps({'manifests': manifests}).encode()
            files = {name: mock.Mock() for name in ('manifest.json', 'sbom.json', 'provenance.json', 'platform-digests.tsv')}
            files['manifest.json'].read_bytes.return_value = raw
            files['sbom.json'].read_text.return_value = json.dumps({p: {'SPDX': spdx} for p in ('linux/amd64', 'linux/arm64')})
            files['provenance.json'].read_text.return_value = json.dumps({p: {'SLSA': slsa} for p in ('linux/amd64', 'linux/arm64')})
            with mock.patch('pathlib.Path', side_effect=files.__getitem__), contextlib.redirect_stdout(io.StringIO()), \
                    mock.patch.dict(os.environ, {'DIGEST': digest or 'sha256:' + hashlib.sha256(raw).hexdigest()}):
                exec(compile(source, 'docker-publish.yml', 'exec'), {})
            return files['platform-digests.tsv'].write_text
        written = inspect(children + attestations)
        written.assert_called_once_with('linux/amd64\tsha256:' + 'a' * 64 + '\nlinux/arm64\tsha256:' + 'b' * 64 + '\n')
        inspect(children + attestations, slsa={'buildDefinition': {
            'buildType': 'https://github.com/moby/buildkit/blob/master/docs/attestations/slsa-definitions.md',
            'resolvedDependencies': [{'uri': 'fixture'}] * 4}, 'runDetails': {}})
        for invalid in (children[:1] + attestations, children + children[:1] + attestations, children + attestations[:1],
                        [children[0], {**children[1], 'digest': 'not-a-digest'}] + attestations,
                        children + [attestations[0], {**attestations[1], 'annotations': {
                            **attestations[1]['annotations'], 'vnd.docker.reference.type': 'unrelated'}}]):
            with self.subTest(manifests=invalid), self.assertRaises(AssertionError):
                inspect(invalid)
        for options in ({'digest': 'sha256:' + 'f' * 64}, {'spdx': {**spdx, 'packages': []}}, {'slsa': {**slsa, 'materials': []}}):
            with self.subTest(options=options), self.assertRaises(AssertionError):
                inspect(children + attestations, **options)

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

    def test_pulled_release_labels_bind_source_commit_and_license(self):
        source, revision = 'https://github.com/Example-Owner/harbor', 'a' * 40
        labels = {'org.opencontainers.image.source': source, 'org.opencontainers.image.version': '1.0.1',
                  'org.opencontainers.image.revision': revision, 'org.opencontainers.image.licenses': 'MIT',
                  'org.opencontainers.image.description': 'Private, self-hosted Telegram-backed cloud drive',
                  'org.opencontainers.image.documentation': source + '/blob/' + revision + '/README.md'}
        self.assertEqual(check.validate_release_labels({'Config': {'Labels': labels}}, 'Example-Owner/harbor', revision, '1.0.1'), labels)
        for key in labels:
            with self.subTest(key=key), self.assertRaises(check.QualificationError):
                check.validate_release_labels({'Config': {'Labels': {**labels, key: 'incorrect'}}}, 'Example-Owner/harbor', revision, '1.0.1')
        with self.assertRaises(check.QualificationError):
            check.validate_release_labels({'Config': {'Labels': labels}}, '../untrusted', revision, '1.0.1')

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
