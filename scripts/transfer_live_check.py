#!/usr/bin/env python3
"""Run opt-in live tests with copied authorization, never original drive state.

Build storage-live.test first, offline, from the reviewed source. Supply its
read-only Docker volume and tooling image. Only this reviewed control harness
runs on the host; target execution stays in an isolated container. No deletes
are issued to Telegram. Generated fixture metadata/key remain in a protected
volume; the copied authorization is removed after all target processes stop.
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
import time

from phase5_live_recovery_check import config


def run(args):
    if not re.fullmatch(r'sha256:[0-9a-f]{64}', args.image):
        raise ValueError('Use immutable local tooling image ID')
    if not re.fullmatch(r'[a-zA-Z0-9][a-zA-Z0-9_.-]*', args.binary_volume):
        raise ValueError('Invalid compiled-binary volume')
    output = args.output.resolve()
    root = Path(__file__).resolve().parents[1]
    if not output.is_relative_to(root / 'output') or output.exists():
        raise ValueError('Use fresh ignored output directory')
    os.umask(0o077)
    output.mkdir(parents=True, mode=0o700)
    settings = config(args.source_env)
    source_root = args.source_env.resolve().parent
    data = Path(settings.get('DRIVE_DATA_DIR', 'data'))
    data = data if data.is_absolute() else source_root / data
    session = Path(settings.get('TELEGRAM_SESSION_FILE', str(data / 'telegram.session')))
    session = session if session.is_absolute() else source_root / session
    if session.is_symlink() or not session.is_file() or session.stat().st_mode & 0o077:
        raise ValueError('Private regular persisted authorization required')
    prefix = 'harbor-a-live-' + secrets.token_hex(6)
    volume, container = prefix + '-data', prefix
    command = ['docker', '--context', args.context]

    def docker(*parts, data=None, timeout=120):
        result = subprocess.run([*command, *parts], input=data, capture_output=True, timeout=timeout)
        if result.returncode:
            raise RuntimeError('Docker operation failed: ' + parts[0] + '; private output withheld')
        return result.stdout

    # Keep the original lock held until target and cleanup containers have exited.
    with (data / '.lock').open('r+b') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        original = hashlib.sha256(session.read_bytes()).digest()
        docker('volume', 'create', volume)
        docker('run', '--rm', '--network=none', '--read-only', '--user=0', '--cap-drop=ALL', '--cap-add=CHOWN',
               '--security-opt=no-new-privileges', '--memory=128m', '--pids-limit=32',
               '--volume', volume + ':/live', '--entrypoint=sh', args.image,
               '-ec', 'chmod 0700 /live; chown 65532:65532 /live')
        temporary = None
        try:
            with tempfile.TemporaryDirectory(prefix='private-', dir=output) as temporary:
                runtime = {key: settings[key] for key in ('TELEGRAM_APP_ID', 'TELEGRAM_APP_HASH',
                           'TELEGRAM_CHANNEL_ID', 'TELEGRAM_CHANNEL_ACCESS_HASH', 'USERNAME_OR_CHANNEL_LINK') if settings.get(key)}
                runtime.update(TELEGRAM_SESSION_FILE='/live/telegram.session', HARBOR_LIVE_CONFIRM='generated-fixtures-only',
                               HARBOR_LIVE_STATE_DIR='/live/runtime', GOMEMLIMIT='512MiB', GOMAXPROCS='2', HOME='/live', TMPDIR='/tmp')
                env_file = Path(temporary) / 'runtime.env'
                if any('\n' in value or '\r' in value for value in runtime.values()):
                    raise ValueError('Runtime environment contains line breaks')
                env_file.write_text(''.join(key + '=' + value + '\n' for key, value in runtime.items()))
                env_file.chmod(0o600)
                docker('create', '--name', container, '--read-only', '--user=65532:65532',
                       '--cap-drop=ALL', '--security-opt=no-new-privileges', '--memory=768m', '--memory-swap=768m',
                       '--cpus=2', '--pids-limit=128', '--tmpfs=/tmp:rw,nosuid,nodev,mode=1777,size=128m',
                       '--volume', volume + ':/live', '--volume', args.binary_volume + ':/compiled:ro',
                       '--env-file', str(env_file), '--entrypoint=/compiled/storage-live.test', args.image,
                       '-test.run=^TestLiveTransferQualification$', '-test.v', '-test.timeout=22m')
                stream = io.BytesIO()
                with tarfile.open(fileobj=stream, mode='w') as archive:
                    content = session.read_bytes()
                    member = tarfile.TarInfo('telegram.session')
                    member.uid = member.gid = 65532
                    member.mode, member.size = 0o600, len(content)
                    archive.addfile(member, io.BytesIO(content))
                docker('cp', '-a', '-', container + ':/live', data=stream.getvalue())
                started = time.monotonic()
                docker('start', container)
                samples = []
                while True:
                    state = json.loads(docker('inspect', '--format', '{{json .State}}', container))
                    if not state['Running']:
                        break
                    if time.monotonic() - started > 1350:
                        raise TimeoutError('Live test deadline exceeded')
                    sample = json.loads(docker('stats', '--no-stream', '--format', '{{json .}}', container, timeout=15))
                    samples.append({key: sample[key] for key in ('MemUsage', 'CPUPerc', 'NetIO', 'BlockIO')})
                log = docker('logs', container).decode(errors='replace')
                # The reviewed test reports only fixed stages and generated metrics.
                (output / 'live-test.log').write_text(log)
                report = {'result': 'PASS' if state['ExitCode'] == 0 and not state['OOMKilled'] else 'FAIL',
                          'image': args.image, 'binary_volume': args.binary_volume, 'fixture_volume': volume,
                          'exit_code': state['ExitCode'], 'oom_killed': state['OOMKilled'],
                          'elapsed_seconds': time.monotonic() - started, 'samples': samples,
                          'resource_method': 'sampled docker stats; not lifetime RSS peak',
                          'memory_limit_bytes': 768 << 20, 'go_memory_limit': '512MiB', 'original_session_unchanged': False}
                print('Live tests finished: ' + report['result'], flush=True)
        finally:
            # Keep the lock through cleanup, including repeated termination requests.
            handlers = {sig: signal.signal(sig, signal.SIG_IGN) for sig in (signal.SIGINT, signal.SIGTERM)}
            while True:
                try:
                    remaining = docker('ps', '-aq', '--filter', 'name=^/' + container + '$').strip()
                    if remaining:
                        docker('rm', '-f', container)
                    docker('run', '--rm', '--network=none', '--read-only', '--user=65532:65532',
                           '--cap-drop=ALL', '--security-opt=no-new-privileges', '--memory=128m', '--pids-limit=32',
                           '--volume', volume + ':/live', '--entrypoint=sh', args.image,
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
            for sig, handler in handlers.items():
                signal.signal(sig, handler)
        if hashlib.sha256(session.read_bytes()).digest() != original:
            raise RuntimeError('Original authorization changed unexpectedly')
        report['original_session_unchanged'] = True
        report['copied_authorization_removed'] = True
        (output / 'result.json').write_text(json.dumps(report, indent=2) + '\n')
        if report['result'] != 'PASS':
            raise RuntimeError('Live qualification failed; see sanitized evidence')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source-env', required=True, type=Path)
    parser.add_argument('--image', required=True)
    parser.add_argument('--context', required=True)
    parser.add_argument('--binary-volume', required=True)
    parser.add_argument('--output', required=True, type=Path)
    arguments = parser.parse_args()
    def interrupt(*_):
        raise KeyboardInterrupt
    signal.signal(signal.SIGTERM, interrupt)
    try:
        run(arguments)
    except (OSError, RuntimeError, ValueError, TimeoutError, subprocess.SubprocessError) as error:
        raise SystemExit(type(error).__name__ + ': ' + str(error)) from None
