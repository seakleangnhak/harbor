#!/usr/bin/env python3
"""CI and release checks. Docker smoke uses only fresh, disposable local state."""
import argparse
import hashlib
import io
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
import xml.etree.ElementTree as ET

from telegram_qualification import Client, NoRedirect, QualificationError, digest, fixture


ROOT = Path(__file__).resolve().parent.parent
VERSION = re.compile(r'(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)')


def run(*args, env=None, timeout=600):
    result = subprocess.run(args, cwd=ROOT, env=env, capture_output=True, timeout=timeout)
    if result.returncode:
        # Never echo tool output: even a malformed image may print sensitive data.
        raise QualificationError(args[0] + ' failed; exit ' + str(result.returncode))
    return result.stdout


def version(value):
    if not VERSION.fullmatch(value):
        raise argparse.ArgumentTypeError('Use stable major.minor.patch without a v prefix')
    return value


def workflow_policy(ci, publish, verification=None):
    """Additional fail-closed guards for actionlint-validated release workflows."""
    # ponytail: fixed workflow shapes; use parsed policy when adding more release paths.
    for source in (ci, publish, *([verification] if verification is not None else [])):
        if any(word in source for word in ('pull_request_target:', 'write-all', 'secrets: inherit')):
            raise QualificationError('Privileged workflow event or inherited secrets forbidden')
        for action in re.findall(r'\buses:\s*([^\s#]+)', source):
            if not action.startswith('./') and not re.fullmatch(r'[\w.-]+/[\w./-]+@[0-9a-f]{40}', action):
                raise QualificationError('External actions must use full commit SHAs')
        if not re.search(r'^permissions:\n  contents: read\n', source, re.M):
            raise QualificationError('Default workflow token must be read-only')
    if (re.search(r'\$\{\{[^}]*\bsecrets\b', ci) or re.search(r'\b(?:packages|contents|id-token|attestations):\s*write\b', ci)
            or re.search(r'\bpush: true\b', ci) or 'docker/login-action@' in ci):
        raise QualificationError('Ordinary CI must not receive publication credentials')
    required = ('  pull_request:\n', 'branches: [main]', 'arch: [amd64, arm64]', 'push: false',
                'python3 scripts/ci_check.py reproducible', 'python3 scripts/ci_check.py docker-smoke')
    if not all(text in ci for text in required):
        raise QualificationError('CI must retain PR, main, reproducibility and both architecture checks')
    owner_gate = '    if: ${{ github.actor == github.repository_owner && github.triggering_actor == github.repository_owner }}\n'
    required = ("tags: ['v*']", '  workflow_dispatch:\n', 'python3 scripts/release_gate.py',
                'needs: gate', 'uses: ./.github/workflows/ci.yml', 'needs: [gate, verify]',
                '  gate:\n' + owner_gate, '  publish:\n' + owner_gate,
                '      packages: write\n', '${{ secrets.GITHUB_TOKEN }}',
                'platforms: linux/amd64,linux/arm64', 'provenance: mode=min', 'sbom: generator=',
                'python3 scripts/ci_check.py package-check\n',
                'python3 scripts/ci_check.py verify-published --tag "v$HARBOR_VERSION" --digest "$DIGEST"',
                'HARBOR_GHCR_PACKAGE_ID: ${{ vars.HARBOR_GHCR_PACKAGE_ID }}',
                "HARBOR_GHCR_PACKAGE_VISIBILITY: ${{ vars.HARBOR_GHCR_PACKAGE_VISIBILITY || 'private' }}")
    if not all(text in publish for text in required) or '  pull_request:' in publish or 'branches:' in publish:
        raise QualificationError('Publication must remain stable-tag gated after complete validation')
    if (publish.count('packages: write') != 1 or publish.count('python3 scripts/release_gate.py') != 2
            or re.search(r'\b(?:contents|id-token|attestations):\s*write\b', publish)):
        raise QualificationError('Publication permission and immediate pre-login gate required')

    if verification is not None:
        required = ('  workflow_dispatch:', '      release_tag:', '      digest:', '  packages: read',
                    "github.ref == 'refs/heads/main'", 'github.actor == github.repository_owner',
                    'github.triggering_actor == github.repository_owner',
                    'HARBOR_GHCR_PACKAGE_ID: ${{ vars.HARBOR_GHCR_PACKAGE_ID }}',
                    "HARBOR_GHCR_PACKAGE_VISIBILITY: ${{ vars.HARBOR_GHCR_PACKAGE_VISIBILITY || 'private' }}",
                    'verify-published --tag "$RELEASE_TAG" --digest "$DIGEST"')
        if (not all(text in verification for text in required)
                or any(text in verification for text in ('  push:', '  pull_request:', 'docker/build-push-action@'))
                or re.search(r'\b(?:packages|contents|id-token|attestations):\s*write\b', verification)):
            raise QualificationError('Published-image verification must remain owner-only and read-only')


