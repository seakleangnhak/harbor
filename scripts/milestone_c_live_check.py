#!/usr/bin/env python3
"""Bounded 15–30 minute exact-image Telegram qualification; generated fixtures only.

Stop original Harbor first. Coordinator alone supplies private configuration.
Original authorization remains locked until every clone stops and copied session
files are removed. SIGKILL of this harness requires manual clone cleanup before
original Harbor resumes. Fixture metadata/key remain in a protected new volume;
no Telegram messages are deleted. --self-check uses no credentials or Docker.
"""
import argparse
import fcntl
import hashlib
import io
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import signal
import subprocess
import tarfile
import tempfile
import threading
import time
import urllib.request

from phase5_live_recovery_check import config
import telegram_qualification as q


FIXTURE = bytes(range(256)) * (5 * 1024 * 1024 // 256)
MIB = 1 << 20


class TrafficBudget:
    # Includes worst-case whole-envelope range reads and final verification.
    # Protocol retransmissions are not individually observable through this API.
    def __init__(self):
        self.upload = self.download = 0
        self.lock = threading.Lock()

    def reserve(self, upload=0, download=0):
        with self.lock:
            if min(upload, download) < 0 or self.upload + upload > 1536 * MIB or self.download + download > 4096 * MIB:
                raise RuntimeError('Conservative live payload budget exhausted')
            self.upload += upload
            self.download += download



def network_bytes(value):
    match = re.fullmatch(r'([0-9.]+)\s*(B|kB|KB|MB|GB|TB|KiB|MiB|GiB)', value.strip())
    if match is None:
        raise ValueError('Unsupported Docker network metric')
    number, unit = match.groups()
    power = {'B': 1, 'kB': 1000, 'KB': 1000, 'MB': 1000**2, 'GB': 1000**3,
             'TB': 1000**4, 'KiB': 1024, 'MiB': 1024**2, 'GiB': 1024**3}[unit]
    return int(float(number) * power)


def check(condition, message):
    if not condition:
        raise RuntimeError(message)


def session_archive(content, name='telegram.session'):
    check(name in ('telegram.session', 'config.env'), 'Private archive member not allowlisted')
    stream = io.BytesIO()
    with tarfile.open(fileobj=stream, mode='w') as archive:
        member = tarfile.TarInfo(name)
        member.uid = member.gid = 65532
        member.mode, member.size = 0o600, len(content)
        archive.addfile(member, io.BytesIO(content))
    return stream.getvalue()


def run(args):
    check(900 <= args.duration_seconds <= 1800, 'Live interval must be 900–1800 seconds')
    for image in (args.image, args.helper_image, *([args.previous_image] if args.previous_image else [])):
        if not re.fullmatch(r'sha256:[0-9a-f]{64}', image):
            raise ValueError('Use immutable local runtime and helper image IDs')
    root = Path(__file__).resolve().parents[1]
    output = args.output.resolve()
    if not output.is_relative_to(root / 'output') or output.exists():
        raise ValueError('Use fresh ignored output directory')
    os.umask(0o077)
    os.environ['no_proxy'] = '127.0.0.1,localhost,::1'
    output.mkdir(parents=True, mode=0o700)
    settings = config(args.source_env)
    source_root = args.source_env.resolve().parent
    source = Path(settings.get('DRIVE_DATA_DIR', 'data'))
    source = source if source.is_absolute() else source_root / source
    session = Path(settings.get('TELEGRAM_SESSION_FILE', str(source / 'telegram.session')))
    session = session if session.is_absolute() else source_root / session
    if session.is_symlink() or not session.is_file() or session.stat().st_mode & 0o077:
        raise ValueError('Private regular persisted authorization required')
    prefix = 'harbor-c-live-' + secrets.token_hex(6)
    volume, container = prefix + '-data', prefix
    archive_volume, restore_volume = prefix + '-archive', prefix + '-restore'
    archive_holder = prefix + '-archive-holder'
    known_containers = set()

    def docker(*parts, data=None, timeout=120):
        if parts[0] in ('run', 'create'):
            if '--name' not in parts:
                parts = (parts[0], '--name', prefix + '-task-' + secrets.token_hex(4), *parts[1:])
            proposed_name = parts[parts.index('--name') + 1]
            check(not docker('ps', '-aq', '--filter', 'name=^/' + proposed_name + '$').strip(),
                  'Generated container name already exists; refusing reuse')
            known_containers.add(proposed_name)
        result = subprocess.run(['docker', '--context', args.context, *parts], input=data,
                                capture_output=True, timeout=timeout)
        if result.returncode:
            raise RuntimeError('Docker operation failed: ' + parts[0] + '; private output withheld')
        return result.stdout

    def state():
        return json.loads(docker('inspect', '--format', '{{json .State}}', container))

    def sample():
        row = json.loads(docker('stats', '--no-stream', '--format', '{{json .}}', container, timeout=15))
        return {name: row[name] for name in ('MemUsage', 'CPUPerc', 'NetIO', 'BlockIO', 'PIDs')}

    report = {'result': 'FAIL', 'image': args.image, 'helper_image': args.helper_image,
              'fixture_volume': volume, 'fixture_bytes': len(FIXTURE), 'fixture_sha256': q.digest(FIXTURE),
              'memory_limit_bytes': 768 << 20, 'go_memory_limit': '512MiB', 'samples': [],
              'resource_method': 'sampled docker stats; not lifetime RSS peak',
              'requested_interval_seconds': args.duration_seconds, 'cycles': [],
              'paced_cycle_seconds': max(180, args.duration_seconds / 15),
              'budget_method': 'conservative application encrypted-payload reservations, not MTProto RPC/wire counters',
              'upload_budget_bytes': 1536 * MIB, 'download_budget_bytes': 4096 * MIB,
              'original_session_unchanged': False, 'copied_authorization_removed': False}
    budget = TrafficBudget()
    stopped = threading.Event()
    sampler = None
    network_previous = [0, 0]
    network_totals = [0, 0]

    def collect():
        while not stopped.is_set():
            try:
                row = sample()
                row['elapsed_seconds'] = time.monotonic() - interval_started
                report['samples'].append(row)
                counts = [network_bytes(value) for value in row['NetIO'].split('/')]
                for index in range(2):
                    network_totals[index] += max(0, counts[index] - network_previous[index]) if counts[index] >= network_previous[index] else counts[index]
                    network_previous[index] = counts[index]
                # Includes host HTTP traffic, making these stricter than remote-only caps.
                if network_totals[0] > 4608 * MIB or network_totals[1] > 1792 * MIB:
                    report['network_safety_stop'] = True
                    docker('stop', '--time=20', container)
                    return
            except (RuntimeError, OSError, ValueError, subprocess.SubprocessError):
                pass
            stopped.wait(.25)

    check(not docker('volume', 'ls', '-q', '--filter', 'name=^' + prefix).strip(),
          'Generated volume prefix already exists; refusing reuse')
    check(not docker('ps', '-aq', '--filter', 'name=^/' + prefix).strip(),
          'Generated container prefix already exists; refusing reuse')

    # No source database/key/password is copied. Keep original lock through cleanup.
    with (source / '.lock').open('r+b') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        original = hashlib.sha256(session.read_bytes()).digest()
        temporary = None
        created_volumes = []
        try:
            for created_volume in (volume, archive_volume, restore_volume):
                docker('volume', 'create', created_volume)
                created_volumes.append(created_volume)
            for created_volume in (volume, archive_volume, restore_volume):
                docker('run', '--rm', '--network=none', '--read-only', '--user=0', '--cap-drop=ALL',
                       '--cap-add=CHOWN', '--security-opt=no-new-privileges', '--memory=128m', '--cpus=.5', '--pids-limit=32',
                       '--volume', created_volume + ':/live', '--entrypoint=sh', args.helper_image,
                       '-ec', 'chmod 0700 /live; chown 65532:65532 /live')
            with tempfile.TemporaryDirectory(prefix='private-', dir=output) as temporary:
                runtime = {name: settings[name] for name in ('TELEGRAM_APP_ID', 'TELEGRAM_APP_HASH',
                           'TELEGRAM_CHANNEL_ID', 'TELEGRAM_CHANNEL_ACCESS_HASH', 'USERNAME_OR_CHANNEL_LINK') if settings.get(name)}
                runtime.update(DRIVE_STORAGE='telegram', DRIVE_ENV_FILE='-', DRIVE_DATA_DIR='/live',
                               DRIVE_KEY_FILE='/live/content.key', DRIVE_OWNER_PASSWORD_FILE='/live/owner-password',
                               TELEGRAM_SESSION_FILE='/live/telegram.session', DRIVE_ADDR='0.0.0.0:8080',
                               DRIVE_ALLOW_REMOTE_HTTP='true', DRIVE_SECURE_COOKIES='false',
                               GOMEMLIMIT='512MiB', GOMAXPROCS='2', HOME='/live', TMPDIR='/tmp')
                if any('\n' in value or '\r' in value for value in runtime.values()):
                    raise ValueError('Runtime environment contains line breaks')
                env_file = Path(temporary) / 'runtime.env'
                env_file.write_text(''.join(name + '=' + value + '\n' for name, value in runtime.items()))
                env_file.chmod(0o600)
                sandbox = ['--read-only', '--user=65532:65532', '--cap-drop=ALL',
                           '--security-opt=no-new-privileges', '--memory=768m', '--memory-swap=768m',
                           '--cpus=2', '--pids-limit=128', '--tmpfs=/tmp:rw,nosuid,nodev,mode=1777,size=128m',
                           '--volume', volume + ':/live', '--volume', restore_volume + ':/restore', '--env-file', str(env_file)]
                current_image = args.previous_image or args.image
                docker('run', '--rm', '--network=none', *sandbox, current_image, 'init')

                def create():
                    options = list(sandbox)
                    if runtime['DRIVE_DATA_DIR'].startswith('/restore/'):
                        index = options.index(volume + ':/live')
                        check(index > 0 and options[index - 1] == '--volume', 'Generated live mount is not an explicit volume')
                        del options[index - 1:index + 1]
                    docker('create', '--name', container, '--publish=127.0.0.1::8080', *options, current_image)
                    if runtime['DRIVE_DATA_DIR'].startswith('/restore/'):
                        mounts = json.loads(docker('inspect', '--format', '{{json .Mounts}}', container))
                        check(all(mount['Destination'] != '/live' and mount.get('Name') != volume for mount in mounts),
                              'Restored application still exposes generated source volume')
                        check(any(mount['Destination'] == '/restore' and mount.get('Name') == restore_volume for mount in mounts),
                              'Restored application lacks archive restore volume')
                        report.setdefault('archive_only_mount_proofs', []).append({
                            'data_dir': runtime['DRIVE_DATA_DIR'], 'original_live_mount_absent': True,
                            'original_volume_absent': True, 'restore_mount_present': True})

                create()
                docker('cp', '-a', '-', container + ':/live', data=session_archive(session.read_bytes()))
                # Avoid host UID/mode dependence: copy private config with explicit container ownership.
                docker('create', '--name', archive_holder, '--network=none', '--read-only', '--user=65532:65532',
                       '--cap-drop=ALL', '--security-opt=no-new-privileges', '--memory=128m', '--cpus=.5',
                       '--pids-limit=32', '--volume', archive_volume + ':/archive', '--entrypoint=sh', args.helper_image,
                       '-c', 'true')
                docker('cp', '-a', '-', archive_holder + ':/archive', data=session_archive(env_file.read_bytes(), 'config.env'))
                docker('rm', archive_holder)
                password_tar = docker('cp', container + ':/live/owner-password', '-')
                with tarfile.open(fileobj=io.BytesIO(password_tar)) as archive:
                    member = archive.next()
                    check(member is not None and member.isfile() and member.size <= 128,
                          'Generated owner password export invalid')
                    password = Path(temporary) / 'owner-password'
                    password.write_bytes(archive.extractfile(member).read())
                    password.chmod(0o600)

                def start():
                    began = time.monotonic()
                    docker('start', container)
                    ports = json.loads(docker('inspect', '--format', '{{json .NetworkSettings.Ports}}', container))
                    binding = ports['8080/tcp']
                    check(len(binding) == 1 and binding[0]['HostIp'] == '127.0.0.1', 'HTTP port is not loopback-only')
                    url = q.validate_url('http://127.0.0.1:' + binding[0]['HostPort'])
                    opener = urllib.request.build_opener(q.NoRedirect())
                    while time.monotonic() - began < 120:
                        check(state()['Running'], 'Isolated application startup failed; private logs withheld')
                        try:
                            with opener.open(url + '/ready', timeout=2) as response:
                                if response.status == 200:
                                    report.setdefault('startup_seconds', []).append(time.monotonic() - began)
                                    return q.Client(url, password)
                        except OSError:
                            time.sleep(.2)
                    raise TimeoutError('Isolated readiness deadline exceeded')

                def archive(operation, name, image, destination=None):
                    options = ['--data-dir', '/live', '--output', '/archive/' + name, '--config-file', '/archive/config.env'] if operation == 'backup' else ['--archive', '/archive/' + name]
                    if destination:
                        options += ['--destination', destination]
                    docker('run', '--rm', '--network=none', *sandbox,
                        '--volume', archive_volume + ':/archive',
                        '--entrypoint=/app/harbor-recovery', image, operation, *options,
                        '--password-file', '/archive/password', timeout=180)

                docker('run', '--rm', '--network=none', '--read-only', '--user=65532:65532',
                    '--cap-drop=ALL', '--security-opt=no-new-privileges', '--memory=128m', '--cpus=.5', '--pids-limit=32',
                    '--volume', archive_volume + ':/archive', '--entrypoint=sh', args.helper_image,
                    '-ec', 'umask 077; head -c 32 /dev/urandom > /archive/password')
                client = start()
                interval_started = time.monotonic()
                def connection_guard(current):
                    diagnostics, _ = current.request('GET', '/diagnostics')
                    connection = diagnostics.get('storage_connection', {})
                    check(connection.get('state') == 'connected' and connection.get('retry_count', 0) == 0,
                          'Transport retry/disconnection observed; stop bounded live traffic')
                    return diagnostics

                def legacy_seed():
                    data = bytes(range(256)) * 4096
                    budget.reserve(upload=len(data) + 32, download=3 * (len(data) + 32))
                    item, _ = client.request('POST', '/uploads', {'name': prefix + '-old.bin', 'parent_id': '',
                        'size': len(data), 'mime_type': 'application/octet-stream', 'sha256': q.digest(data)}, expect=201)
                    client.request('PUT', f'/uploads/{item["id"]}/chunks/0', data, {'X-Chunk-SHA256': q.digest(data)})
                    client.request('POST', f'/uploads/{item["id"]}/complete', {})
                    return item['id'], data

                if args.previous_image:
                    old_diagnostics = connection_guard(client)
                    check(old_diagnostics['version'] == '1.2.1', 'Previous image must identify as 1.2.1')
                    seed_id, seed_data = legacy_seed()
                    docker('stop', '--time=30', container)
                    check(state()['ExitCode'] == 0 and not state()['OOMKilled'], 'Old-image graceful stop failed')
                    archive('backup', 'pre-upgrade.harbor', current_image)
                    archive('verify', 'pre-upgrade.harbor', current_image)
                    report['pre_upgrade_encrypted_backup_verified'] = True
                    docker('rm', container)
                    current_image = args.image
                    create()
                    client = start()
                    seed_body, _ = client.request('GET', f'/nodes/{seed_id}/content', raw=True, limit=len(seed_data))
                    check(seed_body == seed_data, 'Live upgrade changed old-file content')
                    report['live_v1_2_1_upgrade'] = {'sha256_equal': True, 'session_reconnected': True,
                                                  'previous_image': args.previous_image}
                interval_started = time.monotonic()
                connection_guard(client)
                client.request('GET', '/diagnostics', expect=401, anonymous=True)
                diagnostics, _ = client.request('GET', '/diagnostics')
                check(diagnostics['adapter'] == 'telegram' and diagnostics['version'] == args.version,
                      'Expected current development Telegram image')
                report['version'] = diagnostics['version']
                report['idle'] = sample()
                sampler = threading.Thread(target=collect, daemon=True)
                sampler.start()
                budget.reserve(upload=2 * (len(FIXTURE) + 32), download=5 * (len(FIXTURE) + 32))
                upload, _ = client.request('POST', '/uploads', {'name': prefix + '.bin', 'parent_id': '',
                    'size': len(FIXTURE), 'mime_type': 'application/octet-stream', 'sha256': q.digest(FIXTURE)}, expect=201)
                upload_id = upload['id']
                check(q.ID.fullmatch(upload_id) and upload['part_size'] == 4 << 20 and upload['total_chunks'] == 1,
                      'Unexpected generated upload geometry')
                report['fixture_upload_id'] = upload_id
                began = time.monotonic()
                for index in range(2):
                    part = FIXTURE[index * (4 << 20):(index + 1) * (4 << 20)]
                    upload, _ = client.request('PUT', f'/uploads/{upload_id}/chunks/0/parts/{index}', part,
                                              {'X-Part-SHA256': q.digest(part)})
                check(upload['received_bytes'] == 0 and upload['pending_chunks'][0]['bytes'] == len(FIXTURE),
                      'Browser acceptance incorrectly claimed remote storage')
                report['browser_accept_seconds'] = time.monotonic() - began
                client.request('POST', f'/uploads/{upload_id}/complete', {}, expect=409)
                client.request('POST', f'/uploads/{upload_id}/chunks/0/commit', {'background': True}, expect=202)
                deadline = time.monotonic() + 10
                while time.monotonic() < deadline:
                    upload, _ = client.request('GET', f'/uploads/{upload_id}')
                    if any(item['state'] == 'storing' for item in upload.get('pending_chunks', [])):
                        break
                    check(upload['received_bytes'] == 0, 'Remote save finished before crash could be observed')
                    time.sleep(.02)
                else:
                    raise RuntimeError('Active storing state not observed; crash test not qualified')
                # This observes server storing state; it does not identify an MTProto RPC byte offset.
                docker('kill', '--signal=KILL', container)
                docker('wait', container)
                killed = state()
                check(killed['ExitCode'] == 137 and not killed['OOMKilled'], 'Controlled process kill not confirmed')
                report['sigkill_during_observed_storing'] = True
                client = start()
                recovery_started = time.monotonic()
                upload, _ = client.request('GET', f'/uploads/{upload_id}')
                check(upload['status'] == 'uploading' and
                      (upload['received_bytes'] == len(FIXTURE) or
                       any(item['bytes'] == len(FIXTURE) for item in upload.get('pending_chunks', []))),
                      'Crash lost acknowledged browser parts')
                client.request('GET', '/nodes/' + upload_id, expect=404)
                deadline = time.monotonic() + 180
                while time.monotonic() < deadline:
                    upload, _ = client.request('GET', f'/uploads/{upload_id}')
                    if upload['received_bytes'] == len(FIXTURE):
                        break
                    check(not any(item['state'] == 'failed' for item in upload.get('pending_chunks', [])),
                          'Interrupted remote upload requires manual retry')
                    time.sleep(.1)
                else:
                    raise TimeoutError('Automatic remote-save recovery deadline exceeded')
                node, _ = client.request('POST', f'/uploads/{upload_id}/complete', {})
                check(node['id'] == upload_id and node['sha256'] == q.digest(FIXTURE), 'Published manifest mismatch')
                report['automatic_recovery'] = True
                report['recovery_and_verification_seconds'] = time.monotonic() - recovery_started

                def download(label):
                    began = time.monotonic()
                    content, headers = client.request('GET', f'/nodes/{upload_id}/content?download=1', raw=True,
                                                      limit=len(FIXTURE))
                    check(content == FIXTURE and headers['X-Content-SHA256'] == q.digest(FIXTURE),
                          'Downloaded fixture checksum mismatch')
                    elapsed = time.monotonic() - began
                    report[label] = {'sha256_equal': True, 'bytes': len(content), 'seconds': elapsed,
                                     'mib_per_second': 5 / elapsed}
                    begin, end = (4 << 20) - 17, (4 << 20) + 31
                    content, headers = client.request('GET', f'/nodes/{upload_id}/content',
                        headers={'Range': f'bytes={begin}-{end}'}, expect=206, raw=True)
                    check(content == FIXTURE[begin:end + 1] and
                          headers['Content-Range'] == f'bytes {begin}-{end}/{len(FIXTURE)}', 'HTTP Range mismatch')
                    report[label]['http_206_range'] = True

                download('after_sigkill_download')
                docker('stop', '--time=30', container)
                check(state()['ExitCode'] == 0 and not state()['OOMKilled'], 'Clean shutdown failed')
                docker('rm', container)
                create()
                client = start()
                download('after_recreation_download')
                def background_upload(current, name, data):
                    envelope_count = (len(data) + (100 * MIB) - 1) // (100 * MIB)
                    budget.reserve(upload=len(data) + 32 * envelope_count,
                                   download=len(data) + 32 * envelope_count)
                    item, _ = current.request('POST', '/uploads', {'name': prefix + name, 'parent_id': '',
                        'size': len(data), 'mime_type': 'application/octet-stream', 'sha256': q.digest(data)}, expect=201)
                    check(item['chunk_size'] == 100 * MIB and item['part_size'] == 4 * MIB,
                          'Unexpected multipart geometry')
                    for index in range(item['total_chunks']):
                        chunk = data[index * item['chunk_size']:(index + 1) * item['chunk_size']]
                        for part in range((len(chunk) + item['part_size'] - 1) // item['part_size']):
                            piece = chunk[part * item['part_size']:(part + 1) * item['part_size']]
                            current.request('PUT', f'/uploads/{item["id"]}/chunks/{index}/parts/{part}', piece,
                                            {'X-Part-SHA256': q.digest(piece)})
                        current.request('POST', f'/uploads/{item["id"]}/chunks/{index}/commit',
                                        {'background': True}, expect=202)
                    deadline = time.monotonic() + 240
                    while time.monotonic() < deadline:
                        current_state, _ = current.request('GET', f'/uploads/{item["id"]}')
                        if current_state['received_bytes'] == len(data):
                            node, _ = current.request('POST', f'/uploads/{item["id"]}/complete', {})
                            check(node['sha256'] == q.digest(data), 'Final multipart digest mismatch')
                            return node['id']
                        check(not any(p['state'] == 'failed' for p in current_state.get('pending_chunks', [])),
                              'Background remote save failed; no automatic retry storm')
                        current.request('GET', f'/nodes/{item["id"]}', expect=404)
                        time.sleep(.5)
                    raise TimeoutError('Background completion deadline exceeded')

                multipart = (bytes(range(256)) * ((100 * MIB + 17 + 255) // 256))[:100 * MIB + 17]
                began = time.monotonic()
                multipart_id = background_upload(client, '-multipart.bin', multipart)
                report['multipart'] = {'bytes': len(multipart), 'chunks': 2,
                                       'upload_and_verify_seconds': time.monotonic() - began}
                budget.reserve(download=len(multipart) + 64)
                began = time.monotonic()
                full, headers = client.request('GET', f'/nodes/{multipart_id}/content', raw=True, limit=len(multipart))
                check(q.digest(full) == q.digest(multipart) == headers['X-Content-SHA256'], 'Full multipart SHA mismatch')
                report['multipart']['download_seconds'] = time.monotonic() - began
                report['multipart']['sha256_equal'] = True
                del full

                # Three separate authenticated clients share one bounded saving worker.
                from concurrent.futures import ThreadPoolExecutor
                small = bytes(range(256)) * 4096
                def concurrent_small(index):
                    separate = q.Client(client.base_url, password)
                    identifier = background_upload(separate, f'-parallel-{index}.bin', small)
                    separate.request('POST', '/auth/logout', {}, expect=204)
                    return identifier
                # Budget reservation is synchronized before each upload request.
                with ThreadPoolExecutor(max_workers=3) as pool:
                    parallel_ids = list(pool.map(concurrent_small, range(3)))
                report['concurrent_small_uploads'] = len(parallel_ids)
                for identifier in parallel_ids:
                    budget.reserve(download=len(small) + 32)
                    received, _ = client.request('GET', f'/nodes/{identifier}/content', raw=True, limit=len(small))
                    check(received == small, 'Concurrent fixture SHA mismatch')

                cycle = 0
                while time.monotonic() - interval_started < args.duration_seconds:
                    began = time.monotonic()
                    connection_guard(client)
                    identifier = background_upload(client, f'-paced-{cycle}.bin', small)
                    budget.reserve(download=len(small) + 32)
                    received, _ = client.request('GET', f'/nodes/{identifier}/content', raw=True, limit=len(small))
                    check(received == small, 'Paced fixture SHA mismatch')
                    start_byte = (cycle * 131071) % (99 * MIB)
                    # Conservatively reserve a full envelope even for a warm tiny range.
                    budget.reserve(download=100 * MIB + 32)
                    cold_began = time.monotonic()
                    partial, headers = client.request('GET', f'/nodes/{multipart_id}/content', raw=True,
                        headers={'Range': f'bytes={start_byte}-{start_byte + 65535}'}, expect=206)
                    check(partial == multipart[start_byte:start_byte + 65536], 'Paced range mismatch')
                    first_seconds = time.monotonic() - cold_began
                    budget.reserve(download=100 * MIB + 32)
                    warm_began = time.monotonic()
                    partial, _ = client.request('GET', f'/nodes/{multipart_id}/content', raw=True,
                        headers={'Range': f'bytes={start_byte}-{start_byte + 65535}'}, expect=206)
                    check(partial == multipart[start_byte:start_byte + 65536], 'Repeated range mismatch')
                    row = {'cycle': cycle, 'elapsed_seconds': time.monotonic() - interval_started,
                           'work_seconds': time.monotonic() - began, 'first_range_seconds': first_seconds,
                           'repeat_range_seconds': time.monotonic() - warm_began, 'sha256_equal': True}
                    report['cycles'].append(row)
                    (output / 'progress.json').write_text(json.dumps({'elapsed_seconds': row['elapsed_seconds'],
                        'cycles': len(report['cycles']), 'conservative_upload_bytes': budget.upload,
                        'conservative_download_bytes': budget.download}) + '\n')
                    print('Live interval: ' + str(round(row['elapsed_seconds'])) + ' seconds, ' + str(cycle + 1) + ' cycles', flush=True)
                    cycle += 1
                    remaining = args.duration_seconds - (time.monotonic() - interval_started)
                    if remaining > 0:
                        stopped.wait(min(max(0, report['paced_cycle_seconds'] - row['work_seconds']), remaining))
                connection_guard(client)
                report['actual_interval_seconds'] = time.monotonic() - interval_started
                report['duty_cycle_work_seconds'] = sum(row['work_seconds'] for row in report['cycles'])
                report['reserved_upload_bytes'] = budget.upload
                report['reserved_download_bytes'] = budget.download
                report['budget_caveat'] = 'Payload bounds include whole-envelope ranges and verification; native retransmission bytes uninstrumented'
                client.request('POST', '/auth/logout', {}, expect=204)
                docker('stop', '--time=30', container)
                check(state()['ExitCode'] == 0 and not state()['OOMKilled'], 'Final clean shutdown failed')
                archive('backup', 'post-upgrade.harbor', args.image)
                archive('verify', 'post-upgrade.harbor', args.image)
                archive('restore', 'post-upgrade.harbor', args.image, '/restore/candidate')
                docker('rm', container)
                def restored_runtime(path):
                    # The archive supplies app/channel configuration; original input is no longer used.
                    for key in list(runtime):
                        if key.startswith('TELEGRAM_') or key == 'USERNAME_OR_CHANNEL_LINK':
                            del runtime[key]
                    runtime.update(DRIVE_ENV_FILE=path + '/recovery.env', DRIVE_DATA_DIR=path,
                        DRIVE_KEY_FILE=path + '/content.key', DRIVE_OWNER_PASSWORD_FILE=path + '/owner-password',
                        TELEGRAM_SESSION_FILE=path + '/telegram.session')
                    env_file.write_text(''.join(name + '=' + value + '\n' for name, value in runtime.items()))
                restored_runtime('/restore/candidate')
                create()
                client = start()
                connection_guard(client)
                budget.reserve(download=len(multipart) + 64)
                restored_body, restored_headers = client.request('GET', f'/nodes/{multipart_id}/content',
                    raw=True, limit=len(multipart))
                check(q.digest(restored_body) == q.digest(multipart) == restored_headers['X-Content-SHA256'],
                      'Fresh restored Telegram multipart checksum mismatch')
                del restored_body
                docker('stop', '--time=30', container)
                check(state()['ExitCode'] == 0 and not state()['OOMKilled'], 'Fresh restore clean stop failed')
                report['encrypted_fresh_restore'] = {'sha256_equal': True, 'session_reconnected': True,
                    'source_configuration_used_after_restore': False, 'restored_file_bytes': len(multipart)}
                if args.previous_image:
                    archive('restore', 'pre-upgrade.harbor', args.previous_image, '/restore/rollback')
                    docker('rm', container)
                    restored_runtime('/restore/rollback')
                    current_image = args.previous_image
                    create()
                    client = start()
                    connection_guard(client)
                    budget.reserve(download=len(seed_data) + 32)
                    old_body, _ = client.request('GET', f'/nodes/{seed_id}/content', raw=True, limit=len(seed_data))
                    check(old_body == seed_data, 'Fresh old-version rollback checksum mismatch')
                    client.request('GET', f'/nodes/{multipart_id}', expect=404)
                    docker('stop', '--time=30', container)
                    check(state()['ExitCode'] == 0 and not state()['OOMKilled'], 'Fresh rollback clean stop failed')
                    report['fresh_live_rollback'] = {'sha256_equal': True, 'session_reconnected': True,
                        'old_snapshot_excludes_new_file': True,
                        'new_file_references_preserved_in_generated_source_volume': True}
                check(not report.get('network_safety_stop'), 'Network safety stop prevents qualification PASS')
                report.update(result='PASS', container_recreation=True, clean_shutdown=True,
                              reserved_upload_bytes=budget.upload, reserved_download_bytes=budget.download)
        finally:
            handlers = {kind: signal.signal(kind, signal.SIG_IGN) for kind in (signal.SIGINT, signal.SIGTERM)}
            stopped.set()
            if sampler is not None:
                sampler.join(timeout=20)
            while True:
                try:
                    for cleanup_container in tuple(known_containers):
                        remaining = docker('ps', '-aq', '--filter', 'name=^/' + cleanup_container + '$').strip()
                        if remaining:
                            docker('rm', '-f', cleanup_container)
                    if volume in created_volumes:
                        docker('run', '--rm', '--network=none', '--read-only', '--user=65532:65532',
                               '--cap-drop=ALL', '--security-opt=no-new-privileges', '--memory=128m', '--cpus=.5', '--pids-limit=32',
                               '--volume', volume + ':/live', '--entrypoint=sh', args.helper_image,
                               '-ec', 'rm -f /live/telegram.session /live/.telegram-session-*')
                    # Entirely generated restore/archive volumes are disposable, including torn restore staging.
                    for disposable_volume in (restore_volume, archive_volume):
                        if disposable_volume in created_volumes and subprocess.run(
                                ['docker', '--context', args.context, 'volume', 'inspect', disposable_volume], capture_output=True).returncode == 0:
                            docker('volume', 'rm', disposable_volume)
                    if temporary is not None:
                        try:
                            shutil.rmtree(temporary)
                        except FileNotFoundError:
                            pass
                    break
                except (RuntimeError, OSError, subprocess.SubprocessError):
                    print('Cleanup pending; original authorization remains locked', flush=True)
                    time.sleep(5)
            for kind, handler in handlers.items():
                signal.signal(kind, handler)
            if report.get('network_safety_stop'):
                report['result'] = 'FAIL'
            report['sampled_container_network_rx_bytes'] = network_totals[0]
            report['sampled_container_network_tx_bytes'] = network_totals[1]
            report['copied_authorization_removed'] = True
            report['copied_recovery_config_removed'] = True
            report['restore_volume_removed'] = True
            report['encrypted_archives_and_backup_password_removed'] = True
            report['reserved_upload_bytes'] = budget.upload
            report['reserved_download_bytes'] = budget.download
            report['original_session_unchanged'] = hashlib.sha256(session.read_bytes()).digest() == original
            (output / 'result.json').write_text(json.dumps(report, indent=2) + '\n')
            check(report['original_session_unchanged'], 'Original authorization changed unexpectedly')
    check(report['result'] == 'PASS', 'Bounded live qualification failed')
    print('Docker HTTP/crash qualification: ' + report['result'], flush=True)


def self_check():
    assert len(FIXTURE) == 5 << 20
    assert [len(FIXTURE[n * (4 << 20):(n + 1) * (4 << 20)]) for n in range(2)] == [4 << 20, 1 << 20]
    with tarfile.open(fileobj=io.BytesIO(session_archive(b'synthetic-invalid-authorization'))) as archive:
        member = archive.next()
        assert member.name == 'telegram.session' and member.isfile()
        assert (member.uid, member.gid, member.mode) == (65532, 65532, 0o600)
        assert archive.extractfile(member).read() == b'synthetic-invalid-authorization'
        assert archive.next() is None
    assert network_bytes('1.5GiB') == 1610612736
    assert network_bytes('1024B') == 1024 and network_bytes('1.2MB') == 1200000
    budget = TrafficBudget()
    budget.reserve(upload=1536 * MIB, download=4096 * MIB)
    for options in ({'upload': 1}, {'download': 1}, {'upload': -1}):
        try:
            budget.reserve(**options)
        except RuntimeError:
            pass
        else:
            raise AssertionError('Budget must fail closed')
    print('PASS synthetic geometry, private session archive and strict payload budgets; no live operations')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source-env', type=Path)
    parser.add_argument('--image')
    parser.add_argument('--helper-image')
    parser.add_argument('--context')
    parser.add_argument('--output', type=Path)
    parser.add_argument('--self-check', action='store_true')
    parser.add_argument('--duration-seconds', type=int, default=900)
    parser.add_argument('--previous-image')
    parser.add_argument('--version', default='1.3.0-rc.1')
    arguments = parser.parse_args()
    if arguments.self_check:
        self_check()
    else:
        if any(getattr(arguments, name) is None for name in ('source_env', 'image', 'helper_image', 'context', 'output')):
            parser.error('Live mode requires source-env, image, helper-image, context and output')
        def interrupt(*_):
            raise KeyboardInterrupt
        signal.signal(signal.SIGTERM, interrupt)
        try:
            run(arguments)
        except (Exception, KeyboardInterrupt) as error:
            raise SystemExit('FAIL Docker HTTP/crash qualification: ' + type(error).__name__ + '; private details withheld') from None
