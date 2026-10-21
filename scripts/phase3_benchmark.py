#!/usr/bin/env python3
"""Bounded deterministic HTTP benchmark. Generated fixtures only; never deletes data."""
import argparse
import hashlib
import json
import time
from pathlib import Path

import telegram_qualification as q

MIB = 1 << 20
BLOCK = hashlib.shake_256(b'Harbor Phase 3 generated benchmark v1').digest(MIB)


def content(offset, size):
    start = offset % len(BLOCK)
    return (BLOCK * ((start + size + len(BLOCK) - 1) // len(BLOCK)))[start:start + size]


def checksum(size):
    h = hashlib.sha256()
    for offset in range(0, size, MIB):
        h.update(content(offset, min(MIB, size - offset)))
    return h.hexdigest()


def load_state(path, mode):
    state = json.loads(path.read_text())
    if (not isinstance(state, dict) or state.get('mode') != mode or state.get('status') != 'PASS'
            or not isinstance(state.get('folder_id'), str) or not q.ID.fullmatch(state['folder_id'])
            or not isinstance(state.get('files'), list) or not state['files']
            or not isinstance(state.get('rechecks', []), list)):
        raise q.QualificationError('Invalid benchmark state')
    ids = set()
    for entry in state['files']:
        if not isinstance(entry, dict):
            raise q.QualificationError('Invalid benchmark file')
        identity, size, chunk_size = entry.get('id'), entry.get('size'), entry.get('chunk_size')
        if (not isinstance(identity, str) or not q.ID.fullmatch(identity) or identity in ids
                or type(size) is not int or not MIB <= size <= 512 * MIB or size % MIB
                or type(chunk_size) is not int or not 1 <= chunk_size <= 8 * MIB
                or entry.get('completed') is not True):
            raise q.QualificationError('Invalid benchmark file metadata')
        if (not isinstance(entry.get('chunk_seconds'), list)
                or len(entry['chunk_seconds']) != (size + chunk_size - 1) // chunk_size
                or entry.get('sha256') != checksum(size)):
            raise q.QualificationError('Benchmark manifest differs from generated fixture')
        ids.add(identity)
    return state


def download(client, entry, interrupt=False):
    import urllib.request
    request = urllib.request.Request(client.base_url + '/api/v1/nodes/' + entry['id'] + '/content?download=1')
    started = time.perf_counter()
    h, count = hashlib.sha256(), 0
    with client.client.open(request, timeout=180) as response:
        if response.status != 200 or response.headers['X-Content-SHA256'] != entry['sha256']:
            raise q.QualificationError('Download metadata mismatch')
        while block := response.read(256 << 10):
            if block != content(count, len(block)):
                raise q.QualificationError('Download bytes mismatch')
            count += len(block)
            h.update(block)
            if interrupt:
                return {'interrupted_after_bytes': count}
    elapsed = time.perf_counter() - started
    if count != entry['size'] or h.hexdigest() != entry['sha256']:
        raise q.QualificationError('Download size/SHA-256 mismatch')
    return {'seconds': elapsed, 'mib_per_second': count / MIB / elapsed, 'sha256_equal': True}


def run(args):
    path = Path(args.state_file)
    if path.exists() and not args.recheck:
        raise q.QualificationError('State exists; use --recheck or a new path')
    mode = 'live' if args.live else 'local'
    state = load_state(path, mode) if args.recheck else {'files': [], 'mode': mode, 'phase': args.label}
    client = q.Client(args.base_url, Path(args.password_file))
    sampler = q.ProcessSamples(args.server_pid) if args.server_pid else None
    if sampler:
        sampler.thread.start()
    started = time.perf_counter()
    try:
        diagnostics, _ = client.request('GET', '/diagnostics')
        expected = 'telegram' if args.live else 'local'
        if diagnostics['adapter'] != expected:
            raise q.QualificationError('Adapter mismatch')
        observed = {'connection_before': q.connection_snapshot(diagnostics)} if args.recheck else state
        if not args.recheck:
            state['connection_before'] = q.connection_snapshot(diagnostics)
        if not args.recheck:
            folder, _ = client.request('POST', '/folders', {'name': 'Phase 3 ' + args.label + ' ' + str(time.time_ns()), 'parent_id': ''}, expect=201)
            state['folder_id'] = folder['id']
            q.save_state(path, state)
            for mib in args.sizes_mib:
                size = mib * MIB
                digest = checksum(size)
                begin = time.perf_counter()
                upload, _ = client.request('POST', '/uploads', {'name': f'generated-{mib}MiB.bin', 'parent_id': folder['id'], 'size': size, 'mime_type': 'application/octet-stream', 'sha256': digest}, expect=201)
                entry = {'id': upload['id'], 'size': size, 'sha256': digest, 'chunk_size': upload['chunk_size'], 'chunk_seconds': [], 'completed': False}
                state['files'].append(entry)
                q.save_state(path, state)
                for offset in range(0, size, upload['chunk_size']):
                    block = content(offset, min(upload['chunk_size'], size - offset))
                    chunk_started = time.perf_counter()
                    client.request('PUT', f'/uploads/{upload["id"]}/chunks/{offset // upload["chunk_size"]}', block, headers={'X-Chunk-SHA256': hashlib.sha256(block).hexdigest()})
                    entry['chunk_seconds'].append(time.perf_counter() - chunk_started)
                    q.save_state(path, state)
                finalize = time.perf_counter()
                node, _ = client.request('POST', '/uploads/' + upload['id'] + '/complete', {})
                entry.update({'completed': True, 'finalize_seconds': time.perf_counter() - finalize, 'upload_seconds': time.perf_counter() - begin})
                if node['sha256'] != digest or node['size'] != size:
                    raise q.QualificationError('Published metadata mismatch')
                entry['upload_mib_per_second'] = mib / entry['upload_seconds']
                entry['download'] = download(client, entry)
                q.save_state(path, state)
                print(f'PASS {args.label}: {mib} MiB upload {entry["upload_mib_per_second"]:.3f} MiB/s, download {entry["download"]["mib_per_second"]:.3f} MiB/s', flush=True)
        else:
            checks = []
            for entry in state['files']:
                checks.append({'id': entry['id'], 'download': download(client, entry)})
            observed.update(label=args.label, files=checks)
        diagnostics, _ = client.request('GET', '/diagnostics')
        observed['connection_after'] = q.connection_snapshot(diagnostics)
        observed['elapsed_seconds'] = time.perf_counter() - started
        observed['process'] = sampler.result() if sampler else {'status': 'NOT TESTED'}
        observed['status'] = 'PASS'
        if args.recheck:
            state.setdefault('rechecks', []).append(observed)
        q.save_state(path, state)
        print('PASS benchmark: exact streaming bytes/SHA-256; bounded client buffers; state retained', flush=True)
    finally:
        if sampler:
            sampler.result()
        client.request('POST', '/auth/logout', {}, expect=204)


def parser():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--base-url', type=q.validate_url, required=True)
    p.add_argument('--password-file', required=True)
    p.add_argument('--state-file', required=True)
    p.add_argument('--label', required=True)
    p.add_argument('--sizes-mib', type=int, nargs='+', default=[1, 10, 100])
    p.add_argument('--server-pid', type=int)
    p.add_argument('--recheck', action='store_true')
    modes = p.add_mutually_exclusive_group(required=True)
    modes.add_argument('--live', action='store_true')
    modes.add_argument('--local-check', action='store_true')
    return p


if __name__ == '__main__':
    args = parser().parse_args()
    if not args.sizes_mib or any(n < 1 or n > 512 for n in args.sizes_mib):
        raise SystemExit('FAIL sizes must be 1..512 MiB')
    try:
        run(args)
    except (q.QualificationError, OSError, ValueError, KeyError, TypeError):
        raise SystemExit('FAIL benchmark; retained state records last committed fixture stage') from None
