// Only called against week08-smoke.py's disposable PG/Redis and synthetic data.
import assert from 'node:assert/strict'
import { randomBytes } from 'node:crypto'
import { readdir, access, writeFile, mkdir, rename } from 'node:fs/promises'
import { homedir } from 'node:os'
import { writeFileSync } from 'node:fs'
import { chromium } from '../web/node_modules/playwright/index.mjs'
const origin=process.env.NEXUSDESK_LIVE_TEST_ORIGIN, api=process.env.NEXUSDESK_LIVE_TEST_API
assert(/^http:\/\/127\.0\.0\.1:\d+$/.test(origin)&&/^http:\/\/127\.0\.0\.1:\d+$/.test(api)&&/^nexusdesk_w02d2_test_[a-f0-9]{12}$/.test(process.env.NEXUSDESK_LIVE_TEST_DATABASE))
assert(!process.env.DEEPSEEK_API_KEY&&!process.env.JWT_SECRET_BASE64&&!process.env.DB_PASSWORD)
const out='docs/testing/evidence/week08',tag=randomBytes(6).toString('hex'),password=randomBytes(24).toString('base64url'),email=`week08-${tag}@example.test`
await mkdir(out+'/recording',{recursive:true})
async function request(path,method,body,token,status=200){const r=await fetch(api+'/api/v1'+path,{method,headers:{...(body?{'Content-Type':'application/json'}:{}),...(token?{Authorization:'Bearer '+token}:{})},body:body?JSON.stringify(body):undefined});assert.equal(r.status,status,path);if(status>=400)return;return(await r.json()).data}
await request('/auth/register','POST',{email,display_name:'第八周演示用户',password},undefined,201)
const session=await request('/auth/login','POST',{email,password})
const org=await request('/organizations','POST',{name:'W08 独立验收组织'},session.access_token,201)
const ticket=await request(`/organizations/${org.id}/tickets`,'POST',{title:'待查询工单-'+tag,body:'合成验收工单，状态为OPEN，优先级为高。',priority:3},session.access_token,201)
let executable
for(const dir of (await readdir(homedir()+'/.cache/ms-playwright')).filter(x=>/^chromium-\d+$/.test(x)).sort().reverse()){
 for(const sub of ['chrome-linux64/chrome','chrome-linux/chrome']){const p=homedir()+'/.cache/ms-playwright/'+dir+'/'+sub;try{await access(p);executable=p;break}catch{}}
 if(executable)break
}
assert(executable)
const browser=await chromium.launch({headless:true,executablePath:executable})
const context=await browser.newContext({viewport:{width:1280,height:900},recordVideo:{dir:out+'/recording',size:{width:1280,height:900}}})
const page=await context.newPage();page.setDefaultTimeout(75000)
// Observe bytes consumed by the app without a tee consumer: cancellation still
// propagates through the single TransformStream to the real fetch request.
await page.addInitScript(()=>{
 const original=window.fetch.bind(window);window.__w08Streams=[]
 window.fetch=async(...args)=>{
  const response=await original(...args)
  if(response.ok&&response.body&&response.headers.get('content-type')?.startsWith('text/event-stream')){
   const row={text:''};window.__w08Streams.push(row);const decoder=new TextDecoder()
   const stream=response.body.pipeThrough(new TransformStream({transform(chunk,controller){row.text+=decoder.decode(chunk,{stream:true});controller.enqueue(chunk)},flush(){row.text+=decoder.decode()}}))
   return new Response(stream,{status:response.status,headers:response.headers})
  }
  return response
 }
})
const started=Date.now(), steps=[]
let conversation, firstConversation, captured=0
const pause=ms=>new Promise(r=>setTimeout(r,ms))
function pass(name){steps.push(name);console.log('PASS '+name);writeFileSync(out+'/progress.json',JSON.stringify({steps,elapsed_seconds:Math.round((Date.now()-started)/1000)}))}
async function send(content){
 await page.getByLabel('消息',{exact:true}).fill(content)
 const promised=page.waitForResponse(r=>r.request().method()==='POST'&&/\/ai\/conversations\/[^/]+\/messages$/.test(new URL(r.url()).pathname))
 await page.getByRole('button',{name:'发送',exact:true}).click()
 const response=await promised;assert.equal(response.status(),200,'SSE preflight')
 await page.waitForFunction(()=>{const s=window.__w08Streams.at(-1)?.text||'';return s.includes('event: message_end')||s.includes('event: error')});const text=await page.evaluate(()=>window.__w08Streams.at(-1).text);assert(text.includes('event: message_end'), 'stream did not complete; code='+(/"code":"([A-Z_]+)"/.exec(text)?.[1]||'EOF'))
 const count=(text.match(/event: content_delta/g)||[]).length;captured+=count;assert(count>0)
 await page.getByRole('button',{name:'停止生成',exact:true}).waitFor({state:'hidden'})
 return text
}
async function history(){return request(`/organizations/${org.id}/ai/conversations/${conversation.id}/messages`,'GET',undefined,session.access_token)}
try{
 await page.goto(origin+'/login')
 await page.getByLabel('邮箱',{exact:true}).fill(email);await page.getByLabel('密码',{exact:true}).fill(password)
 await page.getByRole('button',{name:'登录',exact:true}).click();await page.getByRole('button',{name:'退出登录',exact:true}).waitFor()
 await page.evaluate(path=>{history.pushState({},'',path);dispatchEvent(new PopStateEvent('popstate'))},`/organizations/${org.id}/knowledge`)
 await page.locator('input[type=file]').setInputFiles({name:'W08排查指南.md',mimeType:'text/markdown',buffer:Buffer.from('数据库连接超时首先检查DB_HOST和DB_PORT。异步索引失败首先查看knowledge_jobs.failure_code，并确认Worker和Redis运行正常。本文仅为独立验收的合成数据。')})
 const accepted=page.waitForResponse(r=>r.url().endsWith('/jobs')&&r.request().method()==='POST')
 await page.getByRole('button',{name:'上传并异步处理',exact:true}).click();const response=await accepted;assert.equal(response.status(),202);const job=(await response.json()).data
 for(let i=0;i<120;i++){const state=await request(`/organizations/${org.id}/jobs/${job.id}`,'GET',undefined,session.access_token);if(state.status==='SUCCEEDED')break;assert.notEqual(state.status,'FAILED','synthetic ingestion');if(i===119)throw new Error('job timeout');await pause(1000)}
 pass('上传 → Worker → 本地BGE索引')
 await page.getByRole('link',{name:'AI 对话',exact:true}).click()
 await page.getByLabel('对话标题',{exact:true}).fill('第八周：知识查询与受控建单')
 const created=page.waitForResponse(r=>r.url().endsWith('/ai/conversations')&&r.request().method()==='POST')
 await page.getByRole('button',{name:'新建对话',exact:true}).click();conversation=(await(await created).json()).data;firstConversation=conversation
 await page.getByLabel('消息',{exact:true}).waitFor()
 pass('浏览器新建owner私有会话')
 await pause(4000)
 const knowledge=await send('请先调用 search_knowledge 查询：数据库连接超时首先检查什么？只根据检索资料回答。')
 assert(knowledge.includes('event: citation'));let h=await history();const answer=h.messages.filter(m=>m.role==='ASSISTANT').at(-1)
 assert.equal(answer.model_name,'deepseek-flash');assert.match(answer.content,/DB_HOST/);assert(answer.citations.length>0&&answer.citations.every(s=>s.document_id===job.document_id))
 await page.screenshot({path:out+'/chat-sources-desktop.png',fullPage:true});pass('真实DeepSeek流式知识回答与当前组织引用')
 await pause(7000)
 await send(`请调用 get_ticket 查询工单 ${ticket.id}，明确写出它的完整标题、当前状态和优先级，不要创建工单。`)
 h=await history();assert(h.messages.filter(m=>m.role==='ASSISTANT').at(-1).content.includes(tag));pass('真实get_ticket只读查询返回随机合成标题')
 await pause(7000)
 const before=(await request(`/organizations/${org.id}/tickets`,'GET',undefined,session.access_token)).items.length
 await send('请调用 propose_create_ticket 提出一个新工单：标题“排查异步索引失败”，正文“检查Worker与Redis连接，并查看任务failure_code”，优先级高（3）。只提出待确认建议，不要声称已创建。')
 h=await history();assert.equal(h.actions.length,1);const proposal=h.actions[0];assert.equal(proposal.status,'PROPOSED');assert.equal(proposal.ticket_id,null)
 assert.equal((await request(`/organizations/${org.id}/tickets`,'GET',undefined,session.access_token)).items.length,before)
 await page.getByRole('article',{name:'工单提议'}).scrollIntoViewIfNeeded();await page.screenshot({path:out+'/proposal-before-confirm.png',fullPage:true});pass('持久化提议可见且未确认时无工单新增')
 await pause(10000)
 const confirmation=page.waitForResponse(r=>r.url().endsWith('/confirm')&&r.request().method()==='POST')
 await page.getByRole('button',{name:'确认创建工单',exact:true}).click();const confirmed=(await(await confirmation).json()).data;assert.equal(confirmed.status,'EXECUTED');assert(confirmed.ticket_id)
 const repeated=await Promise.all(Array.from({length:4},()=>request(`/organizations/${org.id}/ai/tool-actions/${proposal.id}/confirm`,'POST',undefined,session.access_token)))
 assert(repeated.every(a=>a.ticket_id===confirmed.ticket_id));assert.equal((await request(`/organizations/${org.id}/tickets`,'GET',undefined,session.access_token)).items.length,before+1)
 const audit=await request(`/organizations/${org.id}/tickets/${confirmed.ticket_id}/events`,'GET',undefined,session.access_token)
 assert(audit.items.some(e=>e.event_type==='TICKET_CREATED'&&e.after_data?.ai_proposal_id===proposal.id))
 pass('用户确认 → 工单与审计；并发重复确认返回同一ID')
 await page.screenshot({path:out+'/proposal-executed.png',fullPage:true});await pause(7000)
 await page.getByLabel('对话标题',{exact:true}).fill('第八周：拒绝与取消')
 const createdSecond=page.waitForResponse(r=>r.url().endsWith('/ai/conversations')&&r.request().method()==='POST')
 await page.getByRole('button',{name:'新建对话',exact:true}).click();conversation=(await(await createdSecond).json()).data
 await page.getByLabel('消息',{exact:true}).waitFor()
 await send('请实际调用propose_create_ticket保存一个独立待确认工单：标题“检查数据库连接”，正文“核对DB_HOST和DB_PORT”，优先级普通2。使用propose_create_ticket，不要执行创建。')
 h=await history();assert(h.actions.some(x=>x.status==='PROPOSED'),'model did not save a proposal; prose is not evidence')
 await page.getByRole('button',{name:'拒绝',exact:true}).click();await page.getByRole('heading',{name:'工单提议 · 已拒绝',exact:true}).waitFor();pass('第二个独立提议拒绝且无工单写入')
 h=await history();const rejected=h.actions.find(x=>x.status==='REJECTED');assert(rejected);await request(`/organizations/${org.id}/ai/tool-actions/${rejected.id}/confirm`,'POST',undefined,session.access_token,409)
 const strangerEmail=`week08-other-${tag}@example.test`;await request('/auth/register','POST',{email:strangerEmail,display_name:'其他组织成员',password},undefined,201)
 const stranger=await request('/auth/login','POST',{email:strangerEmail,password});await request('/organizations','POST',{name:'W08其他组织'},stranger.access_token,201)
 await request(`/organizations/${org.id}/ai/conversations/${conversation.id}/messages`,'GET',undefined,stranger.access_token,404);await request(`/organizations/${org.id}/ai/tool-actions/${proposal.id}/confirm`,'POST',undefined,stranger.access_token,404);pass('其他组织读取与确认均404')
 await page.getByLabel('消息',{exact:true}).fill('请用纯文本写一篇约1500字的通用学习计划，不调用工具，逐段输出。')
 const stopping=page.waitForResponse(r=>r.request().method()==='POST'&&/\/ai\/conversations\/[^/]+\/messages$/.test(new URL(r.url()).pathname))
 await page.getByRole('button',{name:'发送',exact:true}).click();assert.equal((await stopping).status(),200);await pause(300);await page.getByRole('button',{name:'停止生成',exact:true}).click()
 for(let i=0;i<40;i++){h=await history();if(h.messages.at(-1).status==='INTERRUPTED')break;await pause(100);if(i===39)throw new Error('cancel did not persist')}
 await page.getByRole('button',{name:'重新读取历史',exact:true}).click();await page.getByRole('heading',{name:'AI · 已中断',exact:true}).waitFor();pass('浏览器停止生成持久化INTERRUPTED')
 await page.setViewportSize({width:390,height:844});await page.screenshot({path:out+'/chat-mobile.png',fullPage:true});assert(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth));pass('移动端布局无横向溢出')
 assert.equal(await page.evaluate(()=>localStorage.length+sessionStorage.length),0)
 await page.setViewportSize({width:1280,height:900})
 await page.getByRole('button',{name:firstConversation.title,exact:true}).click();await page.getByRole('link',{name:`查看工单 #${confirmed.ticket_id}`,exact:true}).click();await page.getByText('排查异步索引失败',{exact:true}).first().waitFor();await page.screenshot({path:out+'/created-ticket.png',fullPage:true});pass('已创建工单可跳转到既有工单工作流')
 // Keep a 3-minute reviewable demo: show ticket/audit and return to the saved chat,
 // rather than storing credentials or external model prompts in a trace archive.
 const remaining=181000-(Date.now()-started)
 if(remaining>0){console.log('Recording saved-result walkthrough');await pause(Math.min(remaining/2,30000));await page.getByRole('link',{name:'AI 对话',exact:true}).click();await page.getByRole('button',{name:firstConversation.title,exact:true}).click();await page.getByRole('heading',{name:'工单提议 · 已执行',exact:true}).scrollIntoViewIfNeeded();await pause(Math.max(0,181000-(Date.now()-started)))}
 const result={status:'PASS',synthetic_data_only:true,model:'deepseek-flash',embedding:'bge-m3:567m',steps,content_delta_events:captured,owner_only:true,unconfirmed_ticket_delta:0,confirmed_ticket_delta:1,duplicate_confirm_requests:4,stop_status:'INTERRUPTED',desktop_mobile:true,browser_credentials_absent:true,duration_seconds:Math.round((Date.now()-started)/1000)}
 await writeFile(out+'/browser.json',JSON.stringify(result,null,2)+'\n')
}finally{await page.screenshot({path:out+'/last-state.png',fullPage:true}).catch(()=>{});const video=page.video();await context.close();if(video){const path=await video.path();await rename(path,out+'/week08-demo.webm')}await browser.close()}
