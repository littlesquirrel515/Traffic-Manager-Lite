'use strict';
// v2.1: ordinary subscriber operations deal only in nodes and node groups.
// Native adoption remains an explicit administrator workflow.
const subscriberLegacyEditor=subscriberEditor;
subscriberEditor=async function(id='',nativeID='',attach=false){
 if(nativeID||id&&!attach)return subscriberLegacyEditor(id,nativeID,attach);
 const [nodes,groups,q,users,assignments]=await Promise.all([api('catalog/nodes'),api('catalog/groups'),api('catalog/quota-defaults'),api('catalog/subscribers'),id?api('catalog/subscriber-node-assignments?subscriber_id='+encodeURIComponent(id)):Promise.resolve([])]);
 const existing=users.find(u=>u.id===id);
 const assigned=new Set(assignments.map(n=>n.node_id));
 const request=Array.from(crypto.getRandomValues(new Uint8Array(16)),v=>v.toString(16).padStart(2,'0')).join('');
 edit(existing?'为 '+existing.name+' 分配节点':'创建订阅用户',
  (existing?'':field('name','订阅用户名称')+field('external_ref','外部业务标识')+field('enabled','全部认证配置成功后启用',true,'checkbox')+quotaForm(q,true))+
  `<div class="wide subscription-picker"><h3>允许使用的代理节点</h3>${existing?'<p class="muted">已分配节点已勾选并保留。本窗口用于追加节点，预览仅处理本次新增节点。</p>':''}${field('subscriber_node_search','搜索节点名称 / 协议')}<p id="subscriber-node-count" role="status"></p><div id="subscriber-node-choices"></div><h3>按节点组选择</h3>${pickList('subscriber_node_groups',groups.filter(g=>g.enabled),[],g=>esc(g.name))}<p class="muted">节点组按本次提交时的成员展开。后续新增成员不会自动扩大用户权限；同一来源的多个节点只配置一个 Client。</p><button type="button" id="subscriber-node-preview">预览配置计划</button><div id="subscriber-node-plan" role="status">请选择节点或节点组，然后预览。</div></div>`+
  field('confirmed','确认按预览计划分配节点，并配置所需的核心认证',false,'checkbox'),async d=>{
   if(!plan||!plan.ready)throw Error('请先预览并处理不可配置的节点');
   if(!d.confirmed)throw Error('请确认节点授权及认证配置计划');
   const subscriber=existing?{id:existing.id}:{name:d.name,external_ref:d.external_ref,enabled:d.enabled};
   if(!existing){const quota=quotaPayload(d,q);if(d.expires_at===quotaLocalTime(q.expires_at))quota.expires_at=null;subscriber.quota=quota}
   const r=await api('catalog/subscriber-node-requests','POST',{...selection(),request_id:request,subscriber,plan_hash:plan.hash,confirmed:true});
   notice(r.message+(r.status==='ready'?'；可通过“创建订阅”取得客户端订阅链接':'；新用户保持未启用，可在节点配置任务中重试'));
  });
 const scope=$('#fields');let plan=null,serial=0;
 const selected=new Set(assigned),selection=()=>({subscriber:existing?{id:existing.id}:{},node_ids:[...selected],group_ids:[...scope.querySelectorAll('[name=subscriber_node_groups]:checked')].map(v=>v.value)});
 const invalidate=()=>{serial++;plan=null;scope.querySelector('[name=confirmed]').checked=false;scope.querySelector('#subscriber-node-plan').textContent='选择已变更，请重新预览配置计划。'};
 const count=()=>{scope.querySelector('#subscriber-node-count').textContent=`已分配 ${assigned.size} 个 · 本次单选新增 ${[...selected].filter(id=>!assigned.has(id)).length} 个（分组在预览时展开）`};
 const draw=()=>{const text=scope.querySelector('[name=subscriber_node_search]').value.toLowerCase();scope.querySelector('#subscriber-node-choices').innerHTML=nodes.filter(n=>[n.name,n.profile.protocol,...n.tags].join(' ').toLowerCase().includes(text)).map(n=>`<label><input type="checkbox" data-subscriber-node="${esc(n.id)}" ${selected.has(n.id)?'checked':''} ${assigned.has(n.id)||n.status!=='ready'?'disabled':''}>${esc(n.name)} · ${esc(n.profile.protocol)} · ${catalogStatus(n.status)} ${assigned.has(n.id)?'<strong>已分配</strong>':''}</label>`).join('')||'<p class="muted">没有匹配节点。请先在连接入口中准备可用节点。</p>';count()};
 scope.addEventListener('change',e=>{if(e.target.dataset.subscriberNode){e.target.checked?selected.add(e.target.dataset.subscriberNode):selected.delete(e.target.dataset.subscriberNode);invalidate();count()}if(e.target.name==='subscriber_node_groups')invalidate()});
 scope.querySelector('[name=subscriber_node_search]').addEventListener('input',draw);draw();
 scope.querySelector('#subscriber-node-preview').onclick=async()=>{
  const run=++serial;plan=null;scope.querySelector('[name=confirmed]').checked=false;
  const output=scope.querySelector('#subscriber-node-plan');output.textContent='正在检查已保存的节点和认证配置能力…';
  try{const p=await api('catalog/subscriber-node-requests/preview','POST',selection());if(run!==serial||!scope.isConnected)return;plan=p;
   output.innerHTML=`<p>已选 ${p.nodes.length} 个节点，涉及 ${p.targets.length} 个认证目标。${p.ready?'可以配置':'部分节点暂不能配置'}</p>${table(['实例 / 协议','配置方式','说明'],p.targets.map(t=>[`${esc(t.instance_name)} / ${esc(t.protocol)}`,esc(({create:'创建认证',reuse:'复用已有认证',blocked:'暂不可用'})[t.status]||t.status),esc(t.reason)]))}<p class="muted">预览不创建账号。提交后保存可重试任务；不会改写核心配置或重启核心。额度不会自动中断已有连接。</p>`;
  }catch(e){if(run===serial&&scope.isConnected)output.textContent=e.message}
 };
};

