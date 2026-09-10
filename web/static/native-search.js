export function openNativeSearch({api, targets = [], onFork}) {
  const prior = document.activeElement;
  const root = document.createElement('dialog');
  root.className = 'native-history native-search';
  root.setAttribute('aria-label', 'Search saved conversations');
  root.innerHTML = `<header><h2>Search saved conversations</h2><button class="ns-close" aria-label="Close saved conversation search">Close</button></header>
    <section class="ns-browse"><form class="ns-form"><label>Conversation text<input class="ns-query" type="search" required maxlength="500" placeholder="Find something discussed…" autocomplete="off"></label><div class="ns-filters"><label>Target<select class="ns-target" aria-label="Target"><option value="">All targets</option></select></label><label>Agent<select class="ns-agent" aria-label="Agent"><option value="">Claude and Codex</option><option value="claude">Claude</option><option value="codex">Codex</option></select></label><button type="submit" class="ns-submit">Search</button><button type="button" class="ns-stop" hidden>Stop search</button></div></form>
    <p class="ns-status" role="status">Search saved messages across workspaces, including conversations you no longer track.</p>
    <button class="ns-retry" hidden>Retry connection</button><details class="ns-progress" hidden><summary>Target progress</summary><div></div></details>
    <div class="ns-results" aria-label="Saved conversation results"></div>
    <details class="ns-advanced"><summary>Search options</summary><p>If a transcript was rewritten, rebuild its search index. Saved conversations remain unchanged.</p><button class="ns-rebuild">Rebuild and search</button></details></section>
    <section class="ns-reader" hidden><div class="ns-reader-head"><button class="ns-back">Back to results</button><p class="ns-location"></p></div><div class="ns-page-controls"><button class="ns-older">Earlier messages</button><button class="ns-newer">Later messages</button><button class="ns-latest">Latest indexed</button><button class="ns-jump-match">Back to match</button><button class="ns-fork" hidden>Fork conversation</button></div><p class="ns-read-status" role="status"></p><div class="nh-messages"></div><form class="ns-fork-form" hidden><h3>Fork saved conversation</h3><p class="ns-fork-warning"></p><label>Launch settings<select class="ns-fork-config" aria-label="Launch settings" required></select></label><label>Session name<input class="ns-fork-name" value="Conversation fork" maxlength="160"></label><label>Workspace<select class="ns-fork-workspace" aria-label="Fork workspace"><option value="shared">Use the same workspace files</option><option value="isolated">New isolated Git worktree</option></select></label><div class="ns-fork-isolated" hidden><label>Branch (blank = automatic)<input class="ns-fork-branch"></label><label>Base commit or branch (blank = HEAD)<input class="ns-fork-base"></label></div><p class="ns-fork-status" role="status"></p><button type="submit">Create fork</button><button type="button" class="ns-fork-cancel">Cancel fork</button></form></section>`;
  const $ = selector => root.querySelector(selector);
  for (const target of targets) {
    const option = document.createElement('option'); option.value = target.id; option.textContent = target.name;
    $('.ns-target').append(option);
  }
  let closed = false, generation = 0, readGeneration = 0, job = null, timer = null, starting = false;
  let forkPending = false, forkContext = null;
  let lastResult = null, pollGeneration = 0, selectedResult = null;
  const fit = () => root.style.setProperty('--search-height', `${window.visualViewport?.height || innerHeight}px`);
  fit(); window.visualViewport?.addEventListener('resize', fit);
  const cancel = id => api(`/conversation-search/${id}`, {method:'DELETE'});
  function render(result) {
    lastResult = result;
    $('.ns-stop').hidden = result.done;
    const ready = result.results.length;
    const unfinished = result.scopes.filter(s => s.state === 'queued' || s.state === 'indexing').length;
    const problems = result.scopes.filter(s => s.error || s.progress.issues?.length || s.progress.oversized_entries);
    $('.ns-status').textContent = `${ready} ${ready === 1 ? 'conversation' : 'conversations'} found${unfinished ? ` · searching ${unfinished} ${unfinished === 1 ? 'profile' : 'profiles'}…` : result.complete ? '' : ' · some profiles could not be fully searched'}${result.scopes.some(s => s.more) ? ' · more matches available; narrow your search' : ''}${!ready && result.complete ? '. Try different words or filters.' : ''}`;
    $('.ns-progress').hidden = !result.scopes.length;
    $('.ns-progress summary').textContent = `Target progress · ${result.scopes.length} ${result.scopes.length === 1 ? 'profile' : 'profiles'}${problems.length ? ` · ${problems.length} with issues` : ''}`;
    const progress = $('.ns-progress div'); progress.replaceChildren();
    for (const scope of result.scopes) {
      const p = document.createElement('p');
      p.textContent = `${scope.target} · ${scope.agent}: ${scope.error || scope.state} · ${scope.progress.documents} conversations${scope.progress.pending_files ? ` · ${scope.progress.pending_files} pending` : ''}${scope.progress.oversized_entries ? ` · ${scope.progress.oversized_entries} oversized entries skipped` : ''}${scope.progress.issues?.length ? ' · '+scope.progress.issues.join('; ') : ''}`;
      progress.append(p);
    }
    const list = $('.ns-results'), scroll = list.scrollTop, focused = document.activeElement?.dataset.resultId;
    const existing = new Map([...list.children].map(node => [node.dataset.resultId, node]));
    for (const hit of result.results) {
      let button = existing.get(hit.id);
      if (!button) {
        button = document.createElement('button'); button.type = 'button'; button.className = 'ns-result'; button.dataset.resultId = hit.id;
        button.append(document.createElement('strong'), document.createElement('small'), document.createElement('span'));
      }
      button.children[0].textContent = hit.title;
      button.children[1].textContent = `${hit.target} · ${hit.agent} · ${hit.cwd}`;
      button.children[2].textContent = hit.snippet;
      button.onclick = () => read(hit, result.id);
      list.append(button); existing.delete(hit.id);
    }
    for (const node of existing.values()) node.remove();
    if (focused) [...list.children].find(n => n.dataset.resultId === focused)?.focus({preventScroll:true});
    list.scrollTop = scroll;
  }
  async function poll(id, version) {
    const request = ++pollGeneration;
    try {
      const result = await api(`/conversation-search/${id}`);
      if (closed || version !== generation || request !== pollGeneration) return;
      $('.ns-retry').hidden = true; render(result);
      if (!result.done) timer = setTimeout(() => poll(id, version), 600);
    } catch (error) {
      if (closed || version !== generation || request !== pollGeneration) return;
      $('.ns-status').textContent = `Could not update search: ${error.message}. Available results are retained.`;
      $('.ns-retry').hidden = false;
    }
  }
  async function start(reset = false) {
    if (starting || !$('.ns-form').reportValidity()) return;
    const version = ++generation, previous = job; job = null; starting = true;
    clearTimeout(timer); $('.ns-submit').disabled = true; $('.ns-rebuild').disabled = true;
    $('.ns-stop').hidden = true; $('.ns-retry').hidden = true;
    $('.ns-status').textContent = 'Starting search…';
    try {
      if (previous && !lastResult?.done) { try { await cancel(previous); } catch (error) { if (error.status !== 404) throw error; } }
      if (closed || version !== generation) return;
      const result = await api('/conversation-search', {method:'POST', body:{query:$('.ns-query').value, target_id:Number($('.ns-target').value) || undefined, agent:$('.ns-agent').value || undefined, reset}});
      if (closed || version !== generation) { cancel(result.id).catch(() => {}); return; }
      job = result.id; render(result);
      if (!result.done) timer = setTimeout(() => poll(job, version), 300);
    } catch (error) {
      if (!closed && version === generation) {
        job = previous;
        $('.ns-status').textContent = `Could not start search: ${error.message}`;
        $('.ns-stop').hidden = !job || lastResult?.done;
      }
    } finally {
      starting = false; $('.ns-submit').disabled = false; $('.ns-rebuild').disabled = false;
    }
  }
  async function read(hit, id, query = '') {
    const version = ++readGeneration; selectedResult = hit.id;
    for (const button of root.querySelectorAll('.ns-page-controls button')) button.disabled = true;
    $('.ns-browse').hidden = true; $('.ns-reader').hidden = false; $('.ns-fork-form').hidden = true; $('.nh-messages').hidden = false; $('.ns-page-controls').hidden = false;
    $('.ns-location').textContent = `${hit.target} · ${hit.agent} · ${hit.cwd}`;
    $('.ns-read-status').textContent = 'Loading matching message…'; $('.nh-messages').replaceChildren(); $('.ns-back').focus();
    try {
      const page = await api(`/conversation-search/${id}/results/${hit.id}${query}`);
      if (closed || version !== readGeneration) return;
      for (const message of page.messages) {
        const card = document.createElement(message.role === 'tool' ? 'details' : 'article');
        card.className = `nh-message nh-${message.role}`;
        if (message.matched) { card.classList.add('ns-match'); if (message.role === 'tool') card.open = true; }
        const heading = document.createElement(message.role === 'tool' ? 'summary' : 'h3');
        heading.textContent = `${message.role === 'user' ? 'You' : message.role === 'tool' ? 'Tool activity' : 'Assistant'}${message.matched ? ' · Matching message' : ''}`;
        const text = document.createElement('pre'); text.textContent = message.text; card.append(heading, text);
        if (message.truncated) { const note = document.createElement('small'); note.textContent = 'Long message shortened in this view.'; card.append(note); }
        $('.nh-messages').append(card);
      }
      $('.ns-older').disabled = page.before == null; $('.ns-newer').disabled = page.after == null;
      $('.ns-latest').disabled = false; $('.ns-jump-match').disabled = false;
      forkContext = {hit,id,choices:(page.fork_options || []).filter(option => option.supported)};
      $('.ns-fork').hidden = !forkContext.choices.length; $('.ns-fork').disabled = false;
      $('.ns-older').onclick = () => read(hit,id,'?before='+page.before);
      $('.ns-newer').onclick = () => read(hit,id,'?after='+page.after);
      $('.ns-latest').onclick = () => read(hit,id,'?latest=1');
      $('.ns-jump-match').onclick = () => read(hit,id);
      $('.ns-read-status').textContent = `${page.page_mode === 'latest' ? 'Latest indexed messages' : page.page_mode && page.page_mode !== 'match' ? 'Saved messages' : 'Matching message with nearby context'}${page.changed_neighbors ? ` · ${page.changed_neighbors} changed messages omitted` : ''}${!page.index_complete ? ' · indexing is incomplete' : ''}.`;
      $('.nh-messages').scrollTop = 0;
      $('.ns-match')?.scrollIntoView({block:'center'});
    } catch (error) {
      if (!closed && version === readGeneration) $('.ns-read-status').textContent = `${error.message}. Return to results and search again.`;
    }
  }
  function forkWarning() {
    const isolated = $('.ns-fork-workspace').value === 'isolated';
    $('.ns-fork-isolated').hidden = !isolated;
    $('.ns-fork-warning').textContent = 'Fork the whole saved conversation, including messages after the match. '+(isolated ? 'Start in a new Git worktree from the selected committed base; uncommitted changes stay in the original workspace.' : 'Both conversations will use the same workspace files.')+' The original conversation and terminal stay intact.';
  }
  $('.ns-fork').onclick = () => {
    if (!forkContext?.choices.length) return;
    const select = $('.ns-fork-config'); select.replaceChildren();
    if (forkContext.choices.length > 1) { const option=document.createElement('option'); option.value=''; option.textContent='Choose launch settings'; select.append(option); }
    for (const choice of forkContext.choices) { const option=document.createElement('option'); option.value=choice.id; option.textContent=choice.label+(choice.model ? ' · '+choice.model : ''); select.append(option); }
    $('.ns-fork-status').textContent=''; $('.ns-fork-form').hidden=false; $('.nh-messages').hidden=true; $('.ns-page-controls').hidden=true; forkWarning(); select.focus();
  };
  $('.ns-fork-workspace').onchange=forkWarning;
  $('.ns-fork-cancel').onclick=()=>{ $('.ns-fork-form').hidden=true; $('.nh-messages').hidden=false; $('.ns-page-controls').hidden=false; $('.ns-fork').focus(); };
  $('.ns-fork-form').onsubmit=async event=>{
    event.preventDefault(); if(forkPending || !forkContext) return;
    const context=forkContext;
    const body={configuration_id:$('.ns-fork-config').value,name:$('.ns-fork-name').value};
    if($('.ns-fork-workspace').value==='isolated') body.worktree={branch:$('.ns-fork-branch').value,base:$('.ns-fork-base').value};
    forkPending=true; const disabled=[];
    for(const control of root.querySelectorAll('button,input,select')){disabled.push([control,control.disabled]);control.disabled=true;}
    $('.ns-fork-status').textContent='Starting fork…';
    try {
      const session=await api(`/conversation-search/${context.id}/results/${context.hit.id}/fork`,{method:'POST',body});
      root.close(); onFork?.(session);
    } catch(error) {if(!closed) $('.ns-fork-status').textContent=error.message;}
    finally {forkPending=false;for(const [control,value] of disabled)control.disabled=value;}
  };
  root.addEventListener('cancel',event=>{if(forkPending)event.preventDefault();});
  $('.ns-form').onsubmit = event => { event.preventDefault(); start(); };
  $('.ns-rebuild').onclick = () => start(true);
  $('.ns-retry').onclick = () => { if (job) { clearTimeout(timer); poll(job, generation); } };
  $('.ns-stop').onclick = async () => {
    if (!job) return; const id = job, version = generation;
    try { const result = await cancel(id); if (!closed && version === generation) { clearTimeout(timer); pollGeneration++; render(result); if (!result.done) poll(id, version); } }
    catch (error) { if (!closed && version === generation) $('.ns-status').textContent = `Could not stop search: ${error.message}`; }
  };
  $('.ns-back').onclick = () => { readGeneration++; $('.ns-reader').hidden = true; $('.ns-browse').hidden = false; [...$('.ns-results').children].find(n => n.dataset.resultId === selectedResult)?.focus({preventScroll:true}); };
  root.addEventListener('keydown', event => {
    event.stopPropagation();
    if (event.isComposing) return;
    const buttons = [...$('.ns-results').children];
    if ((event.key === 'ArrowDown' || event.key === 'ArrowUp') && (event.target === $('.ns-query') || buttons.includes(event.target))) {
      event.preventDefault(); const index = buttons.indexOf(event.target);
      buttons[Math.max(0, Math.min(buttons.length-1, index+(event.key === 'ArrowDown' ? 1 : -1)))]?.focus();
    }
  });
  $('.ns-close').onclick = () => root.close();
  root.addEventListener('close', () => {
    closed = true; generation++; readGeneration++; clearTimeout(timer);
    if (job && !lastResult?.done) cancel(job).catch(() => {});
    window.visualViewport?.removeEventListener('resize', fit); root.remove(); if (prior?.isConnected) prior.focus();
  });
  document.body.append(root); root.showModal(); $('.ns-query').focus(); return root;
}
