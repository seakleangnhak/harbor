#!/usr/bin/env python3
"""Build a local release from an explicit trusted Git commit, never the worktree.

Requires Git, Docker/Buildx and Python 3.12+. No push, tag or deployment is performed.
"""
import argparse
import gzip
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import subprocess
import tarfile
import tempfile


ROOT = Path(__file__).resolve().parent.parent
VERSION = '1.0.0'
PINNED_IMAGE = re.compile(r'\S+@sha256:[0-9a-f]{64}')


def run(*args, cwd=ROOT):
    result = subprocess.run(args, cwd=cwd, check=True, capture_output=True, text=True)
    return result.stdout.strip()


def safe_members(archive):
    for member in archive.getmembers():
        path = PurePosixPath(member.name)
        parts = path.parts
        if (not parts or path.is_absolute() or '..' in parts or
                not (member.isfile() or member.isdir()) or
                any(p in ('.git', '.secrets', 'node_modules', '__pycache__', 'output', 'bin',
                          'backups', 'data', 'test-secrets', 'test-credentials', 'release-artifacts')
                    or p.startswith('data-') for p in parts) or
                any(p.startswith('.env') and p != '.env.example' for p in parts) or
                path.name in ('owner-password', 'storage.json', 'browser-state.json') or
                path.name.endswith(('.key', '.session', '.pem', '.p12', '.pfx', '.harbor', '.backup', '.bak')) or
                '.session.' in path.name or '.harbor.' in path.name or
                re.search(r'\.(?:db|sqlite3?)(?:-|$)', path.name) or
                any(p in path.name for p in ('backup-password', 'recovery-password')) or
                parts[:2] == ('web', 'dist')):
            raise ValueError('Source archive contains an unsafe path or member')
    return archive.getmembers()


def base_images(dockerfile):
    images = re.findall(r'^FROM\s+(?:--platform=\S+\s+)?(\S+)', dockerfile, re.MULTILINE | re.IGNORECASE)
    if len(images) != 3 or any(not PINNED_IMAGE.fullmatch(image) for image in images):
        raise ValueError('All three Docker base images must use immutable SHA-256 digests')
    return images


def source_archive(root, commit, destination, context):
    raw = context.parent / 'source.tar'
    run('git', 'archive', '--format=tar', '--output=' + str(raw), commit, cwd=root)
    with tarfile.open(raw) as archive:
        members = safe_members(archive)
        names = {m.name for m in members if m.isfile()}
        if not {'Dockerfile', 'compose.yaml', 'go.mod', 'go.sum', 'web/package-lock.json'} <= names:
            raise ValueError('Source archive lacks required release files')
        archive.extractall(context, members=members, filter='data')
    with raw.open('rb') as incoming, destination.open('xb') as outgoing:
        with gzip.GzipFile(filename='', mode='wb', fileobj=outgoing, mtime=0) as compressed:
            shutil.copyfileobj(incoming, compressed)
    return base_images((context / 'Dockerfile').read_text())


def fresh_output(path):
    path = Path(os.path.abspath(path))
    if path.exists() or path.is_symlink():
        raise ValueError('Release output already exists; choose a fresh directory')
    resolved = path.resolve()
    if resolved.is_relative_to(ROOT) and subprocess.run(
            ['git', 'check-ignore', '--quiet', '--', str(resolved)], cwd=ROOT).returncode:
        raise ValueError('Release output inside the repository must be Git-ignored')
    resolved.mkdir(mode=0o700, parents=True, exist_ok=False)
    (resolved / 'INCOMPLETE').write_text('Release build has not finished.\n')
    return resolved


