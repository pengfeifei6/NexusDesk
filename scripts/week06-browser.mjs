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
if (!api || !/^http:\/\/127\.0\.0\.1:\d+$/.test(api) || process.env.NEXUSDESK_TEST_W06_RAG !== '1' || !process.env.DB_NAME?.startsWith('nexusdesk_w02d2_test_')) throw new Error('Use scripts/week06-smoke.py with owned fixtures')
const evidenceName = process.env.NEXUSDESK_BROWSER_EVIDENCE || 'week06-browser'
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
let vite, browser, primary, stage = 'startup', viteOutput = ''
async function request(path, method = 'GET', body, token, status = 200) {
  const multipart = body instanceof FormData
  const response = await fetch(api + '/api/v1' + path, { method, headers: { ...(body && !multipart ? { 'Content-Type': 'application/json' } : {}), ...(token ? { Authorization: 'Bearer ' + token } : {}) }, body: body ? multipart ? body : JSON.stringify(body) : undefined, signal: AbortSignal.timeout(75000) })
  assert.equal(response.status, status, `${method} ${path}`)
  if (status === 204) return null
  return (await response.json()).data
}
async function account(name) {
  const email = `w06-${name.toLowerCase()}-${tag}@example.test`
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
try {
  const listener = createServer(); await new Promise(resolve => listener.listen(0, '127.0.0.1', resolve))
  const port = listener.address().port; await new Promise(resolve => listener.close(resolve))
  const origin = `http://127.0.0.1:${port}`
  vite = spawn(process.execPath, [root + 'web/node_modules/vite/bin/vite.js', '--port', String(port), '--strictPort'], { cwd: root + 'web', env: { ...process.env, NEXUSDESK_API_TARGET: api }, stdio: ['ignore', 'pipe', 'pipe'] })
  vite.stdout.on('data', b => { viteOutput += b }); vite.stderr.on('data', b => { viteOutput += b })
  let ready = false
  for (let i = 0; i < 100; i++) { try { if ((await fetch(origin + '/healthz', { signal: AbortSignal.timeout(500) })).ok) { ready = true; break } } catch {} await delay(100) }
  assert(ready, 'Vite proxy did not start')
  browser = await chromium.launch({ headless: true, executablePath: await browserPath() })
  const alice = await account('RAGAlice'), bob = await account('RAGBob')
  const a = await request('/organizations', 'POST', { name: 'W06 检索演示 A' }, alice.token, 201)
  const b = await request('/organizations', 'POST', { name: 'W06 空知识库 B' }, alice.token, 201)
  const foreign = await request('/organizations', 'POST', { name: 'W06 外部组织' }, bob.token, 201)
  assert(foreign.id !== a.id)
  const doc = await upload(a.id, alice.token, '数据库排查.md', '数据库连接超时首先核对 DB_HOST 和 DB_PORT，再检查网络连通性。普通查询超时为 3 秒。请不要把数据库密码写入日志。')
  const path = `/organizations/${a.id}/knowledge/documents/${doc.id}`
  await request(path + '/process', 'POST', undefined, alice.token)
  const page = await newPage(); primary = page
  await login(page, alice.email, origin)
  stage = 'separate extraction and index status'
  await navigate(page, `/organizations/${a.id}/knowledge/${doc.id}`)
  await page.getByText('尚未索引', { exact: false }).waitFor()
  const response = page.waitForResponse(r => r.url().endsWith(path + '/index') && r.request().method() === 'POST')
  await page.getByRole('button', { name: '建立索引', exact: true }).click()
  const indexed = await response; assert.equal(indexed.status(), 200)
  const info = (await indexed.json()).data
  assert.equal(info.status, 'READY'); assert(info.chunk_count > 0)
  await page.getByText('已索引', { exact: false }).waitFor()
  await shot(page, '01-index-ready')
  pass('index-ui', 'READY extracted text starts ABSENT; real BGE index becomes READY with chunk count')
  const reuse = page.waitForResponse(r => r.url().endsWith(path + '/index') && r.request().method() === 'POST')
  await page.getByRole('button', { name: '建立索引', exact: true }).click()
  assert.deepEqual((await (await reuse).json()).data, info)
  pass('index-reuse', 'repeat click reuses same version/timestamp/count')

  stage = 'search and sources'
  await page.getByRole('link', { name: '检索问答', exact: true }).click()
  await page.getByLabel('检索问题', { exact: true }).fill('数据库连接超时首先检查什么？')
  const search = page.waitForResponse(r => r.url().endsWith('/knowledge/search') && r.request().method() === 'POST')
  await page.getByRole('button', { name: '检索文档', exact: true }).click()
  const searchResponse = await search; assert.equal(searchResponse.status(), 200)
  const matches = (await searchResponse.json()).data.items
  assert(matches.length > 0 && matches.every(s => s.document_id === doc.id))
  await page.getByRole('link', { name: '数据库排查.md', exact: true }).waitFor()
  await page.getByText(/相似度用于排序/).waitFor()
  await shot(page, '02-search-sources')
  pass('search-ui', 'real query embedding + exact pgvector search returns scoped source and timing')

  stage = 'grounded real model answer'
  const ask = page.waitForResponse(r => r.url().endsWith('/knowledge/ask') && r.request().method() === 'POST')
  await page.getByRole('button', { name: '基于知识库回答', exact: true }).click()
  const answered = await ask; assert.equal(answered.status(), 200)
  const answer = (await answered.json()).data
  assert.equal(answer.insufficient_evidence, false); assert(answer.sources.length > 0)
  assert(answer.sources.every(s => s.document_id === doc.id)); assert(answer.answer.includes('DB_HOST') && answer.answer.includes('DB_PORT'))
  await page.getByRole('heading', { name: '知识库回答', exact: true }).waitFor()
  await shot(page, '03-grounded-answer')
  await page.getByRole('link', { name: '数据库排查.md', exact: true }).first().click()
  await page.getByRole('heading', { name: '正文预览', exact: true }).waitFor()
  pass('ask-ui', 'real Qwen answer contains checked configuration names and valid source; source opens authenticated document')

  stage = 'organization switch and no evidence'
  await page.getByRole('link', { name: '检索问答', exact: true }).click()
  await page.getByLabel('组织选择').selectOption(String(b.id))
  assert(page.url().endsWith(`/organizations/${b.id}/knowledge/search`))
  assert.equal(await page.getByRole('link', { name: '数据库排查.md', exact: true }).count(), 0)
  await page.getByLabel('检索问题', { exact: true }).fill('数据库连接超时首先检查什么？')
  const empty = page.waitForResponse(r => r.url().endsWith('/knowledge/ask') && r.request().method() === 'POST')
  await page.getByRole('button', { name: '基于知识库回答', exact: true }).click()
  const noEvidence = (await (await empty).json()).data
  assert(noEvidence.insufficient_evidence); assert.deepEqual(noEvidence.sources, [])
  await page.getByText('知识库未检索到足够依据。', { exact: true }).waitFor()
  await shot(page, '04-empty-organization')
  pass('scope-no-evidence', 'organization switch retains search view, clears old sources, empty organization refuses without invented evidence')

  stage = 'cross-organization denied'
  for (const suffix of ['/search', '/ask']) await request(`/organizations/${a.id}/knowledge` + suffix, 'POST', { query: '数据库', top_k: 5 }, bob.token, 404)
  await request(path + '/index', 'POST', undefined, bob.token, 404)
  await request(path + '/index', 'GET', undefined, bob.token, 404)
  pass('four-route-isolation', 'other-organization user receives404 on index read/write, search and ask')

  stage = 'deleted source excluded'
  await request(path, 'DELETE', undefined, alice.token, 204)
  const afterDelete = await request(`/organizations/${a.id}/knowledge/search`, 'POST', { query: '数据库连接超时', top_k: 5 }, alice.token)
  assert.deepEqual(afterDelete.items, [])
  await request(path + '/index', 'GET', undefined, alice.token, 404)
  pass('deleted-index-hidden', 'soft-deleted document cannot be retrieved even when retained chunk rows exist')
  const state = await page.context().storageState(); assert.equal(state.cookies.length, 0); assert(state.origins.every(o => o.localStorage.length === 0))
  assert.equal(await page.evaluate(() => sessionStorage.length), 0)
  await page.reload(); await page.getByRole('heading', { name: '登录工作台', exact: true }).waitFor()
  pass('memory-session', 'RAG flows persist neither credentials nor protected sources in browser storage')
  for (const secret of secrets) assert(!viteOutput.includes(secret))
  await writeFile(out + '/results.json', JSON.stringify({ generated_at: new Date().toISOString(), browser: await browser.version(), real_api: true, real_models: true, mock_responses: false, results }, null, 2) + '\n')
} catch (error) {
  await primary?.screenshot({ path: out + '/last-failure.png', fullPage: true }).catch(() => {})
  let message = error instanceof Error ? error.message : 'unknown failure'
  for (const secret of secrets) message = message.split(secret).join('[redacted fixture]')
  console.error(`FAIL W06 browser stage: ${stage}\n${message}`); process.exitCode = 1
} finally {
  for (const context of contexts) await context.close().catch(() => {})
  await browser?.close()
  if (vite && vite.exitCode === null) { vite.kill('SIGTERM'); await Promise.race([new Promise(resolve => vite.once('exit', resolve)), delay(3000)]); if (vite.exitCode === null) vite.kill('SIGKILL') }
}
