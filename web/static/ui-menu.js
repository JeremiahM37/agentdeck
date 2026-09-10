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
// Keep popovers inside the usable page, including the desktop sidebar and
// mobile bottom navigation. Right-aligned menus near the left edge otherwise
// land underneath navigation and appear visible but cannot be clicked.
function positionMenu(menu) {
  if (!menu.open) return;
  const panel=menu.querySelector('.action-menu-panel');if(!panel)return;
  menu.classList.remove('menu-above');panel.style.transform='';panel.style.maxHeight='';
  let left=8,right=innerWidth-8,top=8,bottom=innerHeight-8;
  const nav=document.querySelector('#tabbar');
  if(nav && getComputedStyle(nav).display!=='none') {
    const b=nav.getBoundingClientRect();
    if(b.width<innerWidth/2 && b.height>innerHeight/2)left=Math.max(left,b.right+8);
    else if(b.width>innerWidth/2 && b.top>innerHeight/2)bottom=Math.min(bottom,b.top-8);
  }
  const header=document.querySelector('#topbar');
  if(header && getComputedStyle(header).display!=='none')top=Math.max(top,header.getBoundingClientRect().bottom+8);
  const box=menu.getBoundingClientRect(),below=bottom-box.bottom-6,above=box.top-top-6;
  const upward=below<Math.min(panel.scrollHeight,420)&&above>below;
  menu.classList.toggle('menu-above',upward);
  panel.style.maxHeight=Math.max(0,Math.min(420,upward?above:below))+'px';
  const bounds=panel.getBoundingClientRect();
  const dx=bounds.left<left?left-bounds.left:bounds.right>right?right-bounds.right:0;
  panel.style.transform=dx?`translateX(${dx}px)`:'';
}
document.addEventListener('toggle',event=>{if(event.target.matches?.('.action-menu'))positionMenu(event.target);},true);
window.addEventListener('resize',()=>document.querySelectorAll('.action-menu[open]').forEach(positionMenu));
