#!/usr/bin/env python3
"""Controlled Harbor HTTP qualification; writes generated fixtures only, never deletes.

Run upload, restart Harbor externally, run resume, restart again, then restart-check.
Live mode requires --live and a Telegram adapter. --local-check exercises this runner
against a disposable local adapter and never qualifies Telegram.
"""
import argparse
import hashlib
import http.cookiejar
import ipaddress
import json
import os
from pathlib import Path
import re
import secrets
import struct
import subprocess
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
import zlib


OUTPUT = Path(__file__).resolve().parent.parent / 'output'
ID = re.compile(r'^[0-9a-f]{32}$')


class QualificationError(Exception):
    pass


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, response, code, message, headers, new_url):
        raise QualificationError('HTTP redirects rejected')


def fixture(name, chunk_size):
    if name == 'small.txt':
        return b'Harbor Telegram qualification. Generated fixture; no personal data.\n', 'text/plain'
    if name == 'small.png':
        def chunk(kind, content):
            return struct.pack('!I', len(content)) + kind + content + struct.pack('!I', zlib.crc32(kind + content))
        pixels = b''.join(b'\0' + bytes((x * 16, y * 16, (x + y) * 8)) * 16 for y, x in enumerate(range(16)))
        return (b'\x89PNG\r\n\x1a\n' + chunk(b'IHDR', struct.pack('!2I5B', 16, 16, 8, 2, 0, 0, 0)) +
                chunk(b'IDAT', zlib.compress(pixels)) + chunk(b'IEND', b'')), 'image/png'
    if name == 'small.pdf':
        stream = b'BT /F1 18 Tf 40 80 Td (Harbor generated qualification fixture) Tj ET'
        objects = [b'<< /Type /Catalog /Pages 2 0 R >>', b'<< /Type /Pages /Kids [3 0 R] /Count 1 >>',
                   b'<< /Type /Page /Parent 2 0 R /MediaBox [0 0 480 160] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>',
                   b'<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>',
                   b'<< /Length ' + str(len(stream)).encode() + b' >>\nstream\n' + stream + b'\nendstream']
        data, offsets = b'%PDF-1.4\n', [0]
        for index, obj in enumerate(objects, 1):
            offsets.append(len(data))
            data += str(index).encode() + b' 0 obj\n' + obj + b'\nendobj\n'
        xref = len(data)
        data += b'xref\n0 6\n0000000000 65535 f \n'
        data += b''.join(f'{offset:010d} 00000 n \n'.encode() for offset in offsets[1:])
        return data + b'trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n' + str(xref).encode() + b'\n%%EOF\n', 'application/pdf'
    if name == 'multipart.bin':
        size = chunk_size + 65557
        return (bytes(range(256)) * ((size + 255) // 256))[:size], 'application/octet-stream'
    raise QualificationError('Unknown fixture name')


def digest(data):
    return hashlib.sha256(data).hexdigest()


def validate_url(value):
    parsed = urllib.parse.urlsplit(value)
    try:
        loopback = parsed.hostname == 'localhost' or ipaddress.ip_address(parsed.hostname).is_loopback
    except (ValueError, TypeError):
        loopback = False
    if not loopback or parsed.scheme not in ('http', 'https') or parsed.username or parsed.password or parsed.path not in ('', '/') or parsed.query or parsed.fragment:
        raise argparse.ArgumentTypeError('Use a loopback HTTP(S) origin without path, credentials or query')
    return value.rstrip('/')


class Client:
    def __init__(self, base_url, password_file):
        self.base_url = validate_url(base_url)
        self.cookies = http.cookiejar.CookieJar()
        self.client = urllib.request.build_opener(NoRedirect(), urllib.request.HTTPCookieProcessor(self.cookies))
        self.anonymous = urllib.request.build_opener(NoRedirect())
        password = password_file.read_text().strip()
        if not password:
            raise QualificationError('Owner password file is empty')
        self.request('POST', '/auth/login', {'password': password})

    def request(self, method, path, body=None, headers=None, expect=200, raw=False, anonymous=False, limit=16 << 20):
        headers = dict(headers or {})
        if isinstance(body, dict):
            body = json.dumps(body).encode()
            headers['Content-Type'] = 'application/json'
        elif body is not None:
            headers.setdefault('Content-Type', 'application/octet-stream')
        request = urllib.request.Request(self.base_url + '/api/v1' + path, body, headers, method=method)
        try:
            response = (self.anonymous if anonymous else self.client).open(request, timeout=180)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            content = response.read(limit + 1)
            if response.status != expect:
                raise QualificationError(f'{method} {path.split("?")[0]}: expected HTTP {expect}, got {response.status}')
            if len(content) > limit:
                raise QualificationError('HTTP response exceeds qualification limit')
            return (content if raw or not content else json.loads(content)), response.headers


def save_state(path, state):
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_name(path.name + '.pending-' + secrets.token_hex(4))
    fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    try:
        with os.fdopen(fd, 'w') as output:
            json.dump(state, output, indent=2, sort_keys=True)
            output.write('\n')
            output.flush()
            os.fsync(output.fileno())
        os.replace(temporary, path)
    finally:
        temporary.unlink(missing_ok=True)


def load_state(path):
    state = json.loads(path.read_text())
    if (state.get('version') != 1 or not re.fullmatch(r'[0-9a-f]{12}', state.get('run_id', '')) or
            not ID.fullmatch(state.get('folder_id', '')) or not 1 <= state.get('chunk_size', 0) <= 8 << 20):
        raise QualificationError('Invalid qualification state')
    if set(state.get('files', {})) != {'small.txt', 'small.png', 'small.pdf', 'multipart.bin'}:
        raise QualificationError('Invalid qualification fixture set')
    for name, entry in state['files'].items():
        content, mime = fixture(name, state['chunk_size'])
        if entry.get('size') != len(content) or entry.get('sha256') != digest(content) or entry.get('mime_type') != mime:
            raise QualificationError('Fixture state does not match generated content')
        if entry.get('upload_id') and not ID.fullmatch(entry['upload_id']):
            raise QualificationError('Invalid tracked upload ID')
    return state


def connection_snapshot(diagnostics):
    connection = diagnostics.get('storage_connection', {})
    return {key: connection[key] for key in ('state', 'retry_count', 'last_checked_at') if key in connection}


class ProcessSamples:
    def __init__(self, pid):
        self.pid, self.samples = pid, []
        self.stop = threading.Event()
        self.thread = threading.Thread(target=self.sample, daemon=True)

    def sample(self):
        while not self.stop.is_set():
            try:
                result = subprocess.run(['ps', '-p', str(self.pid), '-o', '%cpu=', '-o', 'rss='], capture_output=True, text=True, timeout=2)
                cpu, rss = map(float, result.stdout.split())
                self.samples.append((cpu, int(rss * 1024)))
            except (OSError, ValueError, subprocess.TimeoutExpired):
                pass
            self.stop.wait(.25)

    def result(self):
        self.stop.set()
        self.thread.join(timeout=3)
        if not self.samples:
            return {'status': 'NOT TESTED', 'reason': 'No server process samples collected'}
        return {'status': 'PASS', 'method': 'ps %cpu and RSS sampled every 250ms; OS-reported CPU percentage',
                'samples': len(self.samples), 'cpu_percent_mean': sum(x[0] for x in self.samples) / len(self.samples),
                'cpu_percent_max': max(x[0] for x in self.samples), 'rss_bytes_peak_sampled': max(x[1] for x in self.samples)}


def verify_download(client, state, name, entry):
    expected, _ = fixture(name, state['chunk_size'])
    start = time.perf_counter()
    content, headers = client.request('GET', f'/nodes/{entry["upload_id"]}/content?download=1', raw=True, limit=len(expected))
    seconds = time.perf_counter() - start
    if content != expected or digest(content) != entry['sha256'] or headers['X-Content-SHA256'] != entry['sha256']:
        raise QualificationError('Downloaded fixture checksum mismatch')
    return {'bytes': len(content), 'seconds': seconds, 'mib_per_second': len(content) / (1 << 20) / seconds, 'sha256_equal': True}


def finish_upload(client, state, name, path, stage=False):
    entry = state['files'][name]
    content, mime = fixture(name, state['chunk_size'])
    elapsed = entry.setdefault('upload_api_seconds', 0)
    if not entry.get('upload_id'):
        start = time.perf_counter()
        upload, _ = client.request('POST', '/uploads', {'name': name, 'parent_id': state['folder_id'],
                                  'size': len(content), 'mime_type': mime, 'sha256': entry['sha256']}, expect=201)
        elapsed += time.perf_counter() - start
        entry.update(upload_id=upload['id'], chunk_count=upload['total_chunks'])
        save_state(path, state)
    upload, _ = client.request('GET', f'/uploads/{entry["upload_id"]}')
    if upload['chunk_size'] != state['chunk_size'] or upload['size'] != len(content) or upload['sha256'] != entry['sha256']:
        raise QualificationError('Tracked upload metadata differs from fixture')
    received = set(upload['received_chunks'])
    for index in range(upload['total_chunks']):
        if stage and index > 0:
            break
        if index in received:
            continue
        part = content[index * upload['chunk_size']:(index + 1) * upload['chunk_size']]
        start = time.perf_counter()
        client.request('PUT', f'/uploads/{entry["upload_id"]}/chunks/{index}', part,
                       {'X-Chunk-SHA256': digest(part)})
        elapsed += time.perf_counter() - start
        entry['upload_api_seconds'] = elapsed
        save_state(path, state)
    if stage:
        client.request('POST', f'/uploads/{entry["upload_id"]}/complete', {}, expect=409)
        client.request('GET', f'/nodes/{entry["upload_id"]}', expect=404)
        entry['staged_received_chunks'] = [0]
        state['incomplete_publish_rejected'] = True
        save_state(path, state)
        return
    start = time.perf_counter()
    node, _ = client.request('POST', f'/uploads/{entry["upload_id"]}/complete', {})
    finalize_seconds = time.perf_counter() - start
    elapsed += finalize_seconds
    if node['id'] != entry['upload_id'] or node['sha256'] != entry['sha256']:
        raise QualificationError('Completed fixture metadata mismatch')
    entry.update(completed=True, upload_api_seconds=elapsed, finalize_seconds=finalize_seconds,
                 upload_mib_per_second=len(content) / (1 << 20) / elapsed)
    entry['download'] = verify_download(client, state, name, entry)
    save_state(path, state)


def run(args):
    path = args.state_file.resolve()
    if not path.is_relative_to(OUTPUT.resolve()):
        raise QualificationError('State file must be inside project output/ directory')
    if args.phase == 'upload' and path.exists():
        raise QualificationError('State file already exists; use resume or choose a fresh output state file')
    client = Client(args.base_url, args.password_file)
    client.request('GET', '/diagnostics', expect=401, anonymous=True)
    diagnostics, _ = client.request('GET', '/diagnostics')
    adapter = 'local' if args.local_check else 'telegram'
    if diagnostics.get('adapter') != adapter:
        raise QualificationError(f'Qualification requires {adapter} adapter')
    chunk_size = diagnostics.get('chunk_size', 0)
    if not 1 <= chunk_size <= 8 << 20:
        raise QualificationError('Chunk size exceeds controlled qualification workload limit (8 MiB)')
    sampler = ProcessSamples(args.server_pid) if args.server_pid else None
    if sampler:
        sampler.thread.start()
    phase_started = time.perf_counter()
    try:
        if args.phase == 'upload':
            run_id = secrets.token_hex(6)
            folder_name = 'Harbor qualification ' + run_id
            folder, _ = client.request('POST', '/folders', {'name': folder_name, 'parent_id': ''}, expect=201)
            state = {'version': 1, 'run_id': run_id, 'folder_id': folder['id'], 'folder_name': folder_name,
                     'adapter': adapter, 'chunk_size': chunk_size, 'transfer_concurrency': 1, 'files': {}, 'phases': {},
                     'connection_at_start': connection_snapshot(diagnostics),
                     'live_telegram_qualification': 'PARTIAL' if args.live else 'NOT TESTED'}
            for name in ('small.txt', 'small.png', 'small.pdf', 'multipart.bin'):
                content, mime = fixture(name, chunk_size)
                state['files'][name] = {'size': len(content), 'sha256': digest(content), 'mime_type': mime}
            save_state(path, state)
            for name in ('small.txt', 'small.png', 'small.pdf'):
                finish_upload(client, state, name, path)
            finish_upload(client, state, 'multipart.bin', path, stage=True)
        else:
            state = load_state(path)
            if state['adapter'] != adapter or state['chunk_size'] != chunk_size:
                raise QualificationError('Adapter/chunk configuration changed from qualification state')
            folder, _ = client.request('GET', '/nodes/' + state['folder_id'])
            if folder['name'] != 'Harbor qualification ' + state['run_id'] or folder['kind'] != 'folder' or folder['parent_id']:
                raise QualificationError('Tracked qualification folder changed')
            if args.phase == 'resume':
                entry = state['files']['multipart.bin']
                upload, _ = client.request('GET', f'/uploads/{entry["upload_id"]}')
                if not entry.get('completed'):
                    if upload['status'] != 'uploading' or upload['received_chunks'] != [0] or upload['received_bytes'] != chunk_size:
                        raise QualificationError('Interrupted upload did not retain exactly its staged first chunk')
                    client.request('GET', f'/nodes/{entry["upload_id"]}', expect=404)
                    state['resume_state_preserved'] = True
                for name in state['files']:
                    if not state['files'][name].get('completed'):
                        finish_upload(client, state, name, path)
                multipart = state['files']['multipart.bin']
                expected, _ = fixture('multipart.bin', chunk_size)
                start, end = chunk_size - 13, chunk_size + 29
                ranged, _ = client.request('GET', f'/nodes/{multipart["upload_id"]}/content', headers={'Range': f'bytes={start}-{end}'}, expect=206, raw=True)
                if ranged != expected[start:end + 1]:
                    raise QualificationError('Cross-chunk Range mismatch')
                state['cross_chunk_range_equal'] = True
            else:
                if not state.get('resume_state_preserved') or not all(entry.get('completed') for entry in state['files'].values()):
                    raise QualificationError('Complete resume phase before restart-check')
                for name, entry in state['files'].items():
                    entry['restart_download'] = verify_download(client, state, name, entry)
                state['restart_retrieval_verified'] = True
        diagnostics, _ = client.request('GET', '/diagnostics')
        state['connection_at_end'] = connection_snapshot(diagnostics)
        state['phases'][args.phase] = {'status': 'PASS', 'elapsed_seconds': time.perf_counter() - phase_started,
                                     'process': sampler.result() if sampler else {'status': 'NOT TESTED', 'reason': '--server-pid omitted'}}
        if args.phase == 'restart-check' and args.live:
            state['live_telegram_qualification'] = 'PARTIAL: HTTP storage/restart passed; browser qualification recorded separately'
        save_state(path, state)
        print(f'PASS {args.phase}: {adapter} adapter; generated fixtures only; state saved under output/')
        if args.phase == 'upload':
            print('Restart Harbor, then run resume with same state file.')
    finally:
        if sampler:
            sampler.result()
        try:
            client.request('POST', '/auth/logout', {}, expect=204)
        except (OSError, urllib.error.URLError, QualificationError):
            pass


def parser():
    result = argparse.ArgumentParser(description=__doc__)
    result.add_argument('--base-url', type=validate_url, required=True)
    result.add_argument('--password-file', type=Path, required=True)
    result.add_argument('--state-file', type=Path, default=OUTPUT / 'telegram-qualification.json')
    result.add_argument('--phase', choices=('upload', 'resume', 'restart-check'), required=True)
    result.add_argument('--server-pid', type=int)
    mode = result.add_mutually_exclusive_group(required=True)
    mode.add_argument('--live', action='store_true', help='Allow controlled generated-fixture writes to verified Telegram target')
    mode.add_argument('--local-check', action='store_true', help='Test runner against disposable local adapter; does not qualify Telegram')
    return result


if __name__ == '__main__':
    try:
        run(parser().parse_args())
    except (QualificationError, OSError, ValueError, KeyError, urllib.error.URLError) as error:
        # Never emit response bodies, credentials or connection exception details.
        print('FAIL qualification: ' + (str(error) if isinstance(error, QualificationError) else type(error).__name__))
        raise SystemExit(1)
