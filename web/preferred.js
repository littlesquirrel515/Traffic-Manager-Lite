'use strict';
actions['preferred-generate']=async b=>{
 const [nodes,groups]=await Promise.all([api('catalog/nodes'),api('catalog/groups')]);
 const templates=nodes.filter(n=>n.status==='ready'&&n.profile.tls&&!n.profile.reality&&['ws','xhttp','grpc'].includes(n.profile.transport)&&['vless','vmess','trojan'].includes(n.profile.protocol));
 if(!templates.length)throw Error('请先建立一个可用的 TLS WS/XHTTP/gRPC 连接入口节点，作为优选模板');
 let plan=null,serial=0,request='';const makeID=()=>Array.from(crypto.getRandomValues(new Uint8Array(16)),n=>n.toString(16).padStart(2,'0')).join('');
 edit('优选订阅生成',field('template_node_id','模板节点',templates[0].id,'select',templates.map(n=>[n.id,n.name+' · '+n.profile.protocol+'/'+n.profile.transport]))+
 field('group_id','目标分组',b.dataset.id||'','select',[['','创建新分组'],...groups.map(g=>[g.id,g.name+(g.enabled?'':'（已停用）')])])+field('group_name','新分组名称','优选节点组')+field('prefix','节点名称前缀','')+
 field('port','未填写端口时使用',443,'number')+field('limit','本批最多生成数量（1–500）',16,'number')+
 `<p class="wide info-box">以模板的 Host、SNI、Path、XHTTP Mode 等参数生成多个连接地址。先预览再确认；生成后到订阅用户分配节点或分组，沿用统一订阅 URL。这里不会修改核心、创建 Client 或自动授权。</p>`+
 field('input','候选地址 / TXT / CSV 内容','','textarea')+
 `<label class="wide">导入 UTF-8 TXT / CSV 文件<input type="file" id="preferred-file" accept=".txt,.csv,text/plain,text/csv"></label>`+
 field('source_mode','外部优选源格式','feed','select',[['feed','TXT / CSV / Base64 地址列表'],['edgetunnel','edgetunnel BEST_SUB 生成器']])+field('source_url','可选：HTTPS 优选 API / 生成器地址','')+
 `<div class="wide"><button type="button" id="preferred-fetch">读取外部优选源</button><button type="button" id="preferred-random">随机 Cloudflare 候选（未测速）</button><p id="preferred-source-status" role="status"></p><button type="button" id="preferred-preview">预览生成计划</button><div id="preferred-plan" role="status"></div></div>`+
 field('confirmed','确认候选内容与目标分组；网络尚未验证，不会自动分配给用户',false,'checkbox'),async()=>{
  if(!plan)throw Error('请先预览生成计划');if(!scope.querySelector('[name=confirmed]').checked)throw Error('请确认预览中的生成计划');
  const result=await api('catalog/preferred/generate','POST',{...selection(),request_id:request,plan_hash:plan.hash,confirmed:true});notice(`已创建 ${result.created} 个节点，复用 ${result.reused} 个；请到订阅用户分配该分组，客户端更新原订阅链接即可。`);
 });
 const scope=$('#fields');scope.querySelector('[name=input]').maxLength=524288;scope.querySelector('[name=input]').rows=8;scope.querySelector('[name=input]').placeholder='104.16.1.1:443#线路备注\n[2606:4700::1]:8443#IPv6\n优选域名.example:443#线路备注';
 const value=n=>scope.querySelector('[name='+n+']').value;
 const selection=()=>({template_node_id:value('template_node_id'),group_id:value('group_id'),group_name:value('group_name'),prefix:value('prefix'),port:Number(value('port')),limit:Number(value('limit')),input:value('input')});
 const invalidate=()=>{serial++;plan=null;request=makeID();scope.querySelector('[name=confirmed]').checked=false;scope.querySelector('#preferred-plan').textContent='内容已变更，请重新预览。'};
 scope.addEventListener('input',e=>{if(e.target.name!=='confirmed')invalidate()});scope.addEventListener('change',e=>{if(e.target.name!=='confirmed')invalidate()});
 const busy=async(button,action)=>{button.disabled=true;try{await action()}catch(e){scope.querySelector('#preferred-source-status').textContent=e.message}finally{button.disabled=false}};
 scope.querySelector('#preferred-file').onchange=async e=>{const f=e.target.files[0];if(!f)return;if(f.size>524288){scope.querySelector('#preferred-source-status').textContent='文件不能超过512KiB';return}scope.querySelector('[name=input]').value=await f.text();invalidate();scope.querySelector('#preferred-source-status').textContent='文件已读入，等待预览。'};
 scope.querySelector('#preferred-fetch').onclick=e=>busy(e.currentTarget,async()=>{const run=serial;const p=await api('catalog/preferred/import','POST',{url:value('source_url'),mode:value('source_mode'),port:Number(value('port')),limit:Number(value('limit'))});if(!scope.isConnected||run!==serial)return;scope.querySelector('[name=input]').value=p.entries.map(n=>(n.address.includes(':')?'['+n.address+']':n.address)+':'+n.port+(n.note?'#'+n.note:'')).join('\n');invalidate();scope.querySelector('#preferred-source-status').textContent=`已读取 ${p.entries.length} 条；无效 ${p.invalid} 条、重复 ${p.duplicates} 条、超出数量 ${p.truncated} 条。外部账号认证信息不会导入。`});
 scope.querySelector('#preferred-random').onclick=e=>busy(e.currentTarget,async()=>{const run=serial;const p=await api('catalog/preferred/random','POST',{count:Math.min(Number(value('limit')),100),port:Number(value('port'))});if(!scope.isConnected||run!==serial)return;scope.querySelector('[name=input]').value=p.input;invalidate();scope.querySelector('#preferred-source-status').textContent='已抽样 Cloudflare 公网 IPv4 候选（最多100条）；没有测速，不能保证各运营商可用。'});
 scope.querySelector('#preferred-preview').onclick=async()=>{const run=++serial;plan=null;scope.querySelector('[name=confirmed]').checked=false;const output=scope.querySelector('#preferred-plan');output.textContent='正在解析候选并检查模板…';try{const p=await api('catalog/preferred/preview','POST',selection());if(run!==serial||!scope.isConnected)return;plan=p;request=makeID();const profile=p.template.profile;output.innerHTML=`<p>生成计划：${p.entries.length} 个候选 → ${esc(p.group_name)}；复用 ${p.entries.filter(n=>n.existing_node_id).length} 个。无效 ${p.invalid}、重复 ${p.duplicates}、超出数量 ${p.truncated}。</p><p>保留 Host ${esc(profile.host||'—')} / SNI ${esc(profile.sni)} / Path ${esc(profile.path||profile.service_name||'—')} / Mode ${esc(profile.xhttp_mode||'—')}。网络状态：未验证。</p>${table(['节点名称','连接地址','端口','操作'],p.entries.map(n=>[esc(n.name),esc(n.address),n.port,n.existing_node_id?'复用已有节点':'新建入口 / 节点']))}`}catch(e){if(run===serial&&scope.isConnected)output.textContent=e.message}};
 request=makeID();
};

