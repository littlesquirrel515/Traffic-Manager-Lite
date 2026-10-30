/* v2 browser acceptance. All traffic and accounts are temporary fixture data. */
const {spawn}=require('node:child_process'),fs=require('node:fs'),path=require('node:path'),os=require('node:os'),http=require('node:http'),assert=require('node:assert/strict');
const {chromium}=require(process.env.TML_PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(__dirname,'..'),tmp=fs.mkdtempSync(path.join(os.tmpdir(),'tml-v2-e2e-')),output=path.join(root,'dist','ui-verification-v2');fs.mkdirSync(output,{recursive:true});
const password='temporary-fixture-password-12345';let counter=0,browser,backend,logs='';const failures=[];
const fixture=http.createServer((req,res)=>{res.setHeader('Content-Type','application/json');if(req.method!=='GET'||req.url.includes('clear=')){res.statusCode=405;res.end('{}');failures.push('destructive fixture request');return}res.end(req.url==='/traffic'?JSON.stringify({alice:{tx:counter*2000,rx:counter*1000}}):req.url==='/online'?JSON.stringify({alice:0}):'[]')});
async function main(){
 await new Promise(r=>fixture.listen(0,'127.0.0.1',r));const base='http://127.0.0.1:18081';
 backend=spawn(path.join(root,'dist','traffic-manager-lite'+(process.platform==='win32'?'.exe':'')),[],{cwd:root,windowsHide:true,env:{...process.env,TML_CORE_AGENT_URL:'',TML_MANAGED_KEY_FILE:'',TML_LISTEN:'127.0.0.1:18081',TML_DB_PATH:path.join(tmp,'v2.db'),TML_BACKUP_DIR:path.join(tmp,'backups'),TML_CONFIG_ROOT:path.join(tmp,'not-mounted'),TML_ADMIN_PASSWORD:password,TML_SECURE_COOKIE:'false',TML_COLLECT_INTERVAL:'1h',TML_ALLOWED_TARGETS:''}});
 backend.stdout.on('data',b=>logs+=b);backend.stderr.on('data',b=>logs+=b);
 for(let k=0;k<120;k++){try{if((await fetch(base+'/api/v1/health')).ok)break}catch{}await new Promise(r=>setTimeout(r,100))}
 browser=await chromium.launch({headless:true});const page=await browser.newPage({viewport:{width:1440,height:1000}});page.on('pageerror',e=>failures.push(e.message));
 await page.goto(base+'/login.html');await page.locator('[name=username]').fill('admin');await page.locator('[name=password]').fill(password);await page.locator('button[type=submit]').click();await page.waitForURL(base+'/');await page.waitForFunction(()=>typeof csrf==='string'&&csrf.length>0);
 const data=await page.evaluate(async endpoint=>{
  const server=await api('servers','POST',{name:'Remote API fixture',address:'display-only.example',enabled:true});
  const instance=await api('instances','POST',{name:'HY fixture',server_id:server.id,core_type:'hysteria2',api_endpoint:endpoint,api_secret:'fixture-secret',enabled:true});
  if((await api('instances'))[0].config_path)throw Error('configuration path required');
  for(const core of ['xray','v2fly','singbox'])await api('instances','POST',{name:core+' fixture',server_id:server.id,core_type:core,api_endpoint:'127.0.0.1:1',enabled:false});
  return{server,instance};
 },'http://127.0.0.1:'+fixture.address().port);
 await page.goto(base+'/#inbounds');await page.getByRole('button',{name:'登记远程入站',exact:false}).click();await page.locator('#fields [name=tag]').fill('remote-hy');await page.locator('#fields [name=protocol]').selectOption('hysteria2');await page.locator('#fields [name=transport]').selectOption('tcp');await page.locator('#fields [name=listen_port]').fill('443');await page.locator('#fields [name=tls]').check();await page.locator('#edit-form [type=submit]').click();await page.waitForFunction(()=>!document.querySelector('#editor').open);await page.getByText('manual_unverified',{exact:true}).waitFor();
 const model=await page.evaluate(async instance=>{
  const inbound=(await api('catalog/inbound-management')).find(i=>i.instance_id===instance);if(!inbound)throw Error('no manual inbound');
  const credential=await api('catalog/inbounds/'+inbound.id+'/clients','POST',{client_ref:'alice',password:'alice:fixture-password',confirmed:true});
  const request={inbound_id:inbound.id,name:'Remote entrance',enabled:true,request_id:'v2-e2e-request-0001',profile:{address:'edge.example.com',port:443,sni:'origin.example.com',tls:true}};
  const entrance=await api('catalog/connections','POST',request),same=await api('catalog/connections','POST',request);if(entrance.node_id!==same.node_id)throw Error('duplicate node created');
  const adopted=await api('catalog/subscriber-provisioning','POST',{request_id:'browser-native-adopt-001',subscriber:{name:'New Subscriber',enabled:true},mode:'native',inbound_id:inbound.id,credential_id:credential.id,confirmed:true});const subscriber={id:adopted.subscriber_id};if(adopted.status!=='ready')throw Error('native client not adopted');
  let denied=false;try{await api('catalog/subscriber-provisioning','POST',{request_id:'browser-native-adopt-002',subscriber:{name:'Second Subscriber',enabled:true},mode:'native',inbound_id:inbound.id,credential_id:credential.id,confirmed:true})}catch{denied=true}if(!denied)throw Error('double meter ownership');
  const group=await api('catalog/groups','POST',{name:'Remote group',node_ids:[entrance.node_id],enabled:true});
  const delivery=await api('catalog/subscriptions','POST',{subscriber_id:subscriber.id,name:'Remote profile',enabled:true,format:'mihomo',group_ids:[group.id],options:{template:'full',group_name:'Remote group',default_route:'proxy'}});
  const preview=await api('catalog/subscriptions/'+delivery.id+'/preview','POST',{});if(preview.usable!==1)throw Error('empty subscription');return{inbound:inbound.id,subscriber:subscriber.id,token:delivery.token};
 },data.instance.id);
 const response=await fetch(base+'/sub/'+model.token);assert.equal(response.status,200);const profile=await response.text();assert.ok(profile.includes('proxies:'));assert.ok(profile.includes('Remote entrance'));assert.equal((await fetch(base+'/sub/mihomo/removed-legacy-token')).status,404);
 // Hosts uses the persisted inventory even when core APIs are disabled/offline.
 const storedChoices=await page.evaluate(async()=>{const instance=(await api('instances')).find(i=>i.core_type==='singbox');const rows=[];for(const tag of ['choice-active','choice-retired']){const v=await api('catalog/inbounds','POST',{instance_id:instance.id,tag,profile:{protocol:'vless',transport:'ws',enabled:true}});rows.push(v.id)}await api('catalog/inbounds/'+rows[1],'PATCH',{display_name:'Retired fixture',retired:true,confirmed:true});return rows});
 await page.goto(base+'/#hosts');await page.locator('[data-action=connection-create]').first().click();await page.waitForFunction(()=>document.querySelector('#editor').open&&document.querySelector('#fields [name=inbound_id]')?.options.length===4);
 const choiceSelect=page.locator('#fields [name=inbound_id]');assert.equal(await choiceSelect.locator('option').count(),4);assert.equal(await choiceSelect.locator('option[value="'+storedChoices[1]+'"]').evaluate(o=>o.disabled),true,JSON.stringify({failures,html:await choiceSelect.innerHTML()}));assert.ok((await choiceSelect.locator('option[value="'+storedChoices[1]+'"]').innerText()).includes('已退役'));
 await choiceSelect.selectOption(storedChoices[0]);await page.locator('#fields [name=entry_protocol]').selectOption('hysteria2');assert.equal(await choiceSelect.inputValue(),'');assert.equal(await choiceSelect.locator('option[value="'+storedChoices[0]+'"]').count(),0);
 await page.locator('#entry-clear-filters').click();assert.equal(await choiceSelect.locator('option').count(),4);assert.equal(await choiceSelect.inputValue(),'');await choiceSelect.selectOption(storedChoices[0]);const hyID=String(data.instance.id);await page.locator('#fields [name=entry_instance]').selectOption(hyID);assert.equal(await choiceSelect.inputValue(),'');assert.equal(await choiceSelect.locator('option').count(),2);assert.equal(await page.locator('#fields [name=entry_protocol] option[value=vless]').count(),0);await page.locator('#entry-clear-filters').click();await choiceSelect.selectOption(storedChoices[0]);await page.locator('#fields [name=entry_search]').fill('no matching inbound');assert.equal(await choiceSelect.inputValue(),'');assert.equal(await choiceSelect.locator('option').count(),1);await page.locator('#cancel-dialog').click();
	await page.evaluate(id=>{inboundDetailID=id;location.hash='inbounddetail'},model.inbound);await page.locator('[data-action=connection-create]').first().click();await page.locator('#fields [name=name]').fill('Browser secondary');
	assert.equal(await page.locator('#fields select[name^=mode_]').count(),4);assert.equal(await page.locator('#fields [name=path]').count(),0);assert.equal(await page.locator('#fields [name=xhttp_mode]').count(),0);
	for(const [key,value]of [['address','secondary.example.com'],['port','8443'],['sni','origin.example.com']]){await page.locator('#fields [name=mode_'+key+']').selectOption('set');await page.locator('#fields [name='+key+']').fill(value)}
	await page.locator('#fields [name=name]').click();assert.ok((await page.locator('#entry-preview').innerText()).includes('secondary.example.com'));await page.locator('#edit-form [type=submit]').click();await page.waitForFunction(()=>!document.querySelector('#editor').open);
	assert.equal(await page.evaluate(async id=>(await api('catalog/inbounds/'+id+'/clients')).length,model.inbound),1);
 const nativeClient=await page.evaluate(async inbound=>api('catalog/inbounds/'+inbound+'/clients','POST',{client_ref:'native-pool-user',uuid:'11111111-1111-4111-8111-111111111111',confirmed:true}),storedChoices[0]);
 await page.goto(base+'/#subscribers');await page.locator('[data-action=adopt-native-client][data-id="'+nativeClient.id+'"]').click();await page.locator('#fields [name=name]').fill('Native pool subscriber');await page.locator('#fields [name=confirmed]').check();await page.locator('#edit-form [type=submit]').click();await page.waitForFunction(()=>!document.querySelector('#editor').open);
 assert.ok(await page.evaluate(async()=>(await api('catalog/subscribers')).some(s=>s.name==='Native pool subscriber')));assert.equal(await page.locator('[data-action=remote-runtime]').count(),0);
 // Existing assignments are recalled from saved subscriptions, not inferred
 // from all nodes sharing an inbound. Searching must preserve both old/new checks.
 const assignmentNodes=await page.evaluate(async inbound=>{const n=await api('catalog/nodes');return{old:n.find(n=>n.inbound_id===inbound&&n.name==='Remote entrance').id,extra:n.find(n=>n.inbound_id===inbound&&n.name==='Browser secondary').id}},model.inbound);
 await page.locator('[data-action=subscriber-add-client][data-id="'+model.subscriber+'"]').click();
 await page.waitForFunction(()=>document.querySelector('#editor').open&&document.querySelector('#subscriber-node-choices'),{},{timeout:5000});
 const assignedBox=page.locator('[data-subscriber-node="'+assignmentNodes.old+'"]'),extraBox=page.locator('[data-subscriber-node="'+assignmentNodes.extra+'"]');
 assert.equal(await assignedBox.isChecked(),true);assert.equal(await assignedBox.isDisabled(),true);assert.equal(await extraBox.isChecked(),false);
 await extraBox.check();await page.locator('#fields [name=subscriber_node_search]').fill('no matching node');await page.locator('#fields [name=subscriber_node_search]').fill('');
 assert.equal(await assignedBox.isChecked(),true);assert.equal(await extraBox.isChecked(),true);
 await page.locator('#cancel-dialog').click();await page.locator('[data-action=subscriber-add-client][data-id="'+model.subscriber+'"]').click();
 await page.waitForFunction(()=>document.querySelector('#editor').open&&document.querySelector('#subscriber-node-choices'),{},{timeout:5000});
 assert.equal(await assignedBox.isChecked(),true);assert.equal(await extraBox.isChecked(),false);
 await extraBox.check();await page.locator('#subscriber-node-preview').click();
 await page.waitForFunction(()=>document.querySelector('#subscriber-node-plan').textContent.includes('可以配置'),{},{timeout:5000});
 assert.ok((await page.locator('#subscriber-node-plan').innerText()).includes('已选 1 个节点'));
 await page.locator('#fields [name=confirmed]').check();await page.locator('#edit-form [type=submit]').click();await page.waitForFunction(()=>!document.querySelector('#editor').open,{},{timeout:10000});
 await page.locator('[data-action=subscriber-add-client][data-id="'+model.subscriber+'"]').click();await page.waitForFunction(()=>document.querySelector('#editor').open&&document.querySelector('#subscriber-node-choices'),{},{timeout:5000});
 assert.equal(await assignedBox.isChecked(),true);assert.equal(await extraBox.isChecked(),true);assert.equal(await extraBox.isDisabled(),true);await page.locator('#cancel-dialog').click();
 // Prepare only: existing static/native subscription stays valid until a
 // separately confirmed core configuration change. No fake dynamic users.
 await page.goto(base+'/#subscribers');await page.locator('[data-action=hy-auth-prepare][data-id="'+model.inbound+'"]').click();
 await page.waitForFunction(()=>document.querySelector('#editor').open&&document.querySelector('#edit-title').textContent==='准备 Hysteria2 HTTP 认证',{},{timeout:5000});
 await page.locator('#fields [name=confirmed]').check();await page.locator('#edit-form [type=submit]').click();
 await page.waitForFunction(()=>document.querySelector('#hy-auth-config')||document.querySelector('#form-error')?.textContent,{},{timeout:5000});assert.equal(await page.locator('#form-error').innerText(),'');
 const callbackConfig=await page.locator('#hy-auth-config').inputValue();assert.ok(callbackConfig.includes('type: http'));assert.ok(callbackConfig.includes('http://traffic-manager-lite:8080/core-auth/hysteria2/'));await page.locator('#cancel-dialog').click();
 const authState=await page.evaluate(async()=>api('catalog/hysteria-auth'));assert.equal(authState.length,1);assert.equal(authState[0].confirmed,0);assert.equal(authState[0].account_count,1);assert.equal(authState[0].last_success_at,null);assert.ok(!JSON.stringify(authState).includes('token_hash'));
 const noGhost=await page.evaluate(async inbound=>{const before=(await api('catalog/subscribers')).length;let denied=false;try{await api('catalog/subscriber-provisioning','POST',{request_id:'browser-unconfirmed-hy-001',subscriber:{name:'Must not exist',enabled:true},inbound_id:inbound,mode:'managed',confirmed:true})}catch{denied=true}return denied&&before===(await api('catalog/subscribers')).length},model.inbound);assert.equal(noGhost,true);if(process.env.TML_E2E_HY_ONLY){console.log('PASS: bounded HY preparation UI, credential redaction and unconfirmed creation rejection');return}
 // v2.1 ordinary form exposes nodes, not core Inbound selection. The fixture
 // core is intentionally unreachable: pending is expected, never fake success.
 const nodeFlow=await page.evaluate(async server=>{
  const x=(await api('instances')).find(i=>i.core_type==='xray');
  await api('instances/'+x.id,'PATCH',{server_id:server,name:x.name,core_type:'xray',api_endpoint:x.api_endpoint,enabled:true});
  const inbound=await api('catalog/inbounds','POST',{instance_id:x.id,tag:'node-workflow',profile:{protocol:'vless',transport:'ws',enabled:true}});
  const node=await api('catalog/connections','POST',{inbound_id:inbound.id,name:'Business node fixture',enabled:true,profile:{address:'node.example.com',port:443,sni:'origin.example.com'}});
  return node;
 },data.server.id);
 await page.goto(base+'/#subscribers');await page.locator('[data-kind=subscribers][data-action=catalog-edit]').first().click();
 await page.waitForFunction(()=>document.querySelector('#editor').open&&document.querySelector('#subscriber-node-choices'),{},{timeout:5000});
 assert.equal(await page.locator('#fields [name=inbound_id]').count(),0);
 assert.equal(await page.locator('#fields [name=mode]').count(),0);
 await page.locator('#fields [name=name]').fill('Business node subscriber');
 await page.locator('[data-subscriber-node="'+nodeFlow.node_id+'"]').check();await page.locator('#subscriber-node-preview').click();
 await page.waitForFunction(()=>document.querySelector('#subscriber-node-plan').textContent.includes('可以配置'),{},{timeout:5000});
 for(const width of [360,768,1440]){await page.setViewportSize({width,height:1000});assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'node form overflow '+width);await page.screenshot({path:path.join(output,'subscriber-node-form-'+width+'.png'),fullPage:true})}
 await page.locator('#fields [name=confirmed]').check();await page.locator('#edit-form [type=submit]').click();
 await page.waitForFunction(()=>!document.querySelector('#editor').open,{},{timeout:10000});
 const businessUser=await page.evaluate(async()=>{const users=await api('catalog/subscribers'),requests=await api('catalog/subscriber-node-requests'),grants=await api('catalog/subscriber-node-grants');return{user:users.find(u=>u.name==='Business node subscriber'),requests,grants}});
 assert.equal(businessUser.user.enabled,0);assert.equal(businessUser.user.node_authorization_required,1);assert.ok(businessUser.requests.some(p=>p.subscriber_id===businessUser.user.id&&p.status==='pending'));
 assert.ok(businessUser.grants.some(g=>g.subscriber_id===businessUser.user.id&&g.node_id===nodeFlow.node_id));
 await page.locator('[data-action=subscriber-node-details][data-id="'+businessUser.requests.find(p=>p.subscriber_id===businessUser.user.id).request_id+'"]').click();await page.waitForFunction(()=>document.querySelector('#edit-title').textContent==='节点认证配置详情',{},{timeout:5000});assert.ok((await page.locator('#fields').innerText()).includes('pending'));await page.locator('#cancel-dialog').click();
 for(const format of ['raw','base64','mihomo','singbox']){const r=await fetch(base+'/sub/'+model.token+'?format='+format);assert.equal(r.status,200,format);assert.ok((await r.text()).length>20)}
 await page.evaluate(id=>api('instances/'+id+'/collect','POST',{}),data.instance.id);counter=3;await page.evaluate(id=>api('instances/'+id+'/collect','POST',{}),data.instance.id);
 const facts=await page.evaluate(async id=>({clients:await api('catalog/inbounds/'+id+'/clients'),traffic:await api('traffic/summary?dimension=instance&scope=user&range=all')}),model.inbound);assert.equal(facts.clients.length,1);assert.ok(facts.traffic.some(v=>v.upload===6000&&v.download===3000));
 await page.evaluate(id=>{inboundDetailID=id},model.inbound);
 const targets=['dashboard','instances','traffic','online','settings','diagnostics','hosts','nodegroups','subscribers','catalogsubs','subsettings','inbounds','inbounddetail','capabilities','logs','servers','quota','collectionstatus'];let checks=0;
 for(const width of [360,768,1440]){await page.setViewportSize({width,height:1000});for(const target of targets){await page.evaluate(t=>{location.hash=t},target);await page.waitForFunction(()=>!document.querySelector('#content').textContent.includes('正在加载'));await page.waitForTimeout(150);assert.equal(await page.locator('#content').getByText('数据加载失败',{exact:true}).count(),0,target);assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),`${target}: overflow ${width}`);checks++}await page.evaluate(()=>{location.hash='inbounds'});await page.waitForTimeout(250);await page.screenshot({path:path.join(output,'remote-fixture-'+width+'.png'),fullPage:true})}
 const nav=await page.locator('#sidebar').innerText();for(const removed of ['旧版','全局节点库','配置调和','重复数据修复','托管用户'])assert.ok(!nav.includes(removed));
 await page.evaluate(()=>{location.hash='instances'});await page.getByRole('button',{name:'接入实例',exact:false}).click();assert.equal(await page.locator('#fields [name=config_path]').count(),0);assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));await page.locator('#cancel-dialog').click();
 assert.deepEqual(failures,[]);console.log(`PASS: API-only empty onboarding, manual inbound/client, idempotent entry/node, unique meter owner, 4 renderers, HY HTTP-auth preparation without phantom users or native subscription breakage, HY directional fixture traffic, v2.1 existing-assignment recall/search/reopen/delta-submit and node-only creation/preview/pending tasks and 3 form viewports, ${checks} responsive page checks; no configuration mounts or core writes`);
}
main().catch(e=>{console.error(e);console.error('Browser errors:',failures);console.error(logs);process.exitCode=1}).finally(async()=>{if(browser)await browser.close();if(backend)backend.kill();fixture.close()});
