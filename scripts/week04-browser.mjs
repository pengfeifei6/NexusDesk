// Real browser acceptance. The parent disposable runner owns API/database lifecycle.
// No .env, credential files, HAR, traces or console/request-body logging.
import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { access, mkdir, readdir, rename, writeFile } from 'node:fs/promises'
import { homedir } from 'node:os'
import { createServer } from 'node:net'
import { fileURLToPath } from 'node:url'
import { randomBytes } from 'node:crypto'
import { chromium } from '../web/node_modules/playwright/index.mjs'

const root = fileURLToPath(new URL('../', import.meta.url))
const api = process.env.NEXUSDESK_BROWSER_API
if (!api || !/^http:\/\/127\.0\.0\.1:\d+$/.test(api) || process.env.NEXUSDESK_TEST_W04_CLAIM !== '1' || !process.env.DB_NAME?.startsWith('nexusdesk_w02d2_test_')) throw new Error('Use scripts/week04-smoke.py with its owned loopback fixtures')
const evidenceName = process.env.NEXUSDESK_BROWSER_EVIDENCE || 'week04-browser'
if (!/^week\d{2}-[a-z0-9-]+$/.test(evidenceName)) throw new Error('Invalid evidence directory name')
const out = root + 'docs/weekly/evidence/' + evidenceName
await mkdir(out, { recursive: true })
const sensitive = []
const findings = []
const password = randomBytes(24).toString('base64url')
sensitive.push(password)
const tag = randomBytes(6).toString('hex')
const log = (id, detail) => { findings.push({ id, detail }); console.log(`PASS ${id}: ${detail}`) }
const delay = ms => new Promise(resolve => setTimeout(resolve, ms))
let vite, browser, primary
const contexts = []
let stage = 'setup'
let viteOutput = ''
async function request(path, method = 'GET', body, token) {
  const response = await fetch(api + '/api/v1' + path, { method, headers: { ...(body ? { 'Content-Type': 'application/json' } : {}), ...(token ? { Authorization: 'Bearer ' + token } : {}) }, body: body ? JSON.stringify(body) : undefined, signal: AbortSignal.timeout(10000) })
  assert.equal(response.ok, true, `fixture request failed: ${method} ${path} HTTP ${response.status}`)
  return (await response.json()).data
}
async function account(name) {
  const email = `g1-${name.toLowerCase()}-${tag}@example.test`
  const user = await request('/auth/register', 'POST', { email, display_name: name, password })
  const session = await request('/auth/login', 'POST', { email, password })
  sensitive.push(session.access_token)
  return { ...user, email, token: session.access_token }
}
async function newPage(record = false) {
  const context = await browser.newContext({ viewport: { width: 1280, height: 900 }, ...(record ? { recordVideo: { dir: out, size: { width: 1280, height: 900 } } } : {}) })
  contexts.push(context)
  const page = await context.newPage()
  page.setDefaultTimeout(12000)
  return page
}
async function login(page, user, origin) {
  await page.goto(origin + '/login')
  await page.getByLabel('邮箱', { exact: true }).fill(user.email)
  await page.getByLabel('密码', { exact: true }).fill(password)
  await Promise.all([page.waitForResponse(r => r.url().endsWith('/api/v1/me') && r.status() === 200), page.getByRole('button', { name: '登录', exact: true }).click()])
  await page.getByRole('button', { name: '退出登录', exact: true }).waitFor()
}
async function select(page, org) { await page.getByLabel('组织选择').selectOption(String(org.id)) }
async function screenshot(page, name) { await page.screenshot({ path: out + '/' + name + '.png', fullPage: true }) }
async function waitText(page, text) { await page.getByText(text, { exact: false }).first().waitFor() }
async function claimResponse(page) { return page.waitForResponse(r => r.url().endsWith('/claim') && r.request().method() === 'POST') }
async function browserPath() {
  if (process.env.NEXUSDESK_CHROMIUM_PATH) return process.env.NEXUSDESK_CHROMIUM_PATH
  try { await access(chromium.executablePath()); return chromium.executablePath() } catch {}
  // This workstation is Ubuntu 20.04; new Playwright browser downloads no longer
  // target it. Reuse an installed Chromium, recording the actual browser version.
  const cache = process.env.PLAYWRIGHT_BROWSERS_PATH || homedir() + '/.cache/ms-playwright'
  const dirs = (await readdir(cache).catch(() => [])).filter(n => /^chromium-\d+$/.test(n)).sort((a,b) => Number(b.split('-')[1])-Number(a.split('-')[1]))
  for (const dir of dirs) for (const binary of ['chrome-linux64/chrome','chrome-linux/chrome']) {
    const path = `${cache}/${dir}/${binary}`
    try { await access(path); return path } catch {}
  }
  throw new Error('Chromium not installed; run playwright install chromium on a supported OS or set NEXUSDESK_CHROMIUM_PATH')
}

