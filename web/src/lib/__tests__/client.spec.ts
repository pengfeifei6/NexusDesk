import { describe, it, expect, vi } from 'vitest'
import { decode, encode, Client, Scope } from '../client'
describe('lossless protocol and request scope', () => {
  it('preserves int64 identifiers and versions', () => {
    const v = decode('{"id":9223372036854775807,"version":9007199254740993,"priority":2}')
    expect(v.id).toBe('9223372036854775807')
    expect(v.version).toBe('9007199254740993')
    expect(v.priority).toBe(2)
    expect(encode({ expected_version: v.version })).toBe('{"expected_version":9007199254740993}')
  })
  it('invalidates old work on scope change', () => {
    const s = new Scope()
    const a = s.begin()
    const b = s.begin()
    expect(a.current()).toBe(false)
    expect(a.signal.aborted).toBe(true)
    expect(b.current()).toBe(true)
  })
  it('does not expire a replacement session on late 401', async () => {
    let finish!: (r: Response) => void
    vi.stubGlobal(
      'fetch',
      vi.fn(() => new Promise<Response>((r) => (finish = r))),
    )
    const c = new Client()
    c.token = 'old'
    const p = c.request('/me').catch(() => {})
    c.token = 'new'
    finish(new Response('{}', { status: 401 }))
    await p
    expect(c.token).toBe('new')
    vi.unstubAllGlobals()
  })
  it('hides raw HTML failure', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response('<secret>', { status: 500 })),
    )
    await expect(new Client().request('/me')).rejects.toThrow('服务暂时不可用')
    vi.unstubAllGlobals()
  })
})
describe('safe transport failures', () => {
  it.each([
    [403, '没有执行此操作的权限'],
    [404, '资源不存在或无法访问'],
    [409, '数据已变化'],
    [503, '服务暂时不可用'],
  ])('maps HTTP %s without raw messages', async (status, message) => {
    vi.stubGlobal(
      'fetch',
      vi.fn(
        async () =>
          new Response('{"error":{"message":"private database error"}}', {
            status: Number(status),
          }),
      ),
    )
    await expect(new Client().request('/me')).rejects.toThrow(String(message))
    vi.unstubAllGlobals()
  })
  it('rejects non JSON success', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response('<html>private</html>')),
    )
    await expect(new Client().request('/me')).rejects.toThrow('响应格式异常')
    vi.unstubAllGlobals()
  })
  it('reports network errors safely', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => {
        throw new Error('private')
      }),
    )
    await expect(new Client().request('/me')).rejects.toThrow('网络连接失败')
    vi.unstubAllGlobals()
  })
  it('clears only current session on 401', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response('{}', { status: 401 })),
    )
    const c = new Client()
    c.token = 'current'
    c.onExpired = vi.fn()
    await c.request('/me').catch(() => {})
    expect(c.token).toBe('')
    expect(c.onExpired).toHaveBeenCalledOnce()
    vi.unstubAllGlobals()
  })
})
describe('session generations', () => {
  it('does not expire a new login even when issued token text is identical', async () => {
    let finish!: (r: Response) => void
    vi.stubGlobal(
      'fetch',
      vi.fn(() => new Promise<Response>((r) => (finish = r))),
    )
    const c = new Client()
    c.token = 'same-token'
    const p = c.request('/me').catch(() => {})
    c.token = 'same-token'
    finish(new Response('{}', { status: 401 }))
    await p
    expect(c.token).toBe('same-token')
    vi.unstubAllGlobals()
  })
})
describe('response integrity', () => {
  it('rejects malformed success envelopes', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response('{}')),
    )
    await expect(new Client().request('/me')).rejects.toThrow('响应格式异常')
    vi.unstubAllGlobals()
  })
  it('rejects unsafe integers outside known identifier fields', () => {
    expect(() => decode('{"page":9007199254740993}')).toThrow()
  })
  it('distinguishes timeout from cancellation', async () => {
    vi.useFakeTimers()
    vi.stubGlobal(
      'fetch',
      vi.fn(
        (_url, init) =>
          new Promise((_resolve, reject) =>
            init.signal.addEventListener('abort', () => reject(new Error('aborted'))),
          ),
      ),
    )
    const promise = new Client().request('/me')
    const result = promise.catch((error) => error)
    await vi.advanceTimersByTimeAsync(15000)
    expect(await result).toMatchObject({ message: '请求超时，请重试' })
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })
})
describe('response body transport failures', () => {
  it('classifies body stream network failures as network errors', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(
        async () =>
          new Response(
            new ReadableStream({
              start(controller) {
                controller.error(new Error('private stream detail'))
              },
            }),
          ),
      ),
    )
    await expect(new Client().request('/me')).rejects.toThrow('网络连接失败')
    vi.unstubAllGlobals()
  })
  it('classifies cancellation while waiting for response body', async () => {
    const caller = new AbortController()
    let bodyStarted!: () => void
    const started = new Promise<void>((r) => (bodyStarted = r))
    vi.stubGlobal(
      'fetch',
      vi.fn(async (_url, init) => {
        const response = new Response('')
        response.text = () =>
          new Promise((_resolve, reject) => {
            init.signal.addEventListener('abort', () => reject(new Error('private abort')))
            bodyStarted()
          })
        return response
      }),
    )
    const request = new Client().request('/me', { signal: caller.signal })
    const result = request.catch((error) => error)
    await started
    caller.abort()
    expect(await result).toMatchObject({ message: '请求已取消' })
    vi.unstubAllGlobals()
  })
  it('classifies timeout while waiting for response body', async () => {
    vi.useFakeTimers()
    let bodyStarted!: () => void
    const started = new Promise<void>((r) => (bodyStarted = r))
    vi.stubGlobal(
      'fetch',
      vi.fn(async (_url, init) => {
        const response = new Response('')
        response.text = () =>
          new Promise((_resolve, reject) => {
            init.signal.addEventListener('abort', () => reject(new Error('private timeout')))
            bodyStarted()
          })
        return response
      }),
    )
    const result = new Client().request('/me').catch((error) => error)
    await started
    await vi.advanceTimersByTimeAsync(15000)
    expect(await result).toMatchObject({ message: '请求超时，请重试' })
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })
})

