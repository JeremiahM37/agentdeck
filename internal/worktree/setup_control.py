"""Cooperative setup cancellation: only the owning worker signals its children."""
import fcntl,json,os,pathlib,re,signal,stat,subprocess,time

class SetupCancelled(ValueError):
    pass

class SetupControl:
    def __init__(self, plan):
        token=plan.get('token','')
        if not re.fullmatch('[0-9a-f]{32}',token) or not os.path.isabs(plan['path']):
            raise ValueError('Invalid setup cancellation identity')
        self.identity={'token':token,'path':os.path.realpath(plan['path']),
                       'repo':os.path.realpath(plan['repo']) if plan['repo'] else ''}
        parent=pathlib.Path(self.identity['path']).parent
        parent.mkdir(parents=True,exist_ok=True)
        self.path=parent/('.agentdeck-setup-'+token+'.json')
        self.file_identity=None
        self.access()

    def access(self, cancel=False):
        fd=os.open(self.path,os.O_RDWR|os.O_CREAT|os.O_NOFOLLOW|os.O_NONBLOCK,0o600)
        with os.fdopen(fd,'r+') as file:
            fcntl.flock(file,fcntl.LOCK_EX)
            info=os.fstat(file.fileno())
            if not stat.S_ISREG(info.st_mode) or info.st_nlink!=1:
                raise ValueError('Setup cancellation record is not a private regular file')
            identity=(info.st_dev,info.st_ino)
            if self.file_identity is not None and self.file_identity!=identity:
                raise ValueError('Setup cancellation record was replaced')
            self.file_identity=identity
            current=os.stat(self.path,follow_symlinks=False)
            if (info.st_dev,info.st_ino)!=(current.st_dev,current.st_ino):
                raise ValueError('Setup cancellation record was replaced')
            raw=file.read()
            saved=json.loads(raw) if raw else dict(self.identity,cancelled=False)
            if any(saved.get(k)!=v for k,v in self.identity.items()):
                raise ValueError('Setup cancellation ownership does not match')
            if cancel:saved['cancelled']=True
            if cancel or not raw:
                file.seek(0);json.dump(saved,file);file.truncate();file.flush();os.fsync(file.fileno())
            return saved.get('cancelled') is True

    def check(self):
        if self.access():raise SetupCancelled('Workspace setup cancelled; allocated files retained for inspection')

    def wait(self, child, timeout, process_group=None):
        deadline=time.monotonic()+timeout
        try:
            while True:
                self.check()
                remaining=deadline-time.monotonic()
                if remaining<=0:raise subprocess.TimeoutExpired(child.args,timeout)
                try:
                    result=child.communicate(timeout=min(.2,remaining))
                    self.check()
                    return result
                except subprocess.TimeoutExpired:pass
        except BaseException:
            # This Popen is still owned and unreaped, so its PID cannot have been
            # reused. A cancellation requester never signals a recorded PID.
            if child.poll() is None:
                try:os.killpg(process_group or child.pid,signal.SIGKILL)
                except ProcessLookupError:pass
            child.communicate()
            raise
