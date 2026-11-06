'use strict';
// v2.1: ordinary subscriber operations deal only in nodes and node groups.
// Native adoption remains an explicit administrator workflow.
const subscriberLegacyEditor=subscriberEditor;
subscriberEditor=async function(id='',nativeID='',attach=false){
 if(nativeID||id&&!attach)return subscriberLegacyEditor(id,nativeID,attach);
 const [nodes,groups,q,users,assignments]=await Promise.all([api('catalog/nodes'),api('catalog/groups'),api('catalog/quota-defaults'),api('catalog/subscribers'),id?api('catalog/subscriber-node-assignments?subscriber_id='+encodeURIComponent(id)):Promise.resolve([])]);
 const existing=users.find(u=>u.id===id);
 const assigned=new Set(assignments.map(n=>n.node_id));
 const groupMembers=new Map(groups.map(g=>[g.id,new Set(JSON.parse(g.node_ids||'[]'))]));
 const request=Array.from(crypto.getRandomValues(new Uint8Array(16)),v=>v.toString(16).padStart(2,'0')).join('');
 edit(existing?'为 '+existing.name+' 分配节点':'创建订阅用户',
  (existing?'':field('name','订阅用户名称')+field('external_ref','外部业务标识')+field('enabled','全部认证配置成功后启用',true,'checkbox')+quotaForm(q,true))+
  `<div class="wide subscription-picker"><h3>允许使用的节点组与组外节点</h3>${existing?'<p class="muted">已分配节点已勾选；取消勾选可撤销分配，也可以全部取消。保存前会列出新增和删除项。此操作影响后续订阅更新，不会删除核心账号或中断已有连接。</p>':''}${field('subscriber_node_search','搜索节点名称 / 协议')}${field('subscriber_node_scope','显示范围',assigned.size?'assigned':'all','select',[['assigned','仅已分配'],['all','全部节点']])}<p class="muted">组内节点只能整组分配或取消；下方只显示不属于任何分组的节点。已有部分分配的组会显示部分选中，保存前请明确选择整组或取消整组。</p><div class="subscriber-selection-actions"><button type="button" id="subscriber-node-select-visible">勾选筛选结果</button><button type="button" id="subscriber-node-remove-visible">取消筛选结果</button><button type="button" id="subscriber-node-clear">清空全部</button></div><p id="subscriber-node-count" role="status"></p><details open><summary>按组勾选 / 取消</summary><p class="muted">选中整组后，组内有效节点变化会自动影响订阅；新入站仍需有可用认证。多个组共享节点时，修改其中一个组会影响其他组的勾选状态，请预览变更。</p><div id="subscriber-node-group-choices" class="subscriber-node-list"></div></details><p class="muted">组外节点可独立分配；按住 Alt 或 Shift 可连续选择。</p><div id="subscriber-node-choices" class="subscriber-node-list" tabindex="0" role="region" aria-label="节点勾选列表"></div><button type="button" id="subscriber-node-preview">预览变更</button><div id="subscriber-node-plan" role="status">请选择节点或节点组，然后预览。</div></div>`+
  field('confirmed','确认保存预览中的节点权限变更及认证配置',false,'checkbox'),async d=>{
   if(!plan||!plan.ready)throw Error('请先预览并处理不可配置的节点');
   if(!d.confirmed)throw Error('请确认节点授权及认证配置计划');
   const subscriber=existing?{id:existing.id}:{name:d.name,external_ref:d.external_ref,enabled:d.enabled};
   if(!existing){const quota=quotaPayload(d,q);if(d.expires_at===quotaLocalTime(q.expires_at))quota.expires_at=null;subscriber.quota=quota}
   const payload=existing?{...selection(),request_id:request,plan_hash:plan.hash,confirmed:true}:{...selection(),request_id:request,subscriber,plan_hash:plan.hash,confirmed:true};
   const r=await api(existing?'catalog/subscriber-node-edits':'catalog/subscriber-node-requests','POST',payload);
   notice(r.message+(r.status==='ready'?'；订阅链接会在下次更新时应用当前节点权限':'；可在权限变更或节点配置任务中重试'));
  });
 const scope=$('#fields');let plan=null,serial=0;
 const selected=new Set(assigned),selection=()=>({...existing?{subscriber_id:existing.id}:{subscriber:{}},node_ids:[...selected].filter(id=>!groupedIDs.has(id)),group_ids:[...groupMembers].filter(([id,m])=>groupByID.get(id).enabled&&m.size>0&&[...m].every(n=>selected.has(n))).map(([id])=>id)});
 const editable=n=>assigned.has(n.id)||n.status==='ready';
 const byID=new Map(nodes.map(n=>[n.id,n]));
 const groupByID=new Map(groups.map(g=>[g.id,g]));
 const groupedIDs=new Set([...groupMembers.values()].flatMap(m=>[...m]));
 const groupEditable=id=>[...groupMembers.get(id)].filter(n=>byID.has(n)&&editable(byID.get(n)));
 let visible=[],anchor=null,rangeModifier=false;
 const list=scope.querySelector('#subscriber-node-choices'),groupList=scope.querySelector('#subscriber-node-group-choices');
 groupList.innerHTML=groups.filter(g=>groupMembers.has(g.id)).map(g=>`<label><input type="checkbox" data-subscriber-group="${esc(g.id)}"><span>${esc(g.name)} ${g.enabled?'':'（已停用）'} <small data-group-count></small></span></label>`).join('')||'<p class="muted">暂无启用的节点分组</p>';
 const invalidate=()=>{serial++;plan=null;scope.querySelector('[name=confirmed]').checked=false;scope.querySelector('#subscriber-node-plan').textContent='选择已变更，请重新预览配置计划。'};
 const count=()=>{
  scope.querySelector('#subscriber-node-count').textContent=`匹配 ${visible.length} 个组外节点 · 当前选择 ${selected.size} 个 · 新增 ${[...selected].filter(id=>!assigned.has(id)).length} 个 · 取消 ${[...assigned].filter(id=>!selected.has(id)).length} 个`;
  for(const box of groupList.querySelectorAll('[data-subscriber-group]')){
   const members=[...groupMembers.get(box.dataset.subscriberGroup)],n=members.filter(id=>selected.has(id)).length;
   box.checked=members.length>0&&n===members.length;box.indeterminate=n>0&&n<members.length;box.disabled=!members.length||(!n&&(!groupByID.get(box.dataset.subscriberGroup).enabled||members.some(id=>!byID.has(id)||!editable(byID.get(id)))));
   box.closest('label').querySelector('[data-group-count]').textContent=`已选 ${n} / ${members.length}`;
  }
  for(const id of ['subscriber-node-select-visible','subscriber-node-remove-visible'])scope.querySelector('#'+id).disabled=!visible.some(editable);
 };
 const draw=()=>{
  const terms=scope.querySelector('[name=subscriber_node_search]').value.toLowerCase().trim().split(/\s+/).filter(Boolean);
  const assignedOnly=scope.querySelector('[name=subscriber_node_scope]').value==='assigned';
  visible=nodes.filter(n=>!groupedIDs.has(n.id)&&(!assignedOnly||assigned.has(n.id))&&terms.every(t=>[n.name,n.profile.protocol,...(n.tags||[])].join(' ').toLowerCase().includes(t)));
  list.innerHTML=visible.map(n=>`<label><input type="checkbox" data-subscriber-node="${esc(n.id)}" ${selected.has(n.id)?'checked':''} ${!editable(n)?'disabled':''}><span>${esc(n.name)} · ${esc(n.profile.protocol)} · ${catalogStatus(n.status)} ${assigned.has(n.id)?'<strong data-assignment-label>'+ (selected.has(n.id)?'已分配':'将取消分配')+'</strong>':''}</span></label>`).join('')||'<p class="muted">没有匹配节点</p>';count();
 };
 const apply=(ids,on)=>{
  for(const id of ids)on?selected.add(id):selected.delete(id);
  for(const box of list.querySelectorAll('[data-subscriber-node]')){
   box.checked=selected.has(box.dataset.subscriberNode);
   const label=box.closest('label').querySelector('[data-assignment-label]');
   if(label)label.textContent=box.checked?'已分配':'将取消分配';
  }
  invalidate();count();
 };
 list.addEventListener('click',e=>{if(e.target.dataset.subscriberNode)rangeModifier=e.altKey||e.shiftKey});
 scope.addEventListener('change',e=>{
  const box=e.target,id=box.dataset.subscriberNode,gid=box.dataset.subscriberGroup;
  if(id){
   const from=visible.findIndex(n=>n.id===anchor),to=visible.findIndex(n=>n.id===id);
   const ids=rangeModifier&&from>=0&&to>=0?visible.slice(Math.min(from,to),Math.max(from,to)+1).filter(editable).map(n=>n.id):[id];
   anchor=id;rangeModifier=false;apply(ids,box.checked);
  }else if(gid){anchor=null;const members=[...groupMembers.get(gid)];if(box.checked&&(!groupByID.get(gid).enabled||members.some(id=>!byID.has(id)||!editable(byID.get(id))))){count();notice('此组包含暂不可分配节点，请先处理后再整组分配');return}apply(members,box.checked)}
 });
 const filter=()=>{anchor=null;rangeModifier=false;draw()};
 scope.querySelector('[name=subscriber_node_search]').addEventListener('input',filter);
 scope.querySelector('[name=subscriber_node_scope]').addEventListener('change',filter);
 draw();
 scope.querySelector('#subscriber-node-select-visible').onclick=()=>{anchor=null;apply(visible.filter(editable).map(n=>n.id),true)};
 scope.querySelector('#subscriber-node-remove-visible').onclick=()=>{anchor=null;apply(visible.filter(editable).map(n=>n.id),false)};
 scope.querySelector('#subscriber-node-clear').onclick=()=>{anchor=null;apply([...selected],false)};
 scope.querySelector('#subscriber-node-preview').onclick=async()=>{
  const partial=[...groupMembers.entries()].filter(([,members])=>{const n=[...members].filter(id=>selected.has(id)).length;return n>0&&n<members.size});if(partial.length){scope.querySelector('#subscriber-node-plan').textContent='存在部分选中的组；请选择整组或取消整组后再预览。重叠组共享节点，请一并调整。';return}
  const run=++serial;plan=null;scope.querySelector('[name=confirmed]').checked=false;
  const output=scope.querySelector('#subscriber-node-plan');output.textContent='正在检查已保存的节点和认证配置能力…';
  try{const p=await api(existing?'catalog/subscriber-node-edits/preview':'catalog/subscriber-node-requests/preview','POST',selection());if(run!==serial||!scope.isConnected)return;plan=p;
   const targets=existing?(p.provision?.targets||[]):p.targets;const summary=existing?`新增 ${p.added.length} 个 · 取消分配 ${p.removed.length} 个 · 保留 ${p.retained} 个 · 补齐 ${(p.repaired||[]).length} 个原生认证`:`已选 ${p.nodes.length} 个节点，涉及 ${p.targets.length} 个认证目标`;
   output.innerHTML=`<p>${summary}。${p.ready?'可以保存':'部分节点暂不能配置'}</p>${existing&&p.removed.length?'<p>将取消分配：'+p.removed.map(n=>esc(n.node_name)).join('、')+'</p>':''}${table(['实例 / 协议','配置方式','说明'],targets.map(t=>[`${esc(t.instance_name)} / ${esc(t.protocol)}`,esc(({create:'创建认证',reuse:'复用已有认证',shared_native:'复用原生共享账号',resume:'重试原认证任务',blocked:'暂不可用'})[t.status]||t.status),esc(t.reason)]))}<p class="muted">预览不创建账号。提交后保存可重试任务；不会改写核心配置或重启核心。额度不会自动中断已有连接。</p>`;
  }catch(e){if(run===serial&&scope.isConnected)output.textContent=e.message}
 };
};

