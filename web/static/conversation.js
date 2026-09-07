const esc = value => String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const uid = () => globalThis.crypto?.randomUUID?.() || `${Date.now()}-${Math.random().toString(36).slice(2)}`;
let currentClose;

// The reader keeps its DOM and composer while data refreshes. A stream update
// must never erase a draft, move the caret, close the keyboard, or steal scroll.
export function openConversation({kind, id, name, api, attachMic, onClose}) {
  currentClose?.();
  const root = document.createElement('section');
  root.id = 'conversation'; root.className = 'conversation';
  root.setAttribute('role', 'dialog'); root.setAttribute('aria-modal', 'true'); root.setAttribute('aria-labelledby','conversation-title');
  const draftKey = `adk-draft-${kind}-${id}`;
  let draft = {};
  try { draft = JSON.parse(localStorage.getItem(draftKey) || '{}'); } catch {}
  root.innerHTML = `<header class="conversation-head"><div><h2 id="conversation-title">${esc(name)}</h2><p id="conversation-status" role="status">Connecting…</p></div><button class="b" id="conversation-close" aria-label="Close conversation">✕</button></header>
    <div class="reader-controls"><span>${kind === 'session' ? 'Live output · last 500 lines' : 'Task conversation'}</span><button class="b" id="reader-smaller" aria-label="Smaller text">A−</button><button class="b" id="reader-larger" aria-label="Larger text">A+</button><button class="b" id="reader-latest">↓ Latest</button></div>
    <div id="conversation-error" role="status" hidden></div>
    <div id="conversation-log" tabindex="0" aria-label="Agent output"><p class="reader-empty">Loading…</p></div>
    <div id="conversation-approvals"></div>
    <form id="conversation-compose"><label for="conversation-input">Message ${kind === 'session' ? 'this agent' : 'this task'}</label>
      <textarea id="conversation-input" rows="3" maxlength="32000" placeholder="Write a message or use your keyboard’s microphone…"></textarea>
      <p id="conversation-hint">${kind === 'session' ? 'Sends to the same running session. Enter adds a new line.' : 'Follow-ups wait for the current run, then continue in the same worktree.'}</p>
      <div class="compose-actions">${kind === 'task' ? '<label class="interrupt-option"><input id="conversation-interrupt" type="checkbox"> Interrupt and send</label>' : '<button type="button" class="b warn" id="conversation-interrupt-session">Interrupt</button>'}<button type="button" class="b" id="conversation-mic" aria-label="Dictate message">🎙</button><button type="submit" class="b ok grow" id="conversation-send">Send</button></div>
      <p id="conversation-receipt" role="status"></p>
    </form>`;
  document.body.append(root);
  const background=[...document.body.children].filter(el=>el!==root).map(el=>[el,el.inert]);
  background.forEach(([el])=>{el.inert=true;});
  const $ = selector => root.querySelector(selector);
  const input = $('#conversation-input'), log = $('#conversation-log'), status = $('#conversation-status');
  input.value = draft.text || '';
  const interrupt = $('#conversation-interrupt');
  if (interrupt) interrupt.checked = !!draft.interrupt;
  let closed = false, busy = false, sending = false, follow = true, lastSignature = '', latestTask;
  let font = Math.max(16,Math.min(24,Number(localStorage.getItem('adk-reader-font')) || 17));
  const applyFont = () => { root.style.setProperty('--reader-font', `${font}px`); localStorage.setItem('adk-reader-font', font); };
  applyFont();
  $('#reader-smaller').onclick = () => { font=Math.max(16,font-1); applyFont(); };
  $('#reader-larger').onclick = () => { font=Math.min(24,font+1); applyFont(); };
  const bottom = () => { follow = true; log.scrollTop = log.scrollHeight; };
  $('#reader-latest').onclick = bottom;
  log.onscroll = () => { follow=log.scrollHeight-log.clientHeight-log.scrollTop < 70; };
  const persist = () => {
    if (draft.text !== input.value || draft.interrupt !== !!interrupt?.checked) draft.request_id=uid();
    draft.text=input.value; draft.interrupt=!!interrupt?.checked;
    localStorage.setItem(draftKey, JSON.stringify(draft));
  };
  input.addEventListener('input',persist); interrupt?.addEventListener('change',persist);
  input.onkeydown = e => { if (e.key==='Enter' && (e.ctrlKey || e.metaKey) && !e.isComposing) { e.preventDefault(); $('#conversation-compose').requestSubmit(); } };
  attachMic($('#conversation-mic'),input);
  const viewport = () => {
    root.style.height=`${window.visualViewport?.height || innerHeight}px`;
    root.style.top=`${window.visualViewport?.offsetTop || 0}px`;
  };
  viewport(); window.visualViewport?.addEventListener('resize',viewport); window.visualViewport?.addEventListener('scroll',viewport);
  const priorOverflow=document.body.style.overflow; document.body.style.overflow='hidden';
  const priorFocus=document.activeElement;
  const close=()=>{
    if (closed) return; closed=true; persist(); clearInterval(timer);
    window.visualViewport?.removeEventListener('resize',viewport); window.visualViewport?.removeEventListener('scroll',viewport);
    document.body.style.overflow=priorOverflow; background.forEach(([el,was])=>{el.inert=was;}); root.remove(); priorFocus?.focus(); currentClose=null; onClose?.();
  };
  currentClose=close; $('#conversation-close').onclick=close;
  root.onkeydown = e => {
    if (e.key==='Escape') { e.stopPropagation(); close(); }
    if (e.key==='Tab') {
      const els=[...root.querySelectorAll('button:not([disabled]),textarea:not([disabled]),input:not([disabled]),[tabindex="0"]')].filter(e=>e.offsetParent!==null);
      if (e.shiftKey && document.activeElement===els[0]) { e.preventDefault(); els.at(-1).focus(); }
      else if (!e.shiftKey && document.activeElement===els.at(-1)) { e.preventDefault(); els[0].focus(); }
    }
  };
  $('#conversation-close').focus({preventScroll:true});
  const showError = text => { $('#conversation-error').hidden=!text; $('#conversation-error').textContent=text; };
  const updateLog = (signature, fill) => {
    if (signature===lastSignature) return;
    const scroll=log.scrollTop, shouldFollow=follow;
    fill(); lastSignature=signature;
    log.scrollTop=shouldFollow?log.scrollHeight:scroll; follow=shouldFollow;
  };
  async function refresh() {
    if (closed || busy || document.hidden) return;
    busy=true;
    try {
      if (kind==='session') {
        const data=await api(`/sessions/${id}/reader`); if (closed) return;
        status.textContent=`${data.session.agent} · ${data.ended?'Ended':data.session.status} · live reader`;
        $('#conversation-send').disabled=data.ended||sending;
        $('#conversation-interrupt-session').disabled=data.ended;
        updateLog(data.text,()=>{ const pre=document.createElement('pre');pre.className='session-reader';pre.textContent=data.text||'Waiting for agent output…';log.replaceChildren(pre); });
      } else {
        const [task,events,messages,approvals]=await Promise.all([api(`/tasks/${id}`),api(`/tasks/${id}/events`),api(`/tasks/${id}/messages`),api('/approvals?status=pending')]);
        if (closed) return; latestTask=task;
        const lastMessage=messages.at(-1);
        if (lastMessage && !sending && !input.value && lastMessage.status!=='pending') $('#conversation-receipt').textContent=lastMessage.status==='failed' ? `Not delivered: ${lastMessage.error}` : lastMessage.attempt_id ? 'Message delivered to the agent.' : 'Instructions added to the task.';
        status.textContent=`${task.agent} · ${task.status}${task.attempt ? ` · turn ${task.attempt.n}` : ''}`;
        const running=task.status==='running';
        interrupt.disabled=!running || task.target_kind==='sandbox';
        if (interrupt.disabled) interrupt.checked=false;
        $('#conversation-hint').textContent=task.target_kind==='sandbox' && task.status!=='backlog' ? 'Send queues a new sandbox run with your instructions and the previous result.' : task.status==='backlog' ? 'Adds instructions to this task without dispatching it.' : running ? 'Send queues a follow-up. Interrupt and send stops this run first, then continues.' : task.status==='queued' ? 'Your message is added before the queued run starts.' : 'Send continues the task in its existing worktree.';
        const signature=JSON.stringify([task.prompt,events,messages]);
        updateLog(signature,()=>renderTaskLog(log,task,events,messages));
        const ap=$('#conversation-approvals'); ap.replaceChildren();
        for (const a of approvals.filter(a=>a.task_id===id)) {
          const card=document.createElement('div');card.className='reader-approval';
          card.innerHTML=`<strong>Approval needed: ${esc(a.tool_name)}</strong><pre>${esc(JSON.stringify(a.input || a.input_json || {}))}</pre>`;
          for (const [label,decision] of [['Approve','approved'],['Deny','denied']]) {
            const b=document.createElement('button');b.type='button';b.className='b';b.textContent=label;
            b.onclick=async()=>{try{await api(`/approvals/${a.id}/decision`,{method:'POST',body:{decision}});refresh();}catch(e){showError(e.message)}};card.append(b);
          } ap.append(card);
        }
      }
      showError('');
    } catch(e) { if(!closed) showError(`Could not refresh: ${e.message}. Displayed output may be stale.`); }
    finally { busy=false; }
  }
  $('#conversation-interrupt-session')?.addEventListener('click',async()=>{
    try { await api(`/sessions/${id}/send`,{method:'POST',body:{key:'escape'}});$('#conversation-receipt').textContent='Interrupt sent. Check the agent output before sending new instructions.';refresh(); }
    catch(e){showError(e.message)}
  });
  $('#conversation-compose').onsubmit=async e=>{
    e.preventDefault(); if(sending || !input.value.trim()) return;
    persist(); const submitted={...draft,request_id:draft.request_id||uid()};draft.request_id=submitted.request_id;persist();
    sending=true;$('#conversation-send').disabled=true;$('#conversation-receipt').textContent='Sending…';
    try {
      const receipt=await api(kind==='session'?`/sessions/${id}/send`:`/tasks/${id}/messages`,{method:'POST',body:kind==='session'?{text:submitted.text}:submitted});
      if(closed)return;
      // Do not erase text typed while the request was in flight.
      if(input.value===submitted.text){input.value='';draft={};persist();}
      $('#conversation-receipt').textContent=kind==='session'?'Sent to the session.':receipt.status==='delivered'?'Already delivered.':submitted.interrupt?'Saved. Interrupting the current run before continuing.':'Saved. Waiting for delivery to the agent.';
      refresh();
    }catch(e){if(!closed){$('#conversation-receipt').textContent=`Could not confirm delivery: ${e.message}. Your draft is kept.`;}}
    finally{sending=false;if(!closed)$('#conversation-send').disabled=false;}
  };
  const timer=setInterval(refresh,2000);refresh();
}

