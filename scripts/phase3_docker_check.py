#!/usr/bin/env python3
"""Read-only checks for the isolated Phase 3 Telegram container.

Coordinator runs this after session handoff. This script never imports data,
starts/stops containers, reads Telegram secrets, or deletes remote objects.
"""
import argparse
import json
from pathlib import Path
import re
import stat
import subprocess
import urllib.request

from telegram_qualification import Client, NoRedirect, OUTPUT, QualificationError, save_state


PROJECT = 'harbor-phase3-tg'
HEX = re.compile(r'^[0-9a-f]{64}$')
INSPECT = '''{{ $telegram := false }}{{ $data := false }}{{range .Config.Env}}{{if eq . "DRIVE_STORAGE=telegram"}}{{$telegram = true}}{{end}}{{if eq . "DRIVE_DATA_DIR=/data"}}{{$data = true}}{{end}}{{end}}
{"container_id":{{json .Id}},"image_id":{{json .Image}},"started_at":{{json .State.StartedAt}},"state":{{json .State.Status}},"health":{{with index .State "Health"}}{{json .Status}}{{else}}null{{end}},"user":{{json .Config.User}},"read_only":{{json .HostConfig.ReadonlyRootfs}},"cap_drop":{{json .HostConfig.CapDrop}},"security_opt":{{json .HostConfig.SecurityOpt}},"tmpfs":{{json .HostConfig.Tmpfs}},"ports":{{json .NetworkSettings.Ports}},"project":{{json (index .Config.Labels "com.docker.compose.project")}},"service":{{json (index .Config.Labels "com.docker.compose.service")}},"storage_telegram":{{$telegram}},"data_in_volume":{{$data}},"data_mount":{{range .Mounts}}{{if eq .Destination "/data"}}{"type":{{json .Type}},"name":{{json .Name}},"writable":{{json .RW}}}{{end}}{{end}}}
'''


def docker(*args):
    result = subprocess.run(['docker', *args], capture_output=True, text=True, timeout=20)
    if result.returncode:
        raise QualificationError('Docker command failed; inspect Docker locally')
    return result.stdout


def inspect_runtime(port):
    ids = docker('ps', '--no-trunc', '--filter', f'label=com.docker.compose.project={PROJECT}',
                 '--filter', 'label=com.docker.compose.service=drive', '--format', '{{.ID}}').split()
    if len(ids) != 1 or not HEX.fullmatch(ids[0]):
        raise QualificationError('Expected exactly one running isolated drive container')
    return validate_runtime(json.loads(docker('inspect', '--format', INSPECT, ids[0])), port)


def validate_runtime(value, port):
    mount = value.get('data_mount', {})
    tmpfs = value.get('tmpfs') or {}
    checks = {
        'isolated_project': value.get('project') == PROJECT and value.get('service') == 'drive',
        'running': value.get('state') == 'running',
        'healthy': value.get('health') == 'healthy',
        'nonroot': value.get('user') == '65532',
        'readonly_root': value.get('read_only') is True,
        'capabilities_dropped': value.get('cap_drop') == ['ALL'],
        'no_new_privileges': any(option in ('no-new-privileges', 'no-new-privileges:true')
                                 for option in value.get('security_opt', [])),
        'bounded_private_tmp': {'size=131072k', 'noexec', 'nosuid'} <= set(tmpfs.get('/tmp', '').split(','))
                              or {'size=128m', 'noexec', 'nosuid'} <= set(tmpfs.get('/tmp', '').split(',')),
        'loopback_port': value.get('ports') == {'8080/tcp': [{'HostIp': '127.0.0.1', 'HostPort': str(port)}]},
        'telegram_adapter': value.get('storage_telegram') is True,
        'persistent_data_path': value.get('data_in_volume') is True,
        'isolated_volume': mount.get('type') == 'volume' and mount.get('name') == PROJECT + '_drive-data'
                           and mount.get('writable') is True,
    }
    for name, passed in checks.items():
        if not passed:
            raise QualificationError('Runtime check failed: ' + name)
    container_id, image_id = value.get('container_id', ''), value.get('image_id', '')
    started_at = value.get('started_at', '')
    if not HEX.fullmatch(container_id) or not re.fullmatch(r'sha256:[0-9a-f]{64}', image_id):
        raise QualificationError('Invalid Docker identity fields')
    if not re.fullmatch(r'\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d+)?Z', started_at):
        raise QualificationError('Invalid Docker start timestamp')
    return {'container_id': container_id, 'image_id': image_id, 'started_at': started_at,
            'volume': PROJECT + '_drive-data', 'checks': {name: 'PASS' for name in checks}}