const subscriberNodesBase=views.subscribers;
views.subscribers=async()=>{
 const [base,requests,grants]=await Promise.all([subscriberNodesBase(),api('catalog/subscriber-node-requests'),api('catalog/subscriber-node-grants')]);
 return base+`<section class="card panel spacer"><h2>节点授权与配置任务</h2><p class="muted">订阅用户选择节点；核心认证按来源去重配置。新用户在全部目标完成后启用。节点授权与订阅链接分开：链接只发布该用户已授权且具备有效认证的节点。</p>${table(['用户','节点 / 认证目标','状态','说明','操作'],requests.map(p=>[esc(p.subscriber_name),`${p.node_count} / ${p.target_count}`,esc(p.status),esc(p.message),`<button data-action="subscriber-node-details" data-id="${esc(p.request_id)}">配置详情</button>`+(p.status!=='ready'?`<button data-action="subscriber-node-retry" data-id="${esc(p.request_id)}">重试配置任务</button>`:`<button data-action="subscriber-node-subscription" data-id="${esc(p.subscriber_id)}">创建订阅</button>`)]))}<details><summary>查看已分配节点（${grants.length}）</summary>${table(['用户','代理节点'],grants.map(g=>[esc(g.subscriber_name),esc(g.node_name)]))}</details></section>`;
};
actions['subscriber-node-retry']=async b=>{const r=await api('catalog/subscriber-node-requests/'+b.dataset.id+'/retry','POST',{});await render();notice(r.message)};
actions['subscriber-node-subscription']=b=>actions['subscriber-link'](b);
titles.subscribers=['订阅用户','管理用户、额度与节点权限；所需核心认证由 TML 根据代理节点安排。'];