try {
  stage = 'seed real API'
  const alice = await account('Alice'), bob = await account('Bob'), charlie = await account('Charlie'), david = await account('David')
  const a = await request('/organizations', 'POST', { name: 'G1 Support A' }, alice.token)
  const b = await request('/organizations', 'POST', { name: 'G1 Support B' }, david.token)
  for (const user of [bob, charlie]) await request(`/organizations/${a.id}/members`, 'POST', { user_id: user.id, role: 'member' }, alice.token)
  await request(`/organizations/${b.id}/members`, 'POST', { user_id: alice.id, role: 'member' }, david.token)
  await request(`/organizations/${b.id}/tickets`, 'POST', { title: 'B organization only', body: 'Separate organization content', priority: 1 }, david.token)
  for (let i = 1; i <= 21; i++) await request(`/organizations/${a.id}/tickets`, 'POST', { title: `分页样例 ${String(i).padStart(2, '0')}`, body: 'Isolated pagination fixture', priority: 1 }, alice.token)
  const listener = createServer()
  await new Promise(resolve => listener.listen(0, '127.0.0.1', resolve))
  const port = listener.address().port
  await new Promise(resolve => listener.close(resolve))
  const origin = `http://127.0.0.1:${port}`
  vite = spawn(process.execPath, [root + 'web/node_modules/vite/bin/vite.js', '--port', String(port), '--strictPort'], { cwd: root + 'web', env: { ...process.env, NEXUSDESK_API_TARGET: api }, stdio: ['ignore', 'pipe', 'pipe'] })
  vite.stdout.on('data', data => { viteOutput += data.toString() })
  vite.stderr.on('data', data => { viteOutput += data.toString() })
  let ready = false
  for (let i = 0; i < 100; i++) {
    if (vite.exitCode !== null) throw new Error('Vite exited before ready')
    try { const r = await fetch(origin + '/healthz', { signal: AbortSignal.timeout(500) }); if (r.ok) { ready = true; break } } catch {}
    await delay(100)
  }
  assert(ready, 'development proxy did not become healthy')
  browser = await chromium.launch({ headless: true, executablePath: await browserPath(), slowMo: 150 })
  const main = await newPage(true), other = await newPage()
  primary = main
  const video = main.video()

  stage = 'Alice login, list and create'
  let failFirstOrganizations = true
  await main.route('**/api/v1/organizations?*', async route => {
    if (failFirstOrganizations) { failFirstOrganizations = false; await route.abort('failed') }
    else await route.continue()
  })
  await login(main, alice, origin)
  await main.getByRole('alert').filter({ hasText: /网络/ }).waitFor()
  await main.getByRole('button', { name: '重试', exact: true }).click()
  await main.getByLabel('组织选择').locator('option', { hasText: 'G1 Support A' }).waitFor({ state: 'attached' })
  await main.unroute('**/api/v1/organizations?*')
  await main.goBack()
  await main.waitForURL('**/desk')
  assert.equal(await main.getByLabel('密码', { exact: true }).count(), 0)
  log('review-recovery-session', 'initial organization network failure stays visible; Retry fetches real organizations; authenticated Back to login is guarded')
  await select(main, a)
  await main.getByRole('heading', { name: '创建工单', exact: true }).waitFor()
  await main.getByRole('link', { name: '分页样例 21', exact: true }).waitFor()
  const pageTwo = main.waitForResponse(r => r.url().includes(`/organizations/${a.id}/tickets?page=2&`) && r.status() === 200)
  await main.getByRole('button', { name: '下一页工单', exact: true }).click()
  await pageTwo
  await main.getByRole('link', { name: '分页样例 01', exact: true }).waitFor()
  assert.equal(await main.getByRole('link', { name: '分页样例 21', exact: true }).count(), 0)
  await main.getByRole('button', { name: '上一页工单', exact: true }).click()
  await main.getByRole('link', { name: '分页样例 21', exact: true }).waitFor()
  const filtered = main.waitForResponse(r => r.url().includes('priority=3') && r.status() === 200)
  await main.getByLabel('优先级筛选').selectOption('3')
  assert.equal((await (await filtered).json()).data.items.length, 0)
  await waitText(main, '暂无工单')
  await main.getByLabel('优先级筛选').selectOption('')
  await main.getByRole('link', { name: '分页样例 21', exact: true }).waitFor()
  const statusFiltered = main.waitForResponse(r => r.url().includes('status=IN_PROGRESS') && r.status() === 200)
  await main.getByLabel('状态筛选').selectOption('IN_PROGRESS')
  assert.equal((await (await statusFiltered).json()).data.items.length, 0)
  await waitText(main, '暂无工单')
  await main.getByLabel('状态筛选').selectOption('')
  await main.getByRole('link', { name: '分页样例 21', exact: true }).waitFor()
  log('filter-pagination', 'real 21-row data: page2/previous work; priority and status filters use backend results and reset to page1')
  await main.getByLabel('工单标题', { exact: true }).fill('G1：无法接收通知')
  await main.getByLabel('工单描述', { exact: true }).fill('演示：提交后没有收到工作通知，请协助检查。')
  await main.locator('form').getByLabel('优先级').selectOption('3')
  const madeResponse = main.waitForResponse(r => r.url().endsWith(`/organizations/${a.id}/tickets`) && r.request().method() === 'POST')
  await main.getByRole('button', { name: '创建工单', exact: true }).click()
  const made = await madeResponse
  assert.equal(made.status(), 201)
  const ticket = (await made.json()).data
  const path = `/organizations/${a.id}/tickets/${ticket.id}`
  await main.getByRole('heading', { name: 'G1：无法接收通知', exact: true }).waitFor()
  await screenshot(main, '01-created')
  log('C20', 'Alice browser login → /me → select A → real list → POST ticket 201 → detail OPEN')
  await delay(5000) // Presentation pause only; no correctness assertion depends on this delay.

  stage = 'two independent browsers claim the same version'
  await main.getByRole('button', { name: '退出登录', exact: true }).click()
  await login(main, bob, origin)
  await select(main, a)
  await main.getByRole('link', { name: 'G1：无法接收通知', exact: true }).click()
  await login(other, charlie, origin)
  await select(other, a)
  await other.getByRole('link', { name: 'G1：无法接收通知', exact: true }).click()
  let arrived = 0, release
  const both = new Promise(resolve => { release = resolve })
  for (const page of [main, other]) await page.route('**/claim', async route => { arrived++; if (arrived === 2) release(); await both; await route.continue() })
  const responses = [claimResponse(main), claimResponse(other)]
  await Promise.all([main.getByRole('button', { name: '领取工单', exact: true }).click(), other.getByRole('button', { name: '领取工单', exact: true }).click()])
  const claims = await Promise.all(responses)
  assert.deepEqual(claims.map(r => r.status()).sort(), [200, 409])
  const loser = claims[0].status() === 409 ? main : other
  await loser.getByText(/已被领取|已变化|版本发生变化/).first().waitFor()
  await loser.getByText(/已被领取|已变化|版本发生变化/).first().scrollIntoViewIfNeeded()
  const final = await request(path, 'GET', undefined, alice.token)
  assert.equal(final.status, 'OPEN'); assert.equal(final.version, 1)
  const winner = claims[0].status() === 200 ? bob : charlie
  assert.equal(final.assignee_id, winner.id)
  const events = await request(path + '/events', 'GET', undefined, alice.token)
  assert.equal(events.items.filter(e => e.event_type === 'TICKET_ASSIGNED').length, 1)
  await screenshot(loser, '02-claim-conflict')
  log('browser-concurrency-409', 'Bob/Charlie each sent version 0; one 200 and one 409; loser refreshed, final OPEN/version1/one matching assignment event')
  await delay(5000)
  for (const page of [main, other]) await page.unroute('**/claim')
  if (winner.id !== bob.id) { await main.getByRole('button', { name: '退出登录', exact: true }).click(); await login(main, charlie, origin); await select(main, a); await main.getByRole('link', { name: 'G1：无法接收通知', exact: true }).click() }
  stage = 'winner starts processing and comments'
  const transition = main.waitForResponse(r => r.url().endsWith('/transition'))
  await main.getByRole('button', { name: '开始处理', exact: true }).click()
  assert.equal((await transition).status(), 200)
  await waitText(main, 'IN_PROGRESS')
  await main.getByLabel('评论内容', { exact: true }).fill('已领取并开始检查通知配置。')
  await main.getByRole('button', { name: '发表评论', exact: true }).click()
  await waitText(main, '已领取并开始检查通知配置。')
  await waitText(main, 'TICKET_STATUS_CHANGED')
  await screenshot(main, '03-in-progress')
  log('transition-comment-events', 'winner starts IN_PROGRESS, posts real comment, reads actual audit events')
  await delay(5000)

  stage = 'browser offline and recover'
  await main.context().setOffline(true)
  await main.getByRole('button', { name: '刷新详情', exact: true }).click()
  await main.getByRole('alert').filter({ hasText: /网络/ }).waitFor()
  await screenshot(main, '04-network-error')
  await delay(3500)
  await main.context().setOffline(false)
  await main.getByRole('button', { name: '重试', exact: true }).click()
  await main.getByRole('button', { name: '解决工单', exact: true }).waitFor({ state: 'visible' })
  log('C18-network', 'offline fetch shows network error; recovery retries explicitly, no silent write retry')

  stage = 'refresh loses in-memory token'
  const storage = await main.context().storageState()
  assert.equal(storage.cookies.length, 0); assert(storage.origins.every(o => o.localStorage.length === 0))
  assert.equal(await main.evaluate(() => sessionStorage.length), 0)
  const protectedAfterRefresh = []
  const observe = req => { if (req.url().includes('/api/v1/') && !req.url().includes('/auth/')) protectedAfterRefresh.push(req) }
  main.on('request', observe)
  await main.reload()
  await main.getByRole('heading', { name: '登录工作台', exact: true }).waitFor()
  assert.equal(new URL(main.url()).pathname, '/login')
  assert.equal(protectedAfterRefresh.length, 0)
  main.off('request', observe)
  await screenshot(main, '05-refresh-login')
  log('C19', 'refresh redirects login, no protected requests with old token, local/session storage and cookies empty')
  await delay(3000)

  stage = 'cross-organization direct URL refuses read and claim'
  await login(main, david, origin)
  const denied = main.waitForResponse(r => r.url().endsWith('/api/v1' + path))
  // SPA navigation avoids intentionally clearing memory by a full browser goto.
  await main.evaluate(p => { history.pushState({}, '', p); window.dispatchEvent(new PopStateEvent('popstate')) }, path)
  assert.equal((await denied).status(), 404)
  await main.getByRole('alert').filter({ hasText: /不存在|无法访问/ }).waitFor()
  assert.equal(await main.getByRole('heading', { name: 'G1：无法接收通知', exact: true }).count(), 0)
  const wrongPath = `/organizations/${b.id}/tickets/${ticket.id}`
  const deniedOther = main.waitForResponse(r => r.url().endsWith('/api/v1' + wrongPath))
  await main.evaluate(p => { history.pushState({}, '', p); window.dispatchEvent(new PopStateEvent('popstate')) }, wrongPath)
  assert.equal((await deniedOther).status(), 404)
  const forbiddenClaim = await main.evaluate(async ({ path, token }) => (await fetch('/api/v1' + path + '/claim', { method: 'POST', headers: { 'Content-Type': 'application/json', Authorization: 'Bearer ' + token }, body: '{"expected_version":2}' })).status, { path: wrongPath, token: david.token })
  assert.equal(forbiddenClaim, 404)
  await screenshot(main, '06-cross-organization')
  log('cross-organization', 'David direct A URL and B-org/A-ticket URL both 404; forged claim also 404; no A title rendered')
  await delay(5000)

  stage = 'organization switch while prior list request is delayed'
  await main.getByRole('button', { name: '退出登录', exact: true }).click()
  await login(main, alice, origin)
  await select(main, a)
  await main.getByRole('link', { name: 'G1：无法接收通知', exact: true }).waitFor()
  let releaseOld, capturedOld, finishOld, oldRequest
  const oldGate = new Promise(resolve => { releaseOld = resolve })
  const captured = new Promise(resolve => { capturedOld = resolve })
  const oldCompleted = new Promise(resolve => { finishOld = resolve })
  const finished = req => { if (req === oldRequest) finishOld() }
  main.on('requestfinished', finished)
  main.on('requestfailed', finished)
  await main.route(`**/organizations/${a.id}/tickets?*`, async route => { oldRequest = route.request(); capturedOld(); await oldGate; try { await route.continue() } catch {} })
  await main.getByRole('button', { name: '刷新列表', exact: true }).click()
  await captured
  await select(main, b)
  await main.getByRole('link', { name: 'B organization only', exact: true }).waitFor()
  releaseOld()
  await Promise.race([oldCompleted, delay(10000).then(() => { throw new Error('old organization request did not settle after switch') })])
  main.off('requestfinished', finished)
  main.off('requestfailed', finished)
  assert.equal(await main.getByRole('link', { name: 'G1：无法接收通知', exact: true }).count(), 0)
  await main.unroute(`**/organizations/${a.id}/tickets?*`)
  await screenshot(main, '07-organization-switch')
  log('C17', 'switch A→B clears A rows immediately; delayed prior request cannot replace B content')
  await delay(5000)

  stage = '401 expires current session'
  await select(main, a)
  await main.getByRole('link', { name: 'G1：无法接收通知', exact: true }).click()
  // Forward an invalid credential to the real API, not a fabricated 401 response.
  await main.route('**/api/v1/organizations/**', route => route.continue({ headers: { ...route.request().headers(), authorization: 'Bearer invalid-browser-fixture' } }))
  await main.getByRole('button', { name: '刷新详情', exact: true }).click()
  await main.getByRole('heading', { name: '登录工作台', exact: true }).waitFor()
  assert.equal(new URL(main.url()).pathname, '/login')
  log('C18-401', 'real backend 401 clears session and returns login')
  await main.unroute('**/api/v1/organizations/**')
  await delay(3000)

  const allStorage = await Promise.all(contexts.map(c => c.storageState()))
  assert(allStorage.every(s => s.cookies.length === 0 && s.origins.every(o => !o.localStorage.length)))
  await main.context().close()
  const videoPath = await video.path()
  await rename(videoPath, out + '/g1-demo.webm')
  for (const secret of sensitive) assert(!viteOutput.includes(secret), 'credential leaked into Vite output')
  log('G1-artifacts', 'real browser screenshots and g1-demo.webm captured; no password/token/connection string in rendered pages')
  await writeFile(out + '/results.json', JSON.stringify({ generated_at: new Date().toISOString(), browser: await browser.version(), real_api: true, mock_responses: false, results: findings }, null, 2) + '\n')
} catch (error) {
  await primary?.screenshot({ path: out + '/last-failure.png', fullPage: true }).catch(() => {})
  let message = error instanceof Error ? error.message : 'unknown failure'
  for (const secret of sensitive) message = message.split(secret).join('[redacted fixture]')
  // Do not dump API request headers, Vite env, Playwright traces, or login form values.
  console.error(`FAIL browser stage: ${stage}\n${message}`)
  process.exitCode = 1
} finally {
  for (const context of contexts) await context.close().catch(() => {})
  await browser?.close()
  if (vite && vite.exitCode === null) {
    vite.kill('SIGTERM')
    await Promise.race([new Promise(resolve => vite.once('exit', resolve)), delay(3000)])
    if (vite.exitCode === null) vite.kill('SIGKILL')
  }
}
