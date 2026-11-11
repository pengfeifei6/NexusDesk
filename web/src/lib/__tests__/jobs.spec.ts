import { afterEach, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { useKnowledge, type Knowledge } from '../../composables/knowledge'
import KnowledgeDetailView from '../../views/KnowledgeDetailView.vue'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import App from '../../App.vue'
import { routes } from '../../router'
import { useSession } from '../../stores/session'
import { api, ApiError, decode } from '../client'

const doc = {
  id: '12',
  org_id: '10',
  uploaded_by: '1',
  original_filename: 'async.md',
  media_type: 'text/markdown',
  size_bytes: 20,
  status: 'UPLOADED',
  extracted_chars: 0,
}
const job = {
  id: '30',
  org_id: '10',
  document_id: '12',
  requested_by: '1',
  actor_user_id: '1',
  status: 'PENDING',
  phase: 'WAITING',
  run_version: '1',
  attempts: 0,
  max_attempts: 3,
  total_attempts: 0,
  failure_code: null,
  retryable: false,
  last_dispatch_error: null,
}
const wrappers: ReturnType<typeof mount>[] = []
afterEach(() => {
  wrappers.splice(0).forEach((w) => w.unmount())
  vi.useRealTimers()
  vi.restoreAllMocks()
})
async function open(observe?: (knowledge: Knowledge) => void) {
  const pinia = createPinia()
  setActivePinia(pinia)
  const session = useSession()
  session.user = { id: '1', display_name: 'fixture', email: 'fixture@example.test' }
  const fixtureRoutes = routes.map((route) =>
    observe && route.component === KnowledgeDetailView
      ? {
          ...route,
          component: defineComponent({
            setup() {
              observe(useKnowledge())
              return () => h(KnowledgeDetailView)
            },
          }),
        }
      : route,
  )
  const router = createRouter({ history: createMemoryHistory(), routes: fixtureRoutes })
  await router.push('/organizations/10/knowledge/12')
  const w = mount(App, { global: { plugins: [pinia, router] } })
  wrappers.push(w)
  await flushPromises()
  return { w, router, session }
}
function setup(extra?: (path: string, options: Parameters<typeof api.request>[1]) => unknown) {
  return vi.spyOn(api, 'request').mockImplementation(async (path, options) => {
    if (path.startsWith('/organizations?'))
      return {
        items: [
          { id: '10', name: 'A', my_role: 'member' },
          { id: '20', name: 'B', my_role: 'member' },
        ],
      }
    const result = extra?.(path, options)
    if (result !== undefined) return result
    if (path.endsWith('/jobs')) return { items: [job] }
    if (path.endsWith('/index')) return { status: 'ABSENT', chunk_count: 0 }
    if (path.includes('/documents?')) return { items: [] }
    return doc
  })
}
function button(w: ReturnType<typeof mount>, name: string) {
  const b = w.findAll('button').find((b) => b.text() === name)
  expect(b).toBeDefined()
  return b!
}

it('preserves requester and run version as lossless integers', () => {
  expect(decode('{"requested_by":9223372036854775807,"run_version":9223372036854775806}')).toEqual({
    requested_by: '9223372036854775807',
    run_version: '9223372036854775806',
  })
})
it('shows submitted pending status without claiming completion and permits creator cancellation', async () => {
  setup()
  const { w } = await open()
  expect(w.text()).toContain('等待执行')
  expect(w.text()).toContain('0 / 3')
  expect(w.text()).not.toContain('处理完成')
  expect(button(w, '取消任务').exists()).toBe(true)
})
it('polls without overlapping and aborts old results when organization changes', async () => {
  vi.useFakeTimers()
  let resolve!: (v: unknown) => void
  let signal: AbortSignal | undefined
  let polls = 0
  setup((path, options) => {
    if (path.endsWith('/jobs')) {
      polls++
      if (polls > 1) {
        signal = options?.signal
        return new Promise((r) => {
          resolve = r
        })
      }
    }
  })
  const { w, router } = await open()
  await vi.advanceTimersByTimeAsync(2000)
  await vi.advanceTimersByTimeAsync(8000)
  expect(polls).toBe(2)
  await router.push('/organizations/20/knowledge')
  expect(signal?.aborted).toBe(true)
  resolve({ items: [{ ...job, status: 'FAILED', failure_code: 'PRIVATE_OLD_ERROR' }] })
  await flushPromises()
  expect(w.text()).not.toContain('PRIVATE_OLD_ERROR')
})
it('removes protected document content on forbidden polling response', async () => {
  vi.useFakeTimers()
  let n = 0
  setup((path) => {
    if (path.endsWith('/jobs') && ++n > 1) throw new ApiError(404, '资源不可见')
  })
  const { w } = await open()
  expect(w.text()).toContain('async.md')
  await vi.advanceTimersByTimeAsync(2000)
  await flushPromises()
  expect(w.text()).not.toContain('async.md')
  expect(w.text()).toContain('资源不可见')
})
it('retries only failed or canceled jobs owned by current member and sends no body', async () => {
  let sent: unknown
  setup((path, options) => {
    if (path.endsWith('/jobs'))
      return { items: [{ ...job, status: 'FAILED', failure_code: 'MODEL_UNAVAILABLE' }] }
    if (path.endsWith('/retry')) {
      sent = options
      return { ...job, run_version: '2' }
    }
  })
  const { w } = await open()
  await button(w, '重试任务').trigger('click')
  await flushPromises()
  expect(sent).toEqual(expect.objectContaining({ method: 'POST' }))
  expect((sent as { body?: unknown }).body).toBeUndefined()
  expect(w.text()).toContain('等待执行')
})
it('does not show mutation controls to another ordinary member', async () => {
  setup((path) =>
    path.endsWith('/jobs')
      ? { items: [{ ...job, requested_by: '2', status: 'FAILED' }] }
      : undefined,
  )
  const { w } = await open()
  expect(w.findAll('button').some((b) => b.text() === '重试任务')).toBe(false)
})
it('stops automatic polling after five minutes and leaves manual refresh available', async () => {
  vi.useFakeTimers()
  const spy = setup()
  const { w } = await open()
  await vi.advanceTimersByTimeAsync(302000)
  const count = spy.mock.calls.length
  await vi.advanceTimersByTimeAsync(10000)
  expect(spy.mock.calls.length).toBe(count)
  expect(w.text()).toContain('自动刷新已暂停')
  expect(button(w, '刷新任务').exists()).toBe(true)
})
it('pauses hidden-tab polling and resumes safely when visible', async () => {
  vi.useFakeTimers()
  const visibility = vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
  const spy = setup()
  await open()
  visibility.mockReturnValue(true)
  document.dispatchEvent(new Event('visibilitychange'))
  const count = spy.mock.calls.length
  await vi.advanceTimersByTimeAsync(10000)
  expect(spy.mock.calls.length).toBe(count)
  visibility.mockReturnValue(false)
  document.dispatchEvent(new Event('visibilitychange'))
  await flushPromises()
  expect(spy.mock.calls.length).toBe(count + 1)
})
it('refreshes document and index after completion without a reload loop', async () => {
  vi.useFakeTimers()
  let n = 0
  const spy = setup((path) =>
    path.endsWith('/jobs')
      ? { items: [++n === 1 ? job : { ...job, status: 'SUCCEEDED', phase: 'DONE' }] }
      : undefined,
  )
  await open()
  await vi.advanceTimersByTimeAsync(2000)
  await flushPromises()
  const count = spy.mock.calls.length
  await vi.advanceTimersByTimeAsync(10000)
  expect(spy.mock.calls.length).toBe(count)
  expect(spy.mock.calls.filter(([p]) => p.endsWith('/documents/12')).length).toBe(2)
  expect(spy.mock.calls.filter(([p]) => p.endsWith('/index')).length).toBe(2)
})
it('keeps uploaded document after async submission fails, without uploading it again', async () => {
  let uploads = 0
  setup((path, options) => {
    if (path.endsWith('/documents') && options?.method === 'POST') {
      uploads++
      return doc
    }
    if (path.endsWith('/jobs') && options?.method === 'POST')
      throw new ApiError(503, '异步处理暂未启用')
    if (path.endsWith('/jobs')) return { items: [] }
  })
  const { w, router } = await open()
  await router.push('/organizations/10/knowledge')
  await flushPromises()
  const input = w.get('input[type=file]')
  Object.defineProperty(input.element, 'files', {
    value: [new File(['fixture'], 'async.md', { type: 'text/markdown' })],
    configurable: true,
  })
  await input.trigger('change')
  await button(w, '上传并异步处理').trigger('click')
  await flushPromises()
  expect(uploads).toBe(1)
  expect(router.currentRoute.value.path).toBe('/organizations/10/knowledge/12')
  expect(w.text()).toContain('无需重复上传')
  expect(button(w, '提交异步处理').exists()).toBe(true)
})

it.each(['UPLOADED', 'PROCESSING'])(
  'refreshes stale %s document and INDEXING data once when the first job response has succeeded',
  async (status) => {
    vi.useFakeTimers()
    let documentReads = 0
    let indexReads = 0
    const spy = setup((path) => {
      if (path.endsWith('/documents/12'))
        return { ...doc, status: ++documentReads === 1 ? status : 'READY' }
      if (path.endsWith('/index'))
        return { status: ++indexReads === 1 ? 'INDEXING' : 'READY', chunk_count: 2 }
      if (path.endsWith('/jobs')) return { items: [{ ...job, status: 'SUCCEEDED', phase: 'DONE' }] }
      if (path.endsWith('/text')) return { content_text: 'fresh completed body' }
    })
    const { w } = await open()
    await flushPromises()
    expect(documentReads).toBe(2)
    expect(indexReads).toBe(2)
    expect(w.text()).toContain('fresh completed body')
    expect(w.text()).toContain('已索引')
    const count = spy.mock.calls.length
    await vi.advanceTimersByTimeAsync(10000)
    await flushPromises()
    expect(spy.mock.calls.length).toBe(count)
    expect(spy.mock.calls.filter(([path]) => path.endsWith('/jobs'))).toHaveLength(2)
  },
)
it('refreshes success after a hidden initial job read was canceled', async () => {
  vi.useFakeTimers()
  const visibility = vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
  let resolve!: (value: unknown) => void
  let signal: AbortSignal | undefined
  let reads = 0
  const spy = setup((path, options) => {
    if (path.endsWith('/jobs')) {
      if (++reads === 1) {
        signal = options?.signal
        return new Promise((r) => {
          resolve = r
        })
      }
      return { items: [{ ...job, status: 'SUCCEEDED', phase: 'DONE' }] }
    }
  })
  await open()
  visibility.mockReturnValue(true)
  document.dispatchEvent(new Event('visibilitychange'))
  expect(signal?.aborted).toBe(true)
  visibility.mockReturnValue(false)
  document.dispatchEvent(new Event('visibilitychange'))
  await flushPromises()
  resolve({ items: [job] })
  await flushPromises()
  expect(spy.mock.calls.filter(([path]) => path.endsWith('/documents/12'))).toHaveLength(2)
  expect(spy.mock.calls.filter(([path]) => path.endsWith('/index'))).toHaveLength(2)
  const count = spy.mock.calls.length
  await vi.advanceTimersByTimeAsync(10000)
  expect(spy.mock.calls.length).toBe(count)
})
it('refreshes each lossless successful run once while preserving its identity through manual detail reloads', async () => {
  let run = '9223372036854775806'
  const spy = setup((path) =>
    path.endsWith('/jobs')
      ? { items: [{ ...job, status: 'SUCCEEDED', phase: 'DONE', run_version: run }] }
      : undefined,
  )
  const { w } = await open()
  await flushPromises()
  expect(spy.mock.calls.filter(([path]) => path.endsWith('/documents/12'))).toHaveLength(2)
  await button(w, '刷新文档').trigger('click')
  await flushPromises()
  expect(spy.mock.calls.filter(([path]) => path.endsWith('/documents/12'))).toHaveLength(3)
  run = '9223372036854775807'
  await button(w, '刷新任务').trigger('click')
  await flushPromises()
  expect(spy.mock.calls.filter(([path]) => path.endsWith('/documents/12'))).toHaveLength(4)
  await button(w, '刷新任务').trigger('click')
  await flushPromises()
  expect(spy.mock.calls.filter(([path]) => path.endsWith('/documents/12'))).toHaveLength(4)
})
it.each([
  ['jobs', 403],
  ['jobs', 404],
  ['index', 403],
  ['index', 404],
] as const)(
  'invalidates deferred READY text on %s %s denial and permits a fresh explicit retry',
  async (endpoint, status) => {
    let knowledge!: Knowledge
    let resolve!: (value: unknown) => void
    let staleSignal: AbortSignal | undefined
    let freshSignal: AbortSignal | undefined
    let textReads = 0
    let denied = true
    setup((path, options) => {
      if (path.endsWith('/documents/12')) return { ...doc, status: 'READY' }
      if (path.endsWith('/text')) {
        if (++textReads === 1) {
          staleSignal = options?.signal
          return new Promise((r) => {
            resolve = r
          })
        }
        freshSignal = options?.signal
        return { content_text: 'fresh authorized body' }
      }
      if (denied && path.endsWith(`/${endpoint}`)) throw new ApiError(status, '资源不可见')
    })
    const { w } = await open((owner) => {
      knowledge = owner
    })
    expect(textReads).toBe(1)
    expect(staleSignal).toBeDefined()
    expect(staleSignal?.aborted).toBe(true)
    expect(knowledge.detail.value).toBeNull()
    expect(knowledge.preview.value).toBeNull()
    resolve({ content_text: 'revoked protected body' })
    await flushPromises()
    expect(knowledge.detail.value).toBeNull()
    expect(knowledge.preview.value).toBeNull()
    expect(w.text()).not.toContain('revoked protected body')
    denied = false
    await button(w, '重试文档').trigger('click')
    await flushPromises()
    expect(freshSignal).toBeDefined()
    expect(freshSignal).not.toBe(staleSignal)
    expect(freshSignal?.aborted).toBe(false)
    expect(knowledge.detail.value?.status).toBe('READY')
    expect(knowledge.preview.value?.content_text).toBe('fresh authorized body')
    expect(w.text()).toContain('fresh authorized body')
  },
)
