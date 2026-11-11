import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import App from '../../App.vue'
import RAGSourceList from '../../components/RAGSourceList.vue'
import { routes } from '../../router'
import { useSession } from '../../stores/session'
import { api, ApiError, Client } from '../client'

const conversation = {
  id: 'a',
  org_id: '10',
  owner_user_id: '1',
  title: '排查',
  status: 'ACTIVE',
  created_at: 'now',
  updated_at: 'now',
}
const action = {
  id: 'act',
  conversation_id: 'a',
  assistant_message_id: 'msg',
  tool_name: 'propose_create_ticket',
  status: 'PROPOSED',
  org_id: '10',
  proposed_by_user_id: '1',
  arguments: { title: '<img src=x>待办', body: '<script>内容</script>', priority: 3 },
  expires_at: '2099-01-01T00:00:00Z',
  created_at: 'now',
  confirmed_by_user_id: null,
  ticket_id: null,
  confirmed_at: null,
}
const wrappers: ReturnType<typeof mount>[] = []
afterEach(() => {
  wrappers.splice(0).forEach((w) => w.unmount())
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  vi.useRealTimers()
})
function sse(events: string, chunks = 1) {
  const bytes = new TextEncoder().encode(events)
  return new Response(
    new ReadableStream({
      start(c) {
        for (let i = 0; i < bytes.length; i += chunks) c.enqueue(bytes.slice(i, i + chunks))
        c.close()
      },
    }),
    { headers: { 'Content-Type': 'text/event-stream' } },
  )
}
const event = (name: string, data: unknown) => `event: ${name}\ndata: ${JSON.stringify(data)}\n\n`
const end = event('message_end', {
  message_id: 'msg',
  status: 'COMPLETED',
  usage: { input_tokens: 1, output_tokens: 2 },
})
async function open() {
  const pinia = createPinia()
  setActivePinia(pinia)
  const session = useSession()
  session.user = { id: '1', display_name: 'fixture', email: 'a@test.test' }
  const router = createRouter({ history: createMemoryHistory(), routes })
  await router.push('/organizations/10/ai')
  const wrapper = mount(App, { global: { plugins: [pinia, router] } })
  wrappers.push(wrapper)
  await flushPromises()
  return { wrapper, router, session }
}
function setup(extra?: (path: string, options: Parameters<typeof api.request>[1]) => unknown) {
  return vi.spyOn(api, 'request').mockImplementation(async (path, options) => {
    if (path.startsWith('/organizations?'))
      return {
        items: [
          { id: '10', name: 'A', my_role: 'admin' },
          { id: '20', name: 'B', my_role: 'member' },
        ],
      }
    const value = extra?.(path, options)
    if (value !== undefined) return value
    if (path.endsWith('/conversations')) return [conversation]
    if (path.endsWith('/messages')) return { messages: [], actions: [action] }
    return { items: [] }
  })
}
function button(w: ReturnType<typeof mount>, label: string) {
  const b = w.findAll('button').find((b) => b.text() === label)
  expect(b).toBeDefined()
  return b!
}
async function select(w: ReturnType<typeof mount>) {
  await button(w, '排查').trigger('click')
  await flushPromises()
}