def package_check(required):
    owner = os.environ.get('GITHUB_REPOSITORY_OWNER', '')
    repository = os.environ.get('GITHUB_REPOSITORY', '')
    if (not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9-]*', owner)
            or not re.fullmatch(re.escape(owner) + r'/[A-Za-z0-9_.-]+', repository, re.I)
            or not os.environ.get('GH_TOKEN')):
        raise QualificationError('Package check needs trusted repository context and workflow token')
    approved_id = os.environ.get('HARBOR_GHCR_PACKAGE_ID', '')
    if approved_id and not re.fullmatch(r'[1-9][0-9]{0,19}', approved_id):
        raise QualificationError('Approved package ID must be a positive decimal integer')
    expected_visibility = os.environ.get('HARBOR_GHCR_PACKAGE_VISIBILITY') or 'private'
    if expected_visibility not in ('private', 'public'):
        raise QualificationError('Expected package visibility must be private or public')
    response = subprocess.run(['gh', 'api', '--hostname', 'github.com', '--include',
                              '-H', 'Accept: application/vnd.github+json',
                              '-H', 'X-GitHub-Api-Version: 2022-11-28',
                              'users/' + owner + '/packages/container/harbor'],
                             capture_output=True, text=True, timeout=60)
    header, separator, body = response.stdout.partition('\n\n')
    status = re.match(r'HTTP/\S+ ([0-9]{3})\b', header)
    if not status or not separator:
        raise QualificationError('Package API returned an invalid response')
    if status[1] == '404' and not required and not approved_id:
        return {'package': 'harbor', 'state': 'not_found'}
    if response.returncode or status[1] != '200':
        raise QualificationError('Package access or required package verification failed')
    package = json.loads(body)
    if (not isinstance(package, dict) or package.get('name') != 'harbor'
            or package.get('visibility') != expected_visibility):
        raise QualificationError('Package name or owner-approved visibility mismatch')
    if approved_id and (type(package.get('id')) is not int or str(package['id']) != approved_id):
        raise QualificationError('Package differs from owner-approved package ID')
    if 'repository' in package:
        linked = package['repository']
        if (not isinstance(linked, dict) or not isinstance(linked.get('full_name'), str)
                or linked['full_name'].lower() != repository.lower()):
            raise QualificationError('Package repository linkage mismatch')
        linkage = 'api_verified'
    elif approved_id:
        # GITHUB_TOKEN omits this field; owner checks linkage before pinning the package ID.
        linkage = 'owner_verified_package_id; current linkage requires owner API check'
    else:
        raise QualificationError('Package linkage unavailable; owner-verified package ID required')
    return {'package': 'harbor', 'visibility': expected_visibility, 'repository_linkage': linkage}


