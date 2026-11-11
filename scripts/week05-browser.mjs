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
if (!api || !/^http:\/\/127\.0\.0\.1:\d+$/.test(api) || process.env.NEXUSDESK_TEST_W05_KNOWLEDGE !== '1' || !process.env.DB_NAME?.startsWith('nexusdesk_w02d2_test_')) throw new Error('Use scripts/week05-smoke.py with owned fixtures')
const evidenceName = process.env.NEXUSDESK_BROWSER_EVIDENCE || 'week05-browser'
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
  const response = await fetch(api + '/api/v1' + path, { method, headers: { ...(body && !multipart ? { 'Content-Type': 'application/json' } : {}), ...(token ? { Authorization: 'Bearer ' + token } : {}) }, body: body ? multipart ? body : JSON.stringify(body) : undefined, signal: AbortSignal.timeout(15000) })
  assert.equal(response.status, status, `${method} ${path}`)
  if (status === 204) return null
  return (await response.json()).data
}
async function account(name) {
  const email = `w05-${name.toLowerCase()}-${tag}@example.test`
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
  contexts.push(ctx); const page = await ctx.newPage(); page.setDefaultTimeout(12000); return page
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
  stage = 'Unicode onboarding code-point boundaries'
  const unicode = await newPage(), unicodeEmail = `w05-unicode-${tag}@example.test`
  const unicodePassword = '🧪'.repeat(65), unicodeName = '🧪'.repeat(80)
  secrets.push(unicodePassword)
  await unicode.goto(origin + '/register')
  await unicode.getByLabel('显示名称', { exact: true }).fill(unicodeName)
  await unicode.getByLabel('邮箱', { exact: true }).fill(unicodeEmail)
  await unicode.getByLabel('密码', { exact: true }).fill('🧪'.repeat(8))
  let invalidRequestSent = false
  const monitor = req => { if (req.url().endsWith('/auth/register')) invalidRequestSent = true }
  unicode.on('request', monitor)
  await unicode.getByRole('button', { name: '注册', exact: true }).click()
  await unicode.getByRole('alert').filter({ hasText: '密码需 15～128 个字符' }).waitFor()
  assert.equal(invalidRequestSent, false); unicode.off('request', monitor)
  await unicode.getByLabel('密码', { exact: true }).fill(unicodePassword)
  const unicodeRegistration = unicode.waitForResponse(r => r.url().endsWith('/auth/register'))
  await unicode.getByRole('button', { name: '注册', exact: true }).click()
  const unicodeRegistered = await unicodeRegistration; assert.equal(unicodeRegistered.status(), 201)
  assert.equal((await unicodeRegistered.json()).data.display_name, unicodeName)
  await unicode.getByRole('heading', { name: '登录工作台', exact: true }).waitFor()
  await login(unicode, unicodeEmail, origin, unicodePassword)
  await unicode.getByLabel('组织名称', { exact: true }).fill('🧪'.repeat(100))
  const unicodeOrg = unicode.waitForResponse(r => r.url().endsWith('/api/v1/organizations') && r.request().method() === 'POST')
  await unicode.getByRole('button', { name: '创建组织', exact: true }).click()
  const unicodeCreated = await unicodeOrg; assert.equal(unicodeCreated.status(), 201)
  assert.equal((await unicodeCreated.json()).data.name, '🧪'.repeat(100))
  await unicode.close()
  pass('unicode-onboarding', 'supplementary-plane input follows API code-point limits; short password blocked, valid registration/login and 100-character organization succeed')
  const page = await newPage(); primary = page
  stage = 'UI signup and create first organization'
  const email = `w05-ui-${tag}@example.test`
  await page.goto(origin + '/login'); await page.getByRole('link', { name: '注册账号', exact: true }).click()
  await page.getByLabel('显示名称', { exact: true }).fill('W05 演示成员')
  await page.getByLabel('邮箱', { exact: true }).fill(email)
  await page.getByLabel('密码', { exact: true }).fill(password)
  const registration = page.waitForResponse(r => r.url().endsWith('/auth/register'))
  await page.getByRole('button', { name: '注册', exact: true }).click()
  const registered = await registration; assert.equal(registered.status(), 201)
  const user = (await registered.json()).data
  await page.getByRole('heading', { name: '登录工作台', exact: true }).waitFor()
  const token = await login(page, email, origin)
  assert.equal((await request('/organizations', 'GET', undefined, token)).items.length, 0)
  await shot(page, '01-create-first-organization')
  await page.getByLabel('组织名称', { exact: true }).fill('W05 Support A')
  const createOrg = page.waitForResponse(r => r.url().endsWith('/api/v1/organizations') && r.request().method() === 'POST')
  await page.getByRole('button', { name: '创建组织', exact: true }).click()
  const createdOrg = await createOrg; assert.equal(createdOrg.status(), 201)
  const a = (await createdOrg.json()).data; assert.equal(a.my_role, 'admin')
  await page.getByRole('heading', { name: '创建工单', exact: true }).waitFor()
  await page.getByLabel('工单标题', { exact: true }).fill('W05 新账号网页建单')
  await page.getByLabel('工单描述', { exact: true }).fill('注册并创建组织后即可使用工单功能。')
  await page.getByRole('button', { name: '创建工单', exact: true }).click()
  await page.getByRole('heading', { name: 'W05 新账号网页建单', exact: true }).waitFor()
  pass('onboarding', 'real UI register201, login200, first organization201/admin, automatic ticket entry and ticket creation')

  const david = await account('David'), bob = await account('Bob')
  const b = await request('/organizations', 'POST', { name: 'W05 Support B' }, david.token, 201)
  await request(`/organizations/${a.id}/members`, 'POST', { user_id: bob.id, role: 'member' }, token, 201)
  await request(`/organizations/${b.id}/members`, 'POST', { user_id: user.id, role: 'member' }, david.token, 201)
  const base = `/organizations/${a.id}/knowledge/documents`
  stage = 'upload type rejection and actual Markdown'
  await page.getByRole('link', { name: '知识库', exact: true }).click()
  await page.getByRole('heading', { name: '知识库文档', exact: true }).waitFor()
  await page.getByLabel('选择文件', { exact: true }).setInputFiles({ name: 'fake.md', mimeType: 'text/markdown', buffer: Buffer.from([0, 255, 1]) })
  const rejected = page.waitForResponse(r => r.url().endsWith(base) && r.request().method() === 'POST')
  await page.getByRole('button', { name: '上传文档', exact: true }).click()
  assert.equal((await rejected).status(), 415); await page.getByRole('alert').waitFor()
  const body = '# W05 知识库演示\r\n\r\n这是一份原创演示文档。\r\n<script>window.__nexusdeskUnsafe = true</script>\r\n'
  await page.getByLabel('选择文件', { exact: true }).setInputFiles({ name: 'handbook.md', mimeType: 'text/markdown', buffer: Buffer.from(body) })
  const uploaded = page.waitForResponse(r => r.url().endsWith(base) && r.request().method() === 'POST')
  await page.getByRole('button', { name: '上传文档', exact: true }).click()
  const uploadedResponse = await uploaded; assert.equal(uploadedResponse.status(), 201)
  const document = (await uploadedResponse.json()).data
  assert.equal(document.status, 'UPLOADED'); assert.equal(document.sha256_hex, hash(body)); assert(!('storage_key' in document))
  await page.getByRole('heading', { name: 'handbook.md', exact: true }).waitFor()
  await shot(page, '02-uploaded')
  pass('upload', 'binary masquerading as Markdown rejected415; valid real multipart saved201 with SHA256 and UPLOADED metadata')

  stage = 'process, safe preview and byte-identical download'
  const processed = page.waitForResponse(r => r.url().endsWith(`/documents/${document.id}/process`))
  await page.getByRole('button', { name: '提取正文', exact: true }).click()
  assert.equal((await processed).status(), 200)
  await page.getByRole('heading', { name: '正文预览', exact: true }).waitFor()
  assert.equal(await page.locator('pre.body').textContent(), body.replaceAll('\r\n', '\n'))
  assert.equal(await page.evaluate(() => window.__nexusdeskUnsafe), undefined)
  const downloadEvent = page.waitForEvent('download')
  await page.getByRole('button', { name: '下载原文件', exact: true }).click()
  const download = await downloadEvent; assert.equal(download.suggestedFilename(), 'handbook.md')
  const chunks = []; for await (const chunk of await download.createReadStream()) chunks.push(chunk)
  assert.equal(hash(Buffer.concat(chunks)), document.sha256_hex)
  await shot(page, '03-ready-preview')
  pass('process-preview-download', 'real process200/READY, normalized source preview escaped as text, original download hash matches')

  stage = 'ten original samples with organization assignment'
  const sampleIndex = JSON.parse(await readFile(root + 'docs/knowledge/samples/index.json', 'utf8')), manifest = []
  for (const sample of sampleIndex.samples) {
    const org = sample.organization_slot === 'A' ? a : b
    const credential = sample.organization_slot === 'A' ? token : david.token
    const content = await readFile(root + 'docs/knowledge/samples/' + sample.file)
    const metadata = await upload(org.id, credential, sample.file, content)
    const final = await request(`/organizations/${org.id}/knowledge/documents/${metadata.id}/process`, 'POST', undefined, credential)
    assert.equal(final.status, 'READY'); assert.equal(final.sha256_hex, hash(content))
    manifest.push({ ...sample, org_id: org.id, document_id: final.id, status: final.status, sha256_hex: final.sha256_hex })
  }
  await writeFile(root + 'docs/weekly/evidence/' + sampleManifestName, JSON.stringify({ generated_at: new Date().toISOString(), database_scope: 'owned disposable test database, removed after acceptance', documents: manifest }, null, 2) + '\n')
  for (let i = 0; i < 13; i++) await upload(a.id, token, `paging-${i}.txt`, 'fictional paging body')
  pass('samples', '10 original TXT/MD files uploaded and processed READY in two isolated organization scopes; actual manifest saved')

  stage = 'real filter and pagination'
  await page.getByRole('link', { name: '返回知识库列表', exact: false }).click()
  await page.getByRole('link', { name: 'paging-12.txt', exact: true }).waitFor()
  const second = page.waitForResponse(r => r.url().includes(base + '?page=2&') && r.status() === 200)
  await page.getByRole('button', { name: '下一页文档', exact: true }).click(); await second
  await page.getByRole('link', { name: 'handbook.md', exact: true }).waitFor()
  await page.getByLabel('文档状态筛选', { exact: true }).selectOption('READY')
  await page.getByRole('link', { name: 'handbook.md', exact: true }).waitFor()
  assert.equal(await page.getByRole('link', { name: 'paging-12.txt', exact: true }).count(), 0)
  await shot(page, '04-list-filter')
  pass('filter-pagination', 'real page2 and READY filter work and reset page, with server-side metadata only')

  stage = 'failed processing and PDF limits'
  const empty = await upload(a.id, token, 'whitespace.txt', ' \t\n')
  await navigate(page, `/organizations/${a.id}/knowledge/${empty.id}`)
  await page.getByRole('heading', { name: 'whitespace.txt', exact: true }).waitFor()
  const failed = page.waitForResponse(r => r.url().endsWith(`/documents/${empty.id}/process`))
  await page.getByRole('button', { name: '提取正文', exact: true }).click(); assert.equal((await failed).status(), 422)
  await page.locator('.badge').filter({ hasText: 'FAILED' }).waitFor()
  await page.getByRole('button', { name: '提取正文', exact: true }).waitFor()
  const pdf = await upload(a.id, token, 'fixture.pdf', '%PDF-1.7\nSynthetic signature fixture, not a parsed PDF.\n%%EOF\n')
  await navigate(page, `/organizations/${a.id}/knowledge/${pdf.id}`)
  await page.getByText('PDF 暂不支持文本提取，可下载原文件。', { exact: true }).waitFor()
  const pdfProcess = page.waitForResponse(r => r.url().endsWith(`/documents/${pdf.id}/process`))
  await page.getByRole('button', { name: '提取正文', exact: true }).click(); assert.equal((await pdfProcess).status(), 422)
  await page.locator('.badge').filter({ hasText: 'FAILED' }).waitFor()
  await shot(page, '05-pdf-boundary')
  pass('failure-status', 'whitespace yields422/FAILED with retry; PDF stores/downloads but extraction honestly returns422 PDF_UNSUPPORTED')

  stage = 'cross organization and delete permissions'
  const other = await newPage(); await login(other, bob.email, origin)
  await navigate(other, `/organizations/${a.id}/knowledge/${document.id}`)
  await other.getByRole('heading', { name: '正文预览', exact: true }).waitFor()
  assert.equal(await other.getByRole('button', { name: '删除文档', exact: true }).count(), 0)
  await request(`${base}/${document.id}`, 'DELETE', undefined, bob.token, 403)
  await other.getByRole('button', { name: '退出登录', exact: true }).click(); await login(other, david.email, origin)
  const denied = other.waitForResponse(r => r.url().endsWith(`${base}/${document.id}`))
  await navigate(other, `/organizations/${a.id}/knowledge/${document.id}`); assert.equal((await denied).status(), 404)
  await other.getByRole('alert').filter({ hasText: /不存在|无法访问/ }).first().waitFor()
  assert.equal(await other.locator('pre.body').count(), 0)
  await shot(other, '06-cross-organization')
  pass('authorization', 'same-org member reads but cannot delete uploader document; other organization gets404 and no preview')

  stage = 'organization switch and deletion'
  await page.getByRole('link', { name: 'NexusDesk', exact: true }).click()
  await page.getByLabel('组织选择').locator('option', { hasText: 'W05 Support B' }).waitFor({ state: 'attached' })
  await page.getByLabel('组织选择').selectOption(String(b.id))
  await page.getByRole('link', { name: '知识库', exact: true }).click()
  await page.getByRole('link', { name: '09-team-b.md', exact: true }).waitFor()
  assert.equal(await page.getByRole('link', { name: 'handbook.md', exact: true }).count(), 0)
  await page.getByLabel('组织选择').selectOption(String(a.id))
  await navigate(page, `/organizations/${a.id}/knowledge/${document.id}`)
  await page.getByRole('heading', { name: '正文预览', exact: true }).waitFor()
  const deletion = page.waitForResponse(r => r.url().endsWith(`${base}/${document.id}`) && r.request().method() === 'DELETE')
  await page.getByRole('button', { name: '删除文档', exact: true }).click(); assert.equal((await deletion).status(), 204)
  await page.getByRole('heading', { name: '知识库文档', exact: true }).waitFor()
  await request(`${base}/${document.id}/text`, 'GET', undefined, token, 404)
  const afterDelete = await fetch(api + '/api/v1' + base + '/' + document.id + '/download', { headers: { Authorization: 'Bearer ' + token } }); assert.equal(afterDelete.status, 404)
  await shot(page, '07-after-delete')
  pass('switch-delete', 'organization switch shows correct scope; uploader/admin deletes204; subsequent preview/download404')
  const persisted = await page.context().storageState(); assert.equal(persisted.cookies.length, 0); assert(persisted.origins.every(o => o.localStorage.length === 0))
  assert.equal(await page.evaluate(() => sessionStorage.length), 0)
  await page.reload(); await page.getByRole('heading', { name: '登录工作台', exact: true }).waitFor()
  pass('memory-session', 'new knowledge flows retain memory-only credentials; refresh requires login')
  for (const secret of secrets) assert(!viteOutput.includes(secret), 'credential in dev-server output')
  await writeFile(out + '/results.json', JSON.stringify({ generated_at: new Date().toISOString(), browser: await browser.version(), real_api: true, mock_responses: false, results }, null, 2) + '\n')
} catch (error) {
  await primary?.screenshot({ path: out + '/last-failure.png', fullPage: true }).catch(() => {})
  let message = error instanceof Error ? error.message : 'unknown failure'
  for (const secret of secrets) message = message.split(secret).join('[redacted fixture]')
  console.error(`FAIL W05 browser stage: ${stage}\n${message}`); process.exitCode = 1
} finally {
  for (const context of contexts) await context.close().catch(() => {})
  await browser?.close()
  if (vite && vite.exitCode === null) { vite.kill('SIGTERM'); await Promise.race([new Promise(resolve => vite.once('exit', resolve)), delay(3000)]); if (vite.exitCode === null) vite.kill('SIGKILL') }
}