describe('knowledge transport', () => {
  it('decodes uploader IDs losslessly while preserving safe measurements', () => {
    expect(
      decode('{"uploaded_by":9223372036854775807,"size_bytes":5242880,"extracted_chars":11}'),
    ).toEqual({ uploaded_by: '9223372036854775807', size_bytes: 5242880, extracted_chars: 11 })
  })
  it('sends multipart unchanged without a Content-Type so the browser supplies the boundary', async () => {
    const form = new FormData()
    form.append('file', new File(['hello'], 'hello.txt', { type: 'text/plain' }))
    let sent: RequestInit | undefined
    vi.stubGlobal('fetch', async (_url: string, init: RequestInit) => {
      sent = init
      return new Response('{"data":{"id":1}}', { status: 201 })
    })
    const client = new Client()
    client.token = 'fixture-token'
    await expect(client.request('/documents', { method: 'POST', body: form })).resolves.toEqual({
      id: '1',
    })
    expect(sent?.body).toBe(form)
    expect(new Headers(sent?.headers).has('Content-Type')).toBe(false)
    expect(new Headers(sent?.headers).get('Authorization')).toBe('Bearer fixture-token')
    expect(sent?.credentials).toBe('omit')
    vi.unstubAllGlobals()
  })
  it('returns authorized raw bytes for binary responses', async () => {
    let authorization: string | null = null
    vi.stubGlobal('fetch', async (_url: string, init: RequestInit) => {
      authorization = new Headers(init.headers).get('Authorization')
      return new Response(new Uint8Array([0, 1, 255]), {
        headers: { 'Content-Type': 'application/octet-stream' },
      })
    })
    const client = new Client()
    client.token = 'fixture-token'
    const result = await client.request<Blob>('/documents/1/download', { responseType: 'blob' })
    const bytes = await new Promise<ArrayBuffer>((resolve, reject) => {
      const reader = new FileReader()
      reader.onload = () => resolve(reader.result as ArrayBuffer)
      reader.onerror = reject
      reader.readAsArrayBuffer(result)
    })
    expect(Array.from(new Uint8Array(bytes))).toEqual([0, 1, 255])
    expect(authorization).toBe('Bearer fixture-token')
    vi.unstubAllGlobals()
  })
  it.each([
    [413, 'DOCUMENT_TOO_LARGE', '文档超过大小限制'],
    [415, 'UNSUPPORTED_DOCUMENT_TYPE', '文档类型不支持'],
    [422, 'PDF_UNSUPPORTED', 'PDF 暂不支持文本提取'],
    [422, 'EMPTY_TEXT', '文档没有可提取的正文'],
    [504, 'PROCESS_TIMEOUT', '文档处理超时'],
    [422, 'UNKNOWN_PRIVATE_ERROR', '服务暂时不可用'],
  ])('maps allowlisted binary error %s/%s safely', async (status, code, message) => {
    vi.stubGlobal(
      'fetch',
      async () =>
        new Response(JSON.stringify({ error: { code, message: '<private>' } }), { status }),
    )
    await expect(new Client().request('/download', { responseType: 'blob' })).rejects.toThrow(
      message,
    )
    vi.unstubAllGlobals()
  })
  it('expires the current session on binary 401 but ignores a stale binary 401', async () => {
    let finish!: (response: Response) => void
    vi.stubGlobal(
      'fetch',
      () =>
        new Promise<Response>((resolve) => {
          finish = resolve
        }),
    )
    const client = new Client()
    const expired = vi.fn()
    client.onExpired = expired
    client.token = 'old'
    const pending = client.request('/download', { responseType: 'blob' }).catch((e) => e)
    client.token = 'new'
    finish(new Response('{}', { status: 401 }))
    await pending
    expect(client.token).toBe('new')
    expect(expired).not.toHaveBeenCalled()
    const current = client.request('/download', { responseType: 'blob' }).catch((e) => e)
    finish(new Response('{}', { status: 401 }))
    expect(await current).toMatchObject({ status: 401 })
    expect(client.token).toBe('')
    expect(expired).toHaveBeenCalledOnce()
    vi.unstubAllGlobals()
  })
})