def workflows():
    workflow_policy(*[(ROOT / '.github/workflows' / name).read_text() for name in ('ci.yml', 'docker-publish.yml', 'verify-published.yml')])
    documentation = check_readme()
    actionlint = os.environ.get('ACTIONLINT') or shutil.which('actionlint')
    if not actionlint:
        local = ROOT / 'output/open-source/tools/actionlint'
        actionlint = str(local) if local.is_file() else None
    if not actionlint:
        raise QualificationError('Install actionlint v1.7.12 or set ACTIONLINT')
    run(actionlint)
    run(sys.executable, '-m', 'unittest', 'discover', '-s', 'scripts', '-p', '*_test.py')
    return {'workflow_policy': 'PASS', 'actionlint': 'PASS', 'python_tests': 'PASS', **documentation}


def check_readme(root=ROOT):
    source = re.sub(r'```.*?```', '', (root / 'README.md').read_text(), flags=re.S)
    anchors = {re.sub(r'[^\w\s-]', '', heading.lower()).replace(' ', '-')
               for heading in re.findall(r'^#{1,6} (.+)$', source, re.M)}
    targets = re.findall(r'!?\[[^\]]*\]\(([^\s)]+)(?:\s+"[^"]*")?\)', source)
    targets += re.findall(r'\b(?:href|src|srcset)="([^"]+)"', source)
    local = set()
    for target in targets:
        parsed = urllib.parse.urlsplit(target)
        if parsed.scheme in ('https', 'http', 'mailto'):
            continue
        if parsed.scheme or parsed.netloc or parsed.query:
            raise QualificationError('Unsupported local README link')
        if parsed.path:
            path = (root / urllib.parse.unquote(parsed.path)).resolve()
            if not path.is_relative_to(root.resolve()) or not path.exists():
                raise QualificationError('Missing or unsafe local README resource')
            local.add(str(path.relative_to(root.resolve())))
        elif parsed.fragment and urllib.parse.unquote(parsed.fragment) not in anchors:
            raise QualificationError('Missing README heading anchor')
    svg_files = list((root / 'docs/assets').glob('*.svg'))
    for path in svg_files:
        source = path.read_text()
        if '<!DOCTYPE' in source or '<!ENTITY' in source:
            raise QualificationError('SVG document declarations forbidden')
        for node in ET.fromstring(source).iter():
            if node.tag.rsplit('}', 1)[-1] in ('script', 'foreignObject', 'image', 'iframe'):
                raise QualificationError('SVG must use vector shapes without active or embedded content')
            for key, value in node.attrib.items():
                key = key.rsplit('}', 1)[-1].lower()
                if key.startswith('on') or key == 'href' and not value.startswith('#'):
                    raise QualificationError('SVG active or external resource forbidden')
        if '@import' in source or any(not value.strip().startswith('#') for value in re.findall(r'url\(\s*["\']?([^\)"\']+)', source)):
            raise QualificationError('SVG external CSS resource forbidden')
    return {'readme_local_resources': len(local), 'readme_heading_anchors': 'PASS', 'safe_svg_assets': len(svg_files)}


def reproducible(target_version):
    hashes = {name: [] for name in ('drive', 'harbor-recovery')}
    seconds = []
    with tempfile.TemporaryDirectory(prefix='harbor-reproducible-') as temporary:
        for attempt in ('first', 'second'):
            start = time.monotonic()
            cache = Path(temporary) / attempt / 'cache'
            cache.mkdir(parents=True)
            environment = dict(os.environ, CGO_ENABLED='0', GOGC='40', GOCACHE=str(cache))
            for name, tags in (('drive', ['-tags', 'telegram']), ('harbor-recovery', [])):
                destination = Path(temporary) / attempt / name
                flags = '-s -w' + (' -X main.buildVersion=' + target_version if name == 'drive' else '')
                run('go', 'build', '-p', '1', *tags, '-trimpath', '-buildvcs=false',
                    '-ldflags=' + flags, '-o', str(destination), './cmd/' + name, env=environment)
                with destination.open('rb') as stream:
                    hashes[name].append(hashlib.file_digest(stream, 'sha256').hexdigest())
            seconds.append(round(time.monotonic() - start, 3))
    if any(values[0] != values[1] for values in hashes.values()):
        raise QualificationError('Independent binary builds differ')
    return {'version': target_version, 'fresh_cache_build_seconds': seconds,
            'reproducible': {name: {'sha256': values[0], 'identical_builds': 2} for name, values in hashes.items()}}