def sha256(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def image_manifest_digest(path):
    with tarfile.open(path) as archive:
        descriptors = json.load(archive.extractfile('index.json'))['manifests']
        if len(descriptors) != 1 or not re.fullmatch(r'sha256:[0-9a-f]{64}', descriptors[0]['digest']):
            raise ValueError('Expected one immutable release image manifest')
        digest = descriptors[0]['digest']
        with archive.extractfile('blobs/sha256/' + digest.removeprefix('sha256:')) as stream:
            if hashlib.file_digest(stream, 'sha256').hexdigest() != digest.removeprefix('sha256:'):
                raise ValueError('Release image manifest integrity failed')
        return digest


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--ref', required=True, help='Explicit trusted committed source ref')
    parser.add_argument('--version', default=VERSION, help='Stable release version, without v prefix')
    parser.add_argument('--output', type=Path)
    parser.add_argument('--image', help='New local image name:tag')
    parser.add_argument('--platform', choices=('linux/arm64', 'linux/amd64'))
    args = parser.parse_args()
    version = args.version
    if not re.fullmatch(r'(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)', version):
        parser.error('--version must be stable major.minor.patch')
    args.output = args.output or ROOT / 'output/releases' / ('v' + version)
    args.image = args.image or 'harbor:v' + version
    if not re.fullmatch(r'[a-z0-9][a-z0-9._-]*:[A-Za-z0-9_][A-Za-z0-9_.-]*', args.image):
        parser.error('--image must be a local name:tag')
    commit = run('git', 'rev-parse', '--verify', '--end-of-options', args.ref + '^{commit}')
    committed_at = int(run('git', 'show', '-s', '--format=%ct', commit))
    epoch = 0
    tree = run('git', 'rev-parse', commit + '^{tree}')
    platform = args.platform or run('docker', 'version', '--format', '{{.Server.Os}}/{{.Server.Arch}}')
    if platform not in ('linux/arm64', 'linux/amd64'):
        raise ValueError('Release platform must be linux/arm64 or linux/amd64')
    if subprocess.run(['docker', 'image', 'inspect', args.image], stdout=subprocess.DEVNULL,
                      stderr=subprocess.DEVNULL).returncode == 0:
        raise ValueError('Local image tag already exists; choose a new tag')
    output = fresh_output(args.output)
    source = output / ('harbor-v' + version + '-source.tar.gz')
    image_tar = output / ('harbor-v' + version + '-' + platform.replace('/', '-') + '.tar')
    with tempfile.TemporaryDirectory(prefix='harbor-release-') as temporary:
        context = Path(temporary) / 'context'
        context.mkdir()
        images = source_archive(ROOT, commit, source, context)
        if version != VERSION and 'ARG HARBOR_VERSION' not in (context / 'Dockerfile').read_text():
            raise ValueError('Historical source does not support a build version override')
        command = ['docker', 'buildx', 'build', '--platform', platform, '--provenance=false',
                   '--build-arg', 'GO_TAGS=telegram', '--build-arg', 'HARBOR_VERSION=' + version,
                   '--build-arg', 'SOURCE_DATE_EPOCH=' + str(epoch),
                   '--tag', args.image, '--output', 'type=docker,dest=' + str(image_tar) + ',rewrite-timestamp=true',
                   str(context)]
        with (output / 'build.log').open('xb') as log:
            subprocess.run(command, cwd=context, stdout=log, stderr=subprocess.STDOUT, check=True)
    run('docker', 'load', '--input', str(image_tar))
    image_id = run('docker', 'image', 'inspect', '--format', '{{.Id}}', args.image)
    observed = run('docker', 'image', 'inspect', '--format', '{{.Os}}/{{.Architecture}}', image_id)
    if observed != platform or not re.fullmatch(r'sha256:[0-9a-f]{64}', image_id):
        raise ValueError('Built image identity or platform does not match')
    artifacts = {path.name: {'sha256': sha256(path), 'bytes': path.stat().st_size}
                 for path in (source, image_tar)}
    manifest = {'version': version, 'source_ref': args.ref, 'source_commit': commit,
                'source_tree': tree, 'source_commit_timestamp': committed_at,
                'source_date_epoch': epoch, 'platform': platform,
                'image_tag': args.image, 'image_id': image_id,
                'image_manifest_digest': image_manifest_digest(image_tar), 'base_images': images,
                'build_args': {'GO_TAGS': 'telegram', 'HARBOR_VERSION': version, 'SOURCE_DATE_EPOCH': str(epoch)},
                'artifacts': artifacts}
    manifest_file = output / 'release.json'
    manifest_file.write_text(json.dumps(manifest, indent=2, sort_keys=True) + '\n')
    checksums = [sha256(path) + '  ' + path.name + '\n' for path in (source, image_tar, manifest_file)]
    (output / 'SHA256SUMS').write_text(''.join(checksums))
    (output / 'INCOMPLETE').unlink()
    print(json.dumps({'output': str(output), 'image_id': image_id, 'source_commit': commit}))


if __name__ == '__main__':
    try:
        main()
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        # Git/Docker output may include arbitrary source text; retain only safe categories.
        raise SystemExit('Release build failed: ' + type(error).__name__ + '; inspect fresh output build.log if present')
