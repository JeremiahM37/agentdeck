// Terminal frames live outside the board's replaceable DOM. Selecting a tab
// changes visibility only; it never reparents/reloads an existing frame.
const STORAGE = 'adk-terminal-tabs-v1';
function terminalPath(url) {
  const parsed = new URL(url, location.origin);
  const path = parsed.pathname.replace(/^\/term\//, '/terminal/').replace(/\/$/, '');
  if (parsed.origin !== location.origin || !/^\/terminal\/(session|attempt|project)\/[1-9]\d*$/.test(path))
    throw Error('This terminal does not have a valid AgentDeck address.');
  return path;
}

export class TerminalTabs {
  constructor(root, {activate, browse}) {
    this.root = root;
    this.root.inert = true;
    this.activate = activate;
    this.tabs = new Map();
    this.active = null;
    root.innerHTML = `<div class="terminal-tabbar">
      <div class="terminal-tablist" role="tablist" aria-label="Open terminals"></div>
      <a class="b terminal-popout" target="_blank" rel="noopener" title="Open this terminal in a separate browser tab" hidden>Pop out ↗</a>
    </div><div class="terminal-panels"></div>
    <div class="terminal-empty"><h2>No open terminals</h2>
      <p>Attach a session or open a project shell. Its terminal stays here while you switch around AgentDeck.</p>
      <button class="b ok">Open sessions</button></div>`;
    root.querySelector('.terminal-empty button').onclick = browse;
    this.list = root.querySelector('.terminal-tablist');
    this.panels = root.querySelector('.terminal-panels');
    this.popout = root.querySelector('.terminal-popout');
    this.empty = root.querySelector('.terminal-empty');
    try {
      const saved = JSON.parse(sessionStorage.getItem(STORAGE) || '{}');
      for (const tab of (Array.isArray(saved.tabs) ? saved.tabs : []).slice(0, 30)) {
        try { this.add(terminalPath(tab.path), tab.label); } catch {}
      }
      this.active = this.tabs.has(saved.active) ? saved.active : this.tabs.keys().next().value || null;
    } catch {}
    this.render();
  }
  add(path, label) {
    if (this.tabs.has(path)) return this.tabs.get(path);
    const tab = {path, label: String(label || 'Terminal').slice(0, 160)};
    this.tabs.set(path, tab);
    return tab;
  }
  open(url, label) {
    const path = terminalPath(url);
    const tab = this.add(path, label);
    if (label) tab.label = String(label).slice(0, 160);
    this.select(path);
  }
  select(path) {
    if (!this.tabs.has(path)) return;
    this.active = path;
    this.save();
    this.activate();
  }
  show() {
    this.root.hidden = false;
    this.root.inert = false;
    this.render();
    const tab = this.tabs.get(this.active);
    if (!tab) return;
    if (!tab.frame) {
      const panel = document.createElement('div');
      panel.className = 'terminal-tabpanel';
      panel.id = this.panelID(tab.path);
      panel.setAttribute('role', 'tabpanel');
      panel.setAttribute('aria-labelledby', this.buttonID(tab.path));
      const frame = document.createElement('iframe');
      frame.title = tab.label + ' terminal';
      frame.src = tab.path + '?embed=1';
      frame.allow = 'clipboard-read; clipboard-write';
      panel.appendChild(frame);
      this.panels.appendChild(panel);
      tab.panel = panel; tab.frame = frame;
      frame.onload = () => this.notifyVisible(tab);
    }
    for (const entry of this.tabs.values()) {
      if (entry.panel) {
        entry.panel.hidden = entry !== tab;
        entry.panel.inert = entry !== tab;
      }
    }
    this.notifyVisible(tab);
    this.list.querySelector('[aria-selected="true"]')?.scrollIntoView({block:'nearest', inline:'nearest'});
  }
  hide() { this.root.hidden = true; this.root.inert = true; }
  notifyVisible(tab) {
    if (!this.root.hidden && tab.path === this.active)
      tab.frame?.contentWindow?.postMessage({type:'adk-terminal-visible'}, location.origin);
  }
  close(path) {
    const tab = this.tabs.get(path);
    if (!tab) return;
    const keys = [...this.tabs.keys()];
    const index = keys.indexOf(path);
    // Removing an iframe disconnects only its ttyd client. No kill/end API call.
    tab.panel?.remove();
    this.tabs.delete(path);
    if (this.active === path) this.active = keys[index + 1] || keys[index - 1] || null;
    this.save();
    if (!this.root.hidden) this.activate();
    else this.render();
    this.list.querySelector('[aria-selected="true"]')?.focus({preventScroll:true});
  }
  buttonID(path) { return 'terminal-tab-' + path.split('/').slice(-2).join('-'); }
  panelID(path) { return this.buttonID(path) + '-panel'; }
  hash() { return this.active ? '#terminals/' + this.active.split('/').slice(-2).join('/') : '#terminals'; }
  save() {
    try {
      sessionStorage.setItem(STORAGE, JSON.stringify({active:this.active,
        tabs:[...this.tabs.values()].map(({path,label}) => ({path,label}))}));
    } catch {}
  }
  render() {
    this.list.replaceChildren();
    for (const tab of this.tabs.values()) {
      const duplicates = [...this.tabs.values()].filter((entry) => entry.label === tab.label).length > 1;
      const label = tab.label + (duplicates ? ' #' + tab.path.split('/').at(-1) : '');
      const wrap = document.createElement('div');
      wrap.className = 'terminal-tab-item';
      wrap.setAttribute('role', 'presentation');
      const button = document.createElement('button');
      button.className = 'terminal-tab'; button.textContent = label;
      button.title = label;
      button.id = this.buttonID(tab.path);
      button.setAttribute('role', 'tab');
      button.setAttribute('aria-selected', String(tab.path === this.active));
      button.setAttribute('aria-controls', this.panelID(tab.path));
      button.tabIndex = tab.path === this.active ? 0 : -1;
      button.onclick = () => this.select(tab.path);
      button.onkeydown = (e) => {
        const keys = [...this.tabs.keys()]; let next;
        if (e.key === 'ArrowRight') next = keys[(keys.indexOf(tab.path)+1)%keys.length];
        if (e.key === 'ArrowLeft') next = keys[(keys.indexOf(tab.path)+keys.length-1)%keys.length];
        if (e.key === 'Home') next = keys[0];
        if (e.key === 'End') next = keys.at(-1);
        if (e.key === 'Delete') { e.preventDefault(); this.close(tab.path); return; }
        if (next) {
          e.preventDefault(); this.select(next);
          document.getElementById(this.buttonID(next))?.focus({preventScroll:true});
        }
      };
      const close = document.createElement('button');
      close.className = 'terminal-tab-close'; close.textContent = '×';
      close.setAttribute('aria-label', 'Close terminal view: ' + label);
      close.title = 'Close this view; the agent keeps running';
      close.onclick = () => this.close(tab.path);
      wrap.append(button, close); this.list.appendChild(wrap);
    }
    const tab = this.tabs.get(this.active);
    this.popout.hidden = !tab;
    if (tab) this.popout.href = tab.path;
    this.empty.hidden = this.tabs.size > 0;
    this.panels.hidden = this.tabs.size === 0;
    const badge = document.getElementById('terminal-badge');
    badge.hidden = this.tabs.size === 0; badge.textContent = this.tabs.size;
  }
}