def validate_image(image, platform, target_version):
    config = image['Config']
    if (image['Os'] + '/' + image['Architecture'] != platform or config.get('User') not in ('65532', '65532:65532', 'nonroot')
            or config.get('Entrypoint') != ['/app/drive']
            or config.get('Healthcheck', {}).get('Test') != ['CMD', '/app/drive', 'health']
            or config.get('Labels', {}).get('org.opencontainers.image.version') != target_version):
        raise QualificationError('Image platform, version, nonroot entrypoint or health check invalid')


def validate_release_labels(image, repository, revision, target_version):
    if (not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9_.-]+', repository)
            or not re.fullmatch(r'[0-9a-f]{40}', revision)):
        raise QualificationError('Invalid release repository or revision')
    source = 'https://github.com/' + repository
    expected = {'org.opencontainers.image.source': source, 'org.opencontainers.image.version': target_version,
                'org.opencontainers.image.revision': revision, 'org.opencontainers.image.licenses': 'MIT',
                'org.opencontainers.image.description': 'Private, self-hosted Telegram-backed cloud drive',
                'org.opencontainers.image.documentation': source + '/blob/' + revision + '/README.md'}
    if any(image['Config'].get('Labels', {}).get(key) != value for key, value in expected.items()):
        raise QualificationError('Pulled release image OCI labels do not match approved source')
    return expected


def validate_private_modes(archive_bytes, web_setup=False):
    required = {'content.key', 'drive.db', 'storage.json'}
    required.update({'setup/owner-password', 'setup/config.env'} if web_setup else {'owner-password'})
    seen = set()
    with tarfile.open(fileobj=io.BytesIO(archive_bytes)) as archive:
        for member in archive:
            name = member.name.removeprefix('./')
            if name in required:
                if not member.isfile() or member.mode != 0o600:
                    raise QualificationError('Persistent secret/metadata file permissions invalid')
                seen.add(name)
            if name in ('.', 'data', 'setup') and member.mode != 0o700:
                raise QualificationError('Persistent data directory permissions invalid')
    if seen != required:
        raise QualificationError('Persistent secret/metadata files missing')


def wait_ready(container, timeout=90, setup=False):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        port = run('docker', 'port', container, '8080/tcp').decode().strip()
        if re.fullmatch(r'127\.0\.0\.1:[0-9]+', port):
            origin = 'http://' + port
            try:
                path = '/api/v1/setup/status' if setup else '/ready'
                expected = {'required': True} if setup else {'status': 'ready'}
                with urllib.request.build_opener(NoRedirect()).open(origin + path, timeout=2) as response:
                    if response.status == 200 and json.load(response) == expected:
                        run('docker', 'exec', container, '/app/drive', 'health')
                        return origin
            except (OSError, ValueError, urllib.error.URLError):
                pass
        time.sleep(.25)
    raise QualificationError('Isolated container did not reach expected setup/readiness state')


def setup_request(origin, method, path, body=None, token='', expect=200, request_origin=None):
    headers = {'Origin': origin if request_origin is None else request_origin}
    if token:
        headers['X-Harbor-Setup-Token'] = token
    if body is not None:
        body = json.dumps(body).encode()
        headers['Content-Type'] = 'application/json'
    request = urllib.request.Request(origin + '/api/v1/setup' + path, body, headers, method=method)
    try:
        response = urllib.request.build_opener(NoRedirect()).open(request, timeout=15)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        content = response.read(16385)
        if response.status != expect or len(content) > 16384:
            raise QualificationError('First-run setup HTTP response invalid')
        return json.loads(content) if content and expect < 400 else None


