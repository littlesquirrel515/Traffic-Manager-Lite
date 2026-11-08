"use strict";
// The subscriber owns link operations; the navigation page is solely an audit view.
titles.subscriptionlogs=['订阅访问日志','查看客户端请求、来源 IP 与交付结果；链接操作统一在订阅用户中管理。'];
views.subscriptionlogs=subscriptionAccessLogsPage;
let subscriptionLogData=null;
const subscriptionLogFilters={user:'',status:'',format:'',search:''};
async function subscriptionAccessLogsPage(){
 const [logs,links,users]=await Promise.all([api('catalog/access-logs'),api('catalog/subscriptions'),api('catalog/subscribers')]);
 subscriptionLogData={logs,links:new Map(links.map(l=>[l.id,l])),users:new Map(users.map(u=>[u.id,u]))};
 const formats=[...new Set(logs.map(l=>l.format))].sort();
 return `<section class="card panel"><div class="toolbar"><h2>订阅访问日志</h2><a href="#subscribers">前往订阅用户管理链接</a></div><p class="muted">展示最新 200 条请求；筛选针对已加载记录。历史日志继续保存在数据库。已删除链接的记录保留，不推断其用户归属。</p><div class="subscription-log-filters">${field('subscription_log_user','订阅用户',subscriptionLogFilters.user,'select',[['','全部用户'],['deleted','已删除链接 / 无法关联'],...users.map(u=>[u.id,u.name])])}${field('subscription_log_status','请求结果',subscriptionLogFilters.status,'select',[['','全部结果'],['success','成功（2xx）'],['failure','失败（4xx / 5xx）']])}${field('subscription_log_format','返回格式',subscriptionLogFilters.format,'select',[['','全部格式'],...formats.map(f=>[f,f])])}${field('subscription_log_search','搜索来源 IP / 客户端 / 错误',subscriptionLogFilters.search)}</div><button type="button" id="subscription-logs-clear">清空筛选</button><div id="subscription-log-results">${subscriptionLogResults()}</div></section>`;
}
function subscriptionLogResults(){
 const {logs,links,users}=subscriptionLogData;
 const rows=logs.filter(l=>{const link=links.get(l.subscription_id),user=users.get(link?.subscriber_id);const f=subscriptionLogFilters;
  if(f.user&&(f.user==='deleted'?!!link:link?.subscriber_id!==f.user))return false;
  if(f.status==='success'&&(l.status_code<200||l.status_code>=300))return false;
  if(f.status==='failure'&&l.status_code<400)return false;
  if(f.format&&l.format!==f.format)return false;
  return !f.search||[l.client_ip,l.peer_ip,l.user_agent,l.error_code,link?.name,user?.name].join(' ').toLowerCase().includes(f.search.trim().toLowerCase());
 });
 return `<p role="status">已加载 ${logs.length} 条，筛选匹配 ${rows.length} 条</p>`+table(['时间','订阅用户','订阅链接','格式','结果','客户端','来源 IP / 依据'],rows.map(l=>{const link=links.get(l.subscription_id);return [date(l.accessed_at),esc(users.get(link?.subscriber_id)?.name||'无法关联'),esc(link?.name||'链接已删除'),esc(l.format),`${l.status_code}<small>${esc(l.error_code)}</small>`,esc(l.user_agent),`${esc(l.client_ip||'旧记录未保存')}<small>${esc(l.ip_source||'legacy_hash_only')}${l.peer_ip?' · 上游 '+esc(l.peer_ip):''}</small>`]}));
}
function redrawSubscriptionLogs(){const target=$('#subscription-log-results');if(target)target.innerHTML=subscriptionLogResults()}
$('#content').addEventListener('input',e=>{if(e.target.name==='subscription_log_search'){subscriptionLogFilters.search=e.target.value;redrawSubscriptionLogs()}});
$('#content').addEventListener('change',e=>{if(e.target.name?.startsWith('subscription_log_')){const key=e.target.name.slice('subscription_log_'.length);if(key in subscriptionLogFilters){subscriptionLogFilters[key]=e.target.value;redrawSubscriptionLogs()}}});
$('#content').addEventListener('click',e=>{if(e.target.id==='subscription-logs-clear'){for(const key in subscriptionLogFilters)subscriptionLogFilters[key]='';$('#content .subscription-log-filters').querySelectorAll('input,select').forEach(e=>e.value='');redrawSubscriptionLogs()}});

