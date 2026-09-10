"""Shared visible-message decoding for native history and search."""
import json, os, re
UUID = re.compile(r'^[a-fA-F0-9]{8}(?:-[a-fA-F0-9]{4}){3}-[a-fA-F0-9]{12}$')
LIMIT = 2 * 1024 * 1024

def content(value):
    if isinstance(value, str): return value
    if not isinstance(value, list): return ''
    out = []
    for block in value:
        if not isinstance(block, dict): continue
        kind = block.get('type', '')
        if kind in ('text', 'input_text', 'output_text'):
            text = block.get('text', '')
            if isinstance(text, str): out.append(text)
        elif kind == 'tool_use': out.append('Tool: ' + str(block.get('name', '')) + '\n' + json.dumps(block.get('input', {}), ensure_ascii=False))
        elif kind == 'tool_result': out.append('Tool result:\n' + content(block.get('content', '')))
        elif kind in ('image', 'input_image'): out.append('[Image attachment]')
    return '\n'.join(out)


def native_record(row, agent, limit=64000):
    if agent == 'codex':
        p = row.get('payload', {})
        if not isinstance(p,dict): return None
        if row.get('type') != 'response_item': return None
        kind = p.get('type')
        if p.get('channel') not in (None, '', 'final', 'commentary'): return None
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
    return dict(role=role, text=body if limit is None else body[:limit], truncated=limit is not None and len(body)>limit, timestamp=row.get('timestamp', ''))


def native_metadata(file, agent, workspace=None, max_bytes=LIMIT):
    cid = cwd = title = ''
    codex_header_seen = False
    with open(file, 'rb') as f:
        for _ in range(80):
            if f.tell() >= max_bytes: break
            line = f.readline(max_bytes + 1)
            if not line or len(line)>max_bytes: break
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
            item = native_record(row, agent)
            if item and item['role'] == 'user' and not title: title = item['text'].strip().replace('\n', ' ')[:160]
            if cid and cwd and title: break
    if not UUID.fullmatch(str(cid)) or not isinstance(cwd, str) or not cwd: return None
    cwd = os.path.realpath(cwd)
    if workspace is not None and cwd != workspace: return None
    return dict(id=cid, title=title or 'Saved conversation', modified=os.stat(file).st_mtime, agent=agent, cwd=cwd)

