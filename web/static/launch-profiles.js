export function openLaunchProfiles({api, onChange = () => {}}) {
  const prior = document.activeElement;
  const dialog = document.createElement('dialog');
  dialog.className = 'launch-profiles';
  dialog.setAttribute('aria-label', 'Launch profiles');
  dialog.innerHTML = `<header><h2>Launch profiles</h2><button type="button" class="lp-close">Close</button></header>
    <p>Reusable settings for new sessions. Existing sessions keep the settings they started with.</p>
    <div class="lp-picker"><label>Saved profile<select class="lp-select" aria-label="Saved profile"><option value="">New profile</option></select></label><button type="button" class="lp-new">New profile</button></div>
    <form><label>Name<input class="lp-name" required maxlength="120" autocomplete="off"></label>
    <label>Agent<select class="lp-agent" aria-label="Agent" required></select></label>
    <label>Command override<input class="lp-command" maxlength="4096" placeholder="Use the agent's configured command" autocomplete="off"></label>
    <label>Default model<input class="lp-model" maxlength="256" placeholder="Use the agent's default" autocomplete="off"></label>
    <label>Environment (JSON)<textarea class="lp-env" rows="5" spellcheck="false">{}</textarea></label>
    <p class="lp-hint">Profile values override agent and project defaults. Use paths that exist on the target where you launch the session.</p>
    <div class="lp-status" role="status"></div><div class="lp-buttons"><button type="submit" class="lp-save">Save profile</button><button type="button" class="lp-delete" hidden>Delete profile</button><button type="button" class="lp-retry" hidden>Retry loading</button></div></form>`;
  const $ = s => dialog.querySelector(s);
  let mutating = false;
  let rows = [], busy = false, closed = false, selected = '', ready = false;
  const status = text => $('.lp-status').textContent = text;
  function lock(value) {
    busy = value;
    dialog.querySelectorAll('button,input,select,textarea').forEach(el => el.disabled = value);
    $('.lp-save').disabled = value || !ready;
    $('.lp-close').disabled = mutating;
  }
  function close() {
    if (mutating) return;
    closed = true; dialog.close(); dialog.remove(); prior?.focus();
  }
  function choose(id) {
    selected = id;
    const row = rows.find(p => String(p.id) === id);
    $('.lp-select').value = id;
    $('.lp-name').value = row?.name || '';
    $('.lp-agent').value = row?.agent || $('.lp-agent option')?.value || '';
    $('.lp-command').value = row?.command || '';
    $('.lp-model').value = row?.model || '';
    let env = row?.env_json || '{}';
    try { env = JSON.stringify(JSON.parse(env), null, 2); } catch {}
    $('.lp-env').value = env;
    $('.lp-delete').hidden = !row;
    status('');
  }
  function picker() {
    $('.lp-select').replaceChildren(new Option('New profile', ''));
    for (const row of rows) $('.lp-select').append(new Option(`${row.name} · ${row.agent}`, String(row.id)));
  }
  async function load() {
    lock(true); status('Loading profiles…');
    try {
      const [profiles, agents] = await Promise.all([api('/launch-profiles'), api('/agents')]);
      if (closed) return;
      rows = profiles; picker();
      $('.lp-agent').replaceChildren(...agents.map(a => new Option(a.name, a.name)));
      ready = true; $('.lp-retry').hidden = true;
      choose(rows.some(p => String(p.id) === selected) ? selected : '');
    } catch(e) { if (!closed) { status(e.message); $('.lp-retry').hidden = false; } }
    finally { if (!closed) lock(false); }
  }
  $('.lp-close').onclick = close;
  dialog.addEventListener('cancel', e => {e.preventDefault(); close();});
  $('.lp-select').onchange = e => choose(e.target.value);
  $('.lp-new').onclick = () => {choose(''); $('.lp-name').focus();};
  $('.lp-retry').onclick = load;
  $('form').onsubmit = async e => {
    e.preventDefault();
    if (busy || !ready) return;
    let env;
    try {
      env = JSON.parse($('.lp-env').value);
      if (!env || Array.isArray(env) || typeof env !== 'object' || Object.values(env).some(v => typeof v !== 'string')) throw Error();
    } catch { status('Environment must be a JSON object with string values. Your draft is retained.'); return; }
    const body = {name:$('.lp-name').value.trim(), agent:$('.lp-agent').value, command:$('.lp-command').value.trim(), model:$('.lp-model').value.trim(), env_json:JSON.stringify(env)};
    mutating = true; lock(true); status('Saving…');
    try {
      const saved = await api('/launch-profiles' + (selected ? `/${selected}` : ''), {method:selected ? 'PUT' : 'POST', body});
      rows = rows.filter(p => p.id !== saved.id); rows.push(saved); rows.sort((a,b) => a.name.localeCompare(b.name));
      picker(); choose(String(saved.id)); status('Profile saved. Existing sessions keep their captured settings.');
      onChange(saved);
    } catch(e) { status(e.message); }
    finally { mutating = false; lock(false); }
  };
  $('.lp-delete').onclick = async () => {
    if (busy || !selected || !confirm('Delete this reusable profile? Existing sessions and their saved settings remain available.')) return;
    mutating = true; lock(true); status('Deleting…');
    try {
      await api(`/launch-profiles/${selected}`, {method:'DELETE'});
      rows = rows.filter(p => String(p.id) !== selected); picker(); choose('');
      status('Profile deleted. Existing sessions are unchanged.'); onChange();
    } catch(e) { status(e.message); }
    finally { mutating = false; lock(false); }
  };
  document.body.append(dialog); dialog.showModal(); load();
}
