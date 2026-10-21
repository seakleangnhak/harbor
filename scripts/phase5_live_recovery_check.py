#!/usr/bin/env python3
"""Controlled live DR qualification. Stop original Harbor before running.

Reads private source configuration/session only while holding its offline lock.
Never writes source data or deletes Telegram objects. All clone secrets/archives
are removed on exit; only generated fixture IDs/hashes and sanitized evidence remain.
"""
import argparse
import fcntl
import json
import os
from pathlib import Path
import secrets
import shlex
import signal
import stat
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

import telegram_qualification as q

ROOT = Path(__file__).resolve().parents[1]
OUTPUT = ROOT / 'output/phase5'


def config(path):
    info = path.lstat()
    check(stat.S_ISREG(info.st_mode) and info.st_mode & 0o077 == 0, 'Configuration must be private regular file')
    values = {}
    for line in path.read_text().splitlines():
        line = line.strip().removeprefix('export ')
        if not line or line.startswith('#'):
            continue
        name, value = line.split('=', 1)
        words = shlex.split(value, comments=True)
        values[name.strip()] = words[0] if words else ''
    return values


def private_write(path, data):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'wb') as stream:
        stream.write(data)


def check(condition, reason):
    if not condition:
        raise RuntimeError(reason)


def command(args, env=None, success=True):
    result = subprocess.run(list(map(str, args)), cwd=ROOT, env=env, capture_output=True, timeout=180)
    check((result.returncode == 0) == success, 'Recovery command unexpected exit; private output withheld')
    return result


class Server:
    def __init__(self, binary, environment, folder, port):
        self.env = dict(os.environ, DRIVE_ENV_FILE='-', **environment)
        self.env['DRIVE_ADDR'] = f'127.0.0.1:{port}'
        self.url = f'http://127.0.0.1:{port}'
        self.binary, self.folder, self.proc = binary, folder, None

    def start(self):
        with (self.folder / 'server.log').open('ab') as log:
            self.proc = subprocess.Popen([str(self.binary)], cwd=ROOT, env=self.env,
                                         stdin=subprocess.DEVNULL, stdout=log, stderr=log)
        deadline = time.monotonic() + 90
        while time.monotonic() < deadline:
            check(self.proc.poll() is None, 'Isolated Telegram startup failed; private log withheld')
            try:
                with urllib.request.urlopen(self.url + '/ready', timeout=2) as response:
                    if response.status == 200:
                        return
            except OSError:
                time.sleep(.2)
        raise RuntimeError('Isolated readiness deadline exceeded')

    def stop(self):
        if self.proc and self.proc.poll() is None:
            self.proc.terminate()
            try:
                self.proc.wait(timeout=30)
            except subprocess.TimeoutExpired:
                self.proc.kill()
                self.proc.wait()
                raise RuntimeError('Graceful shutdown deadline exceeded')
        self.proc = None


def qualification(server, password, state, phase):
    q.run(argparse.Namespace(base_url=server.url, password_file=password, state_file=state,
                             phase=phase, server_pid=server.proc.pid, live=True, local_check=False))


def assert_metadata(server, password, state, expected):
    client = q.Client(server.url, password)
    try:
        for node_id, fields in expected.items():
            node, _ = client.request('GET', '/nodes/' + node_id)
            check(all(node.get(key) == value for key, value in fields.items()), 'Restored metadata mismatch')
        trash, _ = client.request('GET', '/nodes?view=trash')
        check(any(node['id'] == state['files']['small.pdf']['upload_id'] for node in trash['items']), 'Restored Trash state missing')
        favorites, _ = client.request('GET', '/nodes?view=favorites')
        check(any(node['id'] == state['files']['small.txt']['upload_id'] for node in favorites['items']), 'Restored favorite state missing')
    finally:
        client.request('POST', '/auth/logout', {}, expect=204)


