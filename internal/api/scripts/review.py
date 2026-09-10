"""Read Git changes on the selected target. Never stage, checkout, or write."""
import json, os, stat, subprocess, sys, tempfile

LIMIT = 512 * 1024
scope, requested = sys.argv[2:4]
workspace = os.path.realpath(sys.argv[1])
os.chdir(workspace)
env = dict(os.environ, GIT_OPTIONAL_LOCKS='0', GIT_TERMINAL_PROMPT='0', GIT_LITERAL_PATHSPECS='1')


def git(args, limit=LIMIT, allowed=(0,)):
    # Spool to disk so a large diff cannot fill the server's memory. The parent
    # executor also has a deadline. No repository-defined external diff or textconv programs.
    with tempfile.TemporaryFile() as out, tempfile.TemporaryFile() as err:
        p = subprocess.run(['git', '-c', 'core.fsmonitor=false', '-c', 'color.ui=false',
                            *args], stdout=out, stderr=err, env=env, timeout=15)
        out.seek(0); data = out.read(limit + 1)
        if p.returncode not in allowed:
            err.seek(0)
            raise ValueError(err.read(2048).decode('utf-8', 'replace').strip() or 'Git command failed')
        return data[:limit], len(data) > limit


def text(data):
    return data.decode('utf-8', 'replace')


try:
    if scope not in ('working', 'staged'):
        raise ValueError('Choose working or staged changes')
    top = os.path.realpath(text(git(['rev-parse', '--show-toplevel'])[0]).strip())
    branch = text(git(['symbolic-ref', '--short', '-q', 'HEAD'], allowed=(0, 1))[0]).strip()
    if not branch:
        branch = text(git(['rev-parse', '--short', 'HEAD'], allowed=(0, 128))[0]).strip() or 'unborn'
    raw, cut = git(['status', '--porcelain=v1', '-z', '--untracked-files=all', '--', '.'], 2 * LIMIT)
    if cut:
        raise ValueError('Too many changed paths to review; narrow the session workspace')
    parts = raw.split(b'\0'); files = []; i = 0
    while i < len(parts) and parts[i]:
        item = parts[i]; i += 1
        status = text(item[:2]); name = os.fsdecode(item[3:]); old = None
        if status[0] in 'RC' or status[1] in 'RC':
            old = os.fsdecode(parts[i]); i += 1
        full = os.path.normpath(os.path.join(top, name))
        if os.path.commonpath([workspace, full]) != workspace:
            continue
        rel = os.path.relpath(full, workspace)
        working = status[1] != ' ' or status == '??'
        staged = status[0] not in (' ', '?')
        files.append(dict(path=rel, status=status, working=working, staged=staged,
                          previous_path=old))
        if len(files) > 2000:
            raise ValueError('More than 2000 changed files; narrow the session workspace')
    files.sort(key=lambda f: f['path'].casefold())
    visible = [f for f in files if f[scope]]
    chosen = next((f for f in visible if f['path'] == requested), None) if requested else next(iter(visible), None)
    if requested and chosen is None:
        raise ValueError('This file is no longer in the selected changes; refresh the list')
    patch = ''; truncated = False
    if chosen:
        rel = chosen['path']
        args = ['diff', '--no-ext-diff', '--no-textconv', '--no-color', '--unified=3']
        if chosen['status'] == '??':
            # Do not follow untracked symlinks or open FIFOs/devices. Tracked
            # symlinks are handled by Git itself as links, not target contents.
            full = os.path.join(workspace, rel)
            if not stat.S_ISREG(os.lstat(full).st_mode) or os.path.realpath(full) != full:
                patch = 'Untracked link or special file: content preview unavailable.'
            else:
                data, truncated = git(args + ['--no-index', '--', '/dev/null', rel], allowed=(0, 1))
                patch = text(data)
        else:
            if scope == 'staged': args.append('--cached')
            paths = [rel]
            if chosen['previous_path']:
                prior = os.path.normpath(os.path.join(top, chosen['previous_path']))
                if os.path.commonpath([workspace, prior]) == workspace:
                    paths.append(os.path.relpath(prior, workspace))
            data, truncated = git(args + ['--', *paths])
            patch = text(data)
    print(json.dumps(dict(branch=branch, scope=scope, files=files,
                         path=chosen['path'] if chosen else '', patch=patch,
                         truncated=truncated, limit_bytes=LIMIT)))
except (OSError, ValueError, subprocess.TimeoutExpired) as error:
    print(json.dumps(dict(error=str(error))))
    sys.exit(1)