function renderTaskLog(log,task,events,messages) {
  const opened=new Set([...log.querySelectorAll('details[open]')].map(e=>e.dataset.event));
  const rows=[{time:task.created_at||0,type:'operator',text:task.prompt||task.title,label:'Task'}];
  for(const m of messages)rows.push({time:m.created_at,type:'operator',text:m.text,label:m.status==='pending'?'You · queued for next turn':m.status==='failed'?`Not delivered · ${m.error}`:m.attempt_id?'You · delivered to agent':'You · added to task'});
  for(const e of events){const p=e.payload||{};
    if(e.type==='text')rows.push({time:e.ts,type:'agent',text:p.text,label:`Agent · turn ${e.attempt_n}`});
    else if(e.type==='result')rows.push({time:e.ts,type:'agent',text:p.result||p.text||JSON.stringify(p),label:`Result · turn ${e.attempt_n}`});
    else if(['tool_use','tool_result','verify'].includes(e.type))rows.push({time:e.ts,type:'detail',text:typeof p.content==='string'?p.content:JSON.stringify(p,null,2),label:e.type==='tool_use'?p.name||'Tool':e.type.replace('_',' '),id:String(e.id)});
  }
  rows.sort((a,b)=>(a.time||0)-(b.time||0));
  const fragment=document.createDocumentFragment();
  for(const r of rows){const el=document.createElement(r.type==='detail'?'details':'article');el.className=`reader-message ${r.type}`;
    if(r.type==='detail'){el.dataset.event=r.id;el.open=opened.has(r.id);el.innerHTML=`<summary>${esc(r.label)}</summary><pre></pre>`;el.querySelector('pre').textContent=r.text;}
    else{el.innerHTML=`<div class="reader-speaker">${esc(r.label)}</div><div class="reader-text"></div>`;el.querySelector('.reader-text').textContent=r.text;}
    fragment.append(el);
  }
  log.replaceChildren(fragment);
}