actions['subscriber-node-details']=async b=>{const rows=await api('catalog/subscriber-node-requests/'+b.dataset.id+'/tasks');edit('节点认证配置详情',`<div class="wide">${table(['实例 / 协议','状态','说明','检查时间'],rows.map(t=>[esc(t.instance_name)+' / '+esc(t.protocol),esc(t.status),esc(t.message),date(t.checked_at)]))}</div>`,async()=>{});$('#edit-form [type=submit]').textContent='关闭'};

const subscriberPermissionsBase=views.subscribers;
views.subscribers=async()=>{const [base,edits]=await Promise.all([subscriberPermissionsBase(),api('catalog/subscriber-node-edits')]);return base+`<section class="card panel spacer"><h2>节点权限变更</h2>${table(['用户','状态','说明','操作'],edits.map(e=>[esc(e.subscriber_name),esc(e.status),esc(e.message),e.status==='pending'?`<button data-action="subscriber-edit-retry" data-id="${esc(e.request_id)}">重试新增配置</button>`:'已保存']))}</section>`};
actions['subscriber-edit-retry']=async b=>{const r=await api('catalog/subscriber-node-edits/'+b.dataset.id+'/retry','POST',{});await render();notice(r.message)};
actions['subscriber-link']=async b=>{const r=await api('catalog/subscribers/'+b.dataset.id+'/link','POST',{});await showCatalogToken(r.token)};
actions['catalog-link']=async b=>{const r=await api('catalog/subscriptions/'+b.dataset.id+'/link','POST',{});await showCatalogToken(r.token)};
actions['catalog-rotate']=b=>edit('重置订阅链接',field('confirmed','确认重置：此订阅的旧链接将立即失效，客户端需要更新',false,'checkbox'),async d=>{if(!d.confirmed)throw Error('请确认重置影响');const r=await api('catalog/subscriptions/'+b.dataset.id+'/rotate-token','POST',{});setTimeout(()=>showCatalogToken(r.token),0)});
