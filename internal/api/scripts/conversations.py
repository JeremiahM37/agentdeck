"""Read native saved conversations on the target; paths never come from HTTP."""
import collections, glob, json, os, re, sys
agent, workspace, selected, before = sys.argv[1:5]
workspace = os.path.realpath(workspace)
UUID = re.compile(r'^[a-fA-F0-9]{8}(?:-[a-fA-F0-9]{4}){3}-[a-fA-F0-9]{12}$')
LIMIT = 2 * 1024 * 1024


def content(value):
    if isinstance(value, str): return value
    if not isinstance(value, list): return ''
    out = []
    for block in value:
        if not isinstance(block, dict): continue
        kind = block.get('type', '')
        if kind in ('text', 'input_text', 'output_text'): out.append(block.get('text', ''))
        elif kind == 'tool_use': out.append('Tool: ' + str(block.get('name', '')) + '\n' + json.dumps(block.get('input', {}), ensure_ascii=False))
        elif kind == 'tool_result': out.append('Tool result:\n' + content(block.get('content', '')))
        elif kind in ('image', 'input_image'): out.append('[Image attachment]')
    return '\n'.join(out)


def record(row):
    if agent == 'codex':
        p = row.get('payload', {})
        if not isinstance(p,dict): return None
        if row.get('type') != 'response_item': return None
        kind = p.get('type')
        if p.get('channel') in ('analysis','justify','confidence'): return None
        if kind == 'message': role, body = p.get('role'), content(p.get('content'))
        elif kind in ('function_call', 'custom_tool_call'):
            role, body = 'tool', str(p.get('name', 'Tool')) + '\n' + str(p.get('arguments', p.get('input', '')))
        elif kind in ('function_call_output', 'custom_tool_call_output'):
            role, body = 'tool', str(p.get('output', ''))
        else: return None
    else:
        if row.get('type') not in ('user', 'assistant'): return None
        if row.get('isSidechain'): return None
        msg = row.get('message', {})
        if not isinstance(msg, dict): return None
        role, body = msg.get('role'), content(msg.get('content'))
        if isinstance(msg.get('content'),list) and msg['content'] and all(b.get('type')=='tool_result' for b in msg['content'] if isinstance(b,dict)): role='tool'
    # System/developer context and private reasoning are not conversation prose.
    if role not in ('user', 'assistant', 'tool') or not body: return None
    return dict(role=role, text=body[:64000], truncated=len(body)>64000, timestamp=row.get('timestamp', ''))


def metadata(file):
    cid = cwd = title = ''
    codex_header_seen = False
    with open(file, 'rb') as f:
        for _ in range(80):
            if f.tell() >= LIMIT: break
            line = f.readline(LIMIT + 1)
            if not line or len(line)>LIMIT: break
            try: row = json.loads(line)
            except (ValueError, UnicodeError): continue
            if not isinstance(row, dict): continue
            if agent == 'codex' and row.get('type') == 'session_meta' and not codex_header_seen:
                # Forks copy the parent's header into their history. The first
                # native header belongs to this file; later headers are context.
                codex_header_seen = True
                p = row.get('payload', {})
                if not isinstance(p,dict): continue
                cid = p.get('id', p.get('session_id', '')); cwd = p.get('cwd', '')
            elif agent == 'claude':
                cid = cid or row.get('sessionId', ''); cwd = cwd or row.get('cwd', '')
            item = record(row)
            if item and item['role'] == 'user' and not title: title = item['text'].strip().replace('\n', ' ')[:160]
            if cid and cwd and title: break
    if not UUID.fullmatch(str(cid)) or not cwd or os.path.realpath(cwd) != workspace: return None
    return dict(id=cid, title=title or 'Saved conversation', modified=os.stat(file).st_mtime, agent=agent)


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