describe('AI rate preflight transport', () => {
  it.each([
    [429, 'RATE_LIMITED', '发送过于频繁', '9', 9],
    [503, 'RATE_LIMIT_UNAVAILABLE', '限流服务暂时不可用', '1', 1],
    [429, 'RATE_LIMITED', '发送过于频繁', 'malicious-value', undefined],
    [429, 'RATE_LIMITED', '发送过于频繁', '0', undefined],
    [429, 'RATE_LIMITED', '发送过于频繁', '999999999999999999999', undefined],
  ])('safely maps %s %s and Retry-After %s', async (status, code, message, retry, seconds) => {
    const fetch = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ error: { code, message: 'SECRET body' } }), {
        status: Number(status),
        headers: { 'Retry-After': String(retry) },
      }),
    )
    vi.stubGlobal('fetch', fetch)
    const error = await new Client()
      .stream('/ai', { body: {}, onEvent: () => {} })
      .catch((e: unknown) => e)
    expect(error).toMatchObject({ status, code, retryAfter: seconds })
    expect((error as Error).message).toContain(String(message))
    expect((error as Error).message).not.toContain('SECRET')
    expect((error as Error).message).not.toContain('malicious-value')
    const retryText = seconds ? `${seconds} 秒` : '稍后重试'
    expect((error as Error).message).toContain(retryText)
    expect(fetch).toHaveBeenCalledTimes(1)
  })
})
describe('AI stream transport', () => {
  it('decodes UTF8 split at every byte and lossless IDs, ignoring heartbeat', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValue(
          sse(
            ': ping\n\n' +
              event('content_delta', { message_id: 'msg', delta: '中文🧪' }) +
              'event: citation\ndata: {"message_id":"msg","source":{"chunk_id":9223372036854775807,"document_id":12,"chunk_index":0,"source_name":"manual","excerpt":"evidence","score":0.8,"start_rune":0,"end_rune":8}}\n\n' +
              end,
          ),
        ),
    )
    const c = new Client()
    const seen: unknown[] = []
    await c.stream('/ai', {
      body: { content: '问' },
      onEvent: (name: string, data: unknown) => seen.push({ name, data }),
    })
    expect(seen).toContainEqual({
      name: 'content_delta',
      data: { message_id: 'msg', delta: '中文🧪' },
    })
    expect(seen).toContainEqual({
      name: 'citation',
      data: expect.objectContaining({
        source: expect.objectContaining({ chunk_id: '9223372036854775807', document_id: '12' }),
      }),
    })
  })
  it.each([
    'event: content_delta\ndata: nope\n\n',
    event('content_delta', { message_id: 'msg', delta: 'partial' }),
    'event: unknown\ndata: {}\n\n',
    'event: message_end\ndata: {}\n\n',
  ])('rejects malformed or incomplete streams %s', async (raw) => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(sse(raw)))
    await expect(
      new Client().stream('/ai', { body: {}, onEvent: () => {} }),
    ).rejects.toBeInstanceOf(ApiError)
  })
  it('maps server error text to fixed Chinese', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValue(
          sse(event('error', { code: 'AI_BUSY', message: 'SECRET upstream', retryable: true })),
        ),
    )
    await expect(new Client().stream('/ai', { body: {}, onEvent: () => {} })).rejects.toThrow(
      '生成请求繁忙，请稍后重试',
    )
  })
  it('does not expire a later session on old stream HTTP401', async () => {
    let resolve!: (v: Response) => void
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(
        () =>
          new Promise((r) => {
            resolve = r
          }),
      ),
    )
    const c = new Client()
    c.token = 'old'
    const expired = vi.fn()
    c.onExpired = expired
    const pending = c.stream('/ai', { body: {}, onEvent: () => {} })
    c.token = 'new'
    resolve(new Response('{"error":{"code":"UNAUTHENTICATED"}}', { status: 401 }))
    await expect(pending).rejects.toBeInstanceOf(ApiError)
    expect(c.token).toBe('new')
    expect(expired).not.toHaveBeenCalled()
  })
})
describe('AI stream cancellation and bounds', () => {
  it('aborts a stalled body at the 75 second protection deadline', async () => {
    vi.useFakeTimers()
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValue(
          new Response(new ReadableStream(), { headers: { 'Content-Type': 'text/event-stream' } }),
        ),
    )
    const result = new Client().stream('/ai', { body: {}, onEvent: () => {} })
    const check = result.catch((error: unknown) => error)
    await vi.advanceTimersByTimeAsync(75000)
    expect(await check).toMatchObject({ message: '生成超时，请重新读取历史' })
  })
  it('stops the reader on user cancellation without emitting later data', async () => {
    const controller = new AbortController(),
      emit = vi.fn()
    const cancel = vi.fn()
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(new ReadableStream({ cancel }), {
          headers: { 'Content-Type': 'text/event-stream' },
        }),
      ),
    )
    const result = new Client().stream('/ai', {
      body: {},
      signal: controller.signal,
      onEvent: emit,
    })
    const check = result.catch((error: unknown) => error)
    await flushPromises()
    controller.abort()
    expect(await check).toMatchObject({ message: '请求已取消' })
    expect(emit).not.toHaveBeenCalled()
    expect(cancel).toHaveBeenCalledOnce()
  })
  it('bounds unterminated frame memory and rejects invalid UTF8', async () => {
    for (const response of [
      sse('data: ' + 'a'.repeat(262145), 300000),
      new Response(
        new ReadableStream({
          start(c) {
            c.enqueue(new Uint8Array([0xff]))
            c.close()
          },
        }),
        { headers: { 'Content-Type': 'text/event-stream' } },
      ),
    ]) {
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response))
      await expect(
        new Client().stream('/ai', { body: {}, onEvent: () => {} }),
      ).rejects.toBeInstanceOf(ApiError)
    }
  })
  it('expires only the current stream session on HTTP401', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValue(new Response('{"error":{"code":"UNAUTHENTICATED"}}', { status: 401 })),
    )
    const c = new Client()
    c.token = 'current'
    const expired = vi.fn()
    c.onExpired = expired
    await expect(c.stream('/ai', { body: {}, onEvent: () => {} })).rejects.toThrow('登录已失效')
    expect(c.token).toBe('')
    expect(expired).toHaveBeenCalledOnce()
  })
})
describe('AI quota draft recovery', () => {
  it.each([
    [429, 'RATE_LIMITED'],
    [503, 'RATE_LIMIT_UNAVAILABLE'],
  ])('retains the exact draft after preflight %s without resubmitting', async (status, code) => {
    setup()
    const stream = vi
      .spyOn(api, 'stream')
      .mockRejectedValue(new ApiError(Number(status), '限流：请在 9 秒后重试', String(code)))
    const { wrapper } = await open()
    await select(wrapper)
    await wrapper.get('textarea').setValue('  保留原始问题  ')
    await button(wrapper, '发送').trigger('click')
    await flushPromises()
    expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe('  保留原始问题  ')
    expect(wrapper.text()).toContain('9 秒')
    expect(stream).toHaveBeenCalledTimes(1)
  })
  it.each(['organization', 'session', 'conversation'])(
    'does not restore a stale draft after %s change',
    async (change) => {
      setup((path) =>
        path.endsWith('/conversations')
          ? [conversation, { ...conversation, id: 'b', title: '另一个' }]
          : undefined,
      )
      let reject!: (e: unknown) => void
      vi.spyOn(api, 'stream').mockImplementation(
        () =>
          new Promise((_, r) => {
            reject = r
          }),
      )
      const { wrapper, router, session } = await open()
      await select(wrapper)
      await wrapper.get('textarea').setValue('旧秘密问题')
      await button(wrapper, '发送').trigger('click')
      await flushPromises()
      if (change === 'session') session.clear()
      else if (change === 'organization') await router.push('/organizations/20/ai')
      else await button(wrapper, '另一个').trigger('click')
      reject(new ApiError(429, '发送过于频繁', 'RATE_LIMITED'))
      await flushPromises()
      expect(
        wrapper.find('textarea').exists()
          ? (wrapper.get('textarea').element as HTMLTextAreaElement).value
          : '',
      ).not.toContain('旧秘密问题')
    },
  )
  it('keeps streamed or generic failures from restoring a draft', async () => {
    setup()
    vi.spyOn(api, 'stream').mockImplementation(async (_path, options) => {
      options.onEvent('message_start', {
        message_id: 'm',
        user_message_id: 'u',
        conversation_id: 'a',
        client_request_id: (options.body as { client_request_id: string }).client_request_id,
      })
      throw new ApiError(0, '限流服务暂时不可用', 'RATE_LIMIT_UNAVAILABLE')
    })
    const { wrapper } = await open()
    await select(wrapper)
    await wrapper.get('textarea').setValue('已发送的消息')
    await button(wrapper, '发送').trigger('click')
    await flushPromises()
    expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe('')
  })
})
describe('AI conversation UI', () => {
  it('shows persisted escaped proposal, full preview and confirms without arguments', async () => {
    let sent: unknown
    setup((path, options) => {
      if (path.endsWith('/confirm')) {
        sent = options
        return { ...action, status: 'EXECUTED', ticket_id: '9223372036854775807' }
      }
    })
    const { wrapper } = await open()
    expect(wrapper.get('a[href="/organizations/10/ai"]').text()).toBe('AI 对话')
    await select(wrapper)
    expect(wrapper.text()).toContain('<script>内容</script>')
    expect(wrapper.text()).toContain('组织 10')
    expect(wrapper.text()).toContain('提出者 1')
    expect(wrapper.get('time').attributes('datetime')).toBe(action.expires_at)
    expect(wrapper.find('script').exists()).toBe(false)
    expect(wrapper.find('img').exists()).toBe(false)
    await button(wrapper, '确认创建工单').trigger('click')
    await flushPromises()
    expect(sent).toEqual(expect.objectContaining({ method: 'POST' }))
    expect((sent as { body?: unknown }).body).toBeUndefined()
    expect(wrapper.find('a[href="/organizations/10/tickets/9223372036854775807"]').exists()).toBe(
      true,
    )
  })
  it('rejects persisted proposals and refreshes history after conflicts', async () => {
    let rejected = false
    setup((path) => {
      if (path.endsWith('/reject')) {
        rejected = true
        return { ...action, status: 'REJECTED' }
      }
    })
    const { wrapper } = await open()
    await select(wrapper)
    await button(wrapper, '拒绝').trigger('click')
    await flushPromises()
    expect(rejected).toBe(true)
    expect(wrapper.text()).toContain('已拒绝')
  })
  it('recovers history after generation failure without automatic resubmission', async () => {
    let loads = 0
    setup((path) => {
      if (path.endsWith('/messages')) {
        loads++
        return { messages: [], actions: [action] }
      }
    })
    const stream = vi
      .spyOn(api, 'stream')
      .mockRejectedValue(new ApiError(0, '连接中断，请重新读取历史'))
    const { wrapper } = await open()
    await select(wrapper)
    await wrapper.get('textarea[aria-label="消息"]').setValue('问题')
    await button(wrapper, '发送').trigger('click')
    await flushPromises()
    expect(stream).toHaveBeenCalledTimes(1)
    expect(loads).toBeGreaterThan(1)
    expect(wrapper.text()).toContain('连接中断')
    expect(wrapper.text()).toContain('重新读取历史')
  })
  it.each(['organization', 'session', 'conversation'])(
    'cancels and ignores stale stream after %s change',
    async (change) => {
      setup((path) =>
        path.endsWith('/conversations')
          ? [conversation, { ...conversation, id: 'b', title: '另一个' }]
          : undefined,
      )
      let callback!: (name: string, data: unknown) => void
      let signal!: AbortSignal
      vi.spyOn(api, 'stream').mockImplementation((_path, options) => {
        callback = options.onEvent
        signal = options.signal!
        return new Promise(() => {})
      })
      const { wrapper, router, session } = await open()
      await select(wrapper)
      await wrapper.get('textarea').setValue('问题')
      await button(wrapper, '发送').trigger('click')
      await flushPromises()
      if (change === 'session') session.clear()
      else if (change === 'organization') await router.push('/organizations/20/ai')
      else await button(wrapper, '另一个').trigger('click')
      callback('message_start', {
        message_id: 'late',
        user_message_id: 'u',
        conversation_id: 'a',
        client_request_id: 'old',
      })
      callback('content_delta', { message_id: 'late', delta: '禁止显示的旧答案' })
      await flushPromises()
      expect(signal.aborted).toBe(true)
      expect(wrapper.text()).not.toContain('禁止显示的旧答案')
    },
  )
  it('clears protected history on forbidden proposal action', async () => {
    setup((path) =>
      path.endsWith('/confirm')
        ? Promise.reject(new ApiError(404, '资源不存在或无法访问'))
        : undefined,
    )
    const { wrapper } = await open()
    await select(wrapper)
    await button(wrapper, '确认创建工单').trigger('click')
    await flushPromises()
    expect(wrapper.text()).not.toContain('<script>内容</script>')
    expect(wrapper.text()).toContain('资源不存在')
  })

  it('creates trimmed titled conversation and rejects invalid Unicode without submitting', async () => {
    let created: unknown
    setup((path, options) => {
      if (path.endsWith('/conversations') && options?.method === 'POST') {
        created = options.body
        return { ...conversation, id: 'new', title: '新会话' }
      }
    })
    const { wrapper } = await open()
    await wrapper.get('input[aria-label="对话标题"]').setValue('  新会话  ')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(created).toEqual({ title: '新会话' })
    expect(wrapper.text()).toContain('新会话')
    created = undefined
    await wrapper.get('input').setValue('\ud800')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(created).toBeUndefined()
    expect(wrapper.text()).toContain('1～100')
  })
  it('renders live text, citations and proposals, then stop rereads persisted history', async () => {
    let loads = 0
    setup((path) => {
      if (path.endsWith('/messages')) {
        loads++
        return { messages: [], actions: loads > 1 ? [action] : [] }
      }
    })
    let emit!: (name: string, data: unknown) => void
    let request!: { content: string; client_request_id: string }
    vi.spyOn(api, 'stream').mockImplementation((_path, options) => {
      emit = options.onEvent
      request = options.body as typeof request
      return new Promise(() => {})
    })
    const { wrapper } = await open()
    await select(wrapper)
    await wrapper.get('textarea').setValue('故障')
    await button(wrapper, '发送').trigger('click')
    await flushPromises()
    expect(request.content).toBe('故障')
    expect(request.client_request_id).toMatch(/^[a-f0-9-]{36}$/)
    emit('message_start', {
      message_id: 'msg',
      user_message_id: 'user',
      conversation_id: 'a',
      client_request_id: request.client_request_id,
    })
    emit('content_delta', { message_id: 'msg', delta: '<img src=x>实时文本' })
    emit('citation', {
      message_id: 'msg',
      source: {
        document_id: '9223372036854775807',
        chunk_id: '88',
        chunk_index: 0,
        source_name: '手册',
        score: 0.8,
        excerpt: '<script>证据</script>',
        start_rune: 0,
        end_rune: 4,
      },
    })
    emit('tool_approval_required', { message_id: 'msg', action })
    await flushPromises()
    expect(wrapper.text()).toContain('实时文本')
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.find('script').exists()).toBe(false)
    expect(wrapper.find('a[href="/organizations/10/knowledge/9223372036854775807"]').exists()).toBe(
      true,
    )
    await button(wrapper, '停止生成').trigger('click')
    await flushPromises()
    expect(loads).toBe(2)
    expect(wrapper.text()).toContain('已停止生成')
    expect(wrapper.text()).toContain('确认创建工单')
  })
  it.each([
    ['AI_CONFLICT', 409, '状态已变化'],
    ['AI_EXPIRED', 410, '已过期'],
  ])('reconciles %s action errors using fixed text', async (code, status, expected) => {
    let loads = 0
    setup((path) => {
      if (path.endsWith('/messages')) {
        loads++
        return {
          messages: [],
          actions: [{ ...action, status: loads > 1 ? 'EXPIRED' : 'PROPOSED' }],
        }
      }
      if (path.endsWith('/confirm'))
        return Promise.reject(new ApiError(Number(status), 'SECRET', String(code)))
    })
    const { wrapper } = await open()
    await select(wrapper)
    await button(wrapper, '确认创建工单').trigger('click')
    await flushPromises()
    expect(loads).toBe(2)
    expect(wrapper.text()).toContain(expected)
    expect(wrapper.text()).not.toContain('SECRET')
    expect(wrapper.text()).not.toContain('确认创建工单')
  })
  it('reports bounded history and conversation windows', async () => {
    setup()
    const { wrapper } = await open()
    await select(wrapper)
    expect(wrapper.text()).toContain('最近 50')
    expect(wrapper.text()).toContain('最近 100')
    expect(wrapper.text()).toContain('没有分页')
  })
  it('rejects malformed history sources without rendering protected messages', async () => {
    setup((path) =>
      path.endsWith('/messages')
        ? {
            messages: [
              {
                id: 'bad',
                conversation_id: 'a',
                org_id: '10',
                role: 'ASSISTANT',
                content: 'malformed protected',
                status: 'COMPLETED',
                citations: [{ score: 'bad' }],
              },
            ],
            actions: [],
          }
        : undefined,
    )
    const { wrapper } = await open()
    await select(wrapper)
    expect(wrapper.text()).not.toContain('malformed protected')
    expect(wrapper.text()).toContain('历史响应格式异常')
  })
})

