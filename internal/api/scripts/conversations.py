"""Read native saved conversations on the target; paths never come from HTTP."""
import collections, glob, json, os, re, sys
agent, workspace, selected, before = sys.argv[1:5]
workspace = os.path.realpath(workspace)
UUID = re.compile(r'^[a-fA-F0-9]{8}(?:-[a-fA-F0-9]{4}){3}-[a-fA-F0-9]{12}$')
LIMIT = 2 * 1024 * 1024


# Embedded remotely; importing also supports direct execution during diagnostics.
if 'native_record' not in globals():
    from native_records import native_record, native_metadata

def record(row):
    return native_record(row, agent)

def metadata(file):
    info = native_metadata(file, agent, workspace)
    if info: info.pop('cwd', None)
    return info


try:
    if agent not in ('codex', 'claude'): raise ValueError('Native history is available for Claude and Codex; use terminal history for this agent')
    if selected and not UUID.fullmatch(selected): raise ValueError('Choose a saved conversation ID')
    if agent == 'codex':
        home = os.path.expanduser(os.environ.get('CODEX_HOME', '~/.codex'))
        base = os.path.join(home, 'sessions')
        pattern = os.path.join(base, '**', '*.jsonl')
    else:
        home = os.path.expanduser(os.environ.get('CLAUDE_CONFIG_DIR', '~/.claude'))
        slug = re.sub(r'[^a-zA-Z0-9]', '-', workspace)
        base = os.path.join(home, 'projects', slug)
        pattern = os.path.join(base, '*.jsonl')
    base = os.path.realpath(base)
    paths = []
    for file in glob.iglob(pattern, recursive=agent=='codex'):
        if len(paths)>=10000: raise ValueError('History store exceeds the discovery limit')
        real = os.path.realpath(file)
        if os.path.commonpath([base, real]) != base or not os.path.isfile(real): continue
        if selected and selected not in os.path.basename(file): continue
        paths.append(real)
    paths.sort(key=os.path.getmtime, reverse=True)
    scan_limited = len(paths)>500
    titles={}
    if agent=='codex':
        try:
            with open(os.path.join(home,'session_index.jsonl'),'rb') as index:
                index.seek(0,2); size=index.tell(); index.seek(max(0,size-4*LIMIT))
                if size>4*LIMIT: index.readline(LIMIT)
                for line in index:
                    try:
                        entry=json.loads(line)
                        if isinstance(entry,dict) and isinstance(entry.get('thread_name'),str): titles[entry.get('id')]=entry['thread_name'][:160]
                    except (ValueError,TypeError): pass
        except OSError: pass
    current = native_identity(agent, workspace, home, *sys.argv[5:7]) if not selected and len(sys.argv)>6 else dict(state='unavailable')
    if current.get('id'):
        paths.sort(key=lambda p: current['id'] not in os.path.basename(p))
    conversations = []; chosen = None
    for file in paths[:500]:
        try: info = metadata(file)
        except (OSError, ValueError): continue
        if info is None: continue
        info['title']=titles.get(info['id'],info['title'])
        if selected and info['id'] == selected: chosen = (file, info); break
        if not selected and all(c['id'] != info['id'] for c in conversations): conversations.append(info)
    if not selected:
        current['saved'] = any(c['id']==current.get('id') for c in conversations)
        print(json.dumps(dict(conversations=conversations, scan_limited=scan_limited, current=current)))
    else:
        if chosen is None: raise ValueError('Conversation not found in this workspace on this target')
        file, info = chosen
        size = os.path.getsize(file); end = min(size, int(before)) if before else size
        if end < 0: raise ValueError('Invalid history cursor')
        start = max(0, end-LIMIT)
        items = collections.deque(maxlen=200)
        with open(file, 'rb') as f:
            f.seek(start)
            if start: f.readline(LIMIT); start=f.tell()
            while f.tell()<end:
                offset=f.tell(); line=f.readline(min(LIMIT+1,end-offset))
                if not line: break
                if not line.endswith(b'\n'): continue # trailing in-progress write
                try: row=json.loads(line)
                except (ValueError,UnicodeError): continue
                if not isinstance(row,dict): continue
                item=record(row)
                if item: item['offset']=offset;items.append(item)
        first=items[0]['offset'] if items else start
        older = first if first < end else max(0,end-LIMIT)
        print(json.dumps(dict(conversation=info,messages=list(items),before=older if older>0 and (start or len(items)==200) else None,window_bytes=LIMIT)))
except (OSError,ValueError,TypeError) as error:
    print(json.dumps(dict(error=str(error))))
    sys.exit(1)