def check_download(client, node, content):
    received, headers = client.request('GET', '/nodes/' + node + '/content?download=1', raw=True, limit=len(content))
    if digest(received) != digest(content) or headers.get('X-Content-SHA256') != digest(content):
        raise QualificationError('Downloaded SHA-256 mismatch')
    partial, _ = client.request('GET', '/nodes/' + node + '/content', headers={'Range': 'bytes=17-53'}, raw=True, expect=206)
    if partial != content[17:54]:
        raise QualificationError('Range download mismatch')
    client.request('GET', '/nodes/' + node + '/content', anonymous=True, expect=401)


def docker_smoke(image, platform, target_version, revision=None):
    metadata = json.loads(run('docker', 'image', 'inspect', image))[0]
    validate_image(metadata, platform, target_version)
    labels = validate_release_labels(metadata, os.environ.get('GITHUB_REPOSITORY', ''), revision, target_version) if revision else None
    identity = 'harbor-ci-' + secrets.token_hex(8)
    volume, container, initializer, network = identity + '-data', identity, identity + '-init', identity + '-net'
    created = []
    volume_created = False
    network_created = False
    setup_token = secrets.token_urlsafe(32)
    options = ['--platform', platform, '--read-only', '--cap-drop=ALL', '--security-opt=no-new-privileges',
               '--network', network, '--dns', '127.0.0.1',
               '--pids-limit=128', '--memory=768m', '--cpus=2', '--tmpfs', '/tmp:size=128m,noexec,nosuid,nodev',
               '--env', 'DRIVE_ENV_FILE=-', '--env', 'DRIVE_STORAGE=local',
               '--env', 'DRIVE_SECURE_COOKIES=false', '--env', 'DRIVE_ALLOW_REMOTE_HTTP=true']
    try:
        existing = subprocess.run(['docker', 'volume', 'inspect', volume], capture_output=True)
        if existing.returncode == 0:
            raise QualificationError('Refusing to reuse an existing Docker volume')
        run('docker', 'volume', 'create', volume)
        volume_created = True
        # Internal networks suppress published ports on Docker Desktop. Keep local
        # ingress while disabling outbound masquerading, DNS and peer connectivity.
        run('docker', 'network', 'create', '--opt', 'com.docker.network.bridge.enable_ip_masquerade=false',
            '--opt', 'com.docker.network.bridge.enable_icc=false', network)
        network_created = True
        with tempfile.TemporaryDirectory(prefix='harbor-ci-password-') as temporary:
            password = Path(temporary) / 'owner-password'
            password.write_text(secrets.token_urlsafe(32))
            password.chmod(0o600)
            run('docker', 'create', '--name', initializer, *options,
                '--tmpfs', '/data:size=16m,uid=65532,gid=65532,mode=0700,noexec,nosuid,nodev', image, 'init')
            created.append(initializer)
            run('docker', 'start', '--attach', initializer)
            if json.loads(run('docker', 'inspect', '--format', '{{json .State.ExitCode}}', initializer)) != 0:
                raise QualificationError('Legacy initialization failed')
            with tarfile.open(fileobj=io.BytesIO(run('docker', 'cp', initializer + ':/app/harbor-recovery', '-'))) as archive:
                executable = archive.next()
                if not executable or not executable.isfile() or not executable.mode & 0o111:
                    raise QualificationError('Recovery executable missing from final image')

            def start(setup=False):
                run('docker', 'create', '--name', container, '--publish', '127.0.0.1::8080',
                    '--stop-timeout=20', *options, '--volume', volume + ':/data',
                    '--env', 'DRIVE_SETUP_TOKEN=' + setup_token, image)
                created.append(container)
                run('docker', 'start', container)
                origin = wait_ready(container, setup=setup)
                if setup:
                    return origin
                if setup_request(origin, 'GET', '/status') != {'required': False}:
                    raise QualificationError('First-run setup reopened after restart/recreation')
                setup_request(origin, 'POST', '/account', {'password': password.read_text()}, setup_token, expect=404)
                return Client(origin, password)

            origin = start(setup=True)
            opener = urllib.request.build_opener(NoRedirect())
            with opener.open(origin + '/', timeout=15) as response:
                index = response.read(65537)
                if response.status != 200 or len(index) > 65536 or b'id="root"' not in index:
                    raise QualificationError('Packaged onboarding HTML unavailable')
            assets = re.findall(r'(?:src|href)="(/assets/[A-Za-z0-9._/-]+)"', index.decode())
            if not assets:
                raise QualificationError('Packaged onboarding assets missing')
            for path in assets:
                with opener.open(origin + path, timeout=15) as response:
                    if response.status != 200 or not response.read(1):
                        raise QualificationError('Packaged onboarding asset unavailable')
            setup_request(origin, 'POST', '/verify', {}, expect=401)
            setup_request(origin, 'POST', '/verify', {}, setup_token, expect=403,
                          request_origin='https://example.invalid')
            setup_request(origin, 'POST', '/verify', {}, setup_token)
            account = setup_request(origin, 'POST', '/account', {'password': password.read_text()}, setup_token)
            if not account.get('account_ready'):
                raise QualificationError('First-run owner account was not accepted')
            setup_request(origin, 'POST', '/complete', {'adapter': 'local'}, setup_token)
            client = Client(wait_ready(container), password)
            if setup_request(client.base_url, 'GET', '/status') != {'required': False}:
                raise QualificationError('First-run setup remains available after completion')
            client.request('GET', '/diagnostics', anonymous=True, expect=401)
            diagnostics, _ = client.request('GET', '/diagnostics')
            if (diagnostics.get('version') != target_version or diagnostics.get('adapter') != 'local'
                    or diagnostics.get('encryption') != 'AES-256-GCM' or diagnostics.get('database') != 'ok'):
                raise QualificationError('Runtime version, encryption or local adapter diagnostics invalid')
            client.request('POST', '/folders', {'name': 'rejected-origin'}, {'Origin': 'https://example.invalid'}, expect=403)
            folder, _ = client.request('POST', '/folders', {'name': 'CI fixtures', 'parent_id': ''}, expect=201)
            content, mime = fixture('multipart.bin', diagnostics['chunk_size'])
            upload, _ = client.request('POST', '/uploads', {'name': 'multipart.bin', 'parent_id': folder['id'],
                'size': len(content), 'mime_type': mime, 'sha256': digest(content)}, expect=201)
            chunk_size = upload['chunk_size']
            first = content[:chunk_size]
            client.request('PUT', '/uploads/' + upload['id'] + '/chunks/0', first, {'X-Chunk-SHA256': digest(first)})
            client.request('POST', '/uploads/' + upload['id'] + '/complete', {}, expect=409)
            client.request('GET', '/nodes/' + upload['id'], expect=404)
            run('docker', 'restart', '--time', '20', container)
            client = Client(wait_ready(container), password)
            if setup_request(client.base_url, 'GET', '/status') != {'required': False}:
                raise QualificationError('First-run setup reopened after restart')
            setup_request(client.base_url, 'POST', '/account', {'password': password.read_text()}, setup_token, expect=404)
            resumed, _ = client.request('GET', '/uploads/' + upload['id'])
            if resumed.get('received_chunks') != [0]:
                raise QualificationError('Multipart state did not survive restart')
            for index in range(1, upload['total_chunks']):
                part = content[index * chunk_size:(index + 1) * chunk_size]
                client.request('PUT', '/uploads/' + upload['id'] + '/chunks/' + str(index), part,
                               {'X-Chunk-SHA256': digest(part)})
            node, _ = client.request('POST', '/uploads/' + upload['id'] + '/complete', {})
            check_download(client, node['id'], content)
            run('docker', 'stop', '--time', '20', container)
            if json.loads(run('docker', 'inspect', '--format', '{{json .State.ExitCode}}', container)) != 0:
                raise QualificationError('Container did not shut down cleanly')
            validate_private_modes(run('docker', 'cp', container + ':/data/.', '-'), web_setup=True)
            run('docker', 'rm', container)
            created.remove(container)
            client = start()
            check_download(client, node['id'], content)
            restored, _ = client.request('GET', '/nodes/' + folder['id'])
            if restored.get('name') != 'CI fixtures':
                raise QualificationError('Folder metadata did not survive recreation')
            client.request('POST', '/auth/logout', expect=204)
            client.request('GET', '/diagnostics', expect=401)
            run('docker', 'stop', '--time', '20', container)
            return {'platform': platform, 'version': target_version, 'adapter': 'local',
                    'multipart_bytes': len(content), 'sha256_equal': True, 'range': 'PASS',
                    'auth_and_csrf': 'PASS', 'restart_resume': 'PASS', 'container_recreation': 'PASS',
                    'web_onboarding_and_lockout': 'PASS', 'legacy_init': 'PASS',
                    'private_state_modes': 'PASS', 'nonroot_health_and_executables': 'PASS',
                    'clean_shutdown': 'PASS', 'telegram': 'NOT TESTED', 'release_oci_labels': labels}
    finally:
        for name in reversed(created):
            run('docker', 'rm', '--force', name)
        if volume_created:
            run('docker', 'volume', 'rm', volume)
        if network_created:
            run('docker', 'network', 'rm', network)


