// Native disclosure keeps every action reachable by keyboard and touch.
export function actionMenu(label = 'More', ariaLabel = 'More actions') {
  const menu = document.createElement('details');
  menu.className = 'action-menu';
  const summary = document.createElement('summary');
  summary.textContent = label;
  summary.setAttribute('aria-label', ariaLabel);
  const panel = document.createElement('div');
  panel.className = 'action-menu-panel';
  menu.append(summary, panel);
  panel.addEventListener('click', (event) => {
    if (event.target.closest('button,a')) menu.open = false;
  });
  return {menu, panel};
}
document.addEventListener('pointerdown', (event) => {
  document.querySelectorAll('.action-menu[open]').forEach(menu => {
    if (!menu.contains(event.target)) menu.open = false;
  });
});
document.addEventListener('keydown', (event) => {
  if (event.key !== 'Escape') return;
  document.querySelectorAll('.action-menu[open]').forEach(menu => {
    menu.open = false;
    menu.querySelector('summary').focus();
  });
});
// Open upward near the bottom of the screen; long menus remain scrollable.
document.addEventListener('toggle', event => {
  const menu = event.target;
  if (!menu.matches?.('.action-menu') || !menu.open) return;
  const panel = menu.querySelector('.action-menu-panel');
  if (!panel) return;
  menu.classList.remove('menu-above');
  const box = menu.getBoundingClientRect();
  const below = innerHeight - box.bottom - 16;
  menu.classList.toggle('menu-above', below < panel.scrollHeight && box.top > below);
}, true);
