"""Open a verified indexed match, with neighboring visible messages."""
import argparse, json, os, secrets, sqlite3, stat
from pathlib import Path
if 'NativeSearchIndex' not in globals():
    from native_search import NativeSearchIndex, LINE_LIMIT, message_fingerprint, marked_excerpt
    from native_records import native_metadata, native_record

def read_match(index,document,cid,cwd,offset,fingerprint,query):
    index.db.execute('BEGIN')
    try:
        return _read_match(index,document,cid,cwd,offset,fingerprint,query)
    finally:
        index.db.rollback()

def _read_match(index,document,cid,cwd,offset,fingerprint,query):
    doc=index.db.execute('SELECT * FROM documents WHERE id=? AND cid=? AND cwd=?',(document,cid,cwd)).fetchone()
    if not doc:raise ValueError('Search result is stale; run the search again')
    path=Path(doc['path']).resolve(strict=True)
    base=index.home/('sessions' if index.agent=='codex' else 'projects')
    if base.resolve() not in path.parents or path.suffix!='.jsonl':raise ValueError('Conversation is outside this native profile')
    selected=index.db.execute('SELECT * FROM messages WHERE doc=? AND offset=? AND fingerprint=?',(document,offset,fingerprint)).fetchone()
    if not selected:raise ValueError('Matched message changed; run the search again')
    previous=index.db.execute('SELECT offset,end_offset,fingerprint FROM messages WHERE doc=? AND offset<? ORDER BY offset DESC LIMIT 5',(document,offset)).fetchall()
    following=index.db.execute('SELECT offset,end_offset,fingerprint FROM messages WHERE doc=? AND offset>? ORDER BY offset LIMIT 5',(document,offset)).fetchall()
    rows=list(reversed(previous))+[selected]+list(following);messages=[];changed=0
    with path.open('rb') as file:
        st=os.fstat(file.fileno())
        if not stat.S_ISREG(st.st_mode) or (st.st_dev,st.st_ino)!=(doc['device'],doc['inode']):raise ValueError('Conversation file changed; run the search again')
        info=native_metadata(path,index.agent,cwd,max_bytes=LINE_LIMIT)
        if not info or info['id']!=cid:raise ValueError('Conversation identity changed; run the search again')
        for row in rows:
            if row['offset']:
                file.seek(row['offset']-1)
                if file.read(1)!=b'\n':raise ValueError('Conversation layout changed; run the search again')
            file.seek(row['offset']);line=file.readline(LINE_LIMIT+1)
            message=None
            try:
                if line.endswith(b'\n') and len(line)<=LINE_LIMIT:
                    raw=json.loads(line)
                    if isinstance(raw,dict):message=native_record(raw,index.agent,limit=None)
            except (ValueError,TypeError):pass
            if not message or message_fingerprint(message)!=row['fingerprint']:
                if row['offset']==offset:raise ValueError('Matched message changed; run the search again')
                changed+=1;continue
            matched=row['offset']==offset;truncated=len(message['text'])>64000
            if matched and truncated:
                marker='adk-'+secrets.token_hex(12);opening='['+marker+']';closing='[/'+marker+']'
                match=' AND '.join('"'+term.replace('"','""')+'"' for term in query.strip().split())
                highlighted=index.db.execute('SELECT highlight(message_search,0,?,?) FROM message_search WHERE rowid=? AND message_search MATCH ?',(opening,closing,selected['id'],match)).fetchone()
                if not highlighted:raise ValueError('Search text changed; run the search again')
                message['text'],truncated=marked_excerpt(highlighted[0],opening,closing,64000)
            else:message['text']=message['text'][:64000]
            message.update(offset=row['offset'],matched=matched,truncated=truncated);messages.append(message)
        current=path.stat()
        if (current.st_dev,current.st_ino)!=(st.st_dev,st.st_ino):raise ValueError('Conversation file changed; run the search again')
    return dict(conversation=info,messages=messages,changed_neighbors=changed,
        context_before_count=sum(m['offset']<offset for m in messages),context_after_count=sum(m['offset']>offset for m in messages),index_complete=not bool(doc['pending']))

def main():
    parser=argparse.ArgumentParser();parser.add_argument('agent',choices=('codex','claude'))
    parser.add_argument('document',type=int);parser.add_argument('cid');parser.add_argument('cwd');parser.add_argument('offset',type=int)
    parser.add_argument('fingerprint');parser.add_argument('profile_key');parser.add_argument('query')
    args=parser.parse_args();index=None
    try:
        home=os.path.expanduser(os.environ.get('CODEX_HOME','~/.codex') if args.agent=='codex' else os.environ.get('CLAUDE_CONFIG_DIR','~/.claude'))
        cache=os.environ.get('AGENTDECK_NATIVE_SEARCH_CACHE') or str(Path(os.environ.get('XDG_CACHE_HOME',str(Path.home()/'.cache')))/'agentdeck')
        index=NativeSearchIndex(cache,home,args.agent)
        if index.path.stem!=args.profile_key:raise ValueError('Native profile changed; run the search again')
        print(json.dumps(read_match(index,args.document,args.cid,args.cwd,args.offset,args.fingerprint,args.query),ensure_ascii=False))
    except (OSError,ValueError,sqlite3.Error) as error:
        print(json.dumps(dict(error=str(error))));return 1
    finally:
        if index:index.close()
    return 0

if __name__=='__main__':raise SystemExit(main())