describe('AI permission changes during stream', () => {
  it('clears protected proposals when an in-stream permission error arrives', async () => {
    setup()
    vi.spyOn(api, 'stream').mockRejectedValue(new ApiError(0, '资源不存在或无法访问', 'NOT_FOUND'))
    const { wrapper } = await open()
    await select(wrapper)
    await wrapper.get('textarea').setValue('问题')
    await button(wrapper, '发送').trigger('click')
    await flushPromises()
    expect(wrapper.text()).not.toContain('<script>内容</script>')
    expect(wrapper.text()).toContain('资源不存在或无法访问')
  })
})

describe('AI review regressions', () => {
  it('accepts the same valid stream when network frames are split or coalesced', async () => {
    const raw =
      event('content_delta', { message_id: 'message-identifier', delta: 'x'.repeat(20) }).repeat(
        3000,
      ) + end
    expect(new TextEncoder().encode(raw).length).toBeGreaterThan(262144)
    for (const chunks of [1024, new TextEncoder().encode(raw).length]) {
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue(sse(raw, chunks)))
      const received: string[] = []
      await new Client().stream('/ai', {
        body: {},
        onEvent: (name, data) => {
          if (name === 'content_delta') received.push((data as { delta: string }).delta)
        },
      })
      expect(received.join('')).toBe('x'.repeat(60000))
    }
  })

  it('keeps confirmed action when an earlier generation-completion history response arrives late', async () => {
    let loads = 0
    let resolveHistory!: (value: unknown) => void
    let resolveConfirm!: (value: unknown) => void
    let finishStream!: () => void
    setup((path) => {
      if (path.endsWith('/messages')) {
        loads++
        if (loads === 1) return { messages: [], actions: [action] }
        return new Promise((resolve) => {
          resolveHistory = resolve
        })
      }
      if (path.endsWith('/confirm'))
        return new Promise((resolve) => {
          resolveConfirm = resolve
        })
    })
    vi.spyOn(api, 'stream').mockImplementation(
      () =>
        new Promise((resolve) => {
          finishStream = resolve
        }),
    )
    const { wrapper } = await open()
    await select(wrapper)
    await wrapper.get('textarea').setValue('问题')
    await button(wrapper, '发送').trigger('click')
    await flushPromises()
    await button(wrapper, '确认创建工单').trigger('click')
    finishStream()
    await flushPromises()
    expect(loads).toBe(2)
    resolveConfirm({ ...action, status: 'EXECUTED', ticket_id: '9223372036854775807' })
    await flushPromises()
    expect(wrapper.find('a[href="/organizations/10/tickets/9223372036854775807"]').exists()).toBe(
      true,
    )
    resolveHistory({ messages: [], actions: [action] })
    await flushPromises()
    expect(wrapper.find('a[href="/organizations/10/tickets/9223372036854775807"]').exists()).toBe(
      true,
    )
    expect(wrapper.text()).toContain('已执行')
    expect(wrapper.text()).not.toContain('确认创建工单')
    expect(button(wrapper, '重新读取历史').attributes('disabled')).toBeUndefined()
  })

  it('preserves AI workspace when switching organizations through the organization picker', async () => {
    setup((path) =>
      path.startsWith('/organizations/20/') && path.endsWith('/conversations') ? [] : undefined,
    )
    const { wrapper, router } = await open()
    await wrapper.get('select').setValue('20')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/organizations/20/ai')
    expect(wrapper.text()).toContain('AI 对话')
    expect(wrapper.text()).not.toContain('排查')
  })
})

