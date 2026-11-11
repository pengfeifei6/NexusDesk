// W11 regression snapshot of W10 UI acceptance; writes only W11 artifacts.
// Owned fixture only. Screenshots contain synthetic records; credentials stay out of artifacts.
import assert from 'node:assert/strict'
import { randomBytes } from 'node:crypto'
import { readdir, access, writeFile, mkdir } from 'node:fs/promises'
import { homedir } from 'node:os'
import { chromium } from '../web/node_modules/playwright/index.mjs'
const origin = process.env.NEXUSDESK_LIVE_TEST_ORIGIN, api = process.env.NEXUSDESK_LIVE_TEST_API
assert(/^http:\/\/127\.0\.0\.1:\d+$/.test(origin) && /^http:\/\/127\.0\.0\.1:\d+$/.test(api))
assert(/^nexusdesk_w02d2_test_[a-f0-9]{12}$/.test(process.env.NEXUSDESK_LIVE_TEST_DATABASE))
assert(!process.env.DEEPSEEK_API_KEY && !process.env.JWT_SECRET_BASE64 && !process.env.DB_PASSWORD)
const out = 'docs/testing/evidence/week11', tag = randomBytes(6).toString('hex')
const email = `w11-ui-${tag}@example.test`, password = randomBytes(24).toString('base64url')
const longTitle = '长标题边界验证 · ' + '数据库连接与权限范围'.repeat(12)
await mkdir(out, { recursive: true })
async function request(path, method = 'GET', body, token, status = 200) {
  const r = await fetch(api + '/api/v1' + path, { method, headers: { ...(body ? { 'Content-Type': 'application/json' } : {}), ...(token ? { Authorization: 'Bearer ' + token } : {}) }, body: body ? JSON.stringify(body) : undefined })
  assert.equal(r.status, status, path)
  return (await r.json()).data
}
let executable
for (const dir of (await readdir(homedir() + '/.cache/ms-playwright')).filter(x => /^chromium-\d+$/.test(x)).sort().reverse()) {
  for (const sub of ['chrome-linux64/chrome', 'chrome-linux/chrome']) {
    const p = homedir() + '/.cache/ms-playwright/' + dir + '/' + sub
    try { await access(p); executable = p; break } catch {}
  }
  if (executable) break
}
assert(executable)
const browser = await chromium.launch({ headless: true, executablePath: executable })
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } })
const page = await context.newPage(); page.setDefaultTimeout(30000)
const errors = [], steps = [], shots = []
page.on('pageerror', e => errors.push(e.name))
async function shot(name) {
  await page.evaluate(() => document.fonts.ready)
  await page.screenshot({ path: `${out}/${name}.png`, fullPage: true })
  shots.push(name)
}
function pass(name) { steps.push(name); console.log('PASS ' + name) }
async function nav(name) {
  const link = page.getByRole('navigation', { name: '组织工作区' }).getByRole('link', { name, exact: true })
  if (!await link.isVisible()) await page.getByRole('button', { name: '打开导航' }).click()
  await link.click()
}
async function noOverflow() {
  assert(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1), 'page horizontal overflow')
}
async function boundedTicketTitle() {
  const link = page.getByRole('link', { name: longTitle, exact: true })
  await link.waitFor()
  assert.equal(await link.getAttribute('title'), longTitle)
  const title = link.locator('.ticket-title-text')
  assert.equal(await title.textContent(), longTitle)
  const metrics = await title.evaluate(el => ({ height: el.getBoundingClientRect().height, lineHeight: Number.parseFloat(getComputedStyle(el).lineHeight), fullHeight: el.scrollHeight }))
  assert(metrics.height > 0 && metrics.height <= metrics.lineHeight * 2 + 1, 'ticket title exceeds two lines')
  assert(metrics.fullHeight > metrics.height, 'long-title fixture must exercise visual truncation')
  assert.match(await link.locator('small').innerText(), /^#\d+$/)
  assert(await link.locator('small').isVisible(), 'ticket ID remains visible separately')
}
try {
  await page.goto(origin + '/login'); await page.getByRole('heading', { name: '登录工作台' }).waitFor()
  await shot('01-login')
  await page.getByRole('link', { name: '注册账号', exact: true }).click()
  await shot('02-register')
  await page.getByLabel('显示名称', { exact: true }).fill('林知远')
  await page.getByLabel('邮箱', { exact: true }).fill(email)
  await page.getByLabel('密码', { exact: true }).fill(password)
  await page.getByRole('button', { name: '注册', exact: true }).click()
  await page.waitForURL('**/login?registered=1')
  await page.getByLabel('邮箱', { exact: true }).fill(email)
  await page.getByLabel('密码', { exact: true }).fill(password)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await page.waitForURL('**/desk'); await page.getByLabel('组织名称').waitFor()
  await shot('03-workspace-empty')
  await page.getByLabel('组织名称').fill('晨光技术 · 客户支持')
  await page.getByRole('button', { name: '创建组织', exact: true }).click()
  await page.waitForURL('**/tickets')
  const org = /organizations\/(\d+)/.exec(page.url())[1]
  const session = await request('/auth/login', 'POST', { email, password })
  const token = session.access_token, tickets = `/organizations/${org}/tickets`
  for (const [title, priority] of [['客户门户登录后页面加载缓慢', 3], ['知识库文档更新与发布检查', 2], ['新增成员入组操作指引', 1], ['数据库连接超时排查与复验', 3], [longTitle, 2]]) {
    await request(tickets, 'POST', { title, body: '合成演示记录：请记录影响范围、复现步骤和复验结果。', priority }, token, 201)
  }
  await page.getByRole('button', { name: '刷新列表', exact: true }).click()
  await page.getByRole('link', { name: '客户门户登录后页面加载缓慢' }).waitFor()
  await noOverflow(); await shot('04-tickets')
  await page.getByRole('link', { name: longTitle, exact: true }).click()
  await page.waitForURL(/\/tickets\/\d+$/)
  assert.equal(await page.getByRole('heading', { name: longTitle, exact: true }).textContent(), longTitle)
  await page.getByRole('link', { name: '← 返回工单列表', exact: true }).click()
  await page.waitForURL('**/tickets')
  await page.getByLabel('工单标题', { exact: true }).fill('验证新版工作台的工单协作流程')
  await page.getByLabel('工单描述', { exact: true }).fill('验证创建、领取、处理与评论功能。此条为自动验收合成数据。')
  await page.getByRole('button', { name: '创建工单', exact: true }).click()
  await page.waitForURL(/\/tickets\/\d+$/)
  await page.getByRole('button', { name: '领取工单', exact: true }).click()
  await page.getByRole('button', { name: '开始处理', exact: true }).click()
  await page.getByLabel('评论内容').fill('已复现问题，正在核对配置和连接状态。')
  await page.getByRole('button', { name: '发表评论', exact: true }).click()
  await page.getByText('已复现问题，正在核对配置和连接状态。', { exact: true }).waitFor()
  await page.getByText('添加评论', { exact: true }).waitFor()
  await shot('05-ticket-detail'); pass('register/login/create organization/ticket/claim/transition/comment')
  await nav('知识库')
  await page.getByLabel('选择文件').setInputFiles({ name: '数据库连接排障指南.md', mimeType: 'text/markdown', buffer: Buffer.from('# 数据库连接排障指南\n\n先核对数据库连接地址与端口，再检查网络连通性。记录错误发生时间、影响范围和复验结果。\n\n请勿在工单或日志中记录密码和密钥。') })
  await page.getByRole('button', { name: '上传并异步处理', exact: true }).click()
  await page.waitForURL(/\/knowledge\/\d+$/)
  await page.getByText('处理完成', { exact: false }).first().waitFor({ timeout: 60000 })
  await page.getByRole('heading', { name: '正文预览' }).waitFor()
  await shot('06-document-detail')
  const download = page.waitForEvent('download')
  await page.getByRole('button', { name: '下载原文件', exact: true }).click()
  assert.equal((await download).suggestedFilename(), '数据库连接排障指南.md')
  await nav('知识库'); await page.getByRole('link', { name: /^数据库连接排障指南\.md/ }).waitFor()
  await shot('07-knowledge'); pass('upload/async worker/index/preview/download')
  await nav('检索问答')
  await page.getByLabel('检索问题', { exact: true }).fill('数据库连接超时应该先检查什么？')
  await page.getByLabel('检索策略', { exact: true }).selectOption('hybrid')
  await page.getByRole('button', { name: '基于知识库回答', exact: true }).click()
  await page.getByRole('heading', { name: '知识库回答', exact: true }).waitFor()
  await page.getByText('先核对数据库连接地址与端口，再检查网络连通性。', { exact: true }).waitFor()
  await shot('08-rag'); pass('hybrid retrieval/grounded answer/citations/measured usage')
  await nav('AI 对话')
  await page.getByLabel('对话标题', { exact: true }).fill('数据库故障协作')
  await page.getByRole('button', { name: '新建对话', exact: true }).click()
  await page.getByLabel('消息', { exact: true }).waitFor()
  const before = (await request(tickets, 'GET', undefined, token)).items.length
  await page.getByLabel('消息', { exact: true }).fill('请提议创建数据库连接排障工单。')
  await page.getByRole('button', { name: '发送', exact: true }).click()
  await page.getByRole('button', { name: '确认创建工单', exact: true }).waitFor()
  await page.getByRole('button', { name: '停止生成', exact: true }).waitFor({ state: 'hidden' })
  assert.equal((await request(tickets, 'GET', undefined, token)).items.length, before)
  await shot('09-ai-proposal')
  await page.getByRole('button', { name: '确认创建工单', exact: true }).click()
  await page.getByRole('link', { name: /查看工单 #/ }).waitFor()
  assert.equal((await request(tickets, 'GET', undefined, token)).items.length, before + 1)
  await shot('10-ai-confirmed'); pass('SSE tool proposal/no premature write/human confirmation')
  await page.setViewportSize({ width: 390, height: 844 }); await noOverflow(); await shot('11-mobile-ai')
  await page.getByRole('button', { name: '打开导航' }).click()
  const sidebarLink = page.getByRole('navigation', { name: '组织工作区' }).getByRole('link', { name: '工单', exact: true })
  await sidebarLink.focus(); await page.keyboard.press('Escape')
  await page.waitForFunction(() => document.activeElement?.getAttribute('aria-label') === '打开导航')
  assert.equal(await sidebarLink.isVisible(), false)
  await nav('工单'); await page.getByRole('link', { name: /^排查数据库连接超时/ }).waitFor(); await boundedTicketTitle(); await noOverflow(); await shot('12-mobile-tickets')
  await nav('检索问答'); await page.getByLabel('检索问题', { exact: true }).waitFor(); await noOverflow(); await shot('13-mobile-rag')
  const org2 = await request('/organizations', 'POST', { name: '独立研发组织' }, token, 201)
  await page.setViewportSize({ width: 1440, height: 1000 })
  // Refresh options through /desk, then switch while old-organization tickets are visible.
  await page.getByRole('link', { name: '创建组织', exact: true }).click()
  await page.getByLabel('组织选择').locator('option', { hasText: '独立研发组织' }).waitFor({ state: 'attached' })
  await page.getByLabel('组织选择').selectOption(String(org))
  await page.waitForURL(new RegExp('/organizations/' + org + '/tickets$'))
  const oldTicketTitle = '客户门户登录后页面加载缓慢'
  const oldTicket = page.getByRole('link', { name: oldTicketTitle, exact: true })
  await oldTicket.waitFor()
  const oldTicketHref = await oldTicket.getAttribute('href')
  assert(oldTicketHref && oldTicketHref.startsWith('/organizations/' + org + '/tickets/'))
  const newListResponse = page.waitForResponse(response => {
    const url = new URL(response.url())
    return url.pathname === '/api/v1/organizations/' + org2.id + '/tickets' && response.request().method() === 'GET'
  })
  await page.getByLabel('组织选择').selectOption(String(org2.id))
  await page.waitForURL(new RegExp('/organizations/' + org2.id + '/tickets$'))
  const newList = await newListResponse
  assert.equal(newList.status(), 200)
  assert.deepEqual((await newList.json()).data.items, [])
  await page.getByText('暂无工单', { exact: true }).waitFor()
  assert.equal(await page.getByText(oldTicketTitle, { exact: true }).count(), 0)
  assert.equal(await page.locator(`a[href="${oldTicketHref}"]`).count(), 0)
  assert.equal(await page.getByRole('link', { name: oldTicketTitle, exact: true }).count(), 0)
  pass('390px responsive navigation/no overflow/organization scope change')
  await page.getByRole('button', { name: '退出登录', exact: true }).click()
  await page.waitForURL('**/login'); assert.equal(await page.getByLabel('消息', { exact: true }).count(), 0)
  assert.equal(await page.evaluate(() => localStorage.length + sessionStorage.length), 0)
  assert.deepEqual(errors, [])
  pass('logout clears protected content/no token persistence/no page errors')
  await writeFile(out + '/browser.json', JSON.stringify({ status: 'PASS', synthetic_data_only: true, model: 'deterministic local HTTP fixture, not semantic evaluation', steps, screenshots: shots, page_errors: errors, desktop: '1440x1000', mobile: '390x844' }, null, 2) + '\n')
} finally {
  // Do not save partially filled auth forms if setup fails.
  if (!/\/(login|register)/.test(new URL(page.url() || origin).pathname)) await page.screenshot({ path: out + '/browser-last-state.png', fullPage: true }).catch(() => {})
  await context.close(); await browser.close()
}