def published_index(raw, sbom, provenance, expected_digest):
    if 'sha256:' + hashlib.sha256(raw).hexdigest() != expected_digest:
        raise QualificationError('Published index digest mismatch')
    manifest = json.loads(raw)
    try:
        entries = manifest['manifests']
        platforms = {m['platform']['os'] + '/' + m['platform']['architecture'] for m in entries}
        assert platforms == {'linux/amd64', 'linux/arm64', 'unknown/unknown'}
        attestations = [m for m in entries if m['platform'] == {'os': 'unknown', 'architecture': 'unknown'}]
        assert len(entries) == 4 and len(attestations) == 2
        evidence = []
        for platform in ('linux/amd64', 'linux/arm64'):
            children = [m['digest'] for m in entries
                        if m['platform']['os'] + '/' + m['platform']['architecture'] == platform]
            assert len(children) == 1 and re.fullmatch(r'sha256:[0-9a-f]{64}', children[0])
            bound = [m for m in attestations if m.get('annotations', {}).get('vnd.docker.reference.digest',
                     m.get('annotations', {}).get('com.docker.reference.digest')) == children[0]]
            assert len(bound) == 1 and re.fullmatch(r'sha256:[0-9a-f]{64}', bound[0]['digest'])
            assert bound[0]['annotations'].get('vnd.docker.reference.type',
                   bound[0]['annotations'].get('com.docker.reference.type')) == 'attestation-manifest'
            spdx = sbom[platform]['SPDX']
            slsa = provenance[platform]['SLSA']
            build = slsa.get('buildDefinition', slsa)
            materials = build.get('resolvedDependencies', slsa.get('materials'))
            assert spdx['spdxVersion'].startswith('SPDX-') and spdx['SPDXID'] == 'SPDXRef-DOCUMENT'
            assert isinstance(spdx['packages'], list) and spdx['packages']
            assert build['buildType'] in ('https://mobyproject.org/buildkit@v1',
                   'https://github.com/moby/buildkit/blob/master/docs/attestations/slsa-definitions.md')
            assert isinstance(materials, list) and materials
            evidence.append({'platform': platform, 'child_digest': children[0], 'attestation_digest': bound[0]['digest'],
                             'spdx_version': spdx['spdxVersion'], 'sbom_packages': len(spdx['packages']),
                             'slsa_build_type': build['buildType'], 'slsa_materials': len(materials)})
        assert len({item['child_digest'] for item in evidence}) == 2
    except (AssertionError, AttributeError, KeyError, TypeError):
        raise QualificationError('Published platform or attestation validation failed') from None
    return evidence


