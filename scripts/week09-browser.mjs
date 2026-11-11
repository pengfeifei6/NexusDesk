import assert from 'node:assert/strict'
import { randomBytes, randomUUID } from 'node:crypto'
import { readdir, access, writeFile, mkdir } from 'node:fs/promises'
import { homedir } from 'node:os'
import { chromium } from '../web/node_modules/playwright/index.mjs'
const origin=process.env.NEXUSDESK_LIVE_TEST_ORIGIN, api=process.env.NEXUSDESK_LIVE_TEST_API, control=process.env.NEXUSDESK_FIXTURE_CONTROL
for(const url of [origin,api,control])assert(/^http:\/\/127\.0\.0\.1:\d+$/.test(url))
assert(/^nexusdesk_w02d2_test_[a-f0-9]{12}$/.test(process.env.NEXUSDESK_LIVE_TEST_DATABASE))
assert(!process.env.DEEPSEEK_API_KEY&&!process.env.JWT_SECRET_BASE64&&!process.env.DB_PASSWORD)
const out='docs/testing/evidence/week09',tag=randomBytes(6).toString('hex'),password=randomBytes(24).toString('base64url'),email=`w09-${tag}@example.test`
await mkdir(out,{recursive:true})
async function request(path,method,body,token,status=200){const r=await fetch(api+'/api/v1'+path,{method,headers:{...(body?{'Content-Type':'application/json'}:{}),...(token?{Authorization:'Bearer '+token}:{})},body:body?JSON.stringify(body):undefined});assert.equal(r.status,status,path);return(await r.json()).data}
await request('/auth/register','POST',{email,display_name:'第九周验收用户',password},undefined,201)
const session=await request('/auth/login','POST',{email,password}),token=session.access_token
const org=await request('/organizations','POST',{name:'W09 缓存和限流演示'},token,201)
const base=`/organizations/${org.id}`
await request(base+'/tickets','POST',{title:'缓存演示工单',body:'合成内容',priority:3},token,201)
for(let i=0;i<2;i++)assert.equal((await request(base+'/tickets','GET',undefined,token)).items.length,1)
let executable
for(const dir of (await readdir(homedir()+'/.cache/ms-playwright')).filter(x=>/^chromium-\d+$/.test(x)).sort().reverse()){
 for(const sub of ['chrome-linux64/chrome','chrome-linux/chrome']){const p=homedir()+'/.cache/ms-playwright/'+dir+'/'+sub;try{await access(p);executable=p;break}catch{}}
 if(executable)break
}
assert(executable)
const browser=await chromium.launch({headless:true,executablePath:executable})
const context=await browser.newContext({viewport:{width:1280,height:900}})
const page=await context.newPage();page.setDefaultTimeout(15000)
const steps=[]
async function pass(name){steps.push(name);console.log('PASS '+name)}
async function send(text,status){
 await page.getByLabel('消息',{exact:true}).fill(text)
 const pending=page.waitForResponse(r=>r.request().method()==='POST'&&/\/ai\/conversations\/[^/]+\/messages$/.test(new URL(r.url()).pathname))
 await page.getByRole('button',{name:'发送',exact:true}).click()
 const response=await pending;assert.equal(response.status(),status)
 return response
}
try{
 await page.goto(origin+'/login')
 await page.getByLabel('邮箱',{exact:true}).fill(email);await page.getByLabel('密码',{exact:true}).fill(password)
 await page.getByRole('button',{name:'登录',exact:true}).click();await page.getByRole('button',{name:'退出登录',exact:true}).waitFor()
 await page.evaluate(path=>{history.pushState({},'',path);dispatchEvent(new PopStateEvent('popstate'))},`/organizations/${org.id}/tickets`)
 await page.getByText('缓存演示工单',{exact:true}).waitFor()
 await pass('浏览器工单列表正常读取')
 await page.getByRole('link',{name:'AI 对话',exact:true}).click()
 await page.getByLabel('对话标题',{exact:true}).fill('W09 配额验证')
 const created=page.waitForResponse(r=>r.url().endsWith('/ai/conversations')&&r.request().method()==='POST')
 await page.getByRole('button',{name:'新建对话',exact:true}).click();const conversation=(await(await created).json()).data
 await send('第一条合成消息',200)
 await page.getByText('第九周合成流式响应。',{exact:true}).waitFor()
 await page.getByRole('button',{name:'停止生成',exact:true}).waitFor({state:'hidden'})
 await pass('真实 API SSE + 本地确定性模型传输完成')
 const second='限流后保留这条输入'
 const limited=await send(second,429)
 assert(Number(limited.headers()['retry-after'])>0)
 await page.getByRole('alert').filter({hasText:/频繁|限流/}).waitFor()
 assert.equal(await page.getByLabel('消息',{exact:true}).inputValue(),second)
 await page.screenshot({path:out+'/rate-limit.png',fullPage:true})
 await pass('429 与 Retry-After 提示、输入保留')
 await fetch(control+'/redis/stop',{method:'POST'})
 const start=Date.now()
 await request(base+'/tickets','POST',{title:'Redis中断期间创建',body:'合成内容',priority:2},token,201)
 const current=await request(base+'/tickets','GET',undefined,token)
 assert(current.items.some(t=>t.title==='Redis中断期间创建'))
 assert(Date.now()-start<5000)
 const third='存储恢复后再发送'
 await send(third,503)
 await page.getByRole('alert').filter({hasText:/限流|配额/}).waitFor()
 assert.equal(await page.getByLabel('消息',{exact:true}).inputValue(),third)
 await page.screenshot({path:out+'/redis-unavailable.png',fullPage:true})
 const history=await request(base+`/ai/conversations/${conversation.id}/messages`,'GET',undefined,token)
 assert.equal(history.messages.length,2)
 await pass('真实 Redis 停止：列表回源、写入成功、AI 503 且不新增消息')
 await fetch(control+'/redis/start',{method:'POST'})
 const recovered=await request(base+'/tickets','GET',undefined,token)
 assert(recovered.items.some(t=>t.title==='Redis中断期间创建'))
 const kb=await request(base+'/knowledge/documents','GET',undefined,token)
 assert.equal(kb.items.length,0)
 await pass('Redis 恢复后读取新版本与空知识列表')
 await page.setViewportSize({width:390,height:844})
 await page.screenshot({path:out+'/mobile-rate-limit.png',fullPage:true})
 await writeFile(out+'/browser.json',JSON.stringify({status:'PASS',steps,synthetic_model:true,cloud_calls:0},null,2)+'\n')
}finally{await context.close();await browser.close()}
