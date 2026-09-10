// Group names are data, never HTML, filesystem paths, or object properties.
export function renderSessionGroups(root,items,{mode,query,collapsed,renderCard,onToggle}) {
  if(mode==='none'){items.forEach(s=>root.appendChild(renderCard(s)));return;}
  const tree={children:new Map(),items:[],all:[]};
  for(const session of items){
    const labels=mode==='group' ? (session.group_path?session.group_path.split('/'):['\0']) : [session[mode==='project'?'project_name':'target_name']||'Unassigned'];
    let node=tree;node.all.push(session);
    for(const label of labels){if(!node.children.has(label))node.children.set(label,{label:label==='\0'?'Ungrouped':label,children:new Map(),items:[],all:[]});node=node.children.get(label);node.all.push(session);}
    node.items.push(session);
  }
  function draw(parent,node,path=[]){
    node.items.forEach(s=>parent.appendChild(renderCard(s)));
    for(const [label,child] of [...node.children].sort(([,a],[,b])=>a.label.localeCompare(b.label))){
      const next=[...path,label],key=JSON.stringify([mode,...next]);
      const box=document.createElement('details');box.className='session-group';box.open=!!query||!collapsed.has(key);box.dataset.groupPath=next.join('/').replaceAll('\0','');
      const heading=document.createElement('summary'),name=document.createElement('span'),count=document.createElement('small');name.textContent=child.label;
      const waiting=child.all.filter(s=>s.status==='waiting'&&!s.ended_at).length;
      count.textContent=`${child.all.length}${waiting?' · '+waiting+' waiting':''}`;heading.append(name,count);box.appendChild(heading);
      const body=document.createElement('div');body.className='session-group-body';draw(body,child,next);box.appendChild(body);
      // Programmatic initial opening also emits toggle; only persist user actions.
      heading.addEventListener('click',()=>{const willOpen=!box.open;willOpen?collapsed.delete(key):collapsed.add(key);onToggle();});
      parent.appendChild(box);
    }
  }
  draw(root,tree);
}
