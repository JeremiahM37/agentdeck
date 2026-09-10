"""Snapshot a Claude fork source and its session sidecars without changing it.

Internal helper: lifecycle integration and native-reader seed filtering are still
required before use by production launches.
"""
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import stat
import tempfile
import uuid

MAX_FILE = 64 * 1024 * 1024
MAX_TOTAL = 256 * 1024 * 1024


def fingerprint(path):
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode):
        raise ValueError('Session assets must be regular files')
    return (info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns)


def copy_file(source, destination, budget):
    before = fingerprint(source)
    if before[2] > MAX_FILE or budget[0] + before[2] > MAX_TOTAL:
        raise ValueError('Session history exceeds the fork snapshot limit')
    fd = os.open(source, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, 'rb') as incoming, destination.open('xb') as outgoing:
        opened = os.fstat(incoming.fileno())
        if before != (opened.st_dev, opened.st_ino, opened.st_size, opened.st_mtime_ns):
            raise ValueError('Session history changed while preparing the fork; retry')
        digest = hashlib.sha256()
        count = 0
        while True:
            block = incoming.read(1024 * 1024)
            if not block:
                break
            count += len(block)
            if count > MAX_FILE or budget[0] + count > MAX_TOTAL:
                raise ValueError('Session history exceeds the fork snapshot limit')
            digest.update(block)
            outgoing.write(block)
        outgoing.flush()
        os.fsync(outgoing.fileno())
    destination.chmod(0o600)
    if fingerprint(source) != before or count != before[2]:
        raise ValueError('Session history changed while preparing the fork; retry')
    budget[0] += count
    return {'sha256': digest.hexdigest(), 'size': count}


def snapshot(profile, source_cwd, destination_cwd, conversation_id):
    if str(uuid.UUID(conversation_id)) != conversation_id:
        raise ValueError('Choose an exact conversation ID')
    source_cwd, destination_cwd = map(os.path.realpath, (source_cwd, destination_cwd))
    if source_cwd == destination_cwd:
        raise ValueError('A fork seed needs a different destination workspace')
    projects = Path(profile).expanduser().resolve() / 'projects'
    encode = lambda cwd: re.sub(r'[^a-zA-Z0-9]', '-', cwd)
    source_dir = projects / encode(source_cwd)
    destination_dir = projects / encode(destination_cwd)
    if source_dir == destination_dir:
        raise ValueError('Workspace paths collide in Claude project storage')
    source = source_dir / (conversation_id + '.jsonl')
    destination_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
    stage = Path(tempfile.mkdtemp(prefix='.agentdeck-fork-', dir=destination_dir))
    budget, files, source_versions = [0], {}, {}
    try:
        def copy_tree(path, relative):
            info = path.lstat()
            source_versions[str(path)] = (info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns)
            if stat.S_ISDIR(info.st_mode):
                (stage / relative).mkdir(mode=0o700)
                for child in sorted(path.iterdir()):
                    copy_tree(child, relative / child.name)
            elif stat.S_ISREG(info.st_mode):
                files[str(relative)] = copy_file(path, stage / relative, budget)
            else:
                raise ValueError('Session sidecars contain a link or special file; inspect it before forking')
        copy_tree(source, Path(source.name))
        sidecars = source_dir / conversation_id
        if os.path.lexists(sidecars):
            copy_tree(sidecars, Path(conversation_id))
        matched = False
        with (stage / source.name).open() as transcript:
            for line in transcript:
                if not line.endswith('\n'):
                    raise ValueError('Session history is still being written; retry')
                row = json.loads(line)
                if row.get('sessionId') == conversation_id and row.get('cwd') and os.path.realpath(row['cwd']) == source_cwd:
                    matched = True
        if not matched:
            raise ValueError('Conversation does not belong to the source workspace')
        # Detect changes to a sidecar directory or an earlier file while a later
        # asset was being copied, not only mutation during an individual read.
        for name, version in source_versions.items():
            info = Path(name).lstat()
            if version != (info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns):
                raise ValueError('Session assets changed while preparing the fork; retry')
        manifest = {'conversation_id': conversation_id, 'source_cwd': source_cwd,
                    'destination_cwd': destination_cwd, 'files': files, 'bytes': budget[0]}
        with (stage / 'snapshot.json').open('x') as file:
            json.dump(manifest, file)
            file.flush()
            os.fsync(file.fileno())
        (stage / 'snapshot.json').chmod(0o600)
        return stage, manifest
    except BaseException:
        shutil.rmtree(stage)
        raise