const preferredGroupsPage=views.nodegroups;
const syncStates={pending:'等待同步',ok:'已同步',degraded:'部分可用',stale:'沿用上次发布',blocked:'入口已变更，未覆盖',cached:'沿用缓存',error:'拉取失败',expired:'缓存已过期'};
views.nodegroups=async()=>{
 const [page,jobs]=await Promise.all([preferredGroupsPage(),api('catalog/preferred-sync')]);
 return page+`<section class="card panel spacer"><div class="toolbar"><h2>定时优选同步</h2><button data-action="preferred-sync-create" class="primary">＋ 新建同步组</button></div><p class="muted">给订阅用户分配同步组一次即可。之后只轮换已授权入口的地址和端口，客户端刷新原订阅链接获取更新。</p>${table(['同步组','状态 / 间隔','来源 / 最近有效','最近发布','操作'],jobs.map(j=>[
 esc(j.config.name)+`<small>${j.config.count} 个固定入口</small>`,
 esc(j.enabled?(syncStates[j.status]||j.status):'已暂停')+`<small>${j.config.minutes} 分钟</small>`,
 j.config.sources.map((u,i)=>`<div>${esc(new URL(u).hostname+new URL(u).pathname)}<small>${esc(syncStates[j.sources[i]?.status]||'未知')} · ${date(j.sources[i]?.last_good)}</small></div>`).join(''),
 date(j.last_success)+`<small>最近尝试 ${date(j.last_attempt)}</small>`,
 `<button data-action="preferred-sync-edit" data-id="${esc(j.id)}">编辑来源</button>`+(j.enabled?`<button data-action="preferred-sync-run" data-id="${esc(j.id)}">立即同步</button>`:'')+`<button data-action="preferred-sync-toggle" data-id="${esc(j.id)}" data-enabled="${j.enabled?'false':'true'}">${j.enabled?'暂停':'恢复'}</button>`
 ]))}</section>`;
};
actions['preferred-sync-create']=async()=>{
 const nodes=(await api('catalog/nodes')).filter(n=>n.status==='ready'&&n.profile.tls&&!n.profile.reality&&['ws','xhttp','grpc'].includes(n.profile.transport)&&['vless','vmess','trojan'].includes(n.profile.protocol));
 if(!nodes.length)throw Error('请先准备 TLS WS/XHTTP/gRPC 模板节点');
 edit('新建定时优选同步组',
 field('name','同步组名称','US 优选')+
 field('template_node_id','节点模板',nodes[0].id,'select',nodes.map(n=>[n.id,n.name]))+
 field('sources','公开 HTTPS 来源，每行一个','https://bestcf.pages.dev/s5gy/us.txt\nhttps://bestcf.pages.dev/tiancheng2/us.txt','textarea')+
 field('count','固定入口数量（0 = 首次全部，最多 500）',0,'number')+
 field('minutes','同步间隔（分钟）',10,'number')+
 field('extra_port','同时允许 8443（需确认线路兼容）',false,'checkbox')+
 '<p class="wide info-box">同步只替换地址和端口，保留模板的 Host、SNI 和协议参数。填 0 时按首次读取的全部有效去重候选建立固定入口（最多 500）。之后来源增加不会自动新增入口；创建后请给订阅用户分配该组一次。建议保留原域名节点作为备用。来源失败时保留已有结果。</p>'+
 field('confirmed','确认信任这些来源并允许定时替换连接地址',false,'checkbox'),
 async d=>{await api('catalog/preferred-sync','POST',{name:d.name,template_node_id:d.template_node_id,sources:d.sources.split(/\r?\n/).map(v=>v.trim()).filter(Boolean),count:Number(d.count),minutes:Number(d.minutes),ports:d.extra_port?[443,8443]:[443],confirmed:!!d.confirmed});notice('同步组已创建，请到订阅用户分配该组；后续来源数量增加不会自动创建或授权入口。')});
 $('#fields [name=sources]').maxLength=16400;
};
actions['preferred-sync-run']=async b=>{const j=await api('catalog/preferred-sync/'+b.dataset.id+'/run','POST',{});await render();notice(syncStates[j.status]||j.status)};
actions['preferred-sync-toggle']=async b=>{await api('catalog/preferred-sync/'+b.dataset.id,'PATCH',{enabled:b.dataset.enabled==='true'});await render()};

actions['preferred-sync-edit']=async b=>{
 const j=(await api('catalog/preferred-sync')).find(v=>v.id===b.dataset.id);if(!j)throw Error('同步组不存在');
 edit('编辑同步来源',field('sources','公开 HTTPS 来源，每行一个',j.config.sources.join('\n'),'textarea')+
 field('minutes','同步间隔（分钟）',j.config.minutes,'number')+
 field('confirmed','确认信任这些来源并允许更新已授权入口',false,'checkbox'),
 d=>api('catalog/preferred-sync/'+j.id+'/sources','PUT',{sources:d.sources.split(/\r?\n/).map(v=>v.trim()).filter(Boolean),minutes:d.minutes,revision:j.revision,confirmed:d.confirmed}));
 $('#fields [name=sources]').maxLength=16400;
};
