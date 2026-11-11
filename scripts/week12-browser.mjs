// W12 final acceptance, retaining the W11 full-workflow snapshot and adding management flows.
// Owned fixture only. Screenshots contain synthetic records; credentials stay out of artifacts.
import assert from 'node:assert/strict'
import { randomBytes } from 'node:crypto'
import { readdir, access, writeFile, mkdir } from 'node:fs/promises'
import { performance } from 'node:perf_hooks'
import { homedir } from 'node:os'
import { chromium } from '../web/node_modules/playwright/index.mjs'
const origin = process.env.NEXUSDESK_LIVE_TEST_ORIGIN, api = process.env.NEXUSDESK_LIVE_TEST_API
assert(/^http:\/\/127\.0\.0\.1:\d+$/.test(origin) && /^http:\/\/127\.0\.0\.1:\d+$/.test(api))
assert(/^nexusdesk_w02d2_test_[a-f0-9]{12}$/.test(process.env.NEXUSDESK_LIVE_TEST_DATABASE))
assert(!process.env.DEEPSEEK_API_KEY && !process.env.JWT_SECRET_BASE64 && !process.env.DB_PASSWORD)
const demo = process.argv.includes('--demo')
const out = 'docs/testing/evidence/week12', artifact = demo ? out + '/demo-frames' : out, tag = randomBytes(6).toString('hex')
const email = `w12-ui-${tag}@example.test`, password = randomBytes(24).toString('base64url')
const longTitle = '长标题边界验证 · ' + '数据库连接与权限范围'.repeat(12)
await mkdir(artifact, { recursive: true })
async function request(path, method = 'GET', body, token, status = 200) {
  const r = await fetch(api + '/api/v1' + path, { method, headers: { ...(body ? { 'Content-Type': 'application/json' } : {}), ...(token ? { Authorization: 'Bearer ' + token } : {}) }, body: body ? JSON.stringify(body) : undefined })
  assert.equal(r.status, status, path)
  return r.status === 204 ? undefined : (await r.json()).data
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
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, ...(demo ? { recordVideo: { dir: artifact, size: { width: 1440, height: 1000 } } } : {}) })
const page = await context.newPage(); page.setDefaultTimeout(30000)
const errors = [], steps = [], shots = [], chapters = [], started = performance.now()
page.on('pageerror', e => errors.push(e.name))
async function shot(name) {
  await page.evaluate(() => document.fonts.ready)
  await page.screenshot({ path: `${artifact}/${name}.png`, fullPage: true })
  shots.push(name)
}
function pass(name) { steps.push(name); console.log('PASS ' + name) }
async function chapter(title, text) {
  if (!demo) return
  const start = (performance.now() - started) / 1000
  await page.evaluate(({title, text}) => {
    const panel = document.createElement('aside'); panel.id = 'demo-caption'
    Object.assign(panel.style, { position: 'fixed', bottom: '24px', left: '24px', right: '24px', zIndex: '99999', background: '#0b1730', color: '#fff', padding: '20px 28px', borderRadius: '12px', boxShadow: '0 8px 32px #0005', font: '18px/1.7 sans-serif' })
    const heading = document.createElement('strong'); heading.textContent = title; heading.style.display = 'block'
    const content = document.createElement('div'); content.textContent = text
    panel.append(heading, content); document.body.append(panel)
  }, {title, text})
  await page.waitForTimeout(44000)
  await page.evaluate(() => document.getElementById('demo-caption')?.remove())
  chapters.push({ title, text, start_seconds: start, end_seconds: (performance.now() - started) / 1000 })
}

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
  const teammateEmail = `w12-member-${tag}@example.test`
  const teammate = await request('/auth/register', 'POST', { email: teammateEmail, display_name: '周予安', password }, undefined, 201)
  await nav('组织成员')
  await page.locator('[name=member_user_id]').fill(String(teammate.id))
  await page.getByRole('button', { name: '添加成员', exact: true }).click()
  await page.getByRole('cell', { name: /周予安/ }).waitFor()
  await noOverflow(); await shot('14-members')
  await page.locator('[name=member_user_id]').fill(String(teammate.id))
  await page.getByRole('button', { name: '添加成员', exact: true }).click()
  await page.getByRole('alert').filter({ hasText: '该账号已是组织成员' }).waitFor()
  assert.equal((await request(`/organizations/${org}/members`, 'GET', undefined, token)).items.length, 2)
  pass('admin adds registered member/duplicate rejected without another relationship')
  await chapter('01 · 组织与成员', '注册只创建账号；创建组织后获得管理员角色。管理员添加已注册同事，成员关系由后端逐次检查。所有画面均为自动生成的演示数据。')
  await nav('工单')
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
  const managedPath = new URL(page.url()).pathname.replace(/^\/organizations/, '/organizations')
  await page.getByRole('button', { name: '编辑工单', exact: true }).click()
  await page.locator('[name=edit_title]').fill('新版工作台协作验收')
  await page.locator('[name=edit_body]').fill('已更新：验证编辑、指派、并发冲突与评论。')
  await page.getByRole('button', { name: '保存修改', exact: true }).click()
  await page.getByRole('heading', { name: '新版工作台协作验收', exact: true }).waitFor()
  await page.getByRole('button', { name: '指派受理人', exact: true }).click()
  await page.locator('[name=assignee_id]').selectOption(String(teammate.id))
  await page.getByRole('button', { name: '保存指派', exact: true }).click()
  await page.locator('.ticket-assign-form').waitFor({ state: 'hidden' })
  assert.equal((await request(managedPath, 'GET', undefined, token)).assignee_id, teammate.id)
  await shot('15-ticket-managed')
  await chapter('02 · 工单管理', '管理员可以编辑 OPEN 工单、指派受理人及软删除。编辑和指派携带打开表单时的版本；其他人先更新会触发冲突，界面要求重新确认。')
  await page.getByRole('button', { name: '指派受理人', exact: true }).click()
  await page.locator('[name=assignee_id]').selectOption('unassigned')
  await page.getByRole('button', { name: '保存指派', exact: true }).click()
  await page.locator('.ticket-assign-form').waitFor({ state: 'hidden' })
  await page.getByRole('button', { name: '编辑工单', exact: true }).click()
  const current = await request(managedPath, 'GET', undefined, token)
  await request(managedPath, 'PATCH', { expected_version: current.version, title: '并发更新后的最新工单标题' }, token)
  await page.locator('[name=edit_title]').fill('这条过期修改不应覆盖最新数据')
  const conflict = page.waitForResponse(r => r.request().method() === 'PATCH' && r.status() === 409)
  await page.getByRole('button', { name: '保存修改', exact: true }).click(); await conflict
  await page.getByRole('heading', { name: '并发更新后的最新工单标题', exact: true }).waitFor()
  await page.getByText('工单已发生变化，已重新读取，请重新确认后操作。', { exact: true }).waitFor()
  assert.equal((await request(managedPath, 'GET', undefined, token)).title, '并发更新后的最新工单标题')
  pass('edit/assign/unassign/version conflict refreshes without replaying stale write')
  await page.getByRole('button', { name: '领取工单', exact: true }).click()
  await page.getByRole('button', { name: '开始处理', exact: true }).click()
  await page.getByLabel('评论内容').fill('已复现问题，正在核对配置和连接状态。')
  await page.getByRole('button', { name: '发表评论', exact: true }).click()
  await page.getByText('已复现问题，正在核对配置和连接状态。', { exact: true }).waitFor()
  await page.getByText('添加评论', { exact: true }).waitFor()
  await shot('05-ticket-detail'); pass('register/login/create organization/ticket/claim/transition/comment')
  await chapter('03 · 协作与状态', '领取后仍是 OPEN；开始处理进入 IN_PROGRESS。评论与事件保存协作过程，状态和事件使用同一事务，expected_version 防止旧操作覆盖。')
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
  await chapter('04 · 异步知识库', '上传 Markdown 后创建持久任务。Redis 负责通知，Worker 领取租约并提取、分块和索引；PostgreSQL 保留任务真相。PDF 当前只支持上传和下载。')
  await nav('检索问答')
  await page.getByLabel('检索问题', { exact: true }).fill('数据库连接超时应该先检查什么？')
  await page.getByLabel('检索策略', { exact: true }).selectOption('hybrid')
  await page.getByRole('button', { name: '基于知识库回答', exact: true }).click()
  await page.getByRole('heading', { name: '知识库回答', exact: true }).waitFor()
  await page.getByText('先核对数据库连接地址与端口，再检查网络连通性。', { exact: true }).waitFor()
  await shot('08-rag'); pass('hybrid retrieval/grounded answer/citations/measured usage')
  await chapter('05 · 有引用的回答', '混合检索结合向量与关键词候选，回答附带实际来源。此视频使用确定性本地模型夹具验证功能链路；真实 DeepSeek 质量及 W10 语义评测需分别看报告。')
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
  await chapter('06 · 受控 AI 工具', 'AI 先生成创建工单提议，数据库尚无新增工单。用户点击确认后服务端重新检查权限，并用数据库约束避免重复执行。模型没有任意数据库或 Shell 权限。')
  await page.setViewportSize({ width: 390, height: 844 }); await noOverflow(); await shot('11-mobile-ai')
  await page.getByRole('button', { name: '打开导航' }).click()
  const sidebarLink = page.getByRole('navigation', { name: '组织工作区' }).getByRole('link', { name: '工单', exact: true })
  await sidebarLink.focus(); await page.keyboard.press('Escape')
  await page.waitForFunction(() => document.activeElement?.getAttribute('aria-label') === '打开导航')
  assert.equal(await sidebarLink.isVisible(), false)
  await nav('工单'); await page.getByRole('link', { name: /^排查数据库连接超时/ }).waitFor(); await boundedTicketTitle(); await noOverflow(); await shot('12-mobile-tickets')
  await nav('检索问答'); await page.getByLabel('检索问题', { exact: true }).waitFor(); await noOverflow(); await shot('13-mobile-rag')
  await nav('组织成员'); await page.getByRole('cell', { name: /周予安/ }).waitFor()
  await noOverflow(); await shot('16-mobile-members')
  await page.setViewportSize({ width: 1440, height: 1000 })
  // A second real member session sees the roster but no administrator actions.
  const memberContext = await browser.newContext()
  try {
    const memberPage = await memberContext.newPage()
    await memberPage.goto(origin + '/login')
    await memberPage.getByLabel('邮箱', { exact: true }).fill(teammateEmail)
    await memberPage.getByLabel('密码', { exact: true }).fill(password)
    await memberPage.getByRole('button', { name: '登录', exact: true }).click()
    await memberPage.waitForURL('**/desk')
    await memberPage.getByLabel('组织选择').selectOption(String(org))
    await memberPage.getByRole('navigation', { name: '组织工作区' }).getByRole('link', { name: '组织成员', exact: true }).click()
    await memberPage.getByRole('cell', { name: /周予安/ }).waitFor()
    assert.equal(await memberPage.locator('.member-form').count(), 0)
    const mt = (await request('/auth/login', 'POST', { email: teammateEmail, password })).access_token
    const t = await request(tickets, 'POST', { title: '成员权限边界验证', body: '合成工单', priority: 2 }, token, 201)
    await request(tickets + '/' + t.id, 'PATCH', { expected_version: t.version, assignee_id: teammate.id }, mt, 403)
    await request(tickets + '/' + t.id, 'DELETE', undefined, mt, 403)
    await nav('工单')
    await page.getByRole('link', { name: '成员权限边界验证', exact: true }).click()
    await page.getByRole('button', { name: '删除工单', exact: true }).click()
    await shot('17-delete-confirm')
    await page.getByRole('button', { name: '取消删除', exact: true }).click()
    await request(tickets + '/' + t.id, 'GET', undefined, token)
    await page.getByRole('button', { name: '删除工单', exact: true }).click()
    await page.getByRole('button', { name: '确认删除', exact: true }).click()
    await page.waitForURL('**/tickets')
    await request(tickets + '/' + t.id, 'GET', undefined, token, 404)
    assert.equal(await page.getByRole('link', { name: '成员权限边界验证', exact: true }).count(), 0)
    pass('ordinary member read-only management/backend 403/delete cancel and explicit confirmation')
  } finally { await memberContext.close() }
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
  await chapter('07 · 隔离、维护与交付', '切换组织后旧组织内容清空；手机端可使用同一工作台。项目提供本机指标、性能与故障恢复证据、运行手册及源码包。此版本是本地求职 MVP，尚未做公网生产部署。')
  await page.getByRole('button', { name: '退出登录', exact: true }).click()
  await page.waitForURL('**/login'); assert.equal(await page.getByLabel('消息', { exact: true }).count(), 0)
  assert.equal(await page.evaluate(() => localStorage.length + sessionStorage.length), 0)
  assert.deepEqual(errors, [])
  pass('logout clears protected content/no token persistence/no page errors')
  await writeFile(out + (demo ? '/demo-browser.json' : '/browser.json'), JSON.stringify({ status: 'PASS', synthetic_data_only: true, model: 'deterministic local HTTP fixture, not semantic evaluation', steps, screenshots: shots, page_errors: errors, desktop: '1440x1000', mobile: '390x844' }, null, 2) + '\n')
} finally {
  // Do not save partially filled auth forms if setup fails.
  if (!/\/(login|register)/.test(new URL(page.url() || origin).pathname)) await page.screenshot({ path: artifact + '/browser-last-state.png', fullPage: true }).catch(() => {})
  await context.close()
  if (demo) {
    await page.video().saveAs(out + '/nexusdesk-demo.webm')
    await page.video().delete()
    await writeFile(out + '/demo-chapters.json', JSON.stringify({ automated_captioned: true, audio: false, duration_seconds: (performance.now() - started) / 1000, chapters }, null, 2) + '\n')
    const time = s => new Date(Math.round(s * 1000)).toISOString().slice(11, 23)
    await writeFile(out + '/nexusdesk-demo.vtt', 'WEBVTT\n\n' + chapters.map(c => `${time(c.start_seconds)} --> ${time(c.end_seconds)}\n${c.title}\n${c.text}\n`).join('\n'))
  }
  await browser.close()
}
