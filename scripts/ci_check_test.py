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
                         publish.replace('python3 scripts/ci_check.py verify-published', 'true'),
                         publish.replace('HARBOR_GHCR_PACKAGE_ID: ${{ vars.HARBOR_GHCR_PACKAGE_ID }}', 'UNAPPROVED: value'),
                         publish.replace("HARBOR_GHCR_PACKAGE_VISIBILITY: ${{ vars.HARBOR_GHCR_PACKAGE_VISIBILITY || 'private' }}", 'UNAPPROVED: value'),
                         publish + '\n  pull_request:\n',
                         publish + '\n# id-token: write\n',
                         publish + '\n# packages: write\n'):
            with self.subTest(source='publish'), self.assertRaises(check.QualificationError):
                check.workflow_policy(ci, modified)

    def test_package_privacy_and_repository_linkage_fail_closed(self):
        def response(status, body, code=0):
            return subprocess.CompletedProcess([], code, 'HTTP/2.0 ' + str(status) + '\nHeader: fixture\n\n' + json.dumps(body), '')
        private = {'id': 123, 'name': 'harbor', 'visibility': 'private', 'repository': {'full_name': 'Example-Owner/harbor'}}
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

    def test_owner_verified_package_pin_handles_omitted_linkage_only(self):
        environment = {'GITHUB_REPOSITORY_OWNER': 'Example-Owner', 'GITHUB_REPOSITORY': 'Example-Owner/harbor',
                       'GH_TOKEN': 'synthetic-workflow-token', 'HARBOR_GHCR_PACKAGE_ID': '123'}
        private = {'id': 123, 'name': 'harbor', 'visibility': 'private'}
        def request(body, status=200):
            return subprocess.CompletedProcess([], 0 if status == 200 else 1,
                'HTTP/2.0 ' + str(status) + '\n\n' + json.dumps(body), '')
        with mock.patch.dict(os.environ, environment):
            with mock.patch.object(check.subprocess, 'run', return_value=request(private)):
                self.assertIn('owner_verified_package_id', check.package_check(True)['repository_linkage'])
            for body in ({**private, 'repository': None}, {**private, 'repository': {}},
                         {**private, 'repository': {'full_name': 'Other/harbor'}},
                         {**private, 'visibility': 'public'}, {**private, 'name': 'other'},
                         {**private, 'id': 124}, {**private, 'id': '123'}, {**private, 'id': True}):
                with self.subTest(body=body), mock.patch.object(check.subprocess, 'run', return_value=request(body)):
                    with self.assertRaises(check.QualificationError):
                        check.package_check(False)
            with mock.patch.object(check.subprocess, 'run', return_value=request({}, 404)):
                with self.assertRaises(check.QualificationError):
                    check.package_check(False)
            for pin in ('', '0', '-1', '0123', '123\ninjected', '1' * 21):
                with self.subTest(pin=pin), mock.patch.dict(os.environ, {'HARBOR_GHCR_PACKAGE_ID': pin}), \
                        mock.patch.object(check.subprocess, 'run', return_value=request(private)):
                    with self.assertRaises(check.QualificationError):
                        check.package_check(True)
            linked = {**private, 'repository': {'full_name': 'Example-Owner/harbor'}}
            with mock.patch.object(check.subprocess, 'run', return_value=request({**linked, 'id': 456})):
                with self.assertRaises(check.QualificationError):
                    check.package_check(True)

    def test_package_visibility_matches_explicit_owner_policy(self):
        environment = {'GITHUB_REPOSITORY_OWNER': 'Example-Owner', 'GITHUB_REPOSITORY': 'Example-Owner/harbor',
                       'GH_TOKEN': 'synthetic-workflow-token', 'HARBOR_GHCR_PACKAGE_ID': '123'}
        package = {'id': 123, 'name': 'harbor', 'visibility': 'public',
                   'repository': {'full_name': 'Example-Owner/harbor'}}
        response = subprocess.CompletedProcess([], 0, 'HTTP/2.0 200\n\n' + json.dumps(package), '')
        with mock.patch.dict(os.environ, environment, clear=True):
            with mock.patch.object(check.subprocess, 'run', return_value=response):
                with self.assertRaises(check.QualificationError):
                    check.package_check(True)
            with mock.patch.dict(os.environ, {'HARBOR_GHCR_PACKAGE_VISIBILITY': 'public'}), \
                    mock.patch.object(check.subprocess, 'run', return_value=response):
                self.assertEqual(check.package_check(True)['visibility'], 'public')
                for changed in ({**package, 'visibility': 'private'}, {**package, 'id': 124},
                                {**package, 'repository': {'full_name': 'Other/harbor'}}):
                    response.stdout = 'HTTP/2.0 200\n\n' + json.dumps(changed)
                    with self.assertRaises(check.QualificationError):
                        check.package_check(True)
            for value in ('PUBLIC', 'internal', ' public', 'public\n', 'any'):
                with mock.patch.dict(os.environ, {'HARBOR_GHCR_PACKAGE_VISIBILITY': value}), \
                        mock.patch.object(check.subprocess, 'run') as request:
                    with self.assertRaises(check.QualificationError):
                        check.package_check(True)
                    request.assert_not_called()

    def test_readonly_verifier_workflow_cannot_publish(self):
        ci, publish, verify = [(check.ROOT / '.github/workflows' / name).read_text()
                              for name in ('ci.yml', 'docker-publish.yml', 'verify-published.yml')]
        check.workflow_policy(ci, publish, verify)
        for changed in (verify.replace('packages: read', 'packages: write'),
                        verify.replace("github.ref == 'refs/heads/main'", 'true'),
                        verify.replace('github.actor == github.repository_owner', 'true'),
                        verify.replace('github.triggering_actor == github.repository_owner', 'true'),
                        verify.replace("HARBOR_GHCR_PACKAGE_VISIBILITY: ${{ vars.HARBOR_GHCR_PACKAGE_VISIBILITY || 'private' }}", 'UNAPPROVED: value'),
                        verify + '\n  push:\n', verify + '\n# docker/build-push-action@\n',
                        verify.replace('verify-published --tag', 'true --tag')):
            with self.subTest(source=changed), self.assertRaises(check.QualificationError):
                check.workflow_policy(ci, publish, changed)

    def test_published_index_requires_unique_platform_children(self):
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
            return check.published_index(raw,
                {p: {'SPDX': spdx} for p in ('linux/amd64', 'linux/arm64')},
                {p: {'SLSA': slsa} for p in ('linux/amd64', 'linux/arm64')},
                digest or 'sha256:' + hashlib.sha256(raw).hexdigest())
        self.assertEqual([v['child_digest'] for v in inspect(children + attestations)],
                         ['sha256:' + 'a' * 64, 'sha256:' + 'b' * 64])
        inspect(children + attestations, slsa={'buildDefinition': {
            'buildType': 'https://github.com/moby/buildkit/blob/master/docs/attestations/slsa-definitions.md',
            'resolvedDependencies': [{'uri': 'fixture'}] * 4}, 'runDetails': {}})
        for invalid in (children[:1] + attestations, children + children[:1] + attestations, children + attestations[:1],
                        [children[0], {**children[1], 'digest': 'not-a-digest'}] + attestations,
                        children + [attestations[0], {**attestations[1], 'annotations': {
                            **attestations[1]['annotations'], 'vnd.docker.reference.type': 'unrelated'}}]):
            with self.subTest(manifests=invalid), self.assertRaises(check.QualificationError):
                inspect(invalid)
        for options in ({'digest': 'sha256:' + 'f' * 64}, {'spdx': {**spdx, 'packages': []}}, {'slsa': {**slsa, 'materials': []}}):
            with self.subTest(options=options), self.assertRaises(check.QualificationError):
                inspect(children + attestations, **options)

    def test_published_reference_rejects_untrusted_inputs_before_network(self):
        with mock.patch.dict(os.environ, {'GITHUB_REPOSITORY': 'Example-Owner/harbor'}):
            for tag, digest in (('main', 'sha256:' + 'a' * 64), ('v1.0.0', 'sha256:' + 'a' * 64),
                                ('v1.0.1;echo unsafe', 'sha256:' + 'a' * 64), ('v1.0.1', 'latest')):
                with self.subTest(tag=tag), mock.patch.object(check, 'run') as run:
                    with self.assertRaises(check.QualificationError):
                        check.verify_published(tag, digest)
                    run.assert_not_called()
            with mock.patch.object(check, 'run', return_value=b'commit\n'):
                with self.assertRaises(check.QualificationError):
                    check.verify_published('v1.0.1', 'sha256:' + 'a' * 64)

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

    def test_web_setup_archive_requires_private_owner_and_config(self):
        def archive(mode=0o600, missing=False, directory_mode=0o700):
            data = io.BytesIO()
            with tarfile.open(fileobj=data, mode='w') as output:
                setup = tarfile.TarInfo('./setup')
                setup.type, setup.mode = tarfile.DIRTYPE, directory_mode
                output.addfile(setup)
                for name in ('content.key', 'drive.db', 'storage.json', 'setup/owner-password', 'setup/config.env'):
                    if missing and name == 'setup/config.env':
                        continue
                    member = tarfile.TarInfo('./' + name)
                    member.mode = mode
                    output.addfile(member)
            return data.getvalue()
        check.validate_private_modes(archive(), web_setup=True)
        for content in (archive(mode=0o644), archive(missing=True), archive(directory_mode=0o755)):
            with self.assertRaises(check.QualificationError):
                check.validate_private_modes(content, web_setup=True)

    def test_setup_request_sends_credentials_in_headers_and_bounded_body(self):
        response = io.BytesIO(b'{"account_ready":true}')
        response.status = 200
        with mock.patch.object(check.urllib.request, 'build_opener') as opener:
            opener.return_value.open.return_value = response
            self.assertEqual(check.setup_request('http://127.0.0.1:1234', 'POST', '/account',
                             {'password': 'synthetic-owner-password'}, 'synthetic-setup-token'), {'account_ready': True})
            request = opener.return_value.open.call_args.args[0]
            self.assertEqual(request.full_url, 'http://127.0.0.1:1234/api/v1/setup/account')
            self.assertEqual(request.get_header('X-harbor-setup-token'), 'synthetic-setup-token')
            self.assertEqual(request.get_header('Origin'), 'http://127.0.0.1:1234')
            self.assertEqual(json.loads(request.data), {'password': 'synthetic-owner-password'})
        response = check.urllib.error.HTTPError('http://127.0.0.1:1234/api/v1/setup/account', 404, '', {}, io.BytesIO(b'404 page not found'))
        with mock.patch.object(check.urllib.request, 'build_opener') as opener:
            opener.return_value.open.side_effect = response
            self.assertIsNone(check.setup_request('http://127.0.0.1:1234', 'POST', '/account', {}, expect=404))
        for status, content in ((401, b'{}'), (200, b'x' * 16385)):
            response = io.BytesIO(content)
            response.status = status
            with mock.patch.object(check.urllib.request, 'build_opener') as opener:
                opener.return_value.open.return_value = response
                with self.assertRaises(check.QualificationError):
                    check.setup_request('http://127.0.0.1:1234', 'POST', '/verify', {})

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