let deliveryLoad=0;
const plainShowCatalogToken=showCatalogToken;
showCatalogToken=async function(token,subscriptionID=''){
 const serial=++deliveryLoad;
 let panel=$('#subscriber-delivery-operations');if(!panel){panel=document.createElement('div');panel.id='subscriber-delivery-operations';$('#token-dialog .form-actions').before(panel)}
 panel.replaceChildren();$('#token-dialog h2').textContent='订阅链接';$('#copy-token').textContent='复制地址';
 await plainShowCatalogToken(token);
 if(!subscriptionID||serial!==deliveryLoad||!$('#token-dialog').open)return;
 const [links,users]=await Promise.all([api('catalog/subscriptions'),api('catalog/subscribers')]);
 if(serial!==deliveryLoad||!$('#token-dialog').open)return;
 const selected=links.find(l=>l.id===subscriptionID);if(!selected)return;
 const owner=users.find(u=>u.id===selected.subscriber_id);$('#token-dialog h2').textContent=(owner?.name||'订阅用户')+' · 订阅链接';
 const buttons=l=>`<button type="button" data-delivery-action="catalog-preview" data-id="${esc(l.id)}">脱敏预览</button>${l.unified?'':`<button type="button" data-delivery-action="catalog-export" data-id="${esc(l.id)}">导出配置</button>`}<button type="button" data-delivery-action="catalog-rotate" data-id="${esc(l.id)}">重置链接</button>`;
 const historical=links.filter(l=>l.subscriber_id===selected.subscriber_id&&!l.unified);
 panel.innerHTML=`<p class="info-box">${selected.unified?'统一订阅自动适配客户端，跟随当前用户的节点权限。停用请管理用户状态；重置会使旧地址失效。':'当前为历史独立链接，使用其单独保存的节点范围。推荐使用该用户的统一订阅。'}</p><div class="toolbar">${buttons(selected)}<button type="button" data-delivery-log-user="${esc(selected.subscriber_id)}">访问日志</button></div>${historical.length?`<details class="subscriber-historical-links"><summary>历史独立链接（${historical.length}）</summary><p class="muted">仅列出该用户已有的独立链接；保留历史兼容管理。</p>${table(['名称','格式 / 状态','操作'],historical.map(l=>[esc(l.name),`${esc(l.format)} · ${l.enabled?'启用':'停用'}`,`<button type="button" data-delivery-action="catalog-link" data-id="${esc(l.id)}">查看 / 复制</button><button type="button" data-delivery-action="catalog-edit" data-kind="subscriptions" data-id="${esc(l.id)}">编辑</button>${buttons(l)}<button type="button" data-delivery-action="catalog-delete" data-kind="subscriptions" data-id="${esc(l.id)}">删除</button>`]))}</details>`:''}`;
};
$('#token-dialog').addEventListener('close',()=>{deliveryLoad++;$('#token-url').value='';$('#subscriber-delivery-operations')?.replaceChildren()});
$('#token-dialog').addEventListener('click',async e=>{
 const button=e.target.closest('[data-delivery-action],[data-delivery-log-user]');if(!button)return;
 button.disabled=true;
 try{
  if(button.dataset.deliveryLogUser){for(const key in subscriptionLogFilters)subscriptionLogFilters[key]='';subscriptionLogFilters.user=button.dataset.deliveryLogUser;$('#token-dialog').close();if(location.hash==='#subscriptionlogs')await render();else location.hash='subscriptionlogs';return}
  const action=button.dataset.deliveryAction;$('#token-dialog').close();await actions[action](button);if(!$('#editor').open)await render(true);
 }catch(error){notice(error.message)}finally{button.disabled=false}
});

actions['subscriber-preview']=async b=>showCatalogPreviewResult(await api('catalog/subscribers/'+b.dataset.id+'/preview','POST',{}));
