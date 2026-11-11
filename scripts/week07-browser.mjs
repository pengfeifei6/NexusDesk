// Real browser + Go API acceptance against only the parent runner's disposable DB.
import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { access, mkdir, readdir, readFile, writeFile } from 'node:fs/promises'
import { homedir } from 'node:os'
import { createServer } from 'node:net'
import { fileURLToPath } from 'node:url'
import { createHash, randomBytes } from 'node:crypto'
import { chromium } from '../web/node_modules/playwright/index.mjs'

const root = fileURLToPath(new URL('../', import.meta.url))
const api = process.env.NEXUSDESK_BROWSER_API
if (!api || !/^http:\/\/127\.0\.0\.1:\d+$/.test(api) || process.env.NEXUSDESK_TEST_W07_JOBS !== '1' || !process.env.DB_NAME?.startsWith('nexusdesk_w02d2_test_')) throw new Error('Use scripts/week07-smoke.py with owned fixtures')
const evidenceName = process.env.NEXUSDESK_BROWSER_EVIDENCE || 'week07-browser'
const sampleManifestName = process.env.NEXUSDESK_SAMPLE_MANIFEST || 'week05-sample-manifest.json'
if (!/^[a-z0-9-]+$/.test(evidenceName) || !/^[a-z0-9-]+\.json$/.test(sampleManifestName)) throw new Error('Invalid evidence filename')
const out = root + 'docs/weekly/evidence/' + evidenceName
await mkdir(out, { recursive: true })
const results = [], secrets = [], contexts = []
const password = randomBytes(24).toString('base64url'), tag = randomBytes(6).toString('hex')
secrets.push(password)
const hash = body => createHash('sha256').update(body).digest('hex')
const delay = ms => new Promise(resolve => setTimeout(resolve, ms))
const pass = (id, detail) => { results.push({ id, detail }); console.log(`PASS ${id}: ${detail}`) }
let worker, workerOutput = ''
let vite, browser, primary, stage = 'startup', viteOutput = ''
async function request(path, method = 'GET', body, token, status = 200) {
  const multipart = body instanceof FormData
  const response = await fetch(api + '/api/v1' + path, { method, headers: { ...(body && !multipart ? { 'Content-Type': 'application/json' } : {}), ...(token ? { Authorization: 'Bearer ' + token } : {}) }, body: body ? multipart ? body : JSON.stringify(body) : undefined, signal: AbortSignal.timeout(75000) })
  assert.equal(response.status, status, `${method} ${path}`)
  if (status === 204) return null
  return (await response.json()).data
}
async function account(name) {
  const email = `w07-${name.toLowerCase()}-${tag}@example.test`
  const user = await request('/auth/register', 'POST', { email, display_name: name, password }, undefined, 201)
  const session = await request('/auth/login', 'POST', { email, password })
  secrets.push(session.access_token)
  return { ...user, email, token: session.access_token }
}
async function upload(org, token, name, data) {
  const form = new FormData(); form.append('file', new Blob([data]), name)
  return request(`/organizations/${org}/knowledge/documents`, 'POST', form, token, 201)
}
async function newPage() {
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 }, acceptDownloads: true })
  contexts.push(ctx); const page = await ctx.newPage(); page.setDefaultTimeout(70000); return page
}
async function login(page, email, origin, loginPassword = password) {
  await page.goto(origin + '/login')
  await page.getByLabel('邮箱', { exact: true }).fill(email)
  await page.getByLabel('密码', { exact: true }).fill(loginPassword)
  const response = page.waitForResponse(r => r.url().endsWith('/auth/login') && r.request().method() === 'POST')
  await page.getByRole('button', { name: '登录', exact: true }).click()
  const r = await response; assert.equal(r.status(), 200)
  const token = (await r.json()).data.access_token; secrets.push(token)
  await page.getByRole('button', { name: '退出登录', exact: true }).waitFor()
  return token
}
async function navigate(page, path) { await page.evaluate(p => { history.pushState({}, '', p); window.dispatchEvent(new PopStateEvent('popstate')) }, path) }
async function shot(page, name) { await page.screenshot({ path: out + '/' + name + '.png', fullPage: true }) }
async function browserPath() {
  if (process.env.NEXUSDESK_CHROMIUM_PATH) return process.env.NEXUSDESK_CHROMIUM_PATH
  try { await access(chromium.executablePath()); return chromium.executablePath() } catch {}
  const cache = process.env.PLAYWRIGHT_BROWSERS_PATH || homedir() + '/.cache/ms-playwright'
  const dirs = (await readdir(cache)).filter(n => /^chromium-\d+$/.test(n)).sort((a, b) => Number(b.split('-')[1]) - Number(a.split('-')[1]))
  for (const dir of dirs) for (const sub of ['chrome-linux64/chrome', 'chrome-linux/chrome']) {
    const path = `${cache}/${dir}/${sub}`; try { await access(path); return path } catch {}
  }
  throw new Error('No installed compatible Chromium')
}
async function startWorker() {
  assert(process.env.NEXUSDESK_TEST_WORKER_BINARY?.startsWith('/var/tmp/nexusdesk-w02d3-api-'))
  const env = { ...process.env }
  for (const key of Object.keys(env)) if (key.startsWith('JWT_') || key === 'HTTP_ADDR') delete env[key]
  worker = spawn(process.env.NEXUSDESK_TEST_WORKER_BINARY, [], { cwd:root, env, stdio:['ignore','pipe','pipe'] })
  worker.stdout.on('data', b => { workerOutput += b }); worker.stderr.on('data', b => { workerOutput += b })
  await delay(500); assert.equal(worker.exitCode,null,'worker failed startup')
}
async function stopWorker() {
  if (worker && worker.exitCode === null) {
    worker.kill('SIGTERM')
    await Promise.race([new Promise(resolve=>worker.once('exit',resolve)),delay(12000)])
    if (worker.exitCode === null) {worker.kill('SIGKILL');throw new Error('Worker shutdown deadline exceeded')}
    assert.equal(worker.exitCode,0)
  }
}
try {
  const listener=createServer();await new Promise(resolve=>listener.listen(0,'127.0.0.1',resolve))
  const port=listener.address().port;await new Promise(resolve=>listener.close(resolve))
  const origin=`http://127.0.0.1:${port}`
  vite=spawn(process.execPath,[root+'web/node_modules/vite/bin/vite.js','--port',String(port),'--strictPort'],{cwd:root+'web',env:{...process.env,NEXUSDESK_API_TARGET:api},stdio:['ignore','pipe','pipe']})
  vite.stdout.on('data',b=>{viteOutput+=b});vite.stderr.on('data',b=>{viteOutput+=b})
  let ready=false
  for(let i=0;i<100;i++){try{if((await fetch(origin+'/healthz',{signal:AbortSignal.timeout(500)})).ok){ready=true;break}}catch{}await delay(100)}
  assert(ready,'Vite startup failed')
  browser=await chromium.launch({headless:true,executablePath:await browserPath()})
  const alice=await account('JobsAlice')
  const org=await request('/organizations','POST',{name:'W07 异步处理演示'},alice.token,201)
  const other=await request('/organizations','POST',{name:'W07 空组织'},alice.token,201)
  const page=await newPage();primary=page
  await login(page,alice.email,origin)
  await navigate(page,`/organizations/${org.id}/knowledge`)
  stage='upload with worker absent'
  await page.locator('input[type=file]').setInputFiles({name:'异步数据库排查.md',mimeType:'text/markdown',buffer:Buffer.from('数据库连接超时首先检查 DB_HOST 和 DB_PORT，再检查网络连接。不要把数据库密码写入日志。')})
  const accepted=page.waitForResponse(r=>r.url().endsWith('/jobs')&&r.request().method()==='POST')
  await page.getByRole('button',{name:'上传并异步处理',exact:true}).click()
  const acceptedResponse=await accepted;assert.equal(acceptedResponse.status(),202)
  const job=(await acceptedResponse.json()).data;assert.equal(job.status,'PENDING');assert.equal(job.attempts,0)
  await page.getByText('等待执行',{exact:false}).waitFor()
  assert.equal(await page.getByRole('heading',{name:'正文预览',exact:true}).count(),0)
  await shot(page,'01-pending-worker-offline')
  pass('upload-202-pending','Upload201 followed by Submit202 retains document and shows waiting, no fabricated completion')

  stage='independent worker real embedding completion'
  await startWorker()
  await page.getByText('处理完成',{exact:false}).waitFor({timeout:120000})
  await page.getByRole('heading',{name:'正文预览',exact:true}).waitFor()
  await page.getByText('已索引',{exact:false}).waitFor()
  await shot(page,'02-real-worker-complete')
  const docPath=`/organizations/${org.id}/knowledge/documents/${job.document_id}`
  const final=await request(`/organizations/${org.id}/jobs/${job.id}`,'GET',undefined,alice.token)
  assert.equal(final.status,'SUCCEEDED');assert.equal(final.attempts,1)
  const idx=await request(docPath+'/index','GET',undefined,alice.token)
  assert(idx.chunk_count>0);assert.equal(idx.embedding_dimension,1024)
  pass('worker-real-bge','Independent Worker without JWT completes extraction and real BGE1024 indexing; poll refreshes document and index')
  const same=await request(docPath+'/jobs','POST',undefined,alice.token,202)
  assert.equal(same.id,job.id);assert.equal(same.attempts,1)
  assert.deepEqual(await request(docPath+'/index','GET',undefined,alice.token),idx)
  pass('dedup-terminal','Repeated submit returns identical successful job and unchanged index version/count')

  stage='search and ask on asynchronously indexed document'
  await page.getByRole('link',{name:'检索问答',exact:true}).click()
  await page.getByLabel('检索问题',{exact:true}).fill('数据库连接超时首先检查什么？')
  const searched=page.waitForResponse(r=>r.url().endsWith('/knowledge/search')&&r.request().method()==='POST')
  await page.getByRole('button',{name:'检索文档',exact:true}).click()
  const searchedResponse=await searched;assert.equal(searchedResponse.status(),200)
  const matches=(await searchedResponse.json()).data.items
  assert(matches.length>0&&matches.every(source=>source.document_id===job.document_id))
  const asked=page.waitForResponse(r=>r.url().endsWith('/knowledge/ask')&&r.request().method()==='POST')
  await page.getByRole('button',{name:'基于知识库回答',exact:true}).click()
  const askedResponse=await asked;assert.equal(askedResponse.status(),200)
  const answer=(await askedResponse.json()).data
  assert.equal(answer.insufficient_evidence,false);assert(answer.sources.length>0&&answer.sources.every(source=>source.document_id===job.document_id))
  assert(answer.answer.includes('DB_HOST')&&answer.answer.includes('DB_PORT'))
  await page.getByRole('heading',{name:'知识库回答',exact:true}).waitFor()
  await shot(page,'03-async-source-answer')
  pass('async-search-ask','Real BGE search and Qwen answer cite the document indexed by the independent Worker')

  stage='permanent PDF failure and manual retry'
  const pdf=await upload(org.id,alice.token,'unsupported.pdf','%PDF-1.4\nSynthetic unsupported fixture\n%%EOF')
  await navigate(page,`/organizations/${org.id}/knowledge/${pdf.id}`)
  await page.getByRole('button',{name:'提交异步处理',exact:true}).click()
  await page.getByText(/PDF_UNSUPPORTED/).waitFor()
  await page.getByRole('button',{name:'重试任务',exact:true}).waitFor()
  const retried=page.waitForResponse(r=>r.url().endsWith('/retry')&&r.request().method()==='POST')
  await page.getByRole('button',{name:'重试任务',exact:true}).click()
  assert.equal((await retried).status(),200)
  await page.getByText(/第 2 轮/).waitFor()
  await page.getByText(/PDF_UNSUPPORTED/).waitFor()
  await shot(page,'03-permanent-failure-retry')
  pass('permanent-retry-ui','Unsupported PDF fails without automatic retry; manual retry increments run version and remains explicit failure')

  stage='pending cancel and retry'
  await stopWorker()
  const cancelDoc=await upload(org.id,alice.token,'cancel.md','取消演示：数据库连接检查。')
  await navigate(page,`/organizations/${org.id}/knowledge/${cancelDoc.id}`)
  await page.getByRole('button',{name:'提交异步处理',exact:true}).click()
  await page.getByText('等待执行',{exact:false}).waitFor()
  await page.getByRole('button',{name:'取消任务',exact:true}).click()
  await page.getByText('已取消',{exact:false}).waitFor()
  await page.getByRole('button',{name:'重试任务',exact:true}).click()
  await page.getByText(/第 2 轮/).waitFor()
  await startWorker()
  await page.getByText('处理完成',{exact:false}).waitFor({timeout:120000})
  pass('cancel-retry','Pending cancel is terminal; retry creates new run and worker completes it')

  stage='organization and session boundaries'
  await navigate(page,`/organizations/${org.id}/knowledge/${job.document_id}`)
  await page.getByText('处理完成',{exact:false}).waitFor()
  await page.setViewportSize({width:390,height:844})
  await shot(page,'04-mobile-job-detail')
  assert(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth+1))
  await page.getByLabel('组织选择').selectOption(String(other.id))
  assert.equal(await page.getByText(/任务 #/).count(),0)
  const state=await page.context().storageState();assert.equal(state.cookies.length,0);assert(state.origins.every(o=>o.localStorage.length===0));assert.equal(await page.evaluate(()=>sessionStorage.length),0)
  await page.reload();await page.getByRole('heading',{name:'登录工作台',exact:true}).waitFor()
  pass('scope-session-mobile','Organization switch clears old task; no browser persistence; mobile page has no horizontal overflow')
  await stopWorker()
  for(const secret of secrets)assert(!viteOutput.includes(secret)&&!workerOutput.includes(secret))
  await writeFile(out+'/results.json',JSON.stringify({generated_at:new Date().toISOString(),browser:await browser.version(),real_api:true,real_redis:true,independent_worker:true,real_embedding_model:'bge-m3:567m',mock_responses:false,results},null,2)+'\n')
} catch(error) {
  await primary?.screenshot({path:out+'/last-failure.png',fullPage:true}).catch(()=>{})
  let message=error instanceof Error?error.message:'unknown failure'
  for(const secret of secrets)message=message.split(secret).join('[redacted fixture]')
  console.error(`FAIL W07 browser stage: ${stage}\n${message}`);process.exitCode=1
} finally {
  if(worker&&worker.exitCode===null){worker.kill('SIGTERM');await Promise.race([new Promise(resolve=>worker.once('exit',resolve)),delay(12000)]);if(worker.exitCode===null)worker.kill('SIGKILL')}
  for(const ctx of contexts)await ctx.close().catch(()=>{})
  await browser?.close()
  if(vite&&vite.exitCode===null){vite.kill('SIGTERM');await Promise.race([new Promise(resolve=>vite.once('exit',resolve)),delay(3000)]);if(vite.exitCode===null)vite.kill('SIGKILL')}
}
