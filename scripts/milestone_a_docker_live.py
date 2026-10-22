#!/usr/bin/env python3
"""Opt-in exact-image HTTP/crash qualification; generated fixtures only.

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


def check(condition, message):
    if not condition:
        raise RuntimeError(message)


def session_archive(content):
    stream = io.BytesIO()
    with tarfile.open(fileobj=stream, mode='w') as archive:
        member = tarfile.TarInfo('telegram.session')
        member.uid = member.gid = 65532
        member.mode, member.size = 0o600, len(content)
        archive.addfile(member, io.BytesIO(content))
    return stream.getvalue()


def run(args):
    for image in (args.image, args.helper_image):
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
    prefix = 'harbor-a-http-' + secrets.token_hex(6)
    volume, container = prefix + '-data', prefix

    def docker(*parts, data=None, timeout=120):
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
              'original_session_unchanged': False, 'copied_authorization_removed': False}
    stopped = threading.Event()
    sampler = None

    def collect():
        while not stopped.is_set():
            try:
                report['samples'].append(sample())
            except (RuntimeError, OSError, ValueError, subprocess.SubprocessError):
                pass
            stopped.wait(.25)

    # No source database/key/password is copied. Keep original lock through cleanup.
    with (source / '.lock').open('r+b') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        original = hashlib.sha256(session.read_bytes()).digest()
        docker('volume', 'create', volume)
        temporary = None
        try:
            docker('run', '--rm', '--network=none', '--read-only', '--user=0', '--cap-drop=ALL',
                   '--cap-add=CHOWN', '--security-opt=no-new-privileges', '--memory=128m', '--cpus=.5', '--pids-limit=32',
                   '--volume', volume + ':/live', '--entrypoint=sh', args.helper_image,
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
                           '--volume', volume + ':/live', '--env-file', str(env_file)]
                docker('run', '--rm', '--network=none', *sandbox, args.image, 'init')

                def create():
                    docker('create', '--name', container, '--publish=127.0.0.1::8080', *sandbox, args.image)

                create()
                docker('cp', '-a', '-', container + ':/live', data=session_archive(session.read_bytes()))
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

                client = start()
                client.request('GET', '/diagnostics', expect=401, anonymous=True)
                diagnostics, _ = client.request('GET', '/diagnostics')
                check(diagnostics['adapter'] == 'telegram' and diagnostics['version'] != '1.2.1',
                      'Expected current development Telegram image')
                report['version'] = diagnostics['version']
                report['idle'] = sample()
                sampler = threading.Thread(target=collect, daemon=True)
                sampler.start()
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
                client.request('POST', '/auth/logout', {}, expect=204)
                docker('stop', '--time=30', container)
                check(state()['ExitCode'] == 0 and not state()['OOMKilled'], 'Final clean shutdown failed')
                report.update(result='PASS', container_recreation=True, clean_shutdown=True)
        finally:
            handlers = {kind: signal.signal(kind, signal.SIG_IGN) for kind in (signal.SIGINT, signal.SIGTERM)}
            stopped.set()
            if sampler is not None:
                sampler.join(timeout=20)
            while True:
                try:
                    remaining = docker('ps', '-aq', '--filter', 'name=^/' + container + '$').strip()
                    if remaining:
                        docker('rm', '-f', container)
                    docker('run', '--rm', '--network=none', '--read-only', '--user=65532:65532',
                           '--cap-drop=ALL', '--security-opt=no-new-privileges', '--memory=128m', '--cpus=.5', '--pids-limit=32',
                           '--volume', volume + ':/live', '--entrypoint=sh', args.helper_image,
                           '-ec', 'rm -f /live/telegram.session /live/.telegram-session-*')
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
            report['copied_authorization_removed'] = True
            report['original_session_unchanged'] = hashlib.sha256(session.read_bytes()).digest() == original
            (output / 'result.json').write_text(json.dumps(report, indent=2) + '\n')
            check(report['original_session_unchanged'], 'Original authorization changed unexpectedly')
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
    print('PASS synthetic fixture geometry and protected authorization archive; no live operations')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source-env', type=Path)
    parser.add_argument('--image')
    parser.add_argument('--helper-image')
    parser.add_argument('--context')
    parser.add_argument('--output', type=Path)
    parser.add_argument('--self-check', action='store_true')
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