def verify_transition(current, previous, transition):
    old = previous.get('runtime', {})
    if previous.get('project') != PROJECT or old.get('volume') != current['volume']:
        raise QualificationError('Previous evidence is not from the same isolated volume')
    if transition == 'restart':
        passed = old.get('container_id') == current['container_id'] and old.get('started_at') != current['started_at']
    else:
        passed = old.get('container_id') != current['container_id'] and bool(HEX.fullmatch(old.get('container_id', '')))
    if not passed:
        raise QualificationError('Container transition not observed: ' + transition)


def readiness(base_url, password_file):
    info = password_file.lstat()
    if not stat.S_ISREG(info.st_mode) or info.st_mode & 0o777 != 0o600:
        raise QualificationError('Owner password must be a private regular file with mode 0600')
    opener = urllib.request.build_opener(NoRedirect())
    with opener.open(base_url + '/health', timeout=5) as response:
        if response.status != 200 or json.loads(response.read(4097)) != {'status': 'ok'}:
            raise QualificationError('Liveness check failed')
    client = Client(base_url, password_file)
    try:
        client.request('GET', '/diagnostics', anonymous=True, expect=401)
        client.request('GET', '/nodes/phase3-private-check/content', anonymous=True, expect=401)
        diagnostics, _ = client.request('GET', '/diagnostics')
        connection = diagnostics.get('storage_connection', {})
        if (diagnostics.get('adapter') != 'telegram' or diagnostics.get('database') != 'ok'
                or diagnostics.get('encryption') != 'AES-256-GCM' or connection.get('state') != 'connected'):
            raise QualificationError('Authenticated Telegram readiness check failed')
        retries = connection.get('retry_count')
        chunk_size = diagnostics.get('chunk_size')
        if type(retries) is not int or retries < 0 or type(chunk_size) is not int or not 1 <= chunk_size <= 8 << 20:
            raise QualificationError('Invalid bounded diagnostic metrics')
        return {'liveness': 'PASS', 'unauthenticated_rejection': 'PASS', 'authenticated_readiness': 'PASS',
                'adapter': 'telegram', 'state': 'connected', 'database': 'ok', 'encryption': 'AES-256-GCM',
                'retry_count': retries, 'chunk_size': chunk_size}
    finally:
        client.request('POST', '/auth/logout', expect=204)


def evidence_path(value):
    path = Path(value).resolve()
    if not path.is_relative_to(OUTPUT.resolve()):
        raise argparse.ArgumentTypeError('Evidence must be inside project output/')
    return path


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--port', type=int, default=8084)
    parser.add_argument('--password-file', required=True, type=Path)
    parser.add_argument('--output', required=True, type=evidence_path)
    parser.add_argument('--previous', type=evidence_path)
    parser.add_argument('--transition', choices=('restart', 'recreate'))
    args = parser.parse_args()
    if not 1024 <= args.port <= 65535 or bool(args.previous) != bool(args.transition):
        parser.error('Use an unprivileged port; previous and transition must be supplied together')
    try:
        if args.output.exists():
            raise QualificationError('Evidence already exists; choose a new output path')
        runtime = inspect_runtime(args.port)
        if args.previous:
            verify_transition(runtime, json.loads(args.previous.read_text()), args.transition)
        observed = readiness(f'http://127.0.0.1:{args.port}', args.password_file)
        save_state(args.output, {'version': 1, 'project': PROJECT, 'port': args.port, 'result': 'PASS',
                                'transition': args.transition or 'initial', 'runtime': runtime, 'readiness': observed})
    except (QualificationError, OSError, ValueError, TypeError, KeyError, subprocess.TimeoutExpired) as error:
        if isinstance(error, QualificationError):
            parser.exit(1, 'FAIL ' + str(error) + '\n')
        parser.exit(1, 'FAIL Docker qualification unavailable; inspect local setup and rerun\n')
    print('PASS isolated Telegram runtime, authenticated readiness, security and persistent volume' +
          ('; observed ' + args.transition if args.transition else ''))


if __name__ == '__main__':
    main()