describe('binary body cancellation', () => {
  it.each(['cancel', 'timeout'])('classifies %s during a Blob body read', async (kind) => {
    vi.useFakeTimers()
    const caller = new AbortController()
    let started!: () => void
    const ready = new Promise<void>((resolve) => {
      started = resolve
    })
    vi.stubGlobal('fetch', async (_url: string, init: RequestInit) => {
      const response = new Response('')
      response.blob = () =>
        new Promise((_resolve, reject) => {
          init.signal!.addEventListener('abort', () => reject(new Error('private read failure')))
          started()
        })
      return response
    })
    const pending = new Client()
      .request('/download', { responseType: 'blob', signal: caller.signal })
      .catch((e) => e)
    await ready
    if (kind === 'cancel') caller.abort()
    else await vi.advanceTimersByTimeAsync(15000)
    expect(await pending).toMatchObject({
      status: 0,
      message: kind === 'cancel' ? '请求已取消' : '请求超时，请重试',
    })
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })
  it('keeps cancellation classification while reading an HTTP error body', async () => {
    const caller = new AbortController()
    let started!: () => void
    const ready = new Promise<void>((resolve) => {
      started = resolve
    })
    vi.stubGlobal('fetch', async (_url: string, init: RequestInit) => {
      const response = new Response('', { status: 422 })
      response.text = () =>
        new Promise((_resolve, reject) => {
          init.signal!.addEventListener('abort', () => reject(new Error('private read failure')))
          started()
        })
      return response
    })
    const pending = new Client()
      .request('/download', { responseType: 'blob', signal: caller.signal })
      .catch((e) => e)
    await ready
    caller.abort()
    expect(await pending).toMatchObject({ status: 0, message: '请求已取消' })
    vi.unstubAllGlobals()
  })
})

it.each(['constructor', '__proto__', 'toString'])(
  'rejects inherited error code %s from the message allowlist',
  async (code) => {
    vi.stubGlobal(
      'fetch',
      async () => new Response(JSON.stringify({ error: { code } }), { status: 422 }),
    )
    await expect(new Client().request('/download', { responseType: 'blob' })).rejects.toThrow(
      '服务暂时不可用',
    )
    vi.unstubAllGlobals()
  },
)
