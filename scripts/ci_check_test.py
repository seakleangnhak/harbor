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
    def test_workflows_streams_only_synthetic_test_diagnostics(self):
        with mock.patch.dict(os.environ, {'ACTIONLINT': ''}), \
                mock.patch.object(check, 'workflow_policy'), mock.patch.object(check, 'check_readme', return_value={}), \
                mock.patch.object(check.shutil, 'which', return_value='/synthetic/actionlint'), \
                mock.patch.object(check, 'run') as linter, mock.patch.object(check.subprocess, 'run') as tests:
            self.assertEqual(check.workflows()['python_tests'], 'PASS')
            linter.assert_called_once_with('/synthetic/actionlint')
            tests.assert_called_once_with([check.sys.executable, '-m', 'unittest', 'discover', '-s', 'scripts', '-p', '*_test.py'],
                                          cwd=check.ROOT, check=True, timeout=600)
            tests.side_effect = subprocess.CalledProcessError(1, 'synthetic-tests')
            with self.assertRaises(subprocess.CalledProcessError):
                check.workflows()

    def test_generic_tool_failure_keeps_output_redacted(self):
        response = subprocess.CompletedProcess([], 1, b'synthetic-private-output', b'synthetic-private-error')
        with mock.patch.object(check.subprocess, 'run', return_value=response) as command, \
                self.assertRaisesRegex(check.QualificationError, '^docker failed; exit 1$'):
            check.run('docker', 'inspect', 'fixture')
        self.assertTrue(command.call_args.kwargs['capture_output'])

    def test_verified_ci_binds_latest_main_run_and_returns_safe_receipt(self):
        commit, repository = 'a' * 40, 'Example-Owner/harbor'
        candidate = {'id': 321, 'run_attempt': 2, 'head_sha': commit, 'head_branch': 'main',
                     'event': 'push', 'path': '.github/workflows/ci.yml',
                     'head_repository': {'full_name': repository}, 'created_at': '2026-10-22T01:02:03Z',
                     'status': 'completed', 'conclusion': 'success'}
        environment = {'GITHUB_REPOSITORY': repository, 'GH_TOKEN': 'synthetic-workflow-token'}
        older = {**candidate, 'id': 320, 'conclusion': 'failure'}
        with mock.patch.dict(os.environ, environment, clear=True), \
                mock.patch.object(check, 'run', side_effect=[(commit + '\n').encode(),
                    json.dumps({'workflow_runs': [older, candidate]}).encode()]) as request:
            result = check.verified_ci(commit)
        self.assertEqual(result, {'commit': commit, 'run_id': 321, 'run_attempt': 2,
                                 'run_url': 'https://github.com/Example-Owner/harbor/actions/runs/321'})
        self.assertEqual(request.call_args_list[0].args, ('git', 'rev-parse', 'HEAD'))
        command = request.call_args_list[1].args
        self.assertEqual(command[:4], ('gh', 'api', '--hostname', 'github.com'))
        endpoint, query = command[-1].split('?')
        self.assertEqual(endpoint, 'repos/' + repository + '/actions/workflows/ci.yml/runs')
        self.assertEqual(check.urllib.parse.parse_qs(query), {'head_sha': [commit], 'branch': ['main'],
                                                            'event': ['push'], 'per_page': ['10']})
        self.assertEqual(request.call_args_list[1].kwargs, {'timeout': 60})
        self.assertNotIn(environment['GH_TOKEN'], repr(command) + json.dumps(result))

    def test_verified_ci_rejects_untrusted_context_before_api(self):
        commit = 'a' * 40
        environment = {'GITHUB_REPOSITORY': 'Example-Owner/harbor', 'GH_TOKEN': 'synthetic-workflow-token'}
        with mock.patch.dict(os.environ, environment, clear=True):
            for value in ('main', 'A' * 40, commit + '\n', '../untrusted', None):
                with self.subTest(commit=value), mock.patch.object(check, 'run') as request, \
                        self.assertRaises(check.QualificationError):
                    check.verified_ci(value)
                request.assert_not_called()
            for changes in ({'GITHUB_REPOSITORY': '../Other/harbor'}, {'GITHUB_REPOSITORY': ''}, {'GH_TOKEN': ''}):
                with self.subTest(context=changes), mock.patch.dict(os.environ, changes), \
                        mock.patch.object(check, 'run') as request, self.assertRaises(check.QualificationError):
                    check.verified_ci(commit)
                request.assert_not_called()
            with mock.patch.object(check, 'run', return_value=b'b' * 40) as request, \
                    self.assertRaises(check.QualificationError):
                check.verified_ci(commit)
            self.assertEqual(request.call_count, 1)

    def test_verified_ci_rejects_wrong_run_identity_and_missing_proof(self):
        commit, repository = 'a' * 40, 'Example-Owner/harbor'
        candidate = {'id': 321, 'run_attempt': 1, 'head_sha': commit, 'head_branch': 'main',
                     'event': 'push', 'path': '.github/workflows/ci.yml',
                     'head_repository': {'full_name': repository}, 'created_at': '2026-10-22T01:02:03Z',
                     'status': 'completed', 'conclusion': 'success'}
        invalid = [{**candidate, key: value} for key, value in (
            ('head_sha', 'b' * 40), ('head_branch', 'preview'), ('event', 'pull_request'),
            ('path', '.github/workflows/other.yml'), ('path', '.github/workflows/ci.yml@untrusted'),
            ('head_repository', {'full_name': 'Other/harbor'}), ('head_repository', None),
            ('head_repository', {'full_name': None}), ('id', True), ('id', 0), ('run_attempt', 0),
            ('run_attempt', '1'), ('created_at', 'untrusted'), ('status', 'queued'),
            ('status', 'in_progress'), ('conclusion', 'failure'), ('conclusion', 'skipped'),
            ('conclusion', 'cancelled'), ('conclusion', None))]
        payloads = [{'workflow_runs': [value]} for value in invalid]
        payloads += [{'workflow_runs': []}, {'workflow_runs': None}, {'workflow_runs': [{}]},
                     {'workflow_runs': [candidate] * 11}, [], {}]
        environment = {'GITHUB_REPOSITORY': repository, 'GH_TOKEN': 'synthetic-workflow-token'}
        with mock.patch.dict(os.environ, environment, clear=True):
            for payload in payloads:
                with self.subTest(payload=payload), mock.patch.object(check, 'run', side_effect=[
                        commit.encode(), json.dumps(payload).encode()]), self.assertRaises(check.QualificationError):
                    check.verified_ci(commit)
            with mock.patch.object(check, 'run', side_effect=[commit.encode(), b'not JSON']), \
                    self.assertRaises(check.QualificationError):
                check.verified_ci(commit)

    def test_verified_ci_never_falls_back_to_older_success(self):
        commit, repository = 'a' * 40, 'Example-Owner/harbor'
        success = {'id': 321, 'run_attempt': 1, 'head_sha': commit, 'head_branch': 'main',
                   'event': 'push', 'path': '.github/workflows/ci.yml',
                   'head_repository': {'full_name': repository}, 'created_at': '2026-10-22T01:02:03Z',
                   'status': 'completed', 'conclusion': 'success'}
        newer = [{**success, 'id': 322, 'conclusion': 'failure'},
                 {**success, 'id': 322, 'status': 'queued', 'conclusion': None},
                 {**success, 'run_attempt': 2, 'status': 'in_progress', 'conclusion': None},
                 {**success, 'id': 320, 'created_at': '2026-10-22T02:02:03Z', 'conclusion': 'failure'}]
        with mock.patch.dict(os.environ, {'GITHUB_REPOSITORY': repository, 'GH_TOKEN': 'synthetic-workflow-token'}, clear=True):
            for candidate in newer:
                for runs in ([success, candidate], [candidate, success]):
                    with self.subTest(runs=runs), mock.patch.object(check, 'run', side_effect=[
                            commit.encode(), json.dumps({'workflow_runs': runs}).encode()]), \
                            self.assertRaises(check.QualificationError):
                        check.verified_ci(commit)

    def test_workflow_policy_rejects_credential_and_gate_regressions(self):
        ci = (check.ROOT / '.github/workflows/ci.yml').read_text()
        publish = (check.ROOT / '.github/workflows/docker-publish.yml').read_text()
        check.workflow_policy(ci, publish)
        for modified in (ci.replace('contents: read', 'contents: write'),
                         ci.replace('  pull_request:', '  pull_request_target:'),
                         ci.replace('runner: ubuntu-24.04-arm', 'runner: ubuntu-24.04'),
                         ci.replace('go test -p 1 -race -tags telegram ./...', 'true'),
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
                         publish.replace('python3 scripts/ci_check.py verified-ci', 'true'),
                         publish.replace('      actions: read', '      actions: write'),
                         publish.replace('          HARBOR_COMMIT: ${{ needs.gate.outputs.commit }}',
                                         '          HARBOR_COMMIT: ${{ github.sha }}'),
                         publish.replace('          ref: ${{ needs.gate.outputs.commit }}', '          ref: main', 1),
                         publish + '\n# uses: ./.github/workflows/ci.yml\n',
                         publish.replace('python3 scripts/ci_check.py verify-published', 'true'),
                         publish.replace('HARBOR_GHCR_PACKAGE_ID: ${{ vars.HARBOR_GHCR_PACKAGE_ID }}', 'UNAPPROVED: value'),
                         publish.replace("HARBOR_GHCR_PACKAGE_VISIBILITY: ${{ vars.HARBOR_GHCR_PACKAGE_VISIBILITY || 'private' }}", 'UNAPPROVED: value'),
                         publish + '\n  pull_request:\n',
                         publish + '\n# id-token: write\n',
                         publish + '\n# packages: write\n'):
            with self.subTest(source='publish'), self.assertRaises(check.QualificationError):
                check.workflow_policy(ci, modified)
        for command in ('python3 scripts/release_gate.py', 'python3 scripts/ci_check.py package-check'):
            before, last, after = publish.rpartition(command)
            moved = before + 'true' + after + '\n# ' + last + '\n'
            with self.subTest(late_gate=command), self.assertRaises(check.QualificationError):
                check.workflow_policy(ci, moved)

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

    def test_docker_smoke_uses_supported_setup_for_each_image_version(self):
        executable = io.BytesIO()
        with tarfile.open(fileobj=executable, mode='w') as archive:
            member = tarfile.TarInfo('harbor-recovery')
            member.mode = 0o755
            archive.addfile(member)
        for target_version, web_setup in (('1.0.1', False), ('1.0.10', False),
                                          ('1.1.0', True), ('1.10.0', True)):
            metadata = {'Os': 'linux', 'Architecture': 'arm64', 'Config': {'User': '65532',
                        'Entrypoint': ['/app/drive'], 'Healthcheck': {'Test': ['CMD', '/app/drive', 'health']},
                        'Labels': {'org.opencontainers.image.version': target_version}}}

            def docker(*args, **kwargs):
                if args[:3] == ('docker', 'image', 'inspect'):
                    return json.dumps([metadata]).encode()
                if args[:3] == ('docker', 'inspect', '--format'):
                    return b'0'
                if args[:2] == ('docker', 'cp'):
                    if args[2].endswith(':/data/owner-password'):
                        Path(args[3]).write_text('synthetic-legacy-owner-password')
                    elif args[2].endswith(':/app/harbor-recovery'):
                        return executable.getvalue()
                return b''

            with self.subTest(version=target_version), mock.patch.object(check, 'run', side_effect=docker) as commands, \
                    mock.patch.object(check.subprocess, 'run', return_value=subprocess.CompletedProcess([], 1)), \
                    mock.patch.object(check, 'setup_request') as setup, \
                    mock.patch.object(check, 'wait_ready', side_effect=RuntimeError('startup reached')) as ready:
                with self.assertRaisesRegex(RuntimeError, 'startup reached'):
                    check.docker_smoke('harbor:fixture', 'linux/arm64', target_version)
                ready.assert_called_once()
                self.assertEqual(ready.call_args.kwargs, {'setup': web_setup})
                setup.assert_not_called()
                calls = [call.args for call in commands.call_args_list]
                initializer = next(call for call in calls if call[:2] == ('docker', 'create') and call[-1] == 'init')
                self.assertEqual('--volume' in initializer, not web_setup)
                self.assertEqual(any(str(arg).startswith('/data:size=16m') for arg in initializer), web_setup)
                password_copies = [call for call in calls if call[:2] == ('docker', 'cp')
                                   and call[2].endswith(':/data/owner-password')]
                self.assertEqual(len(password_copies), int(not web_setup))

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

    def test_reproducible_build_compares_distinct_outputs_with_shared_cache(self):
        caches, outputs = [], []
        def build(*args, **kwargs):
            path = Path(args[args.index('-o') + 1])
            path.write_bytes(b'executable-' + path.name.encode())
            self.assertIn('-trimpath', args)
            self.assertIn('-buildvcs=false', args)
            caches.append(kwargs['env']['GOCACHE'])
            outputs.append(path)
        with mock.patch.dict(os.environ, {'GOCACHE': '/synthetic/go-cache'}), \
                mock.patch.object(check, 'run', side_effect=build):
            result = check.reproducible('1.0.1')
            self.assertEqual(set(result['reproducible']), {'drive', 'harbor-recovery'})
            self.assertEqual(result['cache_policy'], 'shared')
            self.assertEqual(len(result['build_seconds']), 2)
        self.assertEqual(caches, ['/synthetic/go-cache'] * 4)
        self.assertEqual(len(set(outputs)), 4)
        self.assertNotEqual(outputs[0].parent, outputs[2].parent)
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
