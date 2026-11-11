import { afterEach, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import App from '../../App.vue'
import { routes } from '../../router'
import { useSession } from '../../stores/session'
import { api } from '../client'
const wrappers: ReturnType<typeof mount>[] = []
afterEach(() => {
  wrappers.splice(0).forEach((w) => w.unmount())
  vi.restoreAllMocks()
})
async function render(path: string, response: (path: string) => unknown) {
  const pinia = createPinia()
  setActivePinia(pinia)
  useSession().user = { id: '1', email: 'owner@example.test', display_name: '林知远' }
  const request = vi.spyOn(api, 'request').mockImplementation(async (p) => response(p))
  const router = createRouter({ history: createMemoryHistory(), routes })
  await router.push(path)
  const wrapper = mount(App, { attachTo: document.body, global: { plugins: [pinia, router] } })
  wrappers.push(wrapper)
  await flushPromises()
  return { wrapper, request, router }
}
it('summarizes only the loaded ticket page and updates after filtering without extra API calls', async () => {
  const { wrapper, request } = await render('/organizations/10/tickets', (path) => {
    if (path.startsWith('/organizations?'))
      return { items: [{ id: '10', name: '设计团队', my_role: 'admin' }] }
    const rows = [
      { id: '1', title: '甲', status: 'OPEN', priority: 3 },
      { id: '2', title: '乙', status: 'IN_PROGRESS', priority: 2 },
      { id: '3', title: '丙', status: 'RESOLVED', priority: 1 },
    ]
    return { items: path.includes('status=RESOLVED') ? [rows[2]] : rows }
  })
  const overview = wrapper.get('[aria-label="当前页工单概览"]')
  expect(overview.text()).toContain('仅统计当前页')
  expect(overview.get('[data-metric=loaded] strong').text()).toBe('3')
  expect(overview.get('[data-metric=open] strong').text()).toBe('1')
  expect(overview.get('[data-metric=working] strong').text()).toBe('1')
  expect(overview.get('[data-metric=urgent] strong').text()).toBe('1')
  expect(request).toHaveBeenCalledTimes(2)
  await wrapper.get('.filters select').setValue('RESOLVED')
  await flushPromises()
  expect(overview.get('[data-metric=loaded] strong').text()).toBe('1')
  expect(overview.get('[data-metric=open] strong').text()).toBe('0')
  expect(request).toHaveBeenCalledTimes(3)
  expect(wrapper.get('a.create-shortcut').attributes('href')).toBe('#ticket-create')
  expect(wrapper.get('#ticket-create h2').text()).toBe('创建工单')
})
it('offers real organization shortcuts with lossless IDs from the loaded organization page', async () => {
  const id = '9007199254740993'
  const { wrapper, router, request } = await render('/desk', (path) =>
    path.startsWith('/organizations?')
      ? { items: [{ id, name: '产品设计协作组', my_role: 'admin' }] }
      : { items: [] },
  )
  const link = wrapper.get('.organization-card')
  expect(link.text()).toContain('产品设计协作组')
  expect(link.attributes('href')).toBe(`/organizations/${id}/tickets`)
  expect(wrapper.text()).toContain('当前页组织')
  expect(request).toHaveBeenCalledTimes(1)
  await link.trigger('click')
  await flushPromises()
  expect(router.currentRoute.value.path).toBe(`/organizations/${id}/tickets`)
})

it('moves focus to ticket creation without changing route, filters, draft or request count', async () => {
  const { wrapper, request, router } = await render('/organizations/10/tickets', (path) =>
    path.startsWith('/organizations?')
      ? { items: [{ id: '10', name: '设计团队', my_role: 'admin' }] }
      : { items: [] },
  )
  await wrapper.get('.filters select').setValue('OPEN')
  await flushPromises()
  await wrapper.get('#ticket-create input').setValue('未提交的工单标题')
  await wrapper.get('#ticket-create textarea').setValue('保留已有描述')
  const before = request.mock.calls.length
  await wrapper.get('a.create-shortcut').trigger('click')
  await flushPromises()
  expect(document.activeElement).toBe(wrapper.get('#ticket-create').element)
  expect(router.currentRoute.value.fullPath).toBe('/organizations/10/tickets')
  expect(wrapper.get<HTMLInputElement>('#ticket-create input').element.value).toBe(
    '未提交的工单标题',
  )
  expect(wrapper.get<HTMLTextAreaElement>('#ticket-create textarea').element.value).toBe(
    '保留已有描述',
  )
  expect(wrapper.get<HTMLSelectElement>('.filters select').element.value).toBe('OPEN')
  expect(request.mock.calls).toHaveLength(before)
})
