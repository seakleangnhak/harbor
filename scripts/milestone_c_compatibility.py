#!/usr/bin/env python3
"""Isolated local-adapter v1.2.1 upgrade and archive-based rollback; no owner state."""
import argparse
import io
import json
import os
from pathlib import Path
import re
import secrets
import subprocess
import tarfile
import time
import urllib.request

import telegram_qualification as q

ROOT = Path(__file__).resolve().parents[1]


def check(value, reason):
    if not value:
        raise RuntimeError(reason)


def private_archive(name, data):
    stream = io.BytesIO()
    with tarfile.open(fileobj=stream, mode='w') as archive:
        member = tarfile.TarInfo(name)
        member.uid = member.gid = 65532
        member.mode, member.size = 0o600, len(data)
        archive.addfile(member, io.BytesIO(data))
    return stream.getvalue()


def qualify(args):
    os.umask(0o077)
    os.environ['no_proxy'] = '127.0.0.1,localhost,::1'
    for image in (args.previous, args.candidate, args.helper):
        check(re.fullmatch(r'sha256:[0-9a-f]{64}', image), 'Immutable local image IDs required')
    out = args.output.resolve()
    check(out.is_relative_to(ROOT / 'output') and not out.exists(), 'Use fresh ignored output')
    out.mkdir(parents=True, mode=0o700)
    prefix = 'harbor-c-compat-' + secrets.token_hex(6)
    volumes = [prefix + suffix for suffix in ('-source', '-archive', '-rollback', '-recovered')]
    source, archives, rollback, recovered = volumes
    created, containers = [], []
    network = prefix + '-net'
    report = {'result': 'FAIL', 'previous_image': args.previous, 'candidate_image': args.candidate,
              'adapter': 'local', 'stages': [], 'owner_data_used': False}
    password = out / 'generated-owner-password'

    def docker(*parts, data=None, timeout=180):
        result = subprocess.run(['docker', '--context', args.context, *parts], input=data,
                                capture_output=True, timeout=timeout)
        check(result.returncode == 0, 'Docker operation failed: ' + parts[0] + '; private output withheld')
        return result.stdout

    sandbox = ['--read-only', '--cap-drop=ALL', '--security-opt=no-new-privileges',
               '--user=65532:65532', '--memory=768m', '--memory-swap=768m', '--cpus=2',
               '--pids-limit=128', '--tmpfs=/tmp:size=128m,noexec,nosuid,nodev',
               '--env=HOME=/tmp', '--env=GOMEMLIMIT=512MiB']

    def environment(data='/data', restore=False):
        values = {'DRIVE_ENV_FILE': data + '/recovery.env' if restore else '-',
                  'DRIVE_DATA_DIR': data, 'DRIVE_STORAGE': 'local',
                  'DRIVE_KEY_FILE': data + '/content.key',
                  'DRIVE_OWNER_PASSWORD_FILE': data + '/owner-password',
                  'DRIVE_ADDR': '0.0.0.0:8080', 'DRIVE_ALLOW_REMOTE_HTTP': 'true',
                  'DRIVE_SECURE_COOKIES': 'false'}
        return [flag for name, value in values.items() for flag in ('--env', name + '=' + value)]

    def start(image, volume=source, restored=False):
        name = prefix + '-' + str(len(containers))
        data = '/restore/data' if restored else '/data'
        docker('create', '--name', name, '--network', network, '--dns=127.0.0.1',
               '--publish=127.0.0.1::8080', '--stop-timeout=20', *sandbox, *environment(data, restored),
               '--volume', volume + (':/restore' if restored else ':/data'), image)
        containers.append(name)
        began = time.monotonic()
        docker('start', name)
        while time.monotonic() - began < 90:
            state = json.loads(docker('inspect', '--format', '{{json .State}}', name))
            check(state['Running'], 'Application exited; private logs withheld')
            port = docker('port', name, '8080/tcp').decode().strip()
            check(re.fullmatch(r'127\.0\.0\.1:[0-9]+', port), 'Expected loopback-only port')
            try:
                with urllib.request.build_opener(q.NoRedirect()).open('http://' + port + '/ready', timeout=2) as response:
                    if response.status == 200:
                        client = q.Client('http://' + port, password)
                        report.setdefault('startup_seconds', []).append(time.monotonic() - began)
                        return name, client
            except OSError:
                time.sleep(.2)
        raise RuntimeError('Readiness timeout')

    def stop(name):
        docker('stop', '--time=20', name)
        state = json.loads(docker('inspect', '--format', '{{json .State}}', name))
        check(state['ExitCode'] == 0 and not state['OOMKilled'], 'Graceful shutdown failed')

    def upload(client, name, content, parent, background=False):
        item, _ = client.request('POST', '/uploads', {'name': name, 'parent_id': parent,
            'size': len(content), 'mime_type': 'application/octet-stream', 'sha256': q.digest(content)}, expect=201)
        for index in range(item['total_chunks']):
            data = content[index * item['chunk_size']:(index + 1) * item['chunk_size']]
            if not background:
                client.request('PUT', f'/uploads/{item["id"]}/chunks/{index}', data,
                               {'X-Chunk-SHA256': q.digest(data)})
            else:
                for part in range((len(data) + item['part_size'] - 1) // item['part_size']):
                    piece = data[part * item['part_size']:(part + 1) * item['part_size']]
                    client.request('PUT', f'/uploads/{item["id"]}/chunks/{index}/parts/{part}', piece,
                                   {'X-Part-SHA256': q.digest(piece)})
                client.request('POST', f'/uploads/{item["id"]}/chunks/{index}/commit', {'background': True}, expect=202)
        deadline = time.monotonic() + 120
        while time.monotonic() < deadline:
            state, _ = client.request('GET', '/uploads/' + item['id'])
            if state['received_bytes'] == len(content):
                node, _ = client.request('POST', '/uploads/' + item['id'] + '/complete', {})
                return node['id']
            client.request('GET', '/nodes/' + item['id'], expect=404)
            time.sleep(.1)
        raise RuntimeError('Background transfer did not drain')

    def verify(client, expected, parent, favorite, trash, label, version):
        diagnostics, _ = client.request('GET', '/diagnostics')
        check(diagnostics['version'] == version and diagnostics['adapter'] == 'local', 'Wrong runtime identity')
        client.request('GET', '/diagnostics', anonymous=True, expect=401)
        for identifier, content in expected.items():
            body, headers = client.request('GET', '/nodes/' + identifier + '/content', raw=True, limit=len(content))
            check(q.digest(body) == q.digest(content) == headers['X-Content-SHA256'], 'SHA-256 mismatch')
            body, _ = client.request('GET', '/nodes/' + identifier + '/content',
                headers={'Range': 'bytes=17-53'}, expect=206, raw=True)
            check(body == content[17:54], 'Range mismatch')
        listing, _ = client.request('GET', '/nodes?parent_id=' + parent)
        check({n['id'] for n in listing['items']} == set(expected), 'Folder metadata mismatch')
        favs, _ = client.request('GET', '/nodes?view=favorites')
        check(favorite in {n['id'] for n in favs['items']}, 'Favorite missing')
        trashed, _ = client.request('GET', '/nodes?view=trash')
        check(trash in {n['id'] for n in trashed['items']}, 'Trash missing')
        report['stages'].append({'stage': label, 'version': version, 'verified_files': len(expected),
                                 'sha256_range_metadata_auth': 'PASS'})

    def recovery(image, operation, archive, volume=None):
        extra = ['--volume', source + ':/data'] if operation == 'backup' else []
        if operation == 'restore':
            extra += ['--volume', volume + ':/restore']
        options = ['--data-dir', '/data', '--output', '/archive/' + archive] if operation == 'backup' else ['--archive', '/archive/' + archive]
        if operation == 'restore':
            options += ['--destination', '/restore/data']
        docker('run', '--rm', '--network=none', *sandbox, *extra,
               '--volume', archives + ':/archive', '--entrypoint=/app/harbor-recovery', image,
               operation, *options, '--password-file', '/archive/password')

    try:
        for volume in volumes:
            exists = subprocess.run(['docker', '--context', args.context, 'volume', 'inspect', volume], capture_output=True)
            check(exists.returncode != 0, 'Refusing to reuse an existing fixture volume')
            docker('volume', 'create', volume)
            created.append(volume)
            docker('run', '--rm', '--network=none', '--read-only', '--user=0', '--cap-drop=ALL',
                   '--cap-add=CHOWN', '--security-opt=no-new-privileges', '--memory=128m', '--cpus=.5',
                   '--pids-limit=32', '--volume', volume + ':/scratch', '--entrypoint=/bin/sh', args.helper,
                   '-ec', 'chmod 0700 /scratch; chown 65532:65532 /scratch')
        docker('network', 'create', '--opt', 'com.docker.network.bridge.enable_ip_masquerade=false',
               '--opt', 'com.docker.network.bridge.enable_icc=false', network)
        docker('run', '--rm', '--network=none', *sandbox, *environment(), '--volume', source + ':/data', args.previous, 'init')
        # Export only the generated fixture password, never owner state.
        holder = prefix + '-holder'
        docker('create', '--name', holder, '--network=none', *sandbox, '--volume', source + ':/data', args.previous)
        containers.append(holder)
        docker('cp', holder + ':/data/owner-password', str(password))
        password.chmod(0o600)
        docker('cp', '-a', '-', holder + ':/data', data=private_archive('archive-password', secrets.token_urlsafe(48).encode()))
        docker('run', '--rm', '--network=none', *sandbox, '--volume', source + ':/data',
               '--volume', archives + ':/archive', '--entrypoint=/bin/sh', args.helper,
               '-ec', 'umask 077; cp /data/archive-password /archive/password; rm /data/archive-password')
        name, client = start(args.previous)
        folder, _ = client.request('POST', '/folders', {'name': 'Compatibility fixtures', 'parent_id': ''}, expect=201)
        nested, _ = client.request('POST', '/folders', {'name': 'Nested', 'parent_id': folder['id']}, expect=201)
        trash, _ = client.request('POST', '/folders', {'name': 'Retained Trash', 'parent_id': ''}, expect=201)
        client.request('DELETE', '/nodes/' + trash['id'], expect=204)
        content, _ = q.fixture('multipart.bin', 100 << 20)
        identifier = upload(client, 'v1.2.1-multipart.bin', content, nested['id'])
        expected = {identifier: content}
        client.request('PATCH', '/nodes/' + identifier, {'favorite': True})
        verify(client, expected, nested['id'], identifier, trash['id'], 'v1.2.1-original', '1.2.1')
        stop(name)
        recovery(args.previous, 'backup', 'pre-upgrade.harbor')
        recovery(args.previous, 'verify', 'pre-upgrade.harbor')
        name, client = start(args.candidate)
        verify(client, expected, nested['id'], identifier, trash['id'], 'candidate-upgrade', args.version)
        new_content = bytes(range(256)) * (5 * 1024 * 1024 // 256)
        new_id = upload(client, 'candidate-background.bin', new_content, nested['id'], background=True)
        expected[new_id] = new_content
        verify(client, expected, nested['id'], identifier, trash['id'], 'candidate-new-upload', args.version)
        stop(name)
        # Preserve newer references before testing rollback against a separate old snapshot.
        recovery(args.candidate, 'backup', 'post-upgrade.harbor')
        recovery(args.candidate, 'verify', 'post-upgrade.harbor')
        recovery(args.previous, 'restore', 'pre-upgrade.harbor', rollback)
        name, client = start(args.previous, rollback, restored=True)
        verify(client, {identifier: content}, nested['id'], identifier, trash['id'], 'fresh-volume-rollback', '1.2.1')
        client.request('GET', '/nodes/' + new_id, expect=404)
        stop(name)
        recovery(args.candidate, 'restore', 'post-upgrade.harbor', recovered)
        name, client = start(args.candidate, recovered, restored=True)
        verify(client, expected, nested['id'], identifier, trash['id'], 'fresh-candidate-restore', args.version)
        stop(name)
        report.update(result='PASS', pre_upgrade_encrypted_backup='PASS', post_upgrade_encrypted_backup='PASS',
                      archive_integrity='PASS', fresh_volume_rollback='PASS', newer_references_preserved='PASS',
                      restored_candidate_sha256='PASS', quiescent_before_rollback=True, telegram='NOT TESTED')
    finally:
        for name in reversed(containers):
            docker('rm', '-f', name)
        for volume in reversed(created):
            docker('volume', 'rm', volume)
        subprocess.run(['docker', '--context', args.context, 'network', 'rm', network], capture_output=True)
        password.unlink(missing_ok=True)
        (out / 'result.json').write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps(report, indent=2))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    for flag in ('previous', 'candidate', 'helper'):
        parser.add_argument('--' + flag, required=True)
    parser.add_argument('--version', default='1.3.0-rc.1')
    parser.add_argument('--context', default='desktop-linux')
    parser.add_argument('--output', type=Path, required=True)
    try:
        qualify(parser.parse_args())
    except (Exception, KeyboardInterrupt) as error:
        raise SystemExit('FAIL isolated compatibility: ' + type(error).__name__ + '; private output withheld') from None
