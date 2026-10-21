#!/usr/bin/env python3
"""Fail closed before CI or GHCR authentication; accepts only the current stable tag."""
import os
from pathlib import Path
import re
import subprocess


STABLE = re.compile(r'v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)')


def git(root, *args):
    return subprocess.check_output(['git', *args], cwd=root, text=True,
                                   stderr=subprocess.DEVNULL).strip()


def qualify(root, event, ref, sha, repository, requested=''):
    if event not in ('push', 'workflow_dispatch') or not ref.startswith('refs/tags/'):
        raise ValueError('Release must run from an existing stable tag')
    tag = ref.removeprefix('refs/tags/')
    match = STABLE.fullmatch(tag)
    if not match or not re.fullmatch(r'[0-9a-f]{40}', sha):
        raise ValueError('Invalid stable release ref')
    version = tuple(map(int, match.groups()))
    if version <= (1, 0, 0):
        raise ValueError('Historical v1.0.0 is immutable; first automated release is v1.0.1')
    if event == 'workflow_dispatch' and requested != tag:
        raise ValueError('Manual input must equal the selected workflow tag')
    if not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9_.-]+', repository):
        raise ValueError('Invalid GitHub repository')
    if (git(root, 'cat-file', '-t', 'refs/tags/' + tag) != 'tag' or
            git(root, 'rev-parse', 'refs/tags/' + tag + '^{commit}') != sha or
            git(root, 'rev-parse', 'HEAD') != sha):
        raise ValueError('Annotated release tag, checkout and workflow commit must agree')
    subprocess.run(['git', 'merge-base', '--is-ancestor', sha, 'refs/remotes/origin/main'],
                   cwd=root, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    for candidate in git(root, 'tag', '--list').splitlines():
        newer = STABLE.fullmatch(candidate)
        if newer and tuple(map(int, newer.groups())) > version:
            raise ValueError('A newer stable tag exists; refusing to move latest backwards')
    for file in ('.github/workflows/ci.yml', '.github/workflows/docker-publish.yml',
                 'scripts/release_gate.py', 'LICENSE'):
        if not (Path(root) / file).is_file():
            raise ValueError('Release tree lacks required workflow or license files')
    return {'tag': tag, 'version': tag[1:], 'minor': '.'.join(map(str, version[:2])),
            'commit': sha, 'image': 'ghcr.io/' + repository.split('/')[0].lower() + '/harbor'}


def main():
    metadata = qualify(Path.cwd(), os.environ.get('GITHUB_EVENT_NAME', ''),
                       os.environ.get('GITHUB_REF', ''), os.environ.get('GITHUB_SHA', ''),
                       os.environ.get('GITHUB_REPOSITORY', ''), os.environ.get('RELEASE_TAG', ''))
    with open(os.environ['GITHUB_OUTPUT'], 'a', encoding='utf-8') as stream:
        for name, value in metadata.items():
            stream.write(name + '=' + value + '\n')
    print('Stable annotated release tag and main ancestry verified.')


if __name__ == '__main__':
    try:
        main()
    except (KeyError, OSError, ValueError, subprocess.SubprocessError) as error:
        message = str(error) if isinstance(error, ValueError) else type(error).__name__
        raise SystemExit('Release gate rejected: ' + message)
