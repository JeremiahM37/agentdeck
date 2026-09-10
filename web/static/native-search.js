export function openNativeSearch({api, targets = []}) {
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
    <section class="ns-reader" hidden><div class="ns-reader-head"><button class="ns-back">Back to results</button><p class="ns-location"></p></div><p class="ns-read-status" role="status"></p><div class="nh-messages"></div></section>`;
  const $ = selector => root.querySelector(selector);
  for (const target of targets) {
    const option = document.createElement('option'); option.value = target.id; option.textContent = target.name;
    $('.ns-target').append(option);
  }
  let closed = false, generation = 0, readGeneration = 0, job = null, timer = null, starting = false;
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
  async function read(hit, id) {
    const version = ++readGeneration; selectedResult = hit.id;
    $('.ns-browse').hidden = true; $('.ns-reader').hidden = false;
    $('.ns-location').textContent = `${hit.target} · ${hit.agent} · ${hit.cwd}`;
    $('.ns-read-status').textContent = 'Loading matching message…'; $('.nh-messages').replaceChildren(); $('.ns-back').focus();
    try {
      const page = await api(`/conversation-search/${id}/results/${hit.id}`);
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
      $('.ns-read-status').textContent = `Matching message with nearby context${page.changed_neighbors ? ` · ${page.changed_neighbors} changed messages omitted` : ''}${!page.index_complete ? ' · indexing is incomplete' : ''}.`;
      $('.ns-match')?.scrollIntoView({block:'center'});
    } catch (error) {
      if (!closed && version === readGeneration) $('.ns-read-status').textContent = `${error.message}. Return to results and search again.`;
    }
  }
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
