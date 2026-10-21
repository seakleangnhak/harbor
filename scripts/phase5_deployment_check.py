#!/usr/bin/env python3
"""Isolated local Docker upgrade/rollback qualification. Never loads project .env.

Requires an already built candidate image and retained Phase 4 image. Uses only
fresh harbor-phase5-ops data; never imports Telegram authorization or user data.
"""
import argparse
import hashlib
import io
import json
import os
from pathlib import Path
import re
import sqlite3
import subprocess
import tarfile
import tempfile
import time
import urllib.request

from telegram_qualification import Client, QualificationError, fixture, save_state


ROOT = Path(__file__).resolve().parent.parent
PROJECT = 'harbor-phase5-ops'
VOLUME = PROJECT + '_drive-data'
IDENTITY = re.compile(r'^[0-9a-f]{64}$')
INSPECT = '''{{ $local := false }}{{ $allow := false }}{{range .Config.Env}}{{if eq . "DRIVE_STORAGE=local"}}{{$local = true}}{{end}}{{if eq . "DRIVE_ALLOW_REMOTE_HTTP=true"}}{{$allow = true}}{{end}}{{end}}
{"id":{{json .Id}},"image":{{json .Image}},"started_at":{{json .State.StartedAt}},"running":{{json .State.Running}},"health":{{with index .State "Health"}}{{json .Status}}{{else}}null{{end}},"exit_code":{{json .State.ExitCode}},"oom":{{json .State.OOMKilled}},"user":{{json .Config.User}},"read_only":{{json .HostConfig.ReadonlyRootfs}},"privileged":{{json .HostConfig.Privileged}},"restart_policy":{{json .HostConfig.RestartPolicy.Name}},"stop_timeout":{{json .Config.StopTimeout}},"cap_drop":{{json .HostConfig.CapDrop}},"security_opt":{{json .HostConfig.SecurityOpt}},"memory":{{json .HostConfig.Memory}},"cpus":{{json .HostConfig.NanoCpus}},"pids":{{json .HostConfig.PidsLimit}},"tmpfs":{{json .HostConfig.Tmpfs}},"ports":{{json .HostConfig.PortBindings}},"project":{{json (index .Config.Labels "com.docker.compose.project")}},"service":{{json (index .Config.Labels "com.docker.compose.service")}},"local":{{$local}},"allow_remote_http":{{$allow}},"data_mount":{{range .Mounts}}{{if eq .Destination "/data"}}{"type":{{json .Type}},"name":{{json .Name}},"writable":{{json .RW}}}{{end}}{{end}}}'''


def docker(*args, env=None, timeout=90):
    result = subprocess.run(['docker', *args], cwd=ROOT, env=env, capture_output=True, timeout=timeout)
    if result.returncode:
        raise QualificationError('Docker operation failed: ' + args[0])
    return result.stdout


def validate_runtime(value, port):
    checks = {
        'isolated_local': value.get('project') == PROJECT and value.get('service') == 'drive' and value.get('local') is True,
        'healthy': value.get('running') is True and value.get('health') == 'healthy',
        'nonroot': value.get('user') == '65532',
        'readonly_root': value.get('read_only') is True,
        'nonprivileged': value.get('privileged') is False,
        'restart_and_shutdown_policy': value.get('restart_policy') == 'unless-stopped' and value.get('stop_timeout') == 20,
        'capabilities_dropped': value.get('cap_drop') == ['ALL'],
        'no_new_privileges': any(s in ('no-new-privileges', 'no-new-privileges:true') for s in value.get('security_opt', [])),
        'loopback_only': value.get('ports') == {'8080/tcp': [{'HostIp': '127.0.0.1', 'HostPort': str(port)}]},
        'explicit_internal_http': value.get('allow_remote_http') is True,
        'bounded_resources': value.get('memory') == 768 << 20 and value.get('cpus') == 2_000_000_000 and value.get('pids') == 128,
        'private_persistent_volume': value.get('data_mount') == {'type': 'volume', 'name': VOLUME, 'writable': True},
        'bounded_tmp': {'noexec', 'nosuid', 'nodev'} <= set((value.get('tmpfs') or {}).get('/tmp', '').split(',')) and
                       any(s in (value.get('tmpfs') or {}).get('/tmp', '') for s in ('size=128m', 'size=131072k')),
    }
    for name, passed in checks.items():
        if not passed:
            raise QualificationError('Runtime check failed: ' + name)
    if not IDENTITY.fullmatch(value.get('id', '')) or not re.fullmatch(r'sha256:[0-9a-f]{64}', value.get('image', '')):
        raise QualificationError('Invalid container or image identity')
    return {'id': value['id'], 'image': value['image'], 'started_at': value['started_at'],
            'volume': VOLUME, 'checks': {name: 'PASS' for name in checks}}


