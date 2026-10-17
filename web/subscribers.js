'use strict';
const subscriberProfileEditor=subscriberEditor;
subscriberEditor=async function(id='',nativeID='',attach=false){
 if(id&&!attach)return subscriberProfileEditor(id);
 const [inbounds,native,q]=await Promise.all([api('catalog/inbound-management'),api('catalog/native-clients'),api('catalog/quota-defaults')]);
 const request=Array.from(crypto.getRandomValues(new Uint8Array(16)),v=>v.toString(16).padStart(2,'0')).join('');
 const candidate=native.find(n=>n.credential_id===nativeID);
 const existing=attach?(await api('catalog/subscribers')).find(s=>s.id===id):null;
 edit('创建订阅用户并配置客户端',field('name','订阅用户名称',candidate?.core_client_ref||'')+field('external_ref','外部业务标识')+field('mode','用户来源',candidate?'native':'managed','select',[['managed','创建 TML 动态客户端'],['native','纳管已有原生客户端']])+field('inbound_id','目标入站','','select',[['','请选择入站'],...inbounds.filter(i=>i.present&&!i.retired&&!i.review).map(i=>[i.id,inboundLabel(i)])])+field('credential_id','已有原生客户端',nativeID,'select',[['','请选择原生客户端'],...native.filter(n=>n.status==='available'&&!JSON.parse(n.owners||'[]').length).map(n=>[n.credential_id,`${n.instance_name} / ${n.tag} / ${n.core_client_ref}`])])+field('enabled','配置成功后启用订阅用户',true,'checkbox')+quotaForm(q,true)+field('confirmed','确认：Xray 动态添加并回读；Hysteria2 使用已确认的 HTTP 认证后端；原生用户仅纳管',false,'checkbox')+'<p id="subscriber-provision-help" class="info-box wide"></p>',async d=>{
  const n=native.find(n=>n.credential_id===d.credential_id),inbound=d.mode==='native'?n?.inbound_id:d.inbound_id;
  if(!inbound)throw new Error('请选择明确的入站及客户端');
  const subscriber=existing?{id:existing.id,name:existing.name,external_ref:existing.external_ref,enabled:existing.enabled}:{name:d.name,external_ref:d.external_ref,enabled:d.enabled};
  if(!existing){const quota=quotaPayload(d,q);if(d.expires_at===quotaLocalTime(q.expires_at))quota.expires_at=null;subscriber.quota=quota}
  const r=await api('catalog/subscriber-provisioning','POST',{request_id:request,subscriber,inbound_id:inbound,credential_id:d.mode==='native'?d.credential_id:'',mode:d.mode,confirmed:d.confirmed});
  notice(r.status==='ready'?r.message:`用户尚未生效：${r.message}；可在创建任务中重试`);
 });
 const scope=$('#fields'),update=()=>{const mode=scope.querySelector('[name=mode]').value,i=inbounds.find(i=>i.id===scope.querySelector('[name=inbound_id]').value);scope.querySelector('[name=inbound_id]').closest('label').hidden=mode==='native';scope.querySelector('[name=credential_id]').closest('label').hidden=mode!=='native';$('#subscriber-provision-help').textContent=mode==='native'?'只绑定已存在的真实 Client，不创建重复账号。凭据不会显示在列表中。':i?.core_type==='hysteria2'?'Hysteria2：请先在本页准备 HTTP 认证后端，完成核心一次性配置并确认；之后新增用户只写 TML 数据库，无需核心 AddUser 或重启。尚未收到实际认证时，运行状态保持 Unknown。':i&&i.core_type!=='xray'?'sing-box 1.14.3 原生 API 无通用 AddUser；SSM 仅管理 managed Shadowsocks，不适用于 VLESS / AnyTLS / Hysteria2。':'Xray 支持的 VLESS / VMess / Trojan：TML 生成认证信息，保存加密 Desired，添加后回读确认。核心重启后仅补回已确认缺失的 TML 客户端，不修改核心配置文件。'};
 scope.addEventListener('change',update);update();
 if(existing){$('#edit-title').textContent='为 '+existing.name+' 添加核心客户端';for(const name of ['name','external_ref','enabled','traffic_gib','quota_enabled','expires_at','expiry_enabled','quota_policy']){const field=scope.querySelector(`[name=${name}]`);if(field){field.disabled=true;field.closest('label').hidden=true}}}
};
const subscribersBasePage=subscribersPage;
views.subscribers=async()=>{
 const [base,native,operations]=await Promise.all([subscribersBasePage(),api('catalog/native-clients'),api('catalog/subscriber-provisioning')]);
 return base+`<section class="card panel spacer"><h2>原生客户端 / 订阅用户来源</h2><p class="muted">来自已登记的真实客户端资产，不从在线或流量列表猜测完整用户。原生用户可直接纳管；不会重建或替换其认证。</p>${table(['实例 / 入站','原生身份','协议 / 资产状态','已归属用户','操作'],native.map(n=>{const owners=JSON.parse(n.owners||'[]');return[`${esc(n.instance_name)}<small>${esc(n.tag)}</small>`,esc(n.core_client_ref),`${esc(n.protocol)} / ${esc(n.status)}`,owners.map(esc).join('、')||'尚未纳管',`<button data-action="adopt-native-client" data-id="${esc(n.credential_id)}" ${owners.length||n.status!=='available'?'disabled':''}>纳入订阅用户</button>`]}))}</section><section class="card panel spacer"><h2>用户创建与恢复状态</h2>${table(['用户 / 入站','来源','创建状态 / 最近核心回读','说明','操作'],operations.map(p=>[`${esc(p.subscriber_id)}<small>${esc(p.inbound_id)}</small>`,esc(p.origin),`${esc(p.status)}<small>${esc(p.runtime_status||(p.message?.startsWith('HTTP')?'HTTP 认证账户；登录证据见后端':'原生用户不自动恢复'))}</small>`,esc(p.message),p.status!=='ready'?`<button data-action="retry-subscriber-provision" data-id="${esc(p.request_id)}">重试原创建任务</button>`:'已绑定']))}</section>`;
};
Object.assign(actions,{'adopt-native-client':b=>subscriberEditor('',b.dataset.id),'retry-subscriber-provision':async b=>{const r=await api('catalog/subscriber-provisioning/'+b.dataset.id+'/retry','POST',{});await render();notice(r.status+'：'+r.message)}});
actions['open-subscriber-users']=()=>{location.hash='subscribers'};
actions['subscriber-add-client']=b=>subscriberEditor(b.dataset.id,'',true);
delete actions['remote-runtime'];delete actions['remote-client'];
titles.subscribers=['订阅用户','统一管理原生与 TML 动态客户端、凭据归属和计量；创建结果以核心实际能力为准。'];

