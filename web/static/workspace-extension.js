// Durable repository additions do not replace or detach the session terminal.
let nextDialog = 0;
export function openWorkspaceExtension({api, session, onChange = () => {}}) {
  const prior = document.activeElement;
  const dialog = document.createElement('dialog');
  dialog.className = 'launch-profiles';
  dialog.setAttribute('aria-label', 'Workspace repositories');
  const suffix = ++nextDialog;
  dialog.innerHTML = `<header><h2>Workspace repositories</h2><button type="button" class="we-close">Close</button></header>
    <p>Add a registered project on the same target. Its checkout uses this workspace’s branch and runs its project setup command. Your existing terminal stays available.</p>
    <pre class="we-progress" style="white-space:pre-wrap;overflow-wrap:anywhere"></pre>
    <form><label for="we-project-${suffix}">Project</label><select id="we-project-${suffix}" required name="project"></select>
    <label for="we-base-${suffix}">Base (optional)</label><input id="we-base-${suffix}" name="base" maxlength="512" placeholder="Default branch">
    <p class="we-hook"></p><button type="submit">Add repository</button></form>
    <p class="we-status" role="status"></p><div class="lp-buttons"><button class="we-check" type="button">Refresh progress</button><button class="we-cancel" type="button" hidden>Cancel addition</button><button class="we-recover" type="button" hidden>Check interrupted addition</button></div>`;
  const $ = s => dialog.querySelector(s), select = $('select'), form = $('form');
  const controller = new AbortController();
  let closed = false, timer, projects = [], operation, workspace, submitting = false, projectsLoaded = false, generation = 0;
  const root = `/sessions/${session.id}/worktree`;
  const active = () => operation && ['running', 'recovering'].includes(operation.state);
  function close() { closed = true; clearTimeout(timer); controller.abort(); dialog.close(); dialog.remove(); if (prior?.isConnected) prior.focus(); }
  function hook() { $('.we-hook').textContent = projects.find(p => String(p.id) === select.value)?.setup_cmd || 'No project setup command.'; }
  function render() {
    const selected = select.value;
    select.replaceChildren();
    for (const p of projects) {
      if (p.target_id !== session.target_id || workspace.repositories.some(r => r.project_id === p.id || r.worktree.repo === p.repo_path)) continue;
      const option = document.createElement('option'); option.value = p.id; option.textContent = p.name; select.append(option);
    }
    if ([...select.options].some(o => o.value === selected)) select.value = selected;
    hook();
    const ready = workspace.state === 'ready' && workspace.repositories.every(r => r.worktree.state === 'ready');
    form.hidden = !!active() || !ready || !select.options.length;
    form.querySelector('button').disabled = submitting || !select.options.length;
    $('.we-progress').textContent = workspace.repositories.map(r => `${r.name}: ${r.worktree.state}\n${r.worktree.setup_output || r.worktree.error || ''}`).join('\n');
    $('.we-status').textContent = operation ? `Addition ${operation.id}: ${operation.state}${operation.cancel_requested ? ' · cancellation requested' : ''}${operation.error ? '\n' + operation.error : ''}` : ready ? (select.options.length ? 'Ready to add a repository.' : 'No other projects on this target.') : 'Workspace needs recovery before another repository can be added. Allocated files are retained.';
    if (ready && !active() && !select.options.length && operation) $('.we-status').textContent += '\nNo other projects on this target.';
    $('.we-cancel').hidden = !active(); $('.we-recover').hidden = operation?.state !== 'recovering';
  }
  async function refresh() {
    clearTimeout(timer);
    const request = ++generation;
    try {
      if (!projectsLoaded) { projects = await api('/projects', {signal:controller.signal}); projectsLoaded = true; }
      const [current, ops] = await Promise.all([api(root, {signal: controller.signal}), api(root + '/operations', {signal: controller.signal})]);
      if (closed || request !== generation) return;
      workspace = current; operation = ops[0]; render();
    } catch (e) { if (!closed) $('.we-status').textContent = e.message; }
    finally { if (!closed && request === generation && active()) timer = setTimeout(refresh, 1500); }
  }
  form.onsubmit = async e => {
    e.preventDefault(); if (submitting || !select.value) return;
    submitting = true; form.querySelector('button').disabled = true;
    try {
      operation = await api(root + '/repositories', {method: 'POST', body: {project_id: Number(select.value), base: $('input').value.trim()}});
      if (!closed) await refresh(); onChange();
    } catch (e) { if (!closed) $('.we-status').textContent = e.message; }
    finally { submitting = false; if (!closed) form.querySelector('button').disabled = !select.options.length; }
  };
  async function action(kind) {
    const captured = operation?.id; if (!captured) return;
    $('.we-cancel').disabled = $('.we-recover').disabled = true;
    try { await api(`${root}/operations/${captured}/${kind}`, {method:'POST', body:{}}); if (!closed) await refresh(); onChange(); }
    catch (e) { if (!closed) $('.we-status').textContent = e.message; }
    finally { if (!closed) $('.we-cancel').disabled = $('.we-recover').disabled = false; }
  }
  select.onchange = hook;
  $('.we-close').onclick = close;
  $('.we-check').onclick = refresh;
  $('.we-cancel').onclick = () => action('cancel');
  $('.we-recover').onclick = () => action('recover');
  dialog.addEventListener('cancel', e => { e.preventDefault(); close(); });
  document.body.append(dialog); dialog.showModal();
  form.hidden = true; $('.we-status').textContent = 'Loading workspace…';
  refresh();
}
