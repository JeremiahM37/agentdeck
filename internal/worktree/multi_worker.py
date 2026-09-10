"""Owned grouped worktrees with durable progress and conservative cleanup."""
import fcntl
import json
import os
import pathlib
import signal
import subprocess
import sys

action, raw, validation, single = sys.argv[1:5]
p = json.loads(raw)
root = pathlib.Path(p['path'])
lock = None
owned = False
namespace = {'__name__': 'workspace_validation'}
exec(compile(validation, '<workspace-validation>', 'exec'), namespace)


def save():
    temp = root / '.agentdeck-state.next'
    # The exclusive operation lock serializes this private metadata file.
    with temp.open('w') as file:
        json.dump(p, file)
        file.flush()
        os.fsync(file.fileno())
    os.replace(temp, root / '.agentdeck-state.json')
    fd = os.open(root, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def identity(plan):
    return (plan['path'], plan['token'], plan['branch'],
            [(r['worktree']['repo'], r['worktree']['path'], r['worktree']['token'],
              r['worktree']['branch']) for r in plan['repositories']])


def busy():
    receipt = root / '.agentdeck-process.json'
    if receipt.exists():
        pgid = json.loads(receipt.read_text())['pgid']
        try:
            os.killpg(pgid, 0)
        except ProcessLookupError:
            return
        # An orphaned worker can remain a zombie under a slow PID 1. Zombies
        # cannot write into the workspace and must not make recovery impossible.
        if pathlib.Path('/proc/self/stat').exists():
            active = False
            for process in pathlib.Path('/proc').iterdir():
                if not process.name.isdigit():
                    continue
                try:
                    fields = (process / 'stat').read_text().rsplit(')', 1)[1].split()
                except (FileNotFoundError, ProcessLookupError):
                    continue
                if int(fields[2]) == pgid and fields[0] not in ('Z', 'X'):
                    active = True
                    break
            if not active:
                return
        raise ValueError('A workspace operation is still running; try again after it finishes')


def check_terminals():
    panes = subprocess.run(['tmux', 'list-panes', '-a', '-F', '#{pane_current_path}'],
                           capture_output=True, text=True, timeout=10)
    if panes.returncode and not any(text in panes.stderr.lower() for text in
                                   ('no server running', 'no such file or directory')):
        raise ValueError('Could not check active terminals; nothing was removed')
    for cwd in panes.stdout.splitlines():
        if cwd and os.path.commonpath([os.path.realpath(cwd), str(root)]) == str(root):
            raise ValueError('A terminal is still using this workspace; end or leave it first')


def run_child(entry, operation):
    busy()
    child_plan = dict(entry['worktree'])
    if operation == 'create':
        child_plan['base'] = child_plan['commit']
    read_fd, write_fd = os.pipe()
    # Child must not mutate before its process group is durably recorded. EOF
    # means the supervisor died before permission, so exit without running Git.
    wrapper = "import os,sys; gate=int(sys.argv.pop(1)); allowed=os.read(gate,1); os.close(gate); exec(compile(sys.argv.pop(1),'<owned-worktree>','exec')) if allowed==b'1' else sys.exit(1)"
    child = None
    try:
        child = subprocess.Popen(
            ['python3', '-c', wrapper, str(read_fd), single, operation,
             json.dumps(child_plan), str(lock.fileno())],
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
            start_new_session=True, pass_fds=(read_fd, lock.fileno()),
        )
        receipt = root / '.agentdeck-process.json'
        with receipt.open('w') as file:
            json.dump({'pgid': child.pid}, file)
            file.flush()
            os.fsync(file.fileno())
        fd = os.open(root, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(fd)
        finally:
            os.close(fd)
        os.write(write_fd, b'1')
    finally:
        os.close(read_fd)
        os.close(write_fd)
    try:
        stdout, stderr = child.communicate(timeout=105)
    except subprocess.TimeoutExpired:
        os.killpg(child.pid, signal.SIGKILL)
        child.communicate()
        raise ValueError('Repository operation timed out; allocation retained for inspection')
    try:
        result = json.loads(stdout)
    except ValueError:
        raise ValueError('Repository worker did not return a result: ' + stderr.strip())
    if result.get('workspace'):
        result['workspace']['base'] = entry['worktree']['base']
        entry['worktree'] = result['workspace']
    save()
    if child.returncode:
        raise ValueError(result.get('error') or 'Repository operation failed')


try:
    if action not in ('create', 'remove', 'check-remove'):
        raise ValueError('Unknown workspace operation')
    if action == 'create':
        p = namespace['preflight'](p)
        root = pathlib.Path(p['path'])
        root.parent.mkdir(parents=True, exist_ok=True)
        root.mkdir(mode=0o700)
        lock = (root / '.agentdeck-lock').open('x+')
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        owned = True
        save()
        for entry in p['repositories']:
            # Resolve all refs before the first mutation; use those exact commits
            # so a moving branch cannot silently change a later repository base.
            run_child(entry, 'create')
        busy()
        p['state'] = 'ready'
        p.pop('error', None)
        save()
    else:
        if root.is_symlink() or not root.is_dir():
            raise ValueError('Workspace root is missing or replaced; inspect it before cleanup')
        lock = (root / '.agentdeck-lock').open('r+')
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        saved = json.loads((root / '.agentdeck-state.json').read_text())
        if identity(saved) != identity(p):
            raise ValueError('Workspace ownership or repository allocation does not match')
        p = saved
        owned = True
        busy()
        check_terminals()
        allowed = {'.agentdeck-lock', '.agentdeck-state.json', '.agentdeck-process.json'}
        allowed.update(pathlib.Path(r['worktree']['path']).name for r in p['repositories'])
        if set(os.listdir(root)) - allowed:
            raise ValueError('Workspace root contains additional files; move them before removal')
        for entry in p['repositories']:
            run_child(entry, 'check-remove')
        if action == 'remove':
            for entry in p['repositories']:
                check_terminals()
                run_child(entry, 'remove')
            busy()
            # Keep the owned root/receipt as a durable recovery record. The agent
            # may have written root-level files during removal; never rmtree it.
            p['state'] = 'removed'
            p.pop('error', None)
            save()
    print(json.dumps({'workspace': p}))
except (OSError, ValueError, KeyError, TypeError, subprocess.TimeoutExpired) as error:
    result = {'error': str(error)}
    if owned:
        p.update(state='failed', error=str(error))
        save()
        result['workspace'] = p
    print(json.dumps(result))
    sys.exit(1)
finally:
    if lock:
        lock.close()
