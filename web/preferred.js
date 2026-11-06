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
 return page+`<section class="card panel spacer"><div class="toolbar"><h2>定时优选同步</h2><button data-action="preferred-sync-create" class="primary">＋ 新建同步组</button></div><p class="muted">给订阅用户分配同步组一次即可。来源增减时自动维护有效节点；旧数据中完整授权的组会转换为动态授权，部分授权保持原样。新增入站需已有可用用户凭据。</p>${table(['同步组','状态 / 间隔','来源 / 最近有效','最近发布','操作'],jobs.map(j=>[
 esc(j.config.name)+`<small>当前发布 ${j.active_count} 个 · 候选 ${j.candidate_count} 个 · 已建 ${j.config.count} 个 · 上限 ${j.config.limit||"自动（最多 500）"}</small>`+(j.limit_reached?`<small>候选超过上限或单来源截断，部分候选未发布</small>`:''),
 esc(j.enabled?(syncStates[j.status]||j.status):'已暂停')+`<small>${j.config.minutes} 分钟</small>`,
 j.config.sources.map((u,i)=>`<div>${esc(new URL(u).hostname+new URL(u).pathname)}<small>${esc(syncStates[j.sources[i]?.status]||'未知')} · ${date(j.sources[i]?.last_good)}</small></div>`).join(''),
 date(j.last_success)+`<small>最近尝试 ${date(j.last_attempt)}</small>`,
 `<button data-action="preferred-sync-history" data-id="${esc(j.id)}">同步记录</button><button data-action="preferred-sync-edit" data-id="${esc(j.id)}">编辑来源 / 容量</button>`+(j.enabled?`<button data-action="preferred-sync-run" data-id="${esc(j.id)}">立即同步</button>`:'')+`<button data-action="preferred-sync-toggle" data-id="${esc(j.id)}" data-enabled="${j.enabled?'false':'true'}">${j.enabled?'暂停':'恢复'}</button>`
 ]))}</section>`;
};
actions['preferred-sync-create']=async()=>{
 const nodes=(await api('catalog/nodes')).filter(n=>n.status==='ready'&&n.profile.tls&&!n.profile.reality&&['ws','xhttp','grpc'].includes(n.profile.transport)&&['vless','vmess','trojan'].includes(n.profile.protocol));
 if(!nodes.length)throw Error('请先准备 TLS WS/XHTTP/gRPC 模板节点');
 edit('新建定时优选同步组',
 field('name','同步组名称','US 优选')+
 field('template_node_id','节点模板',nodes[0].id,'select',nodes.map(n=>[n.id,n.name]))+
 field('sources','公开 HTTPS 来源，每行一个','https://bestcf.pages.dev/s5gy/us.txt\nhttps://bestcf.pages.dev/tiancheng2/us.txt','textarea')+
 field('limit','最多发布节点数（0 = 自动，最多 500）',0,'number')+
 field('minutes','同步间隔（分钟）',10,'number')+
 field('extra_port','同时允许 8443（需确认线路兼容）',false,'checkbox')+
 '<p class="wide info-box">同步只替换地址和端口，保留模板的 Host、SNI 和协议参数。上限是最多发布的节点数，填 0 表示自动采用系统上限 500；当前候选不足上限时只发布实际数量，以后随来源增减。超过上限的候选不会发布。创建后请给订阅用户分配该组一次。建议保留原域名节点作为备用。来源失败时保留已有结果。</p>'+
 field('confirmed','确认信任这些来源并允许定时替换连接地址',false,'checkbox'),
 async d=>{await api('catalog/preferred-sync','POST',{name:d.name,template_node_id:d.template_node_id,sources:d.sources.split(/\r?\n/).map(v=>v.trim()).filter(Boolean),limit:Number(d.limit),minutes:Number(d.minutes),ports:d.extra_port?[443,8443]:[443],confirmed:!!d.confirmed});notice('同步组已创建，请到订阅用户分配该组；来源变化会自动更新该组有效节点。')});
 $('#fields [name=sources]').maxLength=16400;
};
actions['preferred-sync-run']=async b=>{const j=await api('catalog/preferred-sync/'+b.dataset.id+'/run','POST',{});await render();notice(syncStates[j.status]||j.status)};
actions['preferred-sync-toggle']=async b=>{await api('catalog/preferred-sync/'+b.dataset.id,'PATCH',{enabled:b.dataset.enabled==='true'});await render()};