def check_transition(current, previous, kind):
    if current['volume'] != previous['volume']:
        raise QualificationError('Persistent volume changed')
    if kind == 'restart':
        correct = current['id'] == previous['id'] and current['started_at'] != previous['started_at'] and current['image'] == previous['image']
    elif kind == 'recreate':
        correct = current['id'] != previous['id'] and current['image'] == previous['image']
    else:
        correct = current['id'] != previous['id'] and current['image'] != previous['image']
    if not correct:
        raise QualificationError('Transition not observed: ' + kind)


def private_state(container, env):
    content = docker('cp', container + ':/data/.', '-', env=env)
    modes, key_hash = {}, None
    with tarfile.open(fileobj=io.BytesIO(content), mode='r:') as archive:
        for member in archive:
            name = member.name.removeprefix('./').rstrip('/')
            if name in ('.', 'data', 'drive.db', 'drive.db-wal', 'drive.db-shm', '.lock', 'storage.json', 'content.key', 'owner-password'):
                modes[name] = oct(member.mode)
            if name == 'content.key':
                key_hash = hashlib.sha256(archive.extractfile(member).read()).hexdigest()
    if not key_hash or modes.get('content.key') != '0o600' or modes.get('owner-password') != '0o600':
        raise QualificationError('Generated secrets lack private modes')
    return {'modes': modes, 'key_sha256': key_hash}


def database_state(container, env):
    content = docker('cp', container + ':/data/.', '-', env=env)
    with tempfile.TemporaryDirectory(prefix='harbor-docker-schema-') as temporary:
        with tarfile.open(fileobj=io.BytesIO(content), mode='r:') as archive:
            for member in archive:
                name = member.name.removeprefix('./')
                if name in ('drive.db', 'drive.db-wal', 'drive.db-shm') and member.isfile():
                    path = Path(temporary) / name
                    path.write_bytes(archive.extractfile(member).read())
                    path.chmod(0o600)
        with sqlite3.connect(str(Path(temporary) / 'drive.db')) as database:
            if database.execute('PRAGMA quick_check').fetchone() != ('ok',):
                raise QualificationError('Offline metadata integrity check failed')
            return {'schema_version': database.execute('PRAGMA user_version').fetchone()[0],
                    'nodes': database.execute('SELECT count(*) FROM nodes').fetchone()[0],
                    'uploads': database.execute('SELECT count(*) FROM storage_uploads').fetchone()[0],
                    'chunks': database.execute('SELECT count(*) FROM storage_chunks').fetchone()[0]}


def recovery_smoke(image, env):
    volume = PROJECT + '_recovery-check'
    if subprocess.run(['docker', 'volume', 'inspect', volume], capture_output=True).returncode == 0:
        raise QualificationError('Recovery test volume already exists')
    sandbox = None
    options = ['run', '--rm', '--network=none', '--read-only', '--cap-drop=ALL',
               '--security-opt=no-new-privileges', '--user=65532:65532']
    try:
        sandbox = docker('create', '--network=none', '--read-only', '--cap-drop=ALL',
                         '--security-opt=no-new-privileges', '--volume', volume + ':/data', image, 'init', env=env).decode().strip()
        docker('start', '--attach', sandbox, env=env)
        state = private_state(sandbox, env)
        if state['modes'].get('.') != '0o700':
            raise QualificationError('Fresh image data directory is not private')
        docker(*options, '--volume', volume + ':/archive', '--entrypoint', '/bin/sh',
               'golang:1.26.10-bookworm', '-c', 'umask 077; head -c 32 /dev/urandom > /archive/archive-password', env=env)
        recovery = [*options, '--volume', VOLUME + ':/data', '--volume', volume + ':/archive',
                    '--entrypoint', '/app/harbor-recovery', image]
        docker(*recovery, 'backup', '--data-dir', '/data', '--output', '/archive/full.harbor',
               '--password-file', '/archive/archive-password', env=env)
        docker(*recovery, 'verify', '--archive', '/archive/full.harbor',
               '--password-file', '/archive/archive-password', env=env)
        docker(*recovery, 'restore', '--archive', '/archive/full.harbor', '--destination', '/archive/restored',
               '--password-file', '/archive/archive-password', env=env)
        return {'fresh_directory_mode': '0700', 'fresh_secret_modes': '0600',
                'container_encrypted_backup': 'PASS', 'container_archive_verify': 'PASS',
                'container_fresh_restore': 'PASS'}
    finally:
        if sandbox:
            docker('rm', '--force', sandbox, env=env)
        docker('volume', 'rm', volume, env=env)


