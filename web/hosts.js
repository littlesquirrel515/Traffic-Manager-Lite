"use strict";
views.hosts=connectionsPage;
titles.hosts=['连接入口 / Hosts','按同步来源组织入口；手动入口和共享模板独立管理。'];
let hostsInventory=null;
const hostsFilters={search:'',server:'',instance:'',protocol:'',inbound:'',origin:'',publication:''};
const hostsExpanded=new Set();
async function connectionsPage(){
 const [rows,inbounds,nodes]=await Promise.all([api('catalog/endpoints'),api('catalog/inbound-management'),api('catalog/nodes')]);
 const byInbound=new Map(inbounds.map(i=>[i.id,i])),links=new Map();
 for(const n of nodes){const i=byInbound.get(n.inbound_id);if(i&&n.endpoint_id){if(!links.has(n.endpoint_id))links.set(n.endpoint_id,[]);links.get(n.endpoint_id).push(i)}}
 hostsInventory={rows,inbounds,nodes,links};
 const unique=(key,label)=>[...new Map(inbounds.map(i=>[String(i[key]),i[label]])).entries()];
 const filterMarkup=field('hosts_search','搜索名称 / 备注 / 地址 / Host / SNI',hostsFilters.search)+field('hosts_server','服务器',hostsFilters.server,'select',[['','全部服务器'],...unique('server_id','server_name')])+field('hosts_instance','核心实例',hostsFilters.instance,'select',[['','全部实例'],...unique('instance_id','instance_name')])+field('hosts_protocol','协议',hostsFilters.protocol,'select',[['','全部协议'],...unique('protocol','protocol')]);
 const groups=[...new Map(rows.filter(e=>e.sync).map(e=>[e.sync.job_id,e.sync.name])).entries()];
 return `<section class="card panel"><div class="toolbar"><h2>连接入口</h2><button data-action="connection-create" class="primary">＋ 新增连接入口</button><button data-action="endpoint-template">＋ 保存共享模板</button><button data-action="endpoint-batch">批量修改所选手动入口</button></div><div id="hosts-filters">${filterMarkup}${field('hosts_inbound','关联入站',hostsFilters.inbound,'select',[['','全部入站'],...inbounds.map(i=>[i.id,inboundLabel(i)])])}${field('hosts_origin','管理来源',hostsFilters.origin,'select',[['','全部来源'],['manual','手动入口'],['template','共享入口模板'],...groups.map(([id,name])=>['sync:'+id,name])])}${field('hosts_publication','同步发布状态',hostsFilters.publication,'select',[['','全部状态'],['active','正在发布'],['inactive','暂停发布']])}</div><div class="toolbar"><button type="button" id="hosts-expand">展开全部</button><button type="button" id="hosts-collapse">收起同步组</button><button type="button" id="hosts-clear">清空筛选</button></div><p class="muted">同步入口按来源组管理；来源减少时暂停发布的入口保留在原组。手动入口支持逐条编辑和批量修改，备注直接展示。</p><div id="hosts-results">${hostsResults()}</div></section>`;
}
function hostsResults(){
 const {rows,inbounds,nodes}=hostsInventory;
 const origin=e=>e.sync?'sync:'+e.sync.job_id:e.binding_count?'manual':'template';
 const linked=e=>hostsInventory.links.get(e.id)||[];
 const filtered=rows.filter(e=>{
  if(hostsFilters.origin&&origin(e)!==hostsFilters.origin)return false;
  if(hostsFilters.publication&&(!e.sync||(hostsFilters.publication==='active')!==e.sync.active))return false;
  const links=linked(e);
  if(['server','instance','protocol','inbound'].some(k=>hostsFilters[k])&&!links.some(i=>['server','instance','protocol','inbound'].every(k=>!hostsFilters[k]||String(i[k==='inbound'?'id':k==='protocol'?k:k+'_id'])===hostsFilters[k])))return false;
  const text=[e.name,e.description,e.profile.address,e.profile.port,e.profile.host,e.profile.sni,e.sync?.name,...links.map(inboundLabel)].join(' ').toLowerCase();
  return !hostsFilters.search||text.includes(hostsFilters.search.trim().toLowerCase());
 });
 const sections=new Map();
 for(const e of rows){const key=origin(e);if(!sections.has(key))sections.set(key,{name:e.sync?.name||(key==='manual'?'手动入口':'共享入口模板'),sync:e.sync,all:[],visible:[]});sections.get(key).all.push(e)}
 for(const e of filtered)sections.get(origin(e)).visible.push(e);
 const content=[...sections.entries()].filter(([,g])=>g.visible.length).sort(([a],[b])=>Number(!a.startsWith('sync:'))-Number(!b.startsWith('sync:'))).map(([key,g])=>{
  const active=g.all.filter(e=>e.sync?.active).length;
  const counts=g.sync?`正在发布 ${active} · 暂停发布 ${g.all.length-active}`:`共 ${g.all.length} 个`;
  const buttons=g.sync?`<div class="toolbar"><button data-action="preferred-sync-edit" data-id="${esc(g.sync.job_id)}">同步设置</button><button data-action="preferred-sync-history" data-id="${esc(g.sync.job_id)}">同步记录</button><a href="#nodegroups">前往节点分组</a></div><p class="muted">地址和端口由同步维护；保留模板 Host、SNI 与协议参数。${g.sync.enabled?'':'定时同步已暂停；当前发布结果继续保留。'}</p>`:'';
  return `<details class="hosts-group" data-hosts-group="${esc(key)}" ${!g.sync||hostsExpanded.has(key)||Object.values(hostsFilters).some(Boolean)?'open':''}><summary>${esc(g.name)} <small>${counts} · 筛选匹配 ${g.visible.length}</small></summary>${buttons}${hostsTable(g.visible)}</details>`;
 }).join('');
 return `<p role="status">共 ${rows.length} 个入口，筛选匹配 ${filtered.length} 个</p>`+(content||'<p class="empty">没有匹配的连接入口</p>');
}
function hostsTable(rows){return table(['选择','名称 / 类型','备注','公网连接地址','Host / SNI','状态','绑定 / 节点 / 订阅引用','操作'],rows.map(e=>[
 e.sync?'同步维护':`<input type="checkbox" name="endpoint-selected" value="${esc(e.id)}" aria-label="选择 ${esc(e.name)}">`,
 `${esc(e.name)}<small>${esc(e.endpoint_type)}</small>`,`<span class="endpoint-note">${esc(e.description||'—')}</span>`,`${esc(e.profile.address||'待补充')}:${esc(e.profile.port||'—')}`,`${esc(e.profile.host)}<small>${esc(e.profile.sni)}</small>`,e.sync?(e.sync.active?'正在发布':'暂停发布'):(e.enabled?'启用':'停用'),`${e.binding_count} / ${e.references} / ${e.subscriptions}`,
 (e.sync?'':`<button data-action="catalog-edit" data-kind="endpoints" data-id="${esc(e.id)}">编辑共享定义</button><button data-action="connection-create" data-endpoint="${esc(e.id)}">绑定入站</button>`)+`<button data-action="endpoint-references" data-id="${esc(e.id)}">引用情况</button>`+(e.sync?'':`<button data-action="endpoint-copy" data-id="${esc(e.id)}">复制模板</button><button data-action="catalog-delete" data-kind="endpoints" data-id="${esc(e.id)}">删除</button>`)
 ]))}
function redrawHosts(){const target=$('#hosts-results');if(target)target.innerHTML=hostsResults()}
$('#content').addEventListener('input',e=>{if(e.target.name==='hosts_search'){hostsFilters.search=e.target.value;redrawHosts()}});
$('#content').addEventListener('change',e=>{if(e.target.closest('#hosts-filters')){const key=e.target.name.slice(6);if(key in hostsFilters){hostsFilters[key]=e.target.value;redrawHosts()}}});
$('#content').addEventListener('toggle',e=>{const key=e.target.dataset?.hostsGroup;if(key){if(e.target.open)hostsExpanded.add(key);else hostsExpanded.delete(key)}},true);
$('#content').addEventListener('click',e=>{if(!hostsInventory)return;const id=e.target.id;if(id==='hosts-clear'){for(const key in hostsFilters)hostsFilters[key]='';$('#hosts-filters').querySelectorAll('input,select').forEach(e=>e.value='');redrawHosts()}if(id==='hosts-expand'||id==='hosts-collapse'){$('#hosts-results').querySelectorAll('details[data-hosts-group]').forEach(d=>{d.open=id==='hosts-expand'||!d.dataset.hostsGroup.startsWith('sync:')})}});
