#!/usr/bin/env python3
"""Process-level local-drive acceptance. Uses disposable data, no real credentials."""
import argparse
import hashlib
import http.cookiejar
import json
import os
from pathlib import Path
import signal
import socket
import sqlite3
import subprocess
import tempfile
import time
import urllib.error
import urllib.request


class Drive:
    def __init__(self, binary, data):
        self.binary = binary
        self.data = data
        with socket.socket() as sock:
            sock.bind(('127.0.0.1', 0))
            self.port = sock.getsockname()[1]
        self.url = f'http://127.0.0.1:{self.port}'
        self.process = None
        self.cookies = http.cookiejar.CookieJar()
        self.client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(self.cookies))

    def start(self):
        env = dict(os.environ, DRIVE_DATA_DIR=str(self.data), DRIVE_ADDR=f'127.0.0.1:{self.port}',
                   DRIVE_STORAGE='local', DRIVE_SECURE_COOKIES='false', DRIVE_ENV_FILE='-')
        for key in ('DRIVE_OWNER_PASSWORD', 'DRIVE_OWNER_PASSWORD_FILE', 'DRIVE_KEY_FILE'):
            env.pop(key, None)
        self.process = subprocess.Popen([str(self.binary)], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        for _ in range(150):
            if self.process.poll() is not None:
                raise AssertionError('Drive exited during startup')
            try:
                with urllib.request.urlopen(self.url + '/health', timeout=1) as response:
                    if response.status == 200:
                        return
            except (OSError, urllib.error.URLError):
                time.sleep(.05)
        raise AssertionError('Drive failed health check')

    def stop(self):
        if self.process and self.process.poll() is None:
            self.process.send_signal(signal.SIGTERM)
            try:
                self.process.wait(timeout=20)
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.process.wait()

    def request(self, method, path, body=None, headers=None, expect=200, raw=False, authenticated=True):
        headers = dict(headers or {})
        if isinstance(body, dict):
            body = json.dumps(body).encode()
            headers['Content-Type'] = 'application/json'
        elif body is not None:
            headers.setdefault('Content-Type', 'application/octet-stream')
        request = urllib.request.Request(self.url + '/api/v1' + path, body, headers, method=method)
        try:
            response = (self.client.open if authenticated else urllib.request.urlopen)(request, timeout=30)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            content = response.read()
            assert response.status == expect, f'{method} {path}: expected {expect}, got {response.status}: {content[:200]!r}'
            result = content if raw or not content else json.loads(content)
            return result, response.headers


def run(binary):
    with tempfile.TemporaryDirectory(prefix='harbor-acceptance-') as scratch:
        root = Path(scratch)
        data_dir = root / 'data'
        env = dict(os.environ, DRIVE_DATA_DIR=str(data_dir), DRIVE_ENV_FILE='-')
        for key in ('DRIVE_OWNER_PASSWORD_FILE', 'DRIVE_KEY_FILE'):
            env.pop(key, None)
        subprocess.run([str(binary), 'init'], env=env, check=True, stdout=subprocess.DEVNULL)
        password = (data_dir / 'owner-password').read_text().strip()
        drive = Drive(binary, data_dir)
        try:
            drive.start()
            drive.request('GET', '/nodes', expect=401, authenticated=False)
            drive.request('GET', '/diagnostics', expect=401, authenticated=False)
            drive.request('POST', '/auth/login', {'password': 'incorrect-fixture'}, expect=401)
            drive.request('POST', '/auth/login', {'password': password})
            drive.request('POST', '/folders', {'name': 'forbidden', 'parent_id': ''},
                          {'Origin': 'https://untrusted.invalid'}, expect=403)
            print('PASS owner authentication, unauthorized rejection, cross-origin rejection')
            folder, _ = drive.request('POST', '/folders', {'name': 'Acceptance', 'parent_id': ''}, expect=201)
            nested, _ = drive.request('POST', '/folders', {'name': 'Nested', 'parent_id': folder['id']}, expect=201)
            drive.request('PATCH', '/nodes/' + folder['id'], {'parent_id': nested['id']}, expect=409)
            drive.request('POST', '/folders', {'name': 'acceptance', 'parent_id': ''}, expect=409)
            # Exercise the active application geometry, including 100 MiB chunks.
            diagnostics, _ = drive.request('GET', '/diagnostics')
            content = bytes(range(256)) * (diagnostics['chunk_size'] // 256 + 1) + b'Harbor checksum fixture'
            digest = hashlib.sha256(content).hexdigest()
            upload, _ = drive.request('POST', '/uploads', {'name': 'roundtrip.bin', 'parent_id': nested['id'],
                                                        'size': len(content), 'mime_type': 'application/octet-stream', 'sha256': digest}, expect=201)
            uid = upload['id']
            size = upload['chunk_size']
            chunk = content[:size]
            drive.request('PUT', f'/uploads/{uid}/chunks/0', chunk)
            drive.request('PUT', f'/uploads/{uid}/chunks/0', chunk)
            drive.request('PUT', f'/uploads/{uid}/chunks/0', b'x' * len(chunk), expect=409)
            drive.request('POST', f'/uploads/{uid}/complete', {}, expect=409)
            drive.stop()
            drive.start()
            status, _ = drive.request('GET', f'/uploads/{uid}')
            assert status['received_chunks'] == [0] and status['received_bytes'] == size
            for index in range(1, upload['total_chunks']):
                chunk = content[index * size:(index + 1) * size]
                drive.request('PUT', f'/uploads/{uid}/chunks/{index}', chunk,
                              {'X-Chunk-SHA256': hashlib.sha256(chunk).hexdigest()})
            node, _ = drive.request('POST', f'/uploads/{uid}/complete', {})
            repeated, _ = drive.request('POST', f'/uploads/{uid}/complete', {})
            assert node['id'] == repeated['id'] == uid and node['sha256'] == digest
            drive.request('DELETE', f'/uploads/{uid}', expect=409)
            downloaded, headers = drive.request('GET', f'/nodes/{uid}/content?download=1', raw=True)
            assert downloaded == content and hashlib.sha256(downloaded).hexdigest() == digest
            assert headers['X-Content-SHA256'] == digest
            selected, _ = drive.request('GET', f'/nodes/{uid}/content', headers={'Range': f'bytes={size-13}-{size+29}'}, expect=206, raw=True)
            assert selected == content[size-13:size+30]
            drive.request('HEAD', f'/nodes/{uid}/content', raw=True)
            drive.request('GET', f'/nodes/{uid}/content', expect=401, raw=True, authenticated=False)
            print('PASS interrupted transfer, process restart, idempotent retry, checksum roundtrip, cross-chunk Range')
            drive.request('PATCH', '/nodes/' + folder['id'], {'name': 'Renamed'})
            drive.request('PATCH', f'/nodes/{uid}', {'name': 'renamed.bin', 'favorite': True})
            favorites, _ = drive.request('GET', '/nodes?view=favorites')
            assert uid in [n['id'] for n in favorites['items']]
            listing, _ = drive.request('GET', '/nodes?parent_id=' + nested['id'])
            assert [c['name'] for c in listing['breadcrumbs']] == ['My drive', 'Renamed', 'Nested']
            drive.request('DELETE', '/nodes/' + folder['id'], expect=204)
            drive.request('GET', f'/nodes/{uid}/content', expect=404, raw=True)
            drive.request('POST', '/nodes/' + folder['id'] + '/restore', {})
            downloaded, _ = drive.request('GET', f'/nodes/{uid}/content', raw=True)
            assert downloaded == content
            drive.request('PATCH', f'/nodes/{uid}', {'parent_id': ''})
            print('PASS hierarchy cycle rejection, duplicate names, rename, favorites, move, subtree trash/recovery')
            empty, _ = drive.request('POST', '/uploads', {'name': 'empty.txt', 'parent_id': '', 'size': 0, 'mime_type': 'text/plain'}, expect=201)
            drive.request('POST', '/uploads/' + empty['id'] + '/complete', {})
            empty_bytes, _ = drive.request('GET', '/nodes/' + empty['id'] + '/content', raw=True)
            assert empty_bytes == b''
            snapshot, _ = drive.request('GET', '/backup', expect=410)
            assert snapshot['code'] == 'encrypted_backup_required'
            diagnostic, _ = drive.request('GET', '/diagnostics')
            assert diagnostic['adapter'] == 'local' and diagnostic['encryption'] == 'AES-256-GCM'
            print('PASS empty file, diagnostics, plaintext metadata export rejection')
            drive.stop()
            backup_path = root / 'full.harbor'
            backup_password = root / 'backup-password'
            backup_password.write_text(os.urandom(32).hex())
            backup_password.chmod(0o600)
            recovery = Path(__file__).with_name('recovery.py')
            subprocess.run(['python3', str(recovery), 'backup', str(data_dir), str(backup_path), '--password-file', str(backup_password)], check=True, stdout=subprocess.DEVNULL)
            restored = root / 'restored'
            subprocess.run(['python3', str(recovery), 'restore', str(backup_path), str(restored), '--password-file', str(backup_password)], check=True, stdout=subprocess.DEVNULL)
            drive.data = restored
            drive.start()
            drive.request('GET', '/auth/session', expect=401)
            drive.request('POST', '/auth/login', {'password': password})
            downloaded, _ = drive.request('GET', f'/nodes/{uid}/content', raw=True)
            assert downloaded == content and hashlib.sha256(downloaded).hexdigest() == digest
            drive.request('POST', '/auth/logout', {}, expect=204)
            drive.request('GET', '/nodes', expect=401)
            print('PASS offline backup/restore, restored checksum, old-session revocation, logout revocation')
        finally:
            drive.stop()


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=Path, default=Path('bin/drive'))
    arguments = parser.parse_args()
    run(arguments.binary.resolve(strict=True))