def qualify(candidate, previous, port, output):
    if not 1024 <= port <= 65535 or port in (8080, 8081, 8084, 8086, 8087):
        raise QualificationError('Use isolated unprivileged port')
    if output.exists():
        raise QualificationError('Evidence directory already exists; choose new output')
    candidate, previous = (docker('image', 'inspect', image, '--format', '{{.Id}}').decode().strip()
                           for image in (candidate, previous))
    exists = subprocess.run(['docker', 'volume', 'inspect', VOLUME], capture_output=True)
    if exists.returncode == 0:
        raise QualificationError('Isolated volume already exists; never reuses existing data')
    if docker('ps', '--all', '--filter', 'label=com.docker.compose.project=' + PROJECT, '--format', '{{.ID}}').strip():
        raise QualificationError('Isolated project already has containers; never takes over existing deployment')
    output.mkdir(parents=True, mode=0o700)
    output.chmod(0o700)
    password_file = output / 'generated-owner-password'
    override = output / 'image.yaml'
    environment = {key: value for key, value in os.environ.items() if not key.startswith(('DRIVE_', 'TELEGRAM_', 'COMPOSE_')) and key not in ('USERNAME_OR_CHANNEL_LINK', 'GO_TAGS')}
    environment.update(DRIVE_PORT=str(port), DRIVE_STORAGE='local', DRIVE_SECURE_COOKIES='false', GO_TAGS='')
    compose = ['compose', '--env-file', '/dev/null', '-p', PROJECT, '-f', str(ROOT / 'compose.yaml'), '-f', str(override)]
    base = f'http://127.0.0.1:{port}'
    observations = []
    expected = {}
    folder_ids = []
    key_hash = None

    def command(*args):
        return docker(*compose, *args, env=environment)

    def use(image):
        override.write_text('services:\n  drive:\n    image: ' + json.dumps(image) + '\n')
        override.chmod(0o600)

    def container():
        value = command('ps', '--all', '--quiet', 'drive').decode().strip()
        if not IDENTITY.fullmatch(value):
            raise QualificationError('Expected one isolated container')
        return value

    def observe(label, ready=True):
        nonlocal key_hash
        for _ in range(180):
            value = json.loads(docker('inspect', '--format', INSPECT, container(), env=environment))
            if value['health'] == 'healthy':
                break
            if not value['running']:
                raise QualificationError('Isolated container exited before healthy')
            time.sleep(.5)
        current = validate_runtime(value, port)
        if observations:
            check_transition(current, observations[-1]['runtime'], label)
        with urllib.request.urlopen(base + '/health', timeout=5) as response:
            if json.load(response) != {'status': 'ok'}:
                raise QualificationError('Liveness body invalid')
        if ready:
            with urllib.request.urlopen(base + '/ready', timeout=5) as response:
                if json.load(response) != {'status': 'ready'}:
                    raise QualificationError('Readiness body invalid')
        state = private_state(current['id'], environment)
        if key_hash is not None and state['key_sha256'] != key_hash:
            raise QualificationError('Content key changed across transition')
        key_hash = state['key_sha256']
        if ready and (state['modes'].get('drive.db') != '0o600' or state['modes'].get('.') != '0o700'):
            raise QualificationError('Data directory or metadata mode is not private')
        client = Client(base, password_file)
        try:
            client.request('GET', '/diagnostics', expect=401, anonymous=True)
            diagnostics, _ = client.request('GET', '/diagnostics')
            if diagnostics['adapter'] != 'local' or diagnostics['database'] != 'ok':
                raise QualificationError('Local diagnostics invalid')
            for node, (digest, data) in expected.items():
                downloaded, headers = client.request('GET', '/nodes/' + node + '/content', raw=True)
                if hashlib.sha256(downloaded).hexdigest() != digest or headers['X-Content-SHA256'] != digest or downloaded != data:
                    raise QualificationError('Persisted content checksum mismatch')
                client.request('GET', '/nodes/' + node + '/content', expect=401, anonymous=True)
            if expected:
                listing, _ = client.request('GET', '/nodes?parent_id=' + folder_ids[-1])
                if {n['id'] for n in listing['items']} != set(expected):
                    raise QualificationError('Nested metadata changed')
                favorites, _ = client.request('GET', '/nodes?view=favorites')
                if next(iter(expected)) not in {n['id'] for n in favorites['items']}:
                    raise QualificationError('Favorite metadata changed')
                trash, _ = client.request('GET', '/nodes?view=trash')
                if not any(n['name'] == 'Retained Trash' for n in trash['items']):
                    raise QualificationError('Trash metadata changed')
        finally:
            client.request('POST', '/auth/logout', expect=204)
        observations.append({'stage': label, 'runtime': current, 'state': state, 'checksums': {node: data[0] for node, data in expected.items()}, 'result': 'PASS'})
        return current

    try:
        use(previous)
        command('run', '--rm', 'drive', 'init')
        command('up', '--detach', '--no-build')
        docker('cp', container() + ':/data/owner-password', str(password_file), env=environment)
        password_file.chmod(0o600)
        observe('baseline', ready=False)
        client = Client(base, password_file)
        try:
            parent = ''
            for name in ('Phase 5 deployment', 'Nested', 'Deep'):
                folder, _ = client.request('POST', '/folders', {'name': name, 'parent_id': parent}, expect=201)
                parent = folder['id']
                folder_ids.append(parent)
            for name in ('small.txt', 'small.png', 'small.pdf', 'multipart.bin'):
                data, mime = fixture(name, 4 << 20)
                digest = hashlib.sha256(data).hexdigest()
                upload, _ = client.request('POST', '/uploads', {'name': name, 'parent_id': parent, 'size': len(data), 'mime_type': mime, 'sha256': digest}, expect=201)
                for index in range(upload['total_chunks']):
                    chunk = data[index * upload['chunk_size']:(index + 1) * upload['chunk_size']]
                    client.request('PUT', f'/uploads/{upload["id"]}/chunks/{index}', chunk)
                node, _ = client.request('POST', '/uploads/' + upload['id'] + '/complete', {})
                expected[node['id']] = digest, data
            client.request('PATCH', '/nodes/' + next(iter(expected)), {'favorite': True})
            trashed, _ = client.request('POST', '/folders', {'name': 'Retained Trash', 'parent_id': ''}, expect=201)
            client.request('DELETE', '/nodes/' + trashed['id'], expect=204)
        finally:
            client.request('POST', '/auth/logout', expect=204)
        command('stop', '--timeout', '20')
        value = json.loads(docker('inspect', '--format', INSPECT, container(), env=environment))
        if value['exit_code'] != 0 or value['oom']:
            raise QualificationError('Baseline graceful shutdown failed')
        before = database_state(container(), environment)
        # Phase 4 empty-directory COPY left the volume root 0755. This migration
        # changes only permissions on our newly generated, stopped test volume.
        docker('run', '--rm', '--network=none', '--read-only', '--cap-drop=ALL',
               '--security-opt=no-new-privileges', '--user=65532:65532',
               '--volume', VOLUME + ':/data', '--entrypoint', '/bin/chmod',
               'golang:1.26.10-bookworm', '0700', '/data', env=environment)
        use(candidate)
        command('up', '--detach', '--no-build', '--force-recreate')
        observe('upgrade')
        command('restart', '--timeout', '20', 'drive')
        observe('restart')
        command('up', '--detach', '--no-build', '--force-recreate')
        observe('recreate')
        use(previous)
        command('up', '--detach', '--no-build', '--force-recreate')
        observe('rollback', ready=False)
        use(candidate)
        command('up', '--detach', '--no-build', '--force-recreate')
        observe('upgrade')
        command('stop', '--timeout', '20')
        value = json.loads(docker('inspect', '--format', INSPECT, container(), env=environment))
        if value['exit_code'] != 0 or value['oom']:
            raise QualificationError('Candidate graceful shutdown failed')
        after = database_state(container(), environment)
        if before['schema_version'] != 0 or after['schema_version'] != 1 or any(before[k] != after[k] for k in ('nodes', 'uploads', 'chunks')):
            raise QualificationError('Schema or persisted row counts differ unexpectedly')
        recovery = recovery_smoke(candidate, environment)
        result = {'result': 'PASS', 'adapter': 'local', 'project': PROJECT, 'port': port, 'volume': VOLUME,
                  'stages': observations, 'fixture_bytes': sum(len(data) for _, data in expected.values()),
                  'database_before': before, 'database_after': after,
                  'graceful_shutdown': 'PASS', 'volume_permissions_migration': 'PASS',
                  'recovery_cli': recovery,
                  'live_telegram': 'NOT TESTED', 'schema_rollback_scope': 'Additive Phase4-to-Phase5 migration only'}
        save_state(output / 'evidence.json', result)
        print('PASS local Docker upgrade, restart, recreation, rollback, SHA-256, hierarchy, favorites, Trash, auth and graceful shutdown')
    finally:
        try:
            command('down', '--remove-orphans')
        finally:
            password_file.unlink(missing_ok=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--candidate-image', required=True)
    parser.add_argument('--previous-image', default='harbor-phase4-drive:latest')
    parser.add_argument('--port', type=int, default=8092)
    parser.add_argument('--output', type=Path, default=ROOT / 'output/phase5/deployment')
    args = parser.parse_args()
    if not args.output.resolve().is_relative_to(ROOT / 'output'):
        parser.error('Evidence must stay inside project output/')
    try:
        qualify(args.candidate_image, args.previous_image, args.port, args.output.resolve())
    except (QualificationError, OSError, ValueError, subprocess.TimeoutExpired) as error:
        parser.exit(1, 'FAIL ' + (str(error) if isinstance(error, QualificationError) else 'Isolated Docker qualification unavailable') + '\n')


if __name__ == '__main__':
    main()
