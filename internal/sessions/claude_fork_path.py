"""Resolve an exact Claude transcript on its target for a native file-path fork."""
import json
import os
from pathlib import Path
import stat
import sys
import uuid


def resolve(profile, workspace, conversation_id):
    if str(uuid.UUID(conversation_id)) != conversation_id:
        raise ValueError('Choose an exact conversation ID')
    base = (Path(profile).expanduser() / 'projects').resolve()
    workspace = os.path.realpath(workspace)
    matches = []
    # Scan project directories rather than deriving their names: Claude supports
    # hashed long paths and explicitly named project storage directories.
    for count, candidate in enumerate(base.glob('*/' + conversation_id + '.jsonl')):
        if count >= 10000:
            raise ValueError('Native history exceeds the discovery limit')
        path = candidate.resolve()
        if base not in path.parents or not path.is_file():
            continue
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        with os.fdopen(fd, 'rb') as file:
            opened = os.fstat(file.fileno())
            if not stat.S_ISREG(opened.st_mode):
                continue
            cid = cwd = ''
            for _ in range(80):
                if file.tell() >= 2 * 1024 * 1024:
                    break
                line = file.readline(2 * 1024 * 1024 + 1)
                if not line or len(line) > 2 * 1024 * 1024:
                    break
                try:
                    row = json.loads(line)
                except (ValueError, UnicodeError):
                    continue
                if not isinstance(row, dict):
                    continue
                cid = cid or row.get('sessionId', '')
                cwd = cwd or row.get('cwd', '')
                if cid and cwd:
                    break
            current = path.stat()
            if (current.st_dev, current.st_ino) != (opened.st_dev, opened.st_ino):
                raise ValueError('Conversation changed; retry the fork')
            if cid == conversation_id and isinstance(cwd, str) and os.path.realpath(cwd) == workspace:
                matches.append(str(path))
    matches = sorted(set(matches))
    if len(matches) != 1:
        raise ValueError('Exact conversation is missing or ambiguous in the selected native profile')
    return matches[0]


if __name__ == '__main__':
    try:
        print(json.dumps({'path': resolve(os.environ.get('CLAUDE_CONFIG_DIR', '~/.claude'), *sys.argv[1:])}))
    except (OSError, ValueError, TypeError) as error:
        print(json.dumps({'error': str(error)}))
        sys.exit(1)
