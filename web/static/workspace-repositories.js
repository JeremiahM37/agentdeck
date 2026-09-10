// Additional repositories inherit the primary project's target, not its settings.
export function workspaceRepositories(container, projects) {
  const chosen = new Map();
  let primary = null;
  const details = document.createElement('details');
  const summary = document.createElement('summary');
  const hint = document.createElement('p'); hint.className = 'subhint';
  hint.textContent = 'Add up to seven other projects on the same target. Each gets separate files on the new branch.';
  const picker = document.createElement('select'); picker.className = 'f'; picker.setAttribute('aria-label', 'Additional repository');
  const add = document.createElement('button'); add.className = 'b'; add.type = 'button'; add.textContent = 'Add repository';
  const list = document.createElement('div'); list.setAttribute('aria-label', 'Selected repositories');
  details.append(summary, hint, picker, add, list); container.append(details);
  function candidates() {
    return projects.filter(p => primary && p.target_id === primary.target_id && p.id !== primary.id && !chosen.has(p.id));
  }
  function draw() {
    summary.textContent = chosen.size ? `Additional repositories (${chosen.size})` : 'Additional repositories';
    const available = candidates();
    picker.replaceChildren(...available.map(p => new Option(p.name, String(p.id))));
    if (!available.length) picker.append(new Option('No other projects on this target', ''));
    picker.disabled = add.disabled = !available.length || chosen.size >= 7;
    list.replaceChildren();
    for (const [id, base] of chosen) {
      const project = projects.find(p => p.id === id);
      const row = document.createElement('div'); row.className = 'workspace-repository';
      const title = document.createElement('strong'); title.textContent = project.name;
      const label = document.createElement('label'); label.className = 'f'; label.textContent = `Base for ${project.name}`;
      const input = document.createElement('input'); input.className = 'f'; input.value = base; input.placeholder = 'HEAD — current committed revision';
      input.setAttribute('aria-label', `Base for ${project.name}`);
      input.oninput = () => chosen.set(id, input.value);
      const remove = document.createElement('button'); remove.type = 'button'; remove.className = 'b'; remove.textContent = 'Remove';
      remove.setAttribute('aria-label', `Remove ${project.name}`);
      remove.onclick = () => { chosen.delete(id); draw(); picker.focus(); };
      label.append(input); row.append(title, label, remove); list.append(row);
    }
  }
  add.onclick = () => {
    const project = candidates().find(p => String(p.id) === picker.value);
    if (!project || chosen.size >= 7) return;
    chosen.set(project.id, ''); draw(); list.lastElementChild?.querySelector('input')?.focus();
  };
  return {
    sync(projectID) {
      primary = projects.find(p => p.id === Number(projectID)) || null;
      for (const id of chosen.keys()) {
        const p = projects.find(p => p.id === id);
        if (!primary || !p || p.id === primary.id || p.target_id !== primary.target_id) chosen.delete(id);
      }
      draw();
    },
    value() { return [...chosen].map(([project_id, base]) => ({project_id, base: base.trim()})); },
  };
}
