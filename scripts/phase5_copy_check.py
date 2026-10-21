#!/usr/bin/env python3
"""SIGKILL/restart copy qualification; fresh local fixtures only, no .env or Telegram."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import sqlite3
import subprocess
import tempfile
import time
import uuid

from acceptance import Drive

BLOCK = hashlib.shake_256(b'Harbor Phase 5 copy fixture').digest(1 << 20)
SIZE = 32 * len(BLOCK) + 17


def fixture_chunk(offset, size):
    start = offset % len(BLOCK)
    return (BLOCK * ((start + size + len(BLOCK) - 1) // len(BLOCK)))[start:start + size]


def fixture_hash(size=SIZE):
    digest = hashlib.sha256()
    for offset in range(0, size, len(BLOCK)):
        digest.update(fixture_chunk(offset, min(len(BLOCK), size - offset)))
    return digest.hexdigest()


def await_job(drive, identity, expected, timeout=60):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        state, _ = drive.request('GET', '/copy-jobs/' + identity)
        if state['status'] == expected:
            return state
        if state['status'] in ('failed', 'cancelled', 'complete'):
            raise AssertionError('Unexpected copy terminal state: ' + state['status'])
        time.sleep(.02)
    raise AssertionError('Copy job deadline exceeded')


def run(binary):
    with tempfile.TemporaryDirectory(prefix='harbor-copy-phase5-') as scratch:
        data = Path(scratch) / 'data'
        env = dict(os.environ, DRIVE_DATA_DIR=str(data), DRIVE_ENV_FILE='-', DRIVE_STORAGE='local')
        for key in ('DRIVE_OWNER_PASSWORD', 'DRIVE_OWNER_PASSWORD_FILE', 'DRIVE_KEY_FILE'):
            env.pop(key, None)
        subprocess.run([str(binary), 'init'], env=env, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        password = (data / 'owner-password').read_text().strip()
        drive = Drive(binary, data)
        try:
            drive.start()
            drive.request('POST', '/auth/login', {'password': password})
            folder, _ = drive.request('POST', '/folders', {'name': 'Phase 5 original', 'parent_id': ''}, expect=201)
            nested, _ = drive.request('POST', '/folders', {'name': 'Nested', 'parent_id': folder['id']}, expect=201)
            digest = fixture_hash()
            upload, _ = drive.request('POST', '/uploads', {'name': 'generated.bin', 'parent_id': nested['id'], 'size': SIZE,
                                                         'mime_type': 'application/octet-stream', 'sha256': digest}, expect=201)
            for index in range(upload['total_chunks']):
                offset = index * upload['chunk_size']
                drive.request('PUT', '/uploads/' + upload['id'] + '/chunks/' + str(index),
                              fixture_chunk(offset, min(upload['chunk_size'], SIZE - offset)))
            node, _ = drive.request('POST', '/uploads/' + upload['id'] + '/complete', {})
            operation = uuid.uuid4().hex
            spec = {'operation_id': operation, 'source_ids': [folder['id'], nested['id']], 'parent_id': ''}
            planned, _ = drive.request('POST', '/copy-jobs', spec, expect=201)
            repeated, _ = drive.request('POST', '/copy-jobs', spec)
            assert planned['id'] == repeated['id'] and planned['total_entries'] == 3
            interrupted = None
            deadline = time.monotonic() + 15
            with sqlite3.connect(data / 'drive.db', timeout=1) as db:
                while time.monotonic() < deadline:
                    state = db.execute('SELECT coalesce(sum(copied_bytes),0) FROM copy_entries WHERE job_id=?', (operation,)).fetchone()[0]
                    if 0 < state < SIZE:
                        drive.process.kill()
                        drive.process.wait(timeout=10)
                        interrupted = state
                        break
                    time.sleep(.001)
                assert interrupted is not None, 'Could not observe incomplete copy; no crash qualification claimed'
                targets = [row[0] for row in db.execute('SELECT target_id FROM copy_entries WHERE job_id=?', (operation,))]
                assert not any(db.execute('SELECT 1 FROM nodes WHERE id=?', (target,)).fetchone() for target in targets)
                committed = db.execute('SELECT count(*) FROM storage_chunks WHERE upload_id=(SELECT target_id FROM copy_entries WHERE job_id=? AND kind=\'file\')', (operation,)).fetchone()[0]
                assert committed > 0
            drive.start()
            completed = await_job(drive, operation, 'complete')
            assert completed['completed_bytes'] == SIZE and completed['completed_entries'] == 3
            root, _ = drive.request('GET', '/nodes?parent_id=' + completed['result_ids'][0])
            assert [entry['name'] for entry in root['items']] == ['Nested']
            copied, _ = drive.request('GET', '/nodes?parent_id=' + root['items'][0]['id'])
            copied_node = copied['items'][0]
            assert copied_node['sha256'] == digest
            downloaded, headers = drive.request('GET', '/nodes/' + copied_node['id'] + '/content', raw=True)
            assert len(downloaded) == SIZE and hashlib.sha256(downloaded).hexdigest() == digest
            assert headers['X-Content-SHA256'] == digest
            original, _ = drive.request('GET', '/nodes/' + node['id'])
            assert original['sha256'] == digest
            cancel_id = uuid.uuid4().hex
            drive.request('POST', '/copy-jobs', {'operation_id': cancel_id, 'source_ids': [folder['id']], 'parent_id': ''}, expect=201)
            drive.request('DELETE', '/copy-jobs/' + cancel_id, expect=204)
            cancelled = await_job(drive, cancel_id, 'cancelled')
            assert cancelled['result_ids'] == []
            with sqlite3.connect(data / 'drive.db') as db:
                assert db.execute('PRAGMA integrity_check').fetchone()[0] == 'ok'
                assert db.execute("SELECT count(*) FROM storage_uploads WHERE parent_id LIKE ?", ('copy:' + cancel_id + ':%',)).fetchone()[0] == 0
                chunk_count = db.execute('SELECT count(*) FROM storage_chunks WHERE upload_id=?', (copied_node['id'],)).fetchone()[0]
                assert chunk_count == upload['total_chunks']
            return {'status': 'PASS', 'adapter': 'local', 'process_sigkill': True, 'restart_resume': True,
                    'bytes_before_sigkill': interrupted, 'chunks_before_sigkill': committed,
                    'file_bytes': SIZE, 'sha256_equal': True, 'atomic_visibility': True, 'ancestor_selection_pruned': True,
                    'cancellation_cleanup': True, 'original_retained': True, 'copied_chunks': chunk_count}
        finally:
            drive.stop()


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', default='bin/drive')
    args = parser.parse_args()
    print(json.dumps(run(Path(args.binary).resolve()), indent=2))
