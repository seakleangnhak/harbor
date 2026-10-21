#!/usr/bin/env python3
"""Complete encrypted Harbor backup, verification, and fresh-directory restore.

Go recovery tool owns crypto and SQLite snapshots. Passwords are accepted only
through private files or its secure terminal prompt; legacy plaintext tar backups
are deliberately rejected. No dotenv file is automatically loaded.
"""
import argparse
import os
from pathlib import Path
import subprocess


ROOT = Path(__file__).resolve().parent.parent


def invoke(command, *paths, password_file=None, **options):
    binary = os.environ.get('HARBOR_RECOVERY_BINARY')
    args = [binary] if binary else ['go', 'run', './cmd/harbor-recovery']
    args.append(command)
    names = {'backup': ('data-dir', 'output'), 'restore': ('archive', 'destination'), 'verify': ('archive',)}
    for name, path in zip(names[command], paths, strict=True):
        args.extend(['--' + name, str(Path(path).absolute())])
    if password_file is not None:
        args.extend(['--password-file', str(Path(password_file).absolute())])
    for name, value in options.items():
        if value is not None:
            args.extend(['--' + name.replace('_', '-'), str(Path(value).absolute())])
    subprocess.run(args, cwd=ROOT, check=True)


def backup(data, output, password_file=None, **options):
    invoke('backup', data, output, password_file=password_file, **options)


def restore(archive, destination, password_file=None):
    invoke('restore', archive, destination, password_file=password_file)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='command', required=True)
    create = commands.add_parser('backup')
    create.add_argument('data_dir', type=Path)
    create.add_argument('output', type=Path)
    for name in ('key-file', 'owner-password-file', 'session-file', 'config-file'):
        create.add_argument('--' + name, type=Path)
    recover = commands.add_parser('restore')
    recover.add_argument('archive', type=Path)
    recover.add_argument('destination', type=Path)
    verify = commands.add_parser('verify')
    verify.add_argument('archive', type=Path)
    for command in (create, recover, verify):
        command.add_argument('--password-file', type=Path, help='Private backup password file; omit for hidden terminal prompt')
    args = parser.parse_args()
    try:
        if args.command == 'backup':
            backup(args.data_dir, args.output, args.password_file,
                   key_file=args.key_file, owner_password_file=args.owner_password_file,
                   session_file=args.session_file, config_file=args.config_file)
        elif args.command == 'restore':
            restore(args.archive, args.destination, args.password_file)
        else:
            invoke('verify', args.archive, password_file=args.password_file)
    except (OSError, subprocess.CalledProcessError):
        parser.exit(1, 'Encrypted recovery failed. Original drive was not replaced.\n')


if __name__ == '__main__':
    main()
