export function openArchiveHistory({id,name,api}) {
  const previous=document.activeElement,dialog=document.createElement('dialog');
  dialog.className='native-history';dialog.setAttribute('aria-label','Archived terminal output');
  dialog.innerHTML='<header><h2>Archived terminal output</h2><button aria-label="Close archived output">×</button></header><p class="nh-name nh-explain"></p><p class="nh-status" role="status">Loading snapshot…</p><div class="nh-messages" tabindex="0"><pre style="white-space:pre-wrap;overflow-wrap:anywhere"></pre></div>';
  dialog.querySelector('.nh-name').textContent=name;
  dialog.querySelector('button').onclick=()=>dialog.close();
  dialog.addEventListener('keydown',e=>e.stopPropagation());
  let closed=false;
  dialog.addEventListener('close',()=>{closed=true;dialog.remove();previous?.focus();});
  document.body.append(dialog);dialog.showModal();
  api(`/sessions/${id}/archive/history`).then(data=>{if(closed)return;dialog.querySelector('pre').textContent=data.text||'No terminal output was available.';dialog.querySelector('[role=status]').textContent=data.note;}).catch(e=>{if(!closed)dialog.querySelector('[role=status]').textContent=e.message;});
  return dialog;
}