const subscribersWithSources=views.subscribers;
views.subscribers=async()=>{
 const [base,backends,inbounds]=await Promise.all([subscribersWithSources(),api('catalog/hysteria-auth'),api('catalog/inbound-management')]);
 const rows=inbounds.filter(i=>i.core_type==='hysteria2'&&i.protocol==='hysteria2');
 return base+`<section class="card panel spacer"><h2>Hysteria2 HTTP 认证后端</h2><p class="muted">核心一次性配置 HTTP 认证后，TML 通过数据库管理用户。这里准备认证地址及确认状态，不修改核心配置或重启核心。</p>${table(['实例 / 入站','后端状态','账户 / 实际认证证据','操作'],rows.map(i=>{const h=backends.find(h=>h.inbound_id===i.id);return [esc(inboundLabel(i)),!h?'未准备':!h.target_matches?'目标变化，需重新准备':!h.enabled?'已停用':h.confirmed?'已配置（管理员确认）':'待配置核心',h?`${h.account_count} 个账户<small>最近成功认证：${date(h.last_success_at)}</small>`:'Unknown',`<button data-action="hy-auth-prepare" data-id="${esc(i.id)}">${h?'轮换认证地址':'准备认证后端'}</button>${h?`<button data-action="hy-auth-state" data-id="${esc(i.id)}">确认配置 / 停用</button>`:''}`]}))}</section>`;
};
actions['hy-auth-prepare']=async b=>{
 const backends=await api('catalog/hysteria-auth'),existing=backends.find(h=>h.inbound_id===b.dataset.id);
 edit('准备 Hysteria2 HTTP 认证',field('allowed_cidrs','核心回调来源网段（逗号分隔；使用实际 Docker 网络 CIDR）','172.16.0.0/12,127.0.0.0/8,::1/128')+field('import_native','迁移此入站所有已登记原生客户端认证（保留现有登录载荷及统计 ID）',true,'checkbox')+field('native_userpass','原生认证为 userpass：仅用户名忽略大小写（单一 password 认证请取消）',true,'checkbox')+field('confirmed',existing?'确认轮换认证 URL：旧 URL 立即失效，需重新配置核心后确认':'确认准备认证后端；认证 URL 密钥只显示一次',false,'checkbox')+'<p class="info-box wide">静态 userpass 的完整用户必须先登记。迁移这里只导入 TML 已有凭据，不证明覆盖了核心所有用户；不要遗漏原生用户。不会改写核心文件。</p>',async d=>{
  const r=await api('catalog/hysteria-auth/'+b.dataset.id+'/prepare','POST',{confirmed:d.confirmed,rotate:!!existing,import_native:d.import_native,native_userpass:d.native_userpass,allowed_cidrs:d.allowed_cidrs.split(',').map(v=>v.trim()).filter(Boolean)});
  setTimeout(()=>{const config=`auth:
  type: http
  http:
    url: http://traffic-manager-lite:8080${r.callback_path}
    insecure: false
`;edit('认证 URL 仅显示本次，请安全保存',`<p class="info-box wide">已迁移 ${r.imported_native} 个已登记原生客户端。将以下 auth 段合并到独立 Hysteria2 配置中，保留其它配置。完成受控配置切换后，返回“确认配置 / 停用”。本次未修改或重启核心。</p><label class="wide">核心配置片段<textarea id="hy-auth-config" readonly rows="8">${esc(config)}</textarea></label>`,async()=>{});$('#edit-form [type=submit]').textContent='已保存配置片段';},0);
 });
};
actions['hy-auth-state']=async b=>{
 const h=(await api('catalog/hysteria-auth')).find(h=>h.inbound_id===b.dataset.id);if(!h)throw Error('请先准备后端');
 edit('确认 Hysteria2 认证配置',field('enabled','启用 HTTP 认证后端（停用后拒绝新的认证，不踢现有连接）',!!h.enabled,'checkbox')+field('confirmed','已核对核心 auth.type=http、使用当前认证 URL，且已处理全部原生用户',!!h.confirmed,'checkbox')+field('acknowledged','确认以上配置与影响；此操作只保存 TML 状态，不修改或重启核心',false,'checkbox'),d=>api('catalog/hysteria-auth/'+b.dataset.id,'PUT',{revision:h.revision,enabled:d.enabled,confirmed:d.confirmed,acknowledged:d.acknowledged}));
};