def verify_published(tag, expected_digest):
    repository = os.environ.get('GITHUB_REPOSITORY', '')
    if (not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9_.-]+', repository)
            or not tag.startswith('v') or not VERSION.fullmatch(tag[1:])
            or tuple(map(int, tag[1:].split('.'))) <= (1, 0, 0)
            or not re.fullmatch(r'sha256:[0-9a-f]{64}', expected_digest)):
        raise QualificationError('Trusted repository, annotated stable release and immutable digest required')
    ref = 'refs/tags/' + tag
    if run('git', 'cat-file', '-t', ref).strip() != b'tag':
        raise QualificationError('Published source must have an annotated release tag')
    revision = run('git', 'rev-parse', ref + '^{commit}').decode().strip()
    if not re.fullmatch(r'[0-9a-f]{40}', revision):
        raise QualificationError('Invalid release commit')
    run('git', 'merge-base', '--is-ancestor', revision, 'refs/remotes/origin/main')
    package = package_check(True)
    image = 'ghcr.io/' + repository.split('/')[0].lower() + '/harbor'
    reference = image + '@' + expected_digest
    raw = run('docker', 'buildx', 'imagetools', 'inspect', '--raw', reference)
    sbom = json.loads(run('docker', 'buildx', 'imagetools', 'inspect', '--format', '{{json .SBOM}}', reference))
    provenance = json.loads(run('docker', 'buildx', 'imagetools', 'inspect', '--format', '{{json .Provenance}}', reference))
    platforms = published_index(raw, sbom, provenance, expected_digest)
    tests = []
    for platform in platforms:
        child = image + '@' + platform['child_digest']
        run('docker', 'pull', '--platform', platform['platform'], child)
        tests.append(docker_smoke(child, platform['platform'], tag[1:], revision))
    result = {'image': reference, 'tag': tag, 'revision': revision, 'package_policy': package,
              'platforms': platforms, 'smoke_tests': tests,
              'attestation_check': 'descriptor bindings and SPDX/SLSA predicates; raw subjects/signatures not inspected'}
    summary = os.environ.get('GITHUB_STEP_SUMMARY')
    if summary:
        with Path(summary).open('a') as stream:
            stream.write('### Published immutable image verified\n\n`' + reference + '`\n\n'
                         'Tagged source: `' + revision + '`. Both platforms passed pulled-image local-adapter smoke.\n\n'
                         '```json\n' + json.dumps(result, indent=2, sort_keys=True) + '\n```\n')
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='command', required=True)
    commands.add_parser('workflows')
    commands.add_parser('package-check').add_argument('--required', action='store_true')
    published = commands.add_parser('verify-published')
    published.add_argument('--tag', required=True)
    published.add_argument('--digest', required=True)
    reproduce = commands.add_parser('reproducible')
    reproduce.add_argument('--version', required=True, type=version)
    smoke = commands.add_parser('docker-smoke')
    smoke.add_argument('--image', required=True)
    smoke.add_argument('--platform', choices=('linux/amd64', 'linux/arm64'), required=True)
    smoke.add_argument('--version', required=True, type=version)
    smoke.add_argument('--revision', help='Also verify all release OCI labels against this commit and GITHUB_REPOSITORY')
    args = parser.parse_args()
    try:
        if args.command == 'workflows':
            result = workflows()
        elif args.command == 'package-check':
            result = package_check(args.required)
        elif args.command == 'verify-published':
            result = verify_published(args.tag, args.digest)
        elif args.command == 'reproducible':
            result = reproducible(args.version)
        else:
            if args.image.startswith('-') or not re.fullmatch(r'[a-zA-Z0-9][a-zA-Z0-9_./:@-]*', args.image):
                parser.error('Use a Docker image reference without whitespace or options')
            result = docker_smoke(args.image, args.platform, args.version, args.revision)
    except (QualificationError, OSError, ValueError, KeyError, subprocess.SubprocessError) as error:
        parser.exit(1, 'FAIL ' + (str(error) if isinstance(error, QualificationError) else type(error).__name__) + '\n')
    print(json.dumps({'status': 'PASS', **result}, sort_keys=True))


if __name__ == '__main__':
    main()
