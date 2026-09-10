export function openNativeHistory({id, name, api, onFork, onResume}) {
  const prior=document.activeElement, root=document.createElement('dialog');
  root.className='native-history';root.setAttribute('aria-label','Saved conversations');
  root.innerHTML=`<header><div><h2>Saved conversations</h2><p class="nh-name"></p></div><button class="nh-close" aria-label="Close saved conversations">×</button></header><p class="nh-explain">Choose a saved conversation from this workspace. This selection does not change the running terminal.</p><div class="nh-controls"><select aria-label="Conversation" class="nh-select"></select><button class="nh-refresh">Refresh</button><button class="nh-fork">Fork conversation</button><button class="nh-resume" hidden>Resume conversation</button></div><p class="nh-status" role="status"></p><div class="nh-confirm" hidden><p>Create an independent conversation with this history? Both agents will use the same workspace files. The original keeps running.</p><label>New session name <input class="nh-fork-name"></label><div class="nh-fork-workspace"><label>Workspace <select class="nh-workspace"><option value="shared">Use the same files</option><option value="isolated">New isolated Git worktree</option></select></label><div class="nh-worktree-fields" hidden><label>New branch (blank = automatic) <input class="nh-branch"></label><label>Base commit or branch (blank = HEAD) <input class="nh-base"></label></div></div><button class="nh-create">Create fork</button><button class="nh-cancel">Cancel</button></div><div class="nh-messages" tabindex="0" aria-label="Conversation messages"></div><button class="nh-older" hidden>Load earlier messages</button>`;
  const $=s=>root.querySelector(s);$('.nh-name').textContent=name;$('.nh-fork-name').value=(name||'Session')+' · fork';
  let closed=false, version=0, cid='', before=null, pending=false, loading=false, forkSupported=false, resumeSupported=false, action='fork';
  function controls(){const confirming=!$('.nh-confirm').hidden;$('.nh-messages').hidden=confirming;$('.nh-controls').hidden=confirming;$('.nh-explain').hidden=confirming;$('.nh-older').hidden=confirming||before==null||loading||!cid; $('.nh-fork').disabled=!cid||loading||pending||!forkSupported;$('.nh-resume').disabled=!cid||loading||pending||!resumeSupported;$('.nh-create').disabled=pending||loading||!cid||!(action==='resume'?resumeSupported:forkSupported);$('.nh-older').disabled=pending||loading;$('.nh-select').disabled=pending;$('.nh-refresh').disabled=pending;$('.nh-close').disabled=pending;$('.nh-cancel').disabled=pending;for(const field of root.querySelectorAll('.nh-confirm input,.nh-confirm select'))field.disabled=pending; }
  function render(messages,prepend=false){
    const log=$('.nh-messages'),fragment=document.createDocumentFragment();
    for(const message of messages){
      const card=document.createElement(message.role==='tool'?'details':'article');card.className='nh-message nh-'+message.role;
      const label=document.createElement(message.role==='tool'?'summary':'h3');label.textContent=message.role==='user'?'You':message.role==='assistant'?'Assistant':'Tool activity';card.appendChild(label);
      const text=document.createElement('pre');text.textContent=message.text;card.appendChild(text);
      if(message.truncated){const note=document.createElement('small');note.textContent='Long message shortened in this view.';card.appendChild(note);}
      fragment.appendChild(card);
    }
    if(prepend){const old=log.scrollHeight;log.prepend(fragment);log.scrollTop+=log.scrollHeight-old;}else{log.replaceChildren(fragment);log.scrollTop=log.scrollHeight;}
    if(!log.children.length)log.textContent='No readable messages in this window.';
  }
  async function read(older=false){
    if(!cid){version++;loading=false;before=null;$('.nh-messages').replaceChildren();$('.nh-older').hidden=true;$('.nh-status').textContent='Choose a saved conversation to see available actions.';controls();return;}const generation=++version, chosen=cid;loading=true;controls();$('.nh-status').textContent='Loading saved messages…';
    try{
      const page=await api(`/sessions/${id}/conversations/${chosen}${older?'?before='+before:''}`);
      if(closed||generation!==version)return;
      before=page.before;render(page.messages,older);$('.nh-older').hidden=before==null;
      $('.nh-status').textContent=`${page.conversation.agent} · ${chosen}`;
    }catch(e){if(!closed&&generation===version){$('.nh-status').textContent=e.message;$('.nh-messages').replaceChildren();$('.nh-older').hidden=true;}}
    finally{if(generation===version){loading=false;controls();}}
  }
  async function list(){
    const generation=++version;loading=true;forkSupported=false;resumeSupported=false;$('.nh-confirm').hidden=true;$('.nh-older').hidden=true;controls();$('.nh-status').textContent='Finding saved conversations…';
    try{
      const data=await api(`/sessions/${id}/conversations`);if(closed||generation!==version)return;
      forkSupported=!!data.fork_supported;resumeSupported=!!data.resume_supported;$('.nh-resume').hidden=!resumeSupported;$('.nh-select').replaceChildren();const placeholder=document.createElement('option');placeholder.value='';placeholder.textContent='Choose a saved conversation';$('.nh-select').appendChild(placeholder);
      for(const c of data.conversations){const option=document.createElement('option');option.value=c.id;option.textContent=`${c.id===data.current?.id?'Current terminal · ':''}${new Date(c.modified*1000).toLocaleString()} · ${c.title} · ${c.id.slice(0,8)}`;$('.nh-select').appendChild(option);}
      if(!cid&&data.current?.state==='identified'&&data.current.saved)cid=data.current.id;
      if(data.conversations.some(c=>c.id===cid))$('.nh-select').value=cid;
      cid=$('.nh-select').value;loading=false;controls();
      if(cid)await read();else{$('.nh-status').textContent=data.conversations.length?'Choose a saved conversation to see available actions.':'No saved conversations found in this workspace.';$('.nh-messages').replaceChildren();}
      $('.nh-explain').textContent=data.current?.state==='identified'?(data.current.saved?'The current terminal’s conversation is marked in the list. You can choose another saved conversation.':'The current terminal has not saved readable messages yet. You can choose another conversation.') :data.current?.state==='ambiguous'?'Several conversations are active in this terminal. Choose the one you want explicitly.':'The current conversation could not be identified. Choose a saved conversation explicitly.';
      if(data.scan_limited)$('.nh-explain').textContent+=' Showing up to 500 discovered transcript files, prioritizing the current terminal.';
    }catch(e){if(!closed&&generation===version){$('.nh-status').textContent=e.message;loading=false;controls();}}
  }
  $('.nh-select').onchange=()=>{cid=$('.nh-select').value;$('.nh-confirm').hidden=true;read();};
  $('.nh-refresh').onclick=list;$('.nh-older').onclick=()=>read(true);
  function workspaceWarning(){
    const isolated=action==='fork'&&$('.nh-workspace').value==='isolated';
    $('.nh-fork-workspace').hidden=action!=='fork';$('.nh-worktree-fields').hidden=!isolated;
    $('.nh-confirm p').textContent=action==='resume'?'Continue this same saved conversation in its original workspace? The previous terminal must be stopped.':isolated?'Fork the saved history into a new Git worktree from the selected committed base. Uncommitted changes stay in the original workspace. The original conversation is unchanged.':'Create an independent conversation with this history? Both agents will use the same workspace files. The original conversation is unchanged.';
  }
  function confirmAction(kind){action=kind;workspaceWarning();$('.nh-create').textContent=kind==='resume'?'Start resumed session':'Create fork';$('.nh-fork-name').value=(name||'Session')+(kind==='resume'?' · resumed':' · fork');$('.nh-confirm').hidden=false;controls();$('.nh-fork-name').focus();}
  $('.nh-workspace').onchange=workspaceWarning;
  $('.nh-fork').onclick=()=>confirmAction('fork');$('.nh-resume').onclick=()=>confirmAction('resume');
  $('.nh-cancel').onclick=()=>{$('.nh-confirm').hidden=true;controls();};
  $('.nh-create').onclick=async()=>{
    if(pending||loading||!cid||!(action==='resume'?resumeSupported:forkSupported))return;pending=true;controls();$('.nh-status').textContent=action==='resume'?'Resuming conversation…':'Starting fork…';
    const body={conversation_id:cid,name:$('.nh-fork-name').value};if(action==='fork'&&$('.nh-workspace').value==='isolated'){body.background=true;body.worktree={branch:$('.nh-branch').value,base:$('.nh-base').value};}
    try{const session=await api(`/sessions/${id}/${action}`,{method:'POST',body});if(!closed){root.close();if(action==='resume')onResume?.(session);else onFork?.(session);}}
    catch(e){if(!closed)$('.nh-status').textContent=e.message;}
    finally{pending=false;controls();}
  };
  $('.nh-close').onclick=()=>root.close();root.addEventListener('keydown',e=>e.stopPropagation());
  root.addEventListener('cancel',e=>{if(pending)e.preventDefault();});root.addEventListener('close',()=>{closed=true;version++;root.remove();prior?.focus();});
  document.body.appendChild(root);root.showModal();list();return root;
}
