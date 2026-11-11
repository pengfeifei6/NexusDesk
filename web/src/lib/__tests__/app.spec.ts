import { describe, it, expect, vi, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import App from '../../App.vue'
import { routes } from '../../router'
import { useSession } from '../../stores/session'
import { api } from '../client'
afterEach(() => vi.restoreAllMocks())
describe('mounted request isolation', () => {
  it('clears old org immediately and ignores late responses', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    useSession().user = { id: '1', display_name: '测试', email: 'test@example.test' }
    let late!: (v: unknown) => void
    vi.spyOn(api, 'request').mockImplementation(async (path) => {
      if (path.startsWith('/organizations?'))
        return {
          items: [
            { id: '10', name: '甲', my_role: 'member' },
            { id: '20', name: '乙', my_role: 'member' },
          ],
        }
      if (path.startsWith('/organizations/10/tickets?'))
        return new Promise((resolve) => (late = resolve))
      return { items: [{ id: '2', title: '新组织工单', status: 'OPEN', priority: 2 }] }
    })
    const router = createRouter({ history: createMemoryHistory(), routes })
    await router.push('/organizations/10/tickets')
    await router.isReady()
    const wrapper = mount(App, { global: { plugins: [pinia, router] } })
    await flushPromises()
    await router.push('/organizations/20/tickets')
    await flushPromises()
    late({ items: [{ id: '1', title: '旧组织迟到工单', status: 'OPEN', priority: 2 }] })
    await flushPromises()
    expect(wrapper.text()).toContain('新组织工单')
    expect(wrapper.text()).not.toContain('旧组织迟到工单')
    wrapper.unmount()
  })
  it('logout cannot be reversed by a late detail response', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    useSession().user = { id: '1', display_name: '测试', email: 'test@example.test' }
    let late!: (v: unknown) => void
    vi.spyOn(api, 'request').mockImplementation(async (path) =>
      path.startsWith('/organizations?')
        ? { items: [] }
        : new Promise((resolve) => (late = resolve)),
    )
    const router = createRouter({ history: createMemoryHistory(), routes })
    await router.push('/organizations/10/tickets/5')
    const wrapper = mount(App, { global: { plugins: [pinia, router] } })
    await flushPromises()
    await wrapper
      .findAll('button')
      .find((b) => b.text() === '退出登录')!
      .trigger('click')
    await flushPromises()
    late({ id: '5', title: '不应展示的工单' })
    await flushPromises()
    expect(wrapper.text()).not.toContain('不应展示的工单')
    expect(wrapper.find('input[type=password]').exists()).toBe(true)
    wrapper.unmount()
  })
  it('keeps the newest same-organization filter response', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    useSession().user = { id: '1', display_name: '测试', email: 'test@example.test' }
    let late!: (v: unknown) => void
    vi.spyOn(api, 'request').mockImplementation(async (path) => {
      if (path.startsWith('/organizations?')) return { items: [] }
      if (path.includes('status=OPEN')) return new Promise((resolve) => (late = resolve))
      return { items: [{ id: '2', title: '最新筛选结果', status: 'RESOLVED', priority: 2 }] }
    })
    const router = createRouter({ history: createMemoryHistory(), routes })
    await router.push('/organizations/10/tickets')
    const wrapper = mount(App, { global: { plugins: [pinia, router] } })
    await flushPromises()
    const filter = wrapper.findAll('select')[1]!
    await filter.setValue('OPEN')
    await flushPromises()
    await filter.setValue('RESOLVED')
    await flushPromises()
    late({ items: [{ id: '1', title: '旧筛选结果', status: 'OPEN', priority: 2 }] })
    await flushPromises()
    expect(wrapper.text()).toContain('最新筛选结果')
    expect(wrapper.text()).not.toContain('旧筛选结果')
    wrapper.unmount()
  })
})
it('keeps a safe expiry message after redirecting to login', async () => {
  const pinia = createPinia()
  setActivePinia(pinia)
  useSession().user = { id: '1', display_name: '测试', email: 'test@example.test' }
  vi.spyOn(api, 'request').mockResolvedValue({ items: [] })
  const router = createRouter({ history: createMemoryHistory(), routes })
  await router.push('/desk')
  const wrapper = mount(App, { global: { plugins: [pinia, router] } })
  await flushPromises()
  api.onExpired()
  await flushPromises()
  expect(wrapper.text()).toContain('登录已失效，请重新登录')
  wrapper.unmount()
})
it('ignores an older same-organization failure after new filter success', async () => {
  const pinia = createPinia()
  setActivePinia(pinia)
  useSession().user = { id: '1', display_name: '测试', email: 'test@example.test' }
  let rejectOld!: (e: Error) => void
  vi.spyOn(api, 'request').mockImplementation(async (path) => {
    if (path.startsWith('/organizations?')) return { items: [] }
    if (path.includes('status=OPEN')) return new Promise((_resolve, reject) => (rejectOld = reject))
    return { items: [] }
  })
  const router = createRouter({ history: createMemoryHistory(), routes })
  await router.push('/organizations/10/tickets')
  const wrapper = mount(App, { global: { plugins: [pinia, router] } })
  await flushPromises()
  const filter = wrapper.findAll('select')[1]!
  await filter.setValue('OPEN')
  await flushPromises()
  await filter.setValue('RESOLVED')
  await flushPromises()
  rejectOld(new Error('旧筛选错误'))
  await flushPromises()
  expect(wrapper.text()).not.toContain('旧筛选错误')
  wrapper.unmount()
})
it('clears previous session organizations before the next login list resolves', async () => {
  const pinia = createPinia()
  setActivePinia(pinia)
  const session = useSession()
  session.user = { id: '1', display_name: '甲', email: 'a@example.test' }
  let orgCalls = 0
  vi.spyOn(api, 'request').mockImplementation(async () => {
    orgCalls++
    if (orgCalls <= 2)
      return {
        items: Array.from({ length: 20 }, (_, i) => ({
          id: String(i + 10),
          name: '甲的私有组织' + i,
          my_role: 'admin',
        })),
      }
    return new Promise(() => {})
  })
  const router = createRouter({ history: createMemoryHistory(), routes })
  await router.push('/desk')
  const wrapper = mount(App, { global: { plugins: [pinia, router] } })
  await flushPromises()
  expect(wrapper.text()).toContain('甲的私有组织')
  await wrapper
    .findAll('button')
    .find((b) => b.text() === '下一页组织')!
    .trigger('click')
  await flushPromises()
  expect(wrapper.text()).toContain('第 2 页')
  await wrapper
    .findAll('button')
    .find((b) => b.text() === '退出登录')!
    .trigger('click')
  await flushPromises()
  session.user = { id: '2', display_name: '乙', email: 'b@example.test' }
  await router.push('/desk')
  await flushPromises()
  expect(wrapper.text()).not.toContain('甲的私有组织')
  expect(wrapper.text()).toContain('第 1 页')
  wrapper.unmount()
})
it('keeps organization load failure visible and retries the organization request', async () => {
  const pinia = createPinia()
  setActivePinia(pinia)
  useSession().user = { id: '1', display_name: '甲', email: 'a@example.test' }
  let calls = 0
  vi.spyOn(api, 'request').mockImplementation(async () => {
    if (++calls === 1) throw new Error('网络连接失败，请检查连接后重试')
    return { items: [{ id: '10', name: '恢复后的组织', my_role: 'member' }] }
  })
  const router = createRouter({ history: createMemoryHistory(), routes })
  await router.push('/desk')
  const wrapper = mount(App, { global: { plugins: [pinia, router] } })
  await flushPromises()
  expect(wrapper.text()).toContain('网络连接失败')
  await wrapper
    .findAll('button')
    .find((b) => b.text() === '重试')!
    .trigger('click')
  await flushPromises()
  expect(wrapper.text()).toContain('恢复后的组织')
  expect(wrapper.text()).not.toContain('网络连接失败')
  wrapper.unmount()
})
it('retries a failed organization page instead of reloading tickets', async () => {
  const pinia = createPinia()
  setActivePinia(pinia)
  useSession().user = { id: '1', display_name: '甲', email: 'a@example.test' }
  let pageTwoCalls = 0
  vi.spyOn(api, 'request').mockImplementation(async (path) => {
    if (path.startsWith('/organizations?page=2')) {
      if (++pageTwoCalls === 1) throw new Error('组织分页连接失败')
      return { items: [{ id: '99', name: '第二页恢复组织', my_role: 'member' }] }
    }
    if (path.startsWith('/organizations?'))
      return {
        items: Array.from({ length: 20 }, (_, i) => ({
          id: String(i + 1),
          name: '组织' + i,
          my_role: 'member',
        })),
      }
    return { items: [] }
  })
  const router = createRouter({ history: createMemoryHistory(), routes })
  await router.push('/organizations/1/tickets')
  const wrapper = mount(App, { global: { plugins: [pinia, router] } })
  await flushPromises()
  await wrapper
    .findAll('button')
    .find((b) => b.text() === '下一页组织')!
    .trigger('click')
  await flushPromises()
  expect(wrapper.text()).toContain('组织分页连接失败')
  await wrapper
    .findAll('button')
    .find((b) => b.text() === '重试')!
    .trigger('click')
  await flushPromises()
  expect(wrapper.text()).toContain('第二页恢复组织')
  wrapper.unmount()
})
it('redirects authenticated history Back away from the login form', async () => {
  const pinia = createPinia()
  setActivePinia(pinia)
  useSession().user = { id: '1', display_name: '甲', email: 'a@example.test' }
  vi.spyOn(api, 'request').mockResolvedValue({
    items: [{ id: '10', name: '甲的组织', my_role: 'admin' }],
  })
  const router = createRouter({ history: createMemoryHistory(), routes })
  await router.push('/login')
  await router.push('/desk')
  const wrapper = mount(App, { global: { plugins: [pinia, router] } })
  await flushPromises()
  router.back()
  await new Promise((resolve) => setTimeout(resolve, 10))
  await flushPromises()
  expect(router.currentRoute.value.path).toBe('/desk')
  expect(wrapper.find('input[type=password]').exists()).toBe(false)
  wrapper.unmount()
})