actions['preferred-sync-edit']=async b=>{
 const j=(await api('catalog/preferred-sync')).find(v=>v.id===b.dataset.id);if(!j)throw Error('同步组不存在');
 edit('编辑同步来源与容量',field('sources','公开 HTTPS 来源，每行一个',j.config.sources.join('\n'),'textarea')+
 field('minutes','同步间隔（分钟）',j.config.minutes,'number')+
 field('limit','最多发布节点数（0 = 自动，最多 500）',j.config.limit??0,'number')+
 '<p class="wide info-box">上限是最多发布的节点数，0 表示自动采用系统上限 500；不足上限时发布实际有效数量。降低上限会在下次同步时暂停多余入口，保留节点身份和授权；同步失败时保留上次结果。</p>'+
 field('confirmed','确认信任这些来源并允许更新已授权入口',false,'checkbox'),
 d=>api('catalog/preferred-sync/'+j.id+'/sources','PUT',{sources:d.sources.split(/\r?\n/).map(v=>v.trim()).filter(Boolean),minutes:Number(d.minutes),limit:Number(d.limit),revision:j.revision,confirmed:d.confirmed}));
 $('#fields [name=sources]').maxLength=16400;
};

actions['preferred-sync-history']=async b=>{
 const id=b.dataset.id;const [jobs,runs]=await Promise.all([api('catalog/preferred-sync'),api('catalog/preferred-sync/'+id+'/history')]);const job=jobs.find(j=>j.id===id);
 const trigger={initial:'首次创建',manual:'手动同步',scheduled:'定时同步'},errors={fetch_failed:'拉取失败',invalid_format:'格式无效',no_valid_candidates:'无有效候选',apply_failed:'校验或写入失败，保留上次发布',group_changed:'分组成员被人工修改，保留上次发布',slot_changed:'入口被人工修改，保留上次发布',sources_incomplete:'来源不完整，保留上次发布',slot_count_changed:'入口数量异常，保留上次发布',cancelled:'请求中断，保留上次发布'};
 const summaries=runs.map(run=>{
  const detail=run.sources.map(v=>`<li>来源 ${v.position} · ${esc(v.host||'未知主机')} · 标识 ${esc(v.source_id)}：${esc(syncStates[v.status]||v.status)}；响应 ${v.response_bytes} 字节，返回条目 ${v.returned_count} 条，解析去重 ${v.parsed} 条，有效 ${v.valid} 条，无效 ${v.invalid} 条，重复 ${v.duplicates} 条，截断 ${v.truncated} 条；缓存 ${v.cached_count} 条，耗时 ${v.duration_ms} ms${v.error_code?'；'+esc(errors[v.error_code]||v.error_code):''}</li>`).join('');
  return `<details class="sync-history-item"><summary>${esc(date(run.started_at))} · ${esc(syncStates[run.status]||run.status)} · ${esc(trigger[run.trigger]||run.trigger)} · 发布 ${run.active_count} 个</summary><p>结束 ${esc(date(run.finished_at))}；耗时 ${run.duration_ms} ms；合并去重候选（含缓存） ${run.candidate_count} 个；已建 ${run.slot_count} 个；上限 ${run.limit_value} 个。${run.error_code?esc(errors[run.error_code]||run.error_code):''}</p><ul>${detail}</ul></details>`;
 }).join('');
 edit((job?.config.name||'同步组')+' · 同步记录',`<div class="wide"><p class="muted">最近最多 200 条。来源序号按当次配置记录，标识用于区分同一主机上的来源；不保存地址列表或来源路径。失败时保留上次发布。</p>${summaries||'<p>暂无同步记录；升级前的历史无法补录。</p>'}</div>`,async()=>{});$('#edit-form [type=submit]').textContent='关闭';
};
