#!/usr/bin/env python3
"""Qualify one immutable release image with isolated live Telegram and recovery.

Stop original first. This process holds its lock until all clones stop. Only
generated fixtures are written remotely; no Telegram messages are deleted.
"""
import argparse
import fcntl
import io
import json
import os
from pathlib import Path
import re
import secrets
import signal
import subprocess
import tarfile
import tempfile
import threading
import time
import urllib.request

import phase3_benchmark as benchmark
import phase5_live_recovery_check as recovery
import telegram_qualification as q

ROOT = Path(__file__).resolve().parents[1]
PROJECT = 'harbor-phase6-live'
VOLUME = PROJECT + '_drive-data'
ARCHIVE = PROJECT + '-recovery'
RESTORED = PROJECT + '-restored-data'


def check(condition, message):
    if not condition:
        raise q.QualificationError(message)


def docker(*args, data=None, success=True, timeout=180):
    result = subprocess.run(['docker', *map(str, args)], cwd=ROOT, input=data,
                            capture_output=True, timeout=timeout)
    check((result.returncode == 0) == success, 'Docker qualification operation failed: ' + str(args[0]))
    return result.stdout


def private_tar(files):
    stream = io.BytesIO()
    with tarfile.open(fileobj=stream, mode='w') as archive:
        for name, content in files.items():
            member = tarfile.TarInfo(name)
            member.uid = member.gid = 65532
            member.mode, member.size = 0o600, len(content)
            archive.addfile(member, io.BytesIO(content))
    return stream.getvalue()


def read_member(container, path):
    raw = docker('cp', container + ':' + path, '-')
    with tarfile.open(fileobj=io.BytesIO(raw)) as archive:
        members = archive.getmembers()
        check(len(members) == 1 and members[0].isfile(), 'Unexpected private file copy')
        return archive.extractfile(members[0]).read()


def wait_ready(base):
    started = time.monotonic()
    while time.monotonic() - started < 90:
        try:
            with urllib.request.urlopen(base + '/ready', timeout=2) as response:
                if response.status == 200:
                    return time.monotonic() - started
        except OSError:
            time.sleep(.2)
    raise q.QualificationError('Isolated readiness deadline exceeded')


class Samples:
    def __init__(self, container):
        self.container, self.values = container, []
        self.stop = threading.Event()
        self.thread = threading.Thread(target=self.sample, daemon=True)

    def sample(self):
        while not self.stop.is_set():
            try:
                raw = docker('stats', '--no-stream', '--format', '{{json .}}', self.container, timeout=8)
                value = json.loads(raw)
                number, unit = re.fullmatch(r'([0-9.]+)\s*([A-Za-z]+)', value['MemUsage'].split('/')[0].strip()).groups()
                units = {'B': 1, 'KiB': 1024, 'MiB': 1 << 20, 'GiB': 1 << 30}
                self.values.append({'memory_bytes': int(float(number) * units[unit]),
                                    'cpu_percent': float(value['CPUPerc'].removesuffix('%'))})
            except (ValueError, AttributeError, KeyError, OSError, subprocess.SubprocessError, q.QualificationError):
                pass
            self.stop.wait(.25)

    def finish(self):
        self.stop.set()
        self.thread.join(timeout=10)
        check(bool(self.values), 'No container resource samples collected')
        return {'method': 'docker stats --no-stream; sampled working set, not lifetime RSS peak',
                'samples': len(self.values),
                'memory_bytes_peak_sampled': max(v['memory_bytes'] for v in self.values),
                'cpu_percent_peak_sampled': max(v['cpu_percent'] for v in self.values)}


