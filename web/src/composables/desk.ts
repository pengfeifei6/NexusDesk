import { ref, watch, computed, onBeforeUnmount, inject, nextTick } from 'vue'
import type { InjectionKey } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, ApiError, Scope } from '../lib/client'
import { useSession } from '../stores/session'
import type { Organization, Ticket, Comment, TicketEvent, User } from '../lib/types'
export function createDesk() {
  const route = useRoute(),
    router = useRouter(),
    session = useSession()
  const email = ref(''),
    password = ref(''),
    error = ref(''),
    notice = ref(''),
    busy = ref(false)
  const orgs = ref<Organization[]>([]),
    orgPage = ref(1),
    tickets = ref<Ticket[]>([]),
    page = ref(1),
    status = ref(''),
    priority = ref('')
  const detail = ref<Ticket | null>(null),
    comments = ref<Comment[]>([]),
    events = ref<TicketEvent[]>([]),
    commentPage = ref(1),
    eventPage = ref(1)
  const title = ref(''),
    body = ref(''),
    newPriority = ref(2),
    content = ref('')
  const displayName = ref(''),
    organizationName = ref('')
  const selectedOrg = ref<Organization | null>(null)
  const scope = new Scope()
  let active = scope.begin()
  let listSequence = 0,
    commentSequence = 0,
    eventSequence = 0,
    orgSequence = 0
  onBeforeUnmount(() => scope.cancel())
  const orgID = computed(() => String(route.params.orgID || '')),
    ticketID = computed(() => String(route.params.ticketID || ''))
  const base = computed(() => `/organizations/${orgID.value}/tickets`)
  const role = ref<Organization['my_role']>()
  const accessEpoch = ref(0)
  const canWork = computed(
    () => role.value === 'admin' || detail.value?.assignee_id === session.user?.id,
  )
  let retryOperation: () => Promise<unknown> = loadOrgs
  // All consumers must revoke the shared request scope as well as visible refs.
  function invalidateAccess() {
    active = scope.begin()
    listSequence++
    commentSequence++
    eventSequence++
    orgSequence++
    accessEpoch.value++
    detail.value = null
    tickets.value = []
    comments.value = []
    events.value = []
    role.value = undefined
    selectedOrg.value = null
    title.value = ''
    body.value = ''
    content.value = ''
    busy.value = false
    notice.value = ''
    retryOperation = loadOrgs
  }
  function fail(e: unknown, operation: () => Promise<unknown> = load) {
    if (e instanceof ApiError && [403, 404].includes(e.status)) {
      invalidateAccess()
    } else retryOperation = operation
    error.value = e instanceof Error ? e.message : '请求失败'
  }
  async function register() {
    if (busy.value) return
    const nameLength = Array.from(displayName.value.trim()).length
    const passwordLength = Array.from(password.value).length
    if (nameLength < 1 || nameLength > 80) {
      fail(new Error('显示名称需 1～80 个字符'), register)
      return
    }
    if (passwordLength < 15 || passwordLength > 128 || !password.value.trim()) {
      fail(new Error('密码需 15～128 个字符'), register)
      return
    }
    const run = active
    busy.value = true
    error.value = ''
    try {
      await api.request<User>('/auth/register', {
        method: 'POST',
        body: { email: email.value, display_name: displayName.value, password: password.value },
        signal: run.signal,
      })
      if (!run.current()) return
      password.value = ''
      displayName.value = ''
      await router.replace('/login?registered=1')
    } catch (e) {
      if (run.current()) fail(e, register)
    } finally {
      if (run.current()) busy.value = false
    }
  }
  async function createOrganization() {
    if (busy.value) return
    const nameLength = Array.from(organizationName.value.trim()).length
    if (nameLength < 1 || nameLength > 100) {
      fail(new Error('组织名称需 1～100 个字符'), createOrganization)
      return
    }
    const run = active
    busy.value = true
    error.value = ''
    try {
      const organization = await api.request<Organization>('/organizations', {
        method: 'POST',
        body: { name: organizationName.value },
        signal: run.signal,
      })
      if (!run.current()) return
      organizationName.value = ''
      selectedOrg.value = organization
      await router.push(`/organizations/${organization.id}/tickets`)
    } catch (e) {
      if (run.current()) fail(e, createOrganization)
    } finally {
      if (run.current()) busy.value = false
    }
  }
  async function login() {
    if (session.user) {
      await router.replace('/desk')
      return
    }
    const credentials = { email: email.value, password: password.value }
    session.clear()
    await nextTick()
    const run = scope.begin()
    active = run
    busy.value = true
    error.value = ''
    try {
      const data = await api.request<{ access_token: string }>('/auth/login', {
        method: 'POST',
        body: credentials,
        signal: run.signal,
      })
      if (!run.current()) return
      api.token = data.access_token
      const user = await api.request<User>('/me', { signal: run.signal })
      if (!run.current()) return
      session.user = user
      session.expired = false
      password.value = ''
      await router.push('/desk')
    } catch (e) {
      if (run.current()) {
        api.token = ''
        fail(e)
      }
    } finally {
      if (run.current()) busy.value = false
    }
  }
  function logout() {
    scope.cancel()
    session.clear()
    password.value = ''
    router.push('/login')
  }
  async function loadOrgs() {
    const run = active
    const seq = ++orgSequence
    error.value = ''
    busy.value = true
    try {
      const data = await api.request<{ items: Organization[] }>(
        `/organizations?page=${orgPage.value}&page_size=20`,
        { signal: run.signal },
      )
      if (!run.current() || seq !== orgSequence) return
      orgs.value = data.items
      if (orgID.value) {
        const selected = orgID.value
        let items = data.items,
          scanPage = orgPage.value
        let found = items.find((o) => o.id === selected)
        if (!found && scanPage !== 1) {
          scanPage = 1
          items = (
            await api.request<{ items: Organization[] }>(`/organizations?page=1&page_size=20`, {
              signal: run.signal,
            })
          ).items
          found = items.find((o) => o.id === selected)
        }
        while (!found && items.length === 20) {
          scanPage++
          items = (
            await api.request<{ items: Organization[] }>(
              `/organizations?page=${scanPage}&page_size=20`,
              { signal: run.signal },
            )
          ).items
          if (!run.current() || seq !== orgSequence) return
          found = items.find((o) => o.id === selected)
        }
        if (run.current() && seq === orgSequence) {
          role.value = found?.my_role
          selectedOrg.value = found || null
        }
      }
      return true
    } catch (e) {
      if (run.current() && seq === orgSequence) fail(e, loadOrgs)
      return false
    } finally {
      if (run.current() && seq === orgSequence) busy.value = false
    }
  }
  async function load() {
    const run = active
    const seq = ++listSequence
    error.value = ''
    busy.value = true
    try {
      if (!route.path.includes('/tickets')) return
      if (ticketID.value) {
        const d = await api.request<Ticket>(`${base.value}/${ticketID.value}`, {
          signal: run.signal,
        })
        if (!run.current() || seq !== listSequence) return
        detail.value = d
        await Promise.all([loadComments(), loadEvents()])
      } else if (orgID.value && !route.path.includes('/knowledge')) {
        const query = new URLSearchParams({ page: String(page.value), page_size: '20' })
        if (status.value) query.set('status', status.value)
        if (priority.value) query.set('priority', priority.value)
        const d = await api.request<{ items: Ticket[] }>(`${base.value}?${query}`, {
          signal: run.signal,
        })
        if (run.current() && seq === listSequence) tickets.value = d.items
      }
    } catch (e) {
      if (run.current() && seq === listSequence) fail(e)
    } finally {
      if (run.current() && seq === listSequence) busy.value = false
    }
  }
  async function loadComments() {
    const run = active
    const seq = ++commentSequence
    try {
      const d = await api.request<{ items: Comment[] }>(
        `${base.value}/${ticketID.value}/comments?page=${commentPage.value}&page_size=20`,
        { signal: run.signal },
      )
      if (run.current() && seq === commentSequence) comments.value = d.items
    } catch (e) {
      if (run.current() && seq === commentSequence) fail(e, loadComments)
    }
  }
  async function loadEvents() {
    const run = active
    const seq = ++eventSequence
    try {
      const d = await api.request<{ items: TicketEvent[] }>(
        `${base.value}/${ticketID.value}/events?page=${eventPage.value}&page_size=20`,
        { signal: run.signal },
      )
      if (run.current() && seq === eventSequence) events.value = d.items
    } catch (e) {
      if (run.current() && seq === eventSequence) fail(e, loadEvents)
    }
  }
  async function create() {
    const run = active
    busy.value = true
    error.value = ''
    try {
      const d = await api.request<Ticket>(base.value, {
        method: 'POST',
        body: { title: title.value, body: body.value, priority: newPriority.value },
        signal: run.signal,
      })
      if (!run.current()) return
      title.value = ''
      body.value = ''
      router.push(`${base.value}/${d.id}`)
    } catch (e) {
      if (run.current()) fail(e)
    } finally {
      if (run.current()) busy.value = false
    }
  }
  async function action(to?: string) {
    if (!detail.value) return
    const run = active
    busy.value = true
    error.value = ''
    notice.value = ''
    try {
      await api.request(`${base.value}/${ticketID.value}/${to ? 'transition' : 'claim'}`, {
        method: 'POST',
        body: { expected_version: detail.value.version, ...(to ? { to_status: to } : {}) },
        signal: run.signal,
      })
      if (run.current()) await load()
    } catch (e) {
      if (!run.current()) return
      if (e instanceof ApiError && e.status === 409) {
        await load()
        if (run.current())
          notice.value = error.value
            ? '数据已变化；刷新失败，请手动重试。'
            : '数据已变化，已刷新工单，请重新确认。'
      } else fail(e)
    } finally {
      if (run.current()) busy.value = false
    }
  }
  async function comment() {
    const run = active
    busy.value = true
    error.value = ''
    try {
      await api.request(`${base.value}/${ticketID.value}/comments`, {
        method: 'POST',
        body: { content: content.value },
        signal: run.signal,
      })
      if (run.current()) {
        content.value = ''
        await Promise.all([loadComments(), loadEvents()])
      }
    } catch (e) {
      if (run.current()) fail(e)
    } finally {
      if (run.current()) busy.value = false
    }
  }
  watch(
    () => session.epoch,
    () => {
      orgs.value = []
      selectedOrg.value = null
      orgPage.value = 1
      role.value = undefined
    },
    { flush: 'sync' },
  )
  watch(
    () => [route.fullPath, session.epoch],
    async () => {
      const run = scope.begin()
      active = run
      role.value = undefined
      tickets.value = []
      detail.value = null
      comments.value = []
      events.value = []
      title.value = ''
      body.value = ''
      content.value = ''
      error.value = ''
      notice.value =
        route.path === '/login' && route.query.registered === '1' ? '注册成功，请登录' : ''
      password.value = ''
      organizationName.value = ''
      page.value = 1
      commentPage.value = 1
      eventPage.value = 1
      busy.value = false
      if (!session.user) {
        if (!['/login', '/register'].includes(route.path)) router.replace('/login')
        return
      }
      if (['/login', '/register'].includes(route.path)) {
        router.replace('/desk')
        return
      }
      const loaded = await loadOrgs()
      if (run.current() && loaded) await load()
    },
    { immediate: true, flush: 'sync' },
  )
  function selectOrg(event: Event) {
    const id = (event.target as HTMLSelectElement).value
    if (id)
      router.push(
        `/organizations/${id}/${route.path.endsWith('/members') ? 'members' : route.path.endsWith('/ai') ? 'ai' : route.path.endsWith('/knowledge/search') ? 'knowledge/search' : route.path.includes('/knowledge') ? 'knowledge' : 'tickets'}`,
      )
  }
  function changePage(kind: 'org' | 'ticket' | 'comment' | 'event', delta: number) {
    const target = { org: orgPage, ticket: page, comment: commentPage, event: eventPage }[kind]
    target.value += delta
    const loader = { org: loadOrgs, ticket: load, comment: loadComments, event: loadEvents }[kind]
    void loader()
  }
  async function retry() {
    const operation = retryOperation
    const okay = await operation()
    if (operation === loadOrgs && okay && active.current()) await load()
  }
  function filter() {
    page.value = 1
    load()
  }

  return {
    session,
    accessEpoch,
    invalidateAccess,
    displayName,
    organizationName,
    selectedOrg,
    register,
    createOrganization,
    email,
    password,
    error,
    notice,
    busy,
    orgs,
    orgPage,
    tickets,
    page,
    status,
    priority,
    detail,
    comments,
    events,
    commentPage,
    eventPage,
    title,
    body,
    newPriority,
    content,
    orgID,
    ticketID,
    base,
    role,
    canWork,
    login,
    logout,
    loadOrgs,
    load,
    loadComments,
    loadEvents,
    create,
    action,
    comment,
    selectOrg,
    filter,
    changePage,
    retry,
  }
}
export type Desk = ReturnType<typeof createDesk>
export const deskKey: InjectionKey<Desk> = Symbol('desk')
export function useDesk() {
  const desk = inject(deskKey)
  if (!desk) throw new Error('desk context missing')
  return desk
}