const subscriberNodesBase=views.subscribers;
views.subscribers=async()=>{
 const [base,requests,grants]=await Promise.all([subscriberNodesBase(),api('catalog/subscriber-node-requests'),api('catalog/subscriber-node-grants')]);
 return base+`<section class="card panel spacer"><h2>节点授权与配置任务</h2><p class="muted">订阅用户选择节点；核心认证按来源去重配置。新用户在全部目标完成后启用。节点授权与订阅链接分开：链接只发布该用户已授权且具备有效认证的节点。</p>${table(['用户','节点 / 认证目标','状态','说明','操作'],requests.map(p=>[esc(p.subscriber_name),`${p.node_count} / ${p.target_count}`,esc(p.status),esc(p.message),`<button data-action="subscriber-node-details" data-id="${esc(p.request_id)}">配置详情</button>`+(p.status!=='ready'?`<button data-action="subscriber-node-retry" data-id="${esc(p.request_id)}">重试配置任务</button>`:`<button data-action="subscriber-node-subscription" data-id="${esc(p.subscriber_id)}">创建订阅</button>`)]))}<details><summary>查看已分配节点（${grants.length}）</summary>${table(['用户','代理节点'],grants.map(g=>[esc(g.subscriber_name),esc(g.node_name)]))}</details></section>`;
};
actions['subscriber-node-retry']=async b=>{const r=await api('catalog/subscriber-node-requests/'+b.dataset.id+'/retry','POST',{});await render();notice(r.message)};
actions['subscriber-node-subscription']=b=>catalogSubscriptionEditor('',b.dataset.id);
titles.subscribers=['订阅用户','管理用户、额度与节点权限；所需核心认证由 TML 根据代理节点安排。'];

actions['subscriber-node-details']=async b=>{const rows=await api('catalog/subscriber-node-requests/'+b.dataset.id+'/tasks');edit('节点认证配置详情',`<div class="wide">${table(['实例 / 协议','状态','说明','检查时间'],rows.map(t=>[esc(t.instance_name)+' / '+esc(t.protocol),esc(t.status),esc(t.message),date(t.checked_at)]))}</div>`,async()=>{});$('#edit-form [type=submit]').textContent='关闭'};
