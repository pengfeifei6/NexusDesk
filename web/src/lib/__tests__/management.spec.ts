import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import App from '../../App.vue'
import { routes } from '../../router'
import { useSession } from '../../stores/session'
import { api, ApiError, encode } from '../client'
const date = '2026-11-11T10:00:00Z'
const ticket = {
  id: '5',
  org_id: '10',
  creator_id: '1',
  assignee_id: null,
  title: '待修改工单',
  body: '原始描述',
  priority: 2,
  status: 'OPEN',
  version: '9007199254740993',
  created_at: date,
  updated_at: date,
}
const member = {
  user_id: '9007199254740993',
  display_name: '同事甲',
  role: 'member',
  status: 'active',
  created_at: date,
}
type Options = Parameters<typeof api.request>[1]
type Handler = (path: string, options: Options) => Promise<unknown>
const wrappers: ReturnType<typeof mount>[] = []
afterEach(() => {
  wrappers.splice(0).forEach((w) => w.unmount())
  vi.restoreAllMocks()
})
async function setup(path: string, role = 'admin', override?: Handler) {
  const pinia = createPinia()
  setActivePinia(pinia)
  useSession().user = { id: '1', display_name: '管理员', email: 'admin@example.test' }
  const request = vi.spyOn(api, 'request').mockImplementation(async (url, options) => {
    if (url.startsWith('/organizations?'))
      return {
        items: [
          { id: '10', name: '甲组', my_role: role },
          { id: '20', name: '乙组', my_role: role },
        ],
      }
    if (override) {
      const result = await override(url, options)
      if (result !== undefined) return result
    }
    if (url === '/organizations/10/tickets/5') return { ...ticket }
    if (url.includes('/members?')) return { items: [member] }
    return { items: [] }
  })
  const router = createRouter({ history: createMemoryHistory(), routes })
  await router.push(path)
  const wrapper = mount(App, { global: { plugins: [pinia, router] } })
  wrappers.push(wrapper)
  await flushPromises()
  return { wrapper, router, request }
}
function button(w: ReturnType<typeof mount>, text: string) {
  const b = w.findAll('button').find((x) => x.text() === text)
  expect(b).toBeDefined()
  return b!
}
describe('member management', () => {
  it('adds a registered account with a lossless ID and does not load tickets', async () => {
    const { wrapper, request } = await setup('/organizations/10/members')
    expect(wrapper.text()).toContain('同事甲')
    expect(request.mock.calls.some(([p]) => p.includes('/tickets'))).toBe(false)
    await wrapper.get('[name=member_user_id]').setValue('9007199254740993')
    await wrapper.get('[name=member_role]').setValue('member')
    await wrapper.get('form.member-form').trigger('submit')
    await flushPromises()
    const call = request.mock.calls.find(([, o]) => o?.method === 'POST')!
    expect(call[0]).toBe('/organizations/10/members')
    expect(encode(call[1]?.body)).toBe('{"user_id":9007199254740993,"role":"member"}')
  })
  it('keeps ordinary members read-only', async () => {
    const { wrapper } = await setup('/organizations/10/members', 'member')
    expect(wrapper.text()).toContain('同事甲')
    expect(wrapper.find('.member-form').exists()).toBe(false)
  })
  it('rejects noncanonical and overflowing member IDs before a write', async () => {
    const { wrapper, request } = await setup('/organizations/10/members')
    for (const id of ['01', '-1', '0', '1e3', '9223372036854775808']) {
      await wrapper.get('[name=member_user_id]').setValue(id)
      await wrapper.get('.member-form').trigger('submit')
      await flushPromises()
    }
    expect(request.mock.calls.filter(([, o]) => o?.method === 'POST')).toHaveLength(0)
  })
  it('ignores late old-org responses and clears current data on permission loss', async () => {
    let late!: (v: unknown) => void
    let denied = false
    const { wrapper, router } = await setup('/organizations/10/members', 'admin', async (p) => {
      if (p.includes('/10/members?'))
        return new Promise((resolve) => {
          late = resolve
        })
      if (p.includes('/20/members?')) {
        if (denied) throw new ApiError(404, '资源不存在或无法访问')
        return { items: [{ ...member, display_name: '当前同事' }] }
      }
    })
    await router.push('/organizations/20/members')
    await flushPromises()
    late({ items: [{ ...member, display_name: '旧组织同事' }] })
    await flushPromises()
    expect(wrapper.text()).toContain('当前同事')
    expect(wrapper.text()).not.toContain('旧组织同事')
    denied = true
    await button(wrapper, '刷新成员').trigger('click')
    await flushPromises()
    expect(wrapper.text()).not.toContain('当前同事')
    expect(wrapper.find('.member-form').exists()).toBe(false)
  })
  it('distinguishes missing target and duplicate membership without retrying writes', async () => {
    let code = 'USER_NOT_FOUND'
    const { wrapper, request } = await setup(
      '/organizations/10/members',
      'admin',
      async (_p, o) => {
        if (o?.method === 'POST')
          throw new ApiError(code === 'USER_NOT_FOUND' ? 404 : 409, '安全提示', code)
      },
    )
    await wrapper.get('[name=member_user_id]').setValue('9')
    await wrapper.get('.member-form').trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain('同事甲')
    expect(wrapper.find('.member-form').exists()).toBe(true)
    code = 'MEMBER_ALREADY_EXISTS'
    await wrapper.get('.member-form').trigger('submit')
    await flushPromises()
    expect(request.mock.calls.filter(([, o]) => o?.method === 'POST')).toHaveLength(2)
  })
})
describe('ticket management', () => {
  it.each([403, 404])(
    'does not restore protected details from a late read after management returns %s',
    async (status) => {
      let rejectWrite!: (error: unknown) => void
      let finishRead!: (value: unknown) => void
      let delayRead = false
      const { wrapper, request } = await setup(
        '/organizations/10/tickets/5',
        'admin',
        async (path, options) => {
          if (options?.method === 'PATCH')
            return new Promise((_resolve, reject) => {
              rejectWrite = reject
            })
          if (delayRead && path === '/organizations/10/tickets/5')
            return new Promise((resolve) => {
              finishRead = resolve
            })
        },
      )
      await button(wrapper, '编辑工单').trigger('click')
      await wrapper.get('[name=edit_title]').setValue('等待权限确认的修改')
      await wrapper.get('.ticket-edit-form').trigger('submit')
      delayRead = true
      await button(wrapper, '刷新详情').trigger('click')
      rejectWrite(new ApiError(status, '资源不存在或无法访问'))
      await flushPromises()
      expect(wrapper.text()).not.toContain(ticket.title)
      const callsAfterDenial = request.mock.calls.length
      finishRead({ ...ticket, title: '不能重新显示的旧保护详情' })
      await flushPromises()
      expect(wrapper.text()).not.toContain('不能重新显示的旧保护详情')
      expect(wrapper.find('.ticket-management').exists()).toBe(false)
      expect(request.mock.calls).toHaveLength(callsAfterDenial)
    },
  )

  it('edits with the captured lossless version and leaves assignment untouched', async () => {
    const { wrapper, request } = await setup('/organizations/10/tickets/5')
    await button(wrapper, '编辑工单').trigger('click')
    await wrapper.get('[name=edit_title]').setValue('修改后的工单')
    await wrapper.get('.ticket-edit-form').trigger('submit')
    await flushPromises()
    const call = request.mock.calls.find(([, o]) => o?.method === 'PATCH')!
    expect(call[1]?.body).toEqual({
      expected_version: ticket.version,
      title: '修改后的工单',
      body: ticket.body,
      priority: 2,
    })
  })
  it('pages assignees, excludes disabled and assigns a lossless ID', async () => {
    const { wrapper, request } = await setup('/organizations/10/tickets/5', 'admin', async (p) => {
      if (p.includes('/members?page=1'))
        return {
          items: Array.from({ length: 20 }, (_, i) => ({
            ...member,
            user_id: String(i + 10),
            status: 'disabled',
          })),
        }
      if (p.includes('/members?page=2')) return { items: [member] }
    })
    await button(wrapper, '指派受理人').trigger('click')
    await flushPromises()
    expect(wrapper.get('[name=assignee_id]').findAll('option')).toHaveLength(2)
    await button(wrapper, '下一页候选成员').trigger('click')
    await flushPromises()
    await wrapper.get('[name=assignee_id]').setValue(member.user_id)
    await wrapper.get('.ticket-assign-form').trigger('submit')
    await flushPromises()
    expect(request.mock.calls.find(([, o]) => o?.method === 'PATCH')![1]?.body).toEqual({
      expected_version: ticket.version,
      assignee_id: member.user_id,
    })
  })
  it('requires explicit delete confirmation, cancel sends nothing', async () => {
    const { wrapper, request, router } = await setup('/organizations/10/tickets/5')
    await button(wrapper, '删除工单').trigger('click')
    expect(wrapper.text()).toContain('确认删除此工单')
    await button(wrapper, '取消删除').trigger('click')
    expect(request.mock.calls.some(([, o]) => o?.method === 'DELETE')).toBe(false)
    await button(wrapper, '删除工单').trigger('click')
    await button(wrapper, '确认删除').trigger('click')
    await flushPromises()
    const call = request.mock.calls.find(([, o]) => o?.method === 'DELETE')!
    expect(call[1]?.body).toBeUndefined()
    expect(router.currentRoute.value.path).toBe('/organizations/10/tickets')
  })
  it('refreshes on version conflict without automatically replaying the edit', async () => {
    const { wrapper, request } = await setup(
      '/organizations/10/tickets/5',
      'admin',
      async (_p, o) => {
        if (o?.method === 'PATCH') throw new ApiError(409, '数据已变化')
      },
    )
    await button(wrapper, '编辑工单').trigger('click')
    await wrapper.get('[name=edit_title]').setValue('待提交')
    await wrapper.get('.ticket-edit-form').trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain('请重新确认')
    expect(wrapper.find('.ticket-edit-form').exists()).toBe(false)
    expect(request.mock.calls.filter(([, o]) => o?.method === 'PATCH')).toHaveLength(1)
  })
  it('reports a failed refresh after conflict without claiming current content', async () => {
    let conflicted = false
    const { wrapper } = await setup('/organizations/10/tickets/5', 'admin', async (p, o) => {
      if (o?.method === 'PATCH') {
        conflicted = true
        throw new ApiError(409, '数据已变化')
      }
      if (conflicted && p === '/organizations/10/tickets/5')
        throw new ApiError(500, '服务暂时不可用')
    })
    await button(wrapper, '编辑工单').trigger('click')
    await wrapper.get('[name=edit_title]').setValue('待提交')
    await wrapper.get('.ticket-edit-form').trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain('刷新失败')
    expect(wrapper.text()).not.toContain('已重新读取')
  })
  it('does not let a late delete response navigate a new organization', async () => {
    let finish!: (v: unknown) => void
    const { wrapper, router } = await setup(
      '/organizations/10/tickets/5',
      'admin',
      async (_p, o) => {
        if (o?.method === 'DELETE')
          return new Promise((resolve) => {
            finish = resolve
          })
      },
    )
    await button(wrapper, '删除工单').trigger('click')
    await button(wrapper, '确认删除').trigger('click')
    await router.push('/organizations/20/tickets')
    await flushPromises()
    finish(null)
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/organizations/20/tickets')
    expect(wrapper.text()).not.toContain('确认删除此工单')
  })
  it('never offers admin actions to a member and clears details after revoked access', async () => {
    let denied = false
    const { wrapper } = await setup('/organizations/10/tickets/5', 'member', async (p) => {
      if (denied && p === '/organizations/10/tickets/5')
        throw new ApiError(404, '资源不存在或无法访问')
    })
    expect(wrapper.text()).not.toContain('删除工单')
    expect(wrapper.text()).not.toContain('指派受理人')
    denied = true
    await button(wrapper, '刷新详情').trigger('click')
    await flushPromises()
    expect(wrapper.text()).not.toContain('待修改工单')
    expect(wrapper.find('.ticket-edit-form').exists()).toBe(false)
  })
})