def run(args):
    os.umask(0o077)
    OUTPUT.mkdir(parents=True, exist_ok=True)
    source_env = config(args.source_env)
    source = Path(source_env.get('DRIVE_DATA_DIR', 'data')).resolve()
    session = Path(source_env.get('TELEGRAM_SESSION_FILE', str(source / 'telegram.session')))
    info = session.lstat()
    check(stat.S_ISREG(info.st_mode) and info.st_mode & 0o077 == 0, 'Private regular persisted Telegram session required')
    # Holding original lock prevents original process restart during cloned authorization use.
    with (source / '.lock').open('r+b') as source_lock:
        fcntl.flock(source_lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        with tempfile.TemporaryDirectory(prefix='live-dr-', dir=OUTPUT) as temp:
            folder = Path(temp)
            data = folder / 'source'
            data.mkdir(mode=0o700)
            env = {name: value for name, value in source_env.items()
                   if name.startswith('TELEGRAM_') or name == 'USERNAME_OR_CHANNEL_LINK'}
            env.update(DRIVE_STORAGE='telegram', DRIVE_DATA_DIR=str(data),
                       DRIVE_KEY_FILE=str(data / 'content.key'),
                       DRIVE_OWNER_PASSWORD_FILE=str(data / 'owner-password'),
                       TELEGRAM_SESSION_FILE=str(data / 'telegram.session'), DRIVE_SECURE_COOKIES='false')
            env_file = folder / 'qualification.env'
            private_write(env_file, ''.join(name + '=' + json.dumps(value) + '\n' for name, value in sorted(env.items())).encode())
            environment = dict(os.environ, DRIVE_ENV_FILE='-', **env)
            command([args.binary, 'init'], environment)
            private_write(data / 'telegram.session', session.read_bytes())
            password = data / 'owner-password'
            state_path = OUTPUT / ('live-dr-state-' + secrets.token_hex(4) + '.json')
            server = Server(args.binary, env, folder, args.port)
            evidence = {'status': 'PARTIAL', 'live_telegram': True, 'source_data_untouched': True}
            try:
                server.start()
                qualification(server, password, state_path, 'upload')
                server.stop()
                server.start()
                qualification(server, password, state_path, 'resume')
                state = q.load_state(state_path)
                client = q.Client(server.url, password)
                nested, _ = client.request('POST', '/folders', {'name': 'Nested recovery', 'parent_id': state['folder_id']}, expect=201)
                inner, _ = client.request('POST', '/folders', {'name': 'Inner', 'parent_id': nested['id']}, expect=201)
                text_id, pdf_id = [state['files'][name]['upload_id'] for name in ('small.txt', 'small.pdf')]
                client.request('PATCH', '/nodes/' + text_id, {'parent_id': inner['id'], 'favorite': True})
                client.request('DELETE', '/nodes/' + pdf_id, expect=204)
                expected = {nested['id']: {'parent_id': state['folder_id'], 'kind': 'folder'},
                            inner['id']: {'parent_id': nested['id'], 'kind': 'folder'},
                            text_id: {'parent_id': inner['id'], 'favorite': True}}
                client.request('POST', '/auth/logout', {}, expect=204)
                server.stop()
                key_hash = q.digest((data / 'content.key').read_bytes())
                backup_password = folder / 'archive-password'
                private_write(backup_password, secrets.token_urlsafe(48).encode())
                archive = folder / 'complete.harbor'
                common = ['--password-file', backup_password]
                command([args.recovery, 'backup', '--data-dir', data, '--output', archive, '--config-file', env_file, *common])
                command([args.recovery, 'verify', '--archive', archive, *common])
                evidence['encrypted_backup_bytes'] = archive.stat().st_size
                wrong = folder / 'wrong-password'
                private_write(wrong, secrets.token_urlsafe(48).encode())
                bad_destination = folder / 'bad-password-restore'
                command([args.recovery, 'restore', '--archive', archive, '--destination', bad_destination, '--password-file', wrong], success=False)
                check(not bad_destination.exists(), 'Wrong password published destination')
                corrupted = folder / 'corrupt.harbor'
                raw = bytearray(archive.read_bytes()); raw[len(raw) // 2] ^= 1
                private_write(corrupted, raw)
                corrupt_destination = folder / 'corrupt-restore'
                command([args.recovery, 'restore', '--archive', corrupted, '--destination', corrupt_destination, *common], success=False)
                check(not corrupt_destination.exists(), 'Corruption published destination')
                key = data / 'content.key'
                key.rename(data / 'held-key')
                missing_archive = folder / 'missing-key.harbor'
                try:
                    command([args.recovery, 'backup', '--data-dir', data, '--output', missing_archive, '--config-file', env_file, *common], success=False)
                    check(not missing_archive.exists(), 'Missing key published backup')
                finally:
                    (data / 'held-key').rename(key)
                interrupted = folder / 'interrupted-restore'
                process = subprocess.Popen(list(map(str, [args.recovery, 'restore', '--archive', archive, '--destination', interrupted, *common])),
                                           cwd=ROOT, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
                deadline = time.monotonic() + 10
                while process.poll() is None and time.monotonic() < deadline:
                    if list(folder.glob('.harbor-restore-*')):
                        process.send_signal(signal.SIGTERM)
                        break
                    time.sleep(.001)
                process.communicate(timeout=20)
                check(process.returncode != 0 and not interrupted.exists() and not list(folder.glob('.harbor-restore-*')), 'Interrupted restore publication/cleanup failed')
                restored = folder / 'restored'
                # No original key, session, password or configuration supplied to restoration.
                command([args.recovery, 'restore', '--archive', archive, '--destination', restored, *common])
                check(q.digest((restored / 'content.key').read_bytes()) == key_hash, 'Restored key differs')
                check(all((restored / leaf).stat().st_mode & 0o077 == 0 for leaf in ('drive.db', 'content.key', 'owner-password', 'telegram.session', 'recovery.env')), 'Restored permissions unsafe')
                restored_env = config(restored / 'recovery.env')
                server = Server(args.binary, restored_env, folder, args.port)
                server.start()
                # Trashed content cannot be downloaded until restored; preserve/reverify state first.
                assert_metadata(server, restored / 'owner-password', state, expected)
                client = q.Client(server.url, restored / 'owner-password')
                client.request('POST', '/nodes/' + pdf_id + '/restore', {})
                client.request('POST', '/auth/logout', {}, expect=204)
                qualification(server, restored / 'owner-password', state_path, 'restart-check')
                server.stop()
                audit = command([args.binary, 'integrity-content'], dict(os.environ, DRIVE_ENV_FILE='-', **restored_env))
                report = json.loads(audit.stdout)
                check(report['verified_files'] == 4 and not report['issues'], 'Restored full-content audit failed')
                server.start()
                qualification(server, restored / 'owner-password', state_path, 'restart-check')
                evidence.update(status='PASS', encrypted_backup_restore='PASS', restored_restart='PASS',
                                metadata_hierarchy_favorites_trash='PASS', wrong_password='PASS',
                                corruption='PASS', missing_key='PASS', interrupted_restore='PASS',
                                full_content_audit='PASS', verified_files=report['verified_files'],
                                fixture_sha256={name: entry['sha256'] for name, entry in state['files'].items()},
                                archive_includes='database,key,allowlisted_config,owner_password,telegram_session,chunk_manifests',
                                qualification_state=str(state_path.relative_to(ROOT)))
                q.save_state(OUTPUT / 'live-dr-evidence.json', evidence)
                print('PASS live Telegram encrypted disaster recovery: four files, nested metadata, favorites, Trash, restart and negative controls')
            finally:
                server.stop()


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source-env', type=Path, required=True)
    parser.add_argument('--binary', type=Path, default=ROOT / 'bin/drive-telegram-phase5')
    parser.add_argument('--recovery', type=Path, default=ROOT / 'bin/harbor-recovery')
    parser.add_argument('--port', type=int, default=8093)
    def interrupted(signum, frame):
        raise RuntimeError('Qualification interrupted; clone stopped before original lock release')
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    try:
        run(parser.parse_args())
    except Exception as error:
        print('FAIL isolated live DR: ' + (str(error) if isinstance(error, RuntimeError) else type(error).__name__))
        raise SystemExit(1)