def run(args):
    check(re.fullmatch(r'sha256:[0-9a-f]{64}', args.image), 'Use exact local image ID')
    check(1024 <= args.port < 65536, 'Use unprivileged loopback port')
    output = args.output.resolve()
    check(output.is_relative_to((ROOT / 'output').resolve()) and not output.exists(), 'Use fresh ignored output directory')
    os.umask(0o077)
    output.mkdir(parents=True, mode=0o700)
    settings = recovery.config(args.source_env)
    source = Path(settings.get('DRIVE_DATA_DIR', 'data')).resolve()
    session = Path(settings.get('TELEGRAM_SESSION_FILE', str(source / 'telegram.session')))
    # Reuse strict private-file checks before any authorization copy.
    check(session.is_file() and not session.is_symlink() and session.stat().st_mode & 0o077 == 0,
          'Private regular source session required')
    check(not docker('ps', '-aq', '--filter', 'label=com.docker.compose.project=' + PROJECT).strip(), 'Qualification project already exists')
    for volume in (VOLUME, ARCHIVE, RESTORED):
        check(subprocess.run(['docker', 'volume', 'inspect', volume], capture_output=True).returncode != 0,
              'Qualification volume already exists')
    base = 'http://127.0.0.1:' + str(args.port)
    seed, workspace, recovered = PROJECT + '-seed', PROJECT + '-workspace', PROJECT + '-restored'
    restore_seed = PROJECT + '-restore-seed'
    for name in (seed, workspace, recovered, restore_seed):
        check(subprocess.run(['docker', 'inspect', name], capture_output=True).returncode != 0,
              'Qualification helper already exists')
    evidence = {'result': 'PARTIAL', 'image_id': args.image, 'live_cross_dc': 'NOT TESTED', 'stages': []}
    with (source / '.lock').open('r+b') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        with tempfile.TemporaryDirectory(prefix='private-', dir=output) as temporary:
            private = Path(temporary)
            env = {k: v for k, v in settings.items() if (k.startswith('TELEGRAM_') and not k.endswith('_FILE')) or k == 'USERNAME_OR_CHANNEL_LINK'}
            env.update(DRIVE_STORAGE='telegram', DRIVE_PORT=str(args.port), DRIVE_SECURE_COOKIES='false')
            config = private / 'runtime.env'
            recovery.private_write(config, ''.join(k + '=' + json.dumps(v) + '\n' for k, v in sorted(env.items())).encode())
            override = private / 'compose.json'
            override.write_text(json.dumps({'services': {'drive': {'image': args.image}}}))
            compose = ['compose', '--env-file', str(config), '-f', str(ROOT / 'compose.yaml'), '-f', str(override), '-p', PROJECT]
            # Empty application overrides; Docker configuration itself remains available.
            saved = {k: os.environ.pop(k) for k in list(os.environ) if k.startswith(('DRIVE_', 'TELEGRAM_', 'COMPOSE_')) or k == 'USERNAME_OR_CHANNEL_LINK'}
            def dc(*parts):
                return docker(*compose, *parts)

            def runtime():
                identity = dc('ps', '-q', 'drive').decode().strip()
                value = json.loads(docker('inspect', '--format', '{"id":{{json .Id}},"image":{{json .Image}},"started":{{json .State.StartedAt}},"running":{{json .State.Running}}}', identity))
                check(value['image'] == args.image and value['running'], 'Exact release image not running')
                return value

            def qualify(phase):
                q.run(argparse.Namespace(base_url=base, password_file=password, state_file=state,
                                         phase=phase, server_pid=None, live=True, local_check=False))

            def stop():
                dc('stop', '--timeout', '20')
                identity = dc('ps', '-aq', 'drive').decode().strip()
                if identity:
                    status = json.loads(docker('inspect', '--format', '{"running":{{json .State.Running}},"exit":{{json .State.ExitCode}},"oom":{{json .State.OOMKilled}}}', identity))
                    check(status == {'running': False, 'exit': 0, 'oom': False}, 'Clean shutdown failed')

            try:
                docker('create', '--name', seed, '--network=none', '--read-only', '--cap-drop=ALL',
                       '--security-opt=no-new-privileges', '-v', VOLUME + ':/data', args.image, 'init')
                docker('start', '--attach', seed)
                docker('cp', '-a', '-', seed + ':/data', data=private_tar({'telegram.session': session.read_bytes()}))
                password = private / 'owner-password'
                recovery.private_write(password, read_member(seed, '/data/owner-password'))
                docker('rm', seed)
                started = time.monotonic()
                dc('up', '-d', '--no-build', '--pull', 'never')
                wait_ready(base)
                initial = runtime()
                evidence['startup_seconds'] = time.monotonic() - started
                client = q.Client(base, password)
                diagnostics, _ = client.request('GET', '/diagnostics')
                check(diagnostics['version'] == '1.0.0' and diagnostics['adapter'] == 'telegram' and diagnostics['storage_connection']['state'] == 'connected', 'Release diagnostics mismatch')
                client.request('POST', '/auth/logout', {}, expect=204)
                idle = Samples(initial['id']); idle.thread.start(); time.sleep(3)
                evidence['idle_resources'] = idle.finish()
                state = output / 'fixtures.json'
                qualify('upload')
                stop(); dc('start'); wait_ready(base)
                restarted = runtime()
                check(initial['id'] == restarted['id'] and initial['started'] != restarted['started'], 'Container restart not observed')
                qualify('resume')
                stop(); dc('up', '-d', '--no-build', '--pull', 'never', '--force-recreate'); wait_ready(base)
                recreated = runtime()
                check(recreated['id'] != initial['id'], 'Container recreation not observed')
                qualify('restart-check')
                evidence['stages'] = ['encrypted small/multipart upload', 'restart and resume', 'recreation and four SHA-256 downloads']
                sampler = Samples(recreated['id']); sampler.thread.start()
                try:
                    benchmark.run(argparse.Namespace(base_url=base, password_file=str(password), state_file=str(output / 'benchmark.json'),
                                  label='v1-final-container', sizes_mib=args.sizes_mib, server_pid=None, recheck=False, live=True))
                finally:
                    evidence['transfer_resources'] = sampler.finish()
                if args.browser_hold:
                    client = q.Client(base, password)
                    cookies = [{'name': c.name, 'value': c.value, 'domain': c.domain, 'path': c.path,
                                'expires': c.expires or -1, 'httpOnly': True, 'secure': c.secure, 'sameSite': 'Strict'} for c in client.cookies]
                    q.save_state(private / 'browser-state.json', {'cookies': cookies, 'origins': []})
                    q.save_state(output / 'browser-ready.json', {'origin': base, 'private_state': str(private / 'browser-state.json')})
                    print('READY browser qualification; authorization remains exclusively locked', flush=True)
                    deadline = time.monotonic() + 600
                    while not (output / 'browser-complete.json').exists():
                        check(time.monotonic() < deadline, 'Browser qualification deadline exceeded')
                        time.sleep(.25)
                    check(json.loads((output / 'browser-complete.json').read_text())['result'] == 'PASS', 'Browser qualification failed')
                    try:
                        client.request('POST', '/auth/logout', {}, expect=204)
                    except q.QualificationError:
                        pass  # Browser already revoked this same temporary HTTP session.
                fixture_state = q.load_state(state)
                text_id = fixture_state['files']['small.txt']['upload_id']
                pdf_id = fixture_state['files']['small.pdf']['upload_id']
                client = q.Client(base, password)
                nested, _ = client.request('POST', '/folders', {'name': 'Nested recovery', 'parent_id': fixture_state['folder_id']}, expect=201)
                client.request('PATCH', '/nodes/' + text_id, {'parent_id': nested['id'], 'favorite': True})
                client.request('DELETE', '/nodes/' + pdf_id, expect=204)
                client.request('POST', '/auth/logout', {}, expect=204)
                # Complete archive crosses into a fresh volume; restore uses no host key/session/config.
                stop()
                docker('create', '--name', workspace, '--network=none', '--read-only', '-v', ARCHIVE + ':/data', args.image, 'health')
                archive_password = secrets.token_urlsafe(48).encode()
                docker('cp', '-a', '-', workspace + ':/data', data=private_tar({'archive-password': archive_password,
                       'wrong-password': secrets.token_urlsafe(48).encode(), 'runtime.env': config.read_bytes()}))
                options = ['run', '--rm', '--network=none', '--read-only', '--cap-drop=ALL', '--security-opt=no-new-privileges',
                           '-v', VOLUME + ':/data', '-v', ARCHIVE + ':/archive', '--entrypoint', '/app/harbor-recovery', args.image]
                docker(*options, 'backup', '--data-dir', '/data', '--output', '/archive/full.harbor', '--password-file', '/archive/archive-password', '--config-file', '/archive/runtime.env')
                docker(*options, 'verify', '--archive', '/archive/full.harbor', '--password-file', '/archive/archive-password')
                docker(*options, 'restore', '--archive', '/archive/full.harbor', '--destination', '/archive/rejected', '--password-file', '/archive/wrong-password', success=False)
                archive = read_member(workspace, '/data/full.harbor')
                evidence['backup'] = {'encrypted_bytes': len(archive), 'sha256': q.digest(archive), 'wrong_password_rejected': True}
                docker('create', '--name', restore_seed, '--network=none', '--read-only', '-v', RESTORED + ':/data', args.image, 'health')
                docker('cp', '-a', '-', restore_seed + ':/data', data=private_tar({'full.harbor': archive, 'archive-password': archive_password}))
                docker('rm', restore_seed)
                # New volume receives only archive/password, no source config, key, session or DB.
                recovery_options = ['run', '--rm', '--network=none', '--read-only', '--cap-drop=ALL', '--security-opt=no-new-privileges',
                                    '-v', RESTORED + ':/archive', '--entrypoint', '/app/harbor-recovery', args.image]
                docker(*recovery_options, 'restore', '--archive', '/archive/full.harbor', '--destination', '/archive/restored', '--password-file', '/archive/archive-password')
                docker('run', '-d', '--name', recovered, '--read-only', '--cap-drop=ALL', '--security-opt=no-new-privileges',
                       '--memory=768m', '--cpus=2', '--pids-limit=128', '--tmpfs', '/tmp:size=128m,noexec,nosuid,nodev',
                       '-p', '127.0.0.1:' + str(args.port) + ':8080', '-v', RESTORED + ':/archive',
                       '-e', 'DRIVE_ENV_FILE=/archive/restored/recovery.env', '-e', 'DRIVE_DATA_DIR=/archive/restored',
                       '-e', 'DRIVE_ADDR=0.0.0.0:8080', '-e', 'DRIVE_ALLOW_REMOTE_HTTP=true', args.image)
                wait_ready(base)
                client = q.Client(base, password)
                text, _ = client.request('GET', '/nodes/' + text_id)
                folder, _ = client.request('GET', '/nodes/' + nested['id'])
                trash, _ = client.request('GET', '/nodes?view=trash')
                check(text['parent_id'] == nested['id'] and text['favorite'] and folder['parent_id'] == fixture_state['folder_id']
                      and any(node['id'] == pdf_id for node in trash['items']), 'Restored hierarchy/favorite/Trash mismatch')
                client.request('POST', '/nodes/' + pdf_id + '/restore', {})
                client.request('POST', '/auth/logout', {}, expect=204)
                qualify('restart-check')
                benchmark.run(argparse.Namespace(base_url=base, password_file=str(password), state_file=str(output / 'benchmark.json'),
                              label='fresh-container-restore', sizes_mib=args.sizes_mib, server_pid=None, recheck=True, live=True))
                docker('stop', '--time', '20', recovered)
                docker('start', recovered); wait_ready(base); qualify('restart-check')
                docker('stop', '--time', '20', recovered)
                check(docker('inspect', '--format', '{{.State.ExitCode}}', recovered).strip() == b'0', 'Restored clean shutdown failed')
                evidence.update(result='PASS', encrypted_integrity='PASS', persistent_session='PASS', restart='PASS',
                                recreation='PASS', clean_environment_restore='PASS', restored_restart='PASS',
                                hierarchy_favorites_trash='PASS')
            finally:
                # Cleanup must finish before releasing original authorization lock.
                cleanup_ok = True
                for name in (recovered, seed, workspace, restore_seed):
                    try:
                        subprocess.run(['docker', 'rm', '-f', name], capture_output=True, timeout=40)
                    except (OSError, subprocess.SubprocessError):
                        cleanup_ok = False
                try:
                    dc('down', '--timeout', '20')
                    check(not docker('ps', '-q', '--filter', 'label=com.docker.compose.project=' + PROJECT).strip(),
                          'Isolated Compose container still running; keep original stopped')
                    for name in (recovered, seed, workspace, restore_seed):
                        status = subprocess.run(['docker', 'inspect', '--format', '{{.State.Running}}', name],
                                                capture_output=True, timeout=20)
                        check(status.returncode != 0 or status.stdout.strip() == b'false',
                              'Isolated standalone container still running; keep original stopped')
                    check(cleanup_ok, 'Qualification cleanup incomplete; verify clones before original restart')
                    docker('volume', 'rm', ARCHIVE, VOLUME, RESTORED)
                finally:
                    os.environ.update(saved)
            evidence['cleanup'] = 'PASS'
            q.save_state(output / 'evidence.json', evidence)
            print('PASS exact release image: live Telegram, multipart, restart/recreation, encrypted fresh restore and checksums', flush=True)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source-env', type=Path, required=True)
    parser.add_argument('--image', required=True)
    parser.add_argument('--output', type=Path, default=ROOT / 'output/phase6/live')
    parser.add_argument('--port', type=int, default=8093)
    parser.add_argument('--sizes-mib', type=int, nargs='+', default=[1, 10, 100])
    parser.add_argument('--browser-hold', action='store_true')
    args = parser.parse_args()
    check(args.sizes_mib and all(1 <= n <= 100 for n in args.sizes_mib), 'Controlled sizes must be1–100MiB')
    def interrupted(signum, frame):
        raise q.QualificationError('Qualification interrupted; cleanup required before original restart')
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    try:
        run(args)
    except Exception as error:
        print('FAIL release live qualification: ' + (str(error) if isinstance(error, q.QualificationError) else type(error).__name__))
        raise SystemExit(1)