describe('AI live citation ownership', () => {
  it('attaches a live citation only to the assistant while the stream remains open', async () => {
    setup()
    let emit!: (name: string, data: unknown) => void
    let requestID = ''
    vi.spyOn(api, 'stream').mockImplementation((_path, options) => {
      emit = options.onEvent
      requestID = (options.body as { client_request_id: string }).client_request_id
      return new Promise(() => {})
    })
    const { wrapper } = await open()
    await select(wrapper)
    await wrapper.get('textarea').setValue('查询资料')
    await button(wrapper, '发送').trigger('click')
    await flushPromises()
    emit('message_start', {
      message_id: 'assistant',
      user_message_id: 'user',
      conversation_id: 'a',
      client_request_id: requestID,
    })
    const source = {
      document_id: '12',
      chunk_id: '88',
      chunk_index: 0,
      source_name: '手册',
      score: 0.8,
      excerpt: '证据',
      start_rune: 0,
      end_rune: 2,
    }
    emit('citation', { message_id: 'assistant', source })
    await flushPromises()
    expect(wrapper.text()).toContain('正在生成')
    const sources = wrapper.findAllComponents(RAGSourceList)
    expect(sources).toHaveLength(2)
    expect(sources[0]!.props('items')).toEqual([])
    expect(sources[1]!.props('items')).toEqual([source])
  })
})
