// On-demand executable lookup, never an agent launch or authentication probe.
export function openAgentCommands({api, target}) {
  const prior = document.activeElement;
  const controller = new AbortController();
  const dialog = document.createElement('dialog');
  dialog.className = 'agent-commands';
  dialog.setAttribute('aria-label', 'Agent commands');
  dialog.innerHTML = `<header><h2>Agent commands</h2><button class="b ac-close">Close</button></header>
    <p class="ac-target"></p><p>Checks commands on this target’s default PATH. Project and launch profile overrides may differ. This does not check login or model access.</p>
    <div class="ac-status" role="status"></div><ul class="ac-results"></ul><button class="b ac-retry">Check again</button>`;
  const $ = s => dialog.querySelector(s);
  $('.ac-target').textContent = target.name;
  let closed = false;
  function close() { closed = true; controller.abort(); dialog.close(); dialog.remove(); if (prior?.isConnected) prior.focus(); }
  async function load() {
    $('.ac-retry').disabled = true;
    $('.ac-status').textContent = 'Checking target commands…';
    $('.ac-results').replaceChildren();
    try {
      const rows = await api(`/targets/${target.id}/agents`, {signal: controller.signal});
      if (closed) return;
      for (const row of rows) {
        const item = document.createElement('li');
        const name = document.createElement('strong');
        name.textContent = row.name;
        const detail = document.createElement('span');
        const labels = {available: 'Found', missing: 'Not found', unchecked: 'Not checked'};
        detail.textContent = `${labels[row.state] || 'Not checked'} — ${row.path || row.detail || ''}`;
        item.append(name, detail); $('.ac-results').append(item);
      }
      $('.ac-status').textContent = 'Command lookup complete. No agents were started.';
    } catch (e) { if (!closed) $('.ac-status').textContent = e.message; }
    finally { if (!closed) $('.ac-retry').disabled = false; }
  }
  $('.ac-close').onclick = close;
  dialog.addEventListener('cancel', e => {e.preventDefault(); close();});
  $('.ac-retry').onclick = load;
  document.body.append(dialog); dialog.showModal(); load();
}
