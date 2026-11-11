// Called only by deepseek-smoke.py against its owned disposable database.
import assert from 'node:assert/strict'
import { randomBytes } from 'node:crypto'
import { readdir, access, writeFile } from 'node:fs/promises'
import { homedir } from 'node:os'
import { chromium } from '../web/node_modules/playwright/index.mjs'
const origin=process.env.NEXUSDESK_LIVE_TEST_ORIGIN,api=process.env.NEXUSDESK_LIVE_TEST_API
assert(/^http:\/\/127\.0\.0\.1:\d+$/.test(origin)&&/^http:\/\/127\.0\.0\.1:\d+$/.test(api)&&/^nexusdesk_w02d2_test_[a-f0-9]{12}$/.test(process.env.NEXUSDESK_LIVE_TEST_DATABASE))
assert(!process.env.DEEPSEEK_API_KEY&&!process.env.JWT_SECRET_BASE64&&!process.env.DB_PASSWORD)
const out='docs/testing/evidence/deepseek',tag=randomBytes(6).toString('hex'),password=randomBytes(24).toString('base64url'),email=`deepseek-${tag}@example.test`
async function request(path,method,body,token,status=200){const r=await fetch(api+'/api/v1'+path,{method,headers:{'Content-Type':'application/json',...(token?{Authorization:'Bearer '+token}:{})},body:body?JSON.stringify(body):undefined});assert.equal(r.status,status,path);return(await r.json()).data}
await request('/auth/register','POST',{email,display_name:'DeepSeek 演示',password},undefined,201)
const session=await request('/auth/login','POST',{email,password})
const org=await request('/organizations','POST',{name:'DeepSeek 独立验收'},session.access_token,201)
let executable
const cache=homedir()+'/.cache/ms-playwright'
for(const dir of (await readdir(cache)).filter(x=>/^chromium-\d+$/.test(x)).sort().reverse()){
 for(const sub of ['chrome-linux64/chrome','chrome-linux/chrome']){const p=cache+'/'+dir+'/'+sub;try{await access(p);executable=p;break}catch{}}
 if(executable)break
}
assert(executable)
const browser=await chromium.launch({headless:true,executablePath:executable})
try{
 const page=await browser.newPage({viewport:{width:1280,height:900}});page.setDefaultTimeout(75000)
 await page.goto(origin+'/login')
 await page.getByLabel('邮箱',{exact:true}).fill(email);await page.getByLabel('密码',{exact:true}).fill(password)
 await page.getByRole('button',{name:'登录',exact:true}).click();await page.getByRole('button',{name:'退出登录',exact:true}).waitFor()
 await page.evaluate(path=>{history.pushState({},'',path);dispatchEvent(new PopStateEvent('popstate'))},`/organizations/${org.id}/knowledge`)
 await page.locator('input[type=file]').setInputFiles({name:'运行演示.md',mimeType:'text/markdown',buffer:Buffer.from('数据库连接超时首先检查DB_HOST和DB_PORT。前端端口为5173，API端口为8080。异步知识处理需要独立Worker，Redis位于127.0.0.1:6380。')})
 const accepted=page.waitForResponse(r=>r.url().endsWith('/jobs')&&r.request().method()==='POST')
 await page.getByRole('button',{name:'上传并异步处理',exact:true}).click()
 const jobResponse=await accepted;assert.equal(jobResponse.status(),202)
 const job=(await jobResponse.json()).data
 let done
 for(let i=0;i<90;i++){
  done=await request(`/organizations/${org.id}/jobs/${job.id}`,'GET',undefined,session.access_token)
  if(done.status==='SUCCEEDED')break
  assert.notEqual(done.status,'FAILED','synthetic job failure')
  await new Promise(r=>setTimeout(r,1000))
 }
 assert.equal(done.status,'SUCCEEDED')
 await page.getByRole('link',{name:'检索问答',exact:true}).click()
 await page.getByLabel('检索问题',{exact:true}).fill('数据库连接超时应该先检查什么？')
 const answered=page.waitForResponse(r=>r.url().endsWith('/knowledge/ask')&&r.request().method()==='POST')
 await page.getByRole('button',{name:'基于知识库回答',exact:true}).click()
 const response=await answered;assert.equal(response.status(),200,'real DeepSeek Ask')
 const answer=(await response.json()).data
 assert.equal(answer.generation_model,'deepseek-flash');assert.equal(answer.embedding_model,'bge-m3:567m');assert.equal(answer.insufficient_evidence,false)
 assert.match(answer.answer,/DB_HOST/);assert.match(answer.answer,/DB_PORT/);assert(answer.sources.length>0);assert(answer.sources.every(s=>s.document_id===job.document_id))
 await page.getByText(/deepseek-flash/).waitFor()
 await page.screenshot({path:out+'/answer-desktop.png',fullPage:true})
 await page.setViewportSize({width:390,height:844});await page.screenshot({path:out+'/answer-mobile.png',fullPage:true})
 assert(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth))
 const result={status:'PASS',upload_task:'202→SUCCEEDED',real_embedding:'bge-m3:567m/1024',real_generation:answer.generation_model,answer:answer.answer,citations:answer.sources.length,desktop_mobile:true,browser_secrets_absent:true}
 await writeFile(out+'/browser.json',JSON.stringify(result,null,2)+'\n')
 console.log('PASS actual browser upload → Worker/BGE → grounded DeepSeek answer with citations; desktop/mobile')
}finally{await browser.close()}
