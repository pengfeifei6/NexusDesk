import { aiMessages, consumeSSE, validAIEvent } from './ai-stream'
import { parse, stringify, LosslessNumber } from 'lossless-json'
const intKeys =
  /^(id|.*_id|uploaded_by|requested_by|run_version|index_version|version|ticket_version|expected_version)$/
export function decode<T = Record<string, unknown>>(text: string): T {
  return parse(text, (key, value) => {
    if (!(value instanceof LosslessNumber)) return value
    if (intKeys.test(key)) return value.value
    const number = Number(value.value)
    if (['score', 'fusion_score', 'embedding_ms', 'search_ms'].includes(key)) {
      if (
        !Number.isFinite(number) ||
        (key === 'score' ? Math.abs(number) > 1 : number < 0 || number > Number.MAX_SAFE_INTEGER)
      )
        throw new Error('invalid metric response')
      return number
    }
    if (!Number.isSafeInteger(number)) throw new Error('unsafe numeric response')
    return number
  }) as T
}
export function encode(value: unknown): string {
  return stringify(value, (key, v) =>
    intKeys.test(key) && typeof v === 'string' && /^\d+$/.test(v) ? new LosslessNumber(v) : v,
  )!
}
export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
    public code?: string,
    public retryAfter?: number,
  ) {
    super(message)
  }
}
export class Scope {
  private controller = new AbortController()
  begin() {
    this.controller.abort()
    const controller = (this.controller = new AbortController())
    return {
      signal: controller.signal,
      current: () => this.controller === controller && !controller.signal.aborted,
    }
  }
  cancel() {
    this.controller.abort()
  }
}
export class Client {
  private authToken = ''
  private generation = 0
  get token() {
    return this.authToken
  }
  set token(value: string) {
    this.authToken = value
    this.generation++
  }
  onExpired = () => {}
  async stream(
    path: string,
    options: {
      body: unknown
      signal?: AbortSignal
      onEvent: (name: string, data: unknown) => void
    },
  ): Promise<void> {
    const token = this.token
    const generation = this.generation
    const controller = new AbortController()
    let timedOut = false
    let ended = false
    const abort = () => controller.abort()
    options.signal?.addEventListener('abort', abort, { once: true })
    if (options.signal?.aborted) abort()
    const timer = setTimeout(() => {
      timedOut = true
      controller.abort()
    }, 75000)
    try {
      const response = await fetch('/api/v1' + path, {
        method: 'POST',
        headers: {
          ...(token ? { Authorization: `Bearer ${token}` } : {}),
          'Content-Type': 'application/json',
          Accept: 'text/event-stream',
        },
        body: encode(options.body),
        signal: controller.signal,
        credentials: 'omit',
        cache: 'no-store',
      })
      if (!response.ok) {
        if (response.status === 401 && token && this.generation === generation) {
          this.token = ''
          this.onExpired()
        }
        let code: string | undefined
        try {
          const value = decode<{ error?: { code?: unknown } }>(await response.text())
          if (typeof value?.error?.code === 'string') code = value.error.code
        } catch {
          /* Untrusted errors must not reach the UI. */
        }
        const rateRejected =
          (response.status === 429 && code === 'RATE_LIMITED') ||
          (response.status === 503 && code === 'RATE_LIMIT_UNAVAILABLE')
        const rawRetry = response.headers.get('Retry-After') || ''
        const retryAfter =
          rateRejected && /^[1-9][0-9]{0,3}$/.test(rawRetry) && Number(rawRetry) <= 3600
            ? Number(rawRetry)
            : undefined
        const message =
          code && Object.prototype.hasOwnProperty.call(aiMessages, code)
            ? aiMessages[code]!
            : '生成请求失败，请重新读取历史'
        throw new ApiError(
          response.status,
          retryAfter ? `${message}；请在 ${retryAfter} 秒后重试` : message,
          code,
          retryAfter,
        )
      }
      if (
        !response.body ||
        !/^text\/event-stream(?:;|$)/i.test(response.headers.get('content-type') || '')
      )
        throw new ApiError(0, '响应格式异常，请重新读取历史')
      await consumeSSE(
        response.body,
        (name, raw) => {
          if (controller.signal.aborted || generation !== this.generation)
            throw new ApiError(0, '请求已取消')
          let data: unknown
          try {
            data = decode(raw)
          } catch {
            throw new ApiError(0, '响应格式异常，请重新读取历史')
          }
          if (ended || !validAIEvent(name, data))
            throw new ApiError(0, '响应格式异常，请重新读取历史')
          const value = data as Record<string, unknown>
          if (name === 'error') {
            const code = String(value.code)
            throw new ApiError(
              0,
              Object.prototype.hasOwnProperty.call(aiMessages, code)
                ? aiMessages[code]!
                : '生成失败，请重新读取历史',
              code,
            )
          }
          if (name === 'message_end') ended = true
          options.onEvent(name, data)
        },
        controller.signal,
      )
      if (controller.signal.aborted)
        throw new ApiError(0, timedOut ? '生成超时，请重新读取历史' : '请求已取消')
      if (!ended) throw new ApiError(0, '连接中断，请重新读取历史')
    } catch (error) {
      if (controller.signal.aborted)
        throw new ApiError(0, timedOut ? '生成超时，请重新读取历史' : '请求已取消')
      if (error instanceof ApiError) throw error
      throw new ApiError(0, '连接中断或响应格式异常，请重新读取历史')
    } finally {
      clearTimeout(timer)
      options.signal?.removeEventListener('abort', abort)
    }
  }
  async request<T = unknown>(
    path: string,
    options: {
      method?: string
      body?: unknown
      signal?: AbortSignal
      responseType?: 'blob'
      timeoutMs?: number
    } = {},
  ): Promise<T> {
    const timeoutMs = options.timeoutMs ?? 15000
    if (!Number.isInteger(timeoutMs) || timeoutMs < 1 || timeoutMs > 70000)
      throw new ApiError(0, '请求超时设置无效')
    const multipart = options.body instanceof FormData
    const token = this.token
    const generation = this.generation
    const controller = new AbortController()
    let timedOut = false
    const abort = () => controller.abort()
    options.signal?.addEventListener('abort', abort, { once: true })
    if (options.signal?.aborted) abort()
    const timer = setTimeout(() => {
      timedOut = true
      controller.abort()
    }, timeoutMs)
    try {
      const response = await fetch('/api/v1' + path, {
        method: options.method || 'GET',
        headers: {
          ...(token ? { Authorization: `Bearer ${token}` } : {}),
          ...(options.body && !multipart ? { 'Content-Type': 'application/json' } : {}),
        },
        body: multipart
          ? (options.body as FormData)
          : options.body
            ? encode(options.body)
            : undefined,
        signal: controller.signal,
        credentials: 'omit',
        cache: 'no-store',
      })
      if (!response.ok) {
        if (response.status === 401 && token && this.generation === generation) {
          this.token = ''
          this.onExpired()
        }
        let code: string | undefined
        try {
          const envelope = decode<{ error?: { code?: unknown } }>(await response.text())
          if (typeof envelope?.error?.code === 'string') code = envelope.error.code
        } catch (error) {
          if (controller.signal.aborted) throw error
          // Never display arbitrary response text, including proxy HTML.
        }
        const messages: Record<number, Record<string, string>> = {
          400: { INVALID_ASSIGNEE: '受理人必须是本组织正常成员，请刷新候选成员' },
          404: { USER_NOT_FOUND: '该账号不存在，请确认同事已注册并核对账号编号' },
          409: {
            MEMBER_ALREADY_EXISTS: '该账号已是组织成员，请查看成员列表',
            EMAIL_ALREADY_EXISTS: '该邮箱已注册，请登录',
            INDEX_STATE_CONFLICT: '索引状态已变化，请刷新后重试',
            INDEX_SOURCE_CHANGED: '文档依据已变化，请重新建立索引',
            DOCUMENT_STATE_CONFLICT: '文档状态已变化或有异步任务正在处理，请刷新后重试',
            JOB_STATE_CONFLICT: '任务状态已变化，请刷新后重试',
          },
          413: { DOCUMENT_TOO_LARGE: '文档超过大小限制（默认 5 MiB）' },
          415: { UNSUPPORTED_DOCUMENT_TYPE: '文档类型不支持，请上传 TXT、MD 或 PDF' },
          422: {
            INDEX_CONTENT_INVALID: '正文不符合索引要求',
            INDEX_TOO_MANY_CHUNKS: '正文超过 200 块索引上限，请拆分文档',
            INVALID_TEXT: '文档正文不是有效文本',
            EMPTY_TEXT: '文档没有可提取的正文',
            TEXT_TOO_LARGE: '提取正文超过大小限制',
            PDF_UNSUPPORTED: 'PDF 暂不支持文本提取，可下载原文件',
          },
          502: {
            MODEL_INVALID_RESPONSE: '模型返回格式异常，请重试',
            MODEL_UPSTREAM_ERROR: '模型服务返回错误，请重试',
            EMBEDDING_COUNT_MISMATCH: '向量数量不匹配',
            EMBEDDING_DIMENSION_MISMATCH: '向量维度不匹配',
            EMBEDDING_INVALID_VECTOR: '模型返回无效向量',
          },
          503: {
            ASYNC_DISABLED: '异步处理暂未启用，已上传文档仍然保留',
            MODEL_UNAVAILABLE: '模型暂时不可用，请检查本地模型服务',
            MODEL_RATE_LIMITED: '模型服务繁忙，请稍后重试',
            RAG_BUSY: '模型服务繁忙，请稍后重试',
          },
          504: {
            PROCESS_TIMEOUT: '文档处理超时，请重试',
            MODEL_TIMEOUT: '模型响应超时，请重试',
            INDEX_TIMEOUT: '索引超时，请重试',
          },
        }
        const businessMessages = messages[response.status]
        const businessMessage =
          code && businessMessages && Object.prototype.hasOwnProperty.call(businessMessages, code)
            ? businessMessages[code]
            : undefined
        throw new ApiError(
          response.status,
          (code && code.startsWith('AI_') && Object.prototype.hasOwnProperty.call(aiMessages, code)
            ? aiMessages[code]
            : undefined) ||
            businessMessage ||
            (
              {
                400: '输入不符合要求',
                401: '登录失效或邮箱密码错误，请重新登录',
                403: '没有执行此操作的权限',
                404: '资源不存在或无法访问',
                409: '数据已变化，请查看刷新后的内容',
              } as Record<number, string>
            )[response.status] ||
            '服务暂时不可用，请稍后重试',
          code,
        )
      }
      if (response.status === 204) return null as T
      if (options.responseType === 'blob') return (await response.blob()) as T
      const responseText = await response.text()
      try {
        const envelope = decode<{ data: T }>(responseText)
        if (
          !envelope ||
          typeof envelope !== 'object' ||
          !Object.prototype.hasOwnProperty.call(envelope, 'data')
        )
          throw new Error('invalid envelope')
        return envelope.data
      } catch {
        throw new ApiError(0, '响应格式异常，请稍后重试')
      }
    } catch (error) {
      if (error instanceof ApiError) throw error
      if (controller.signal.aborted)
        throw new ApiError(0, timedOut ? '请求超时，请重试' : '请求已取消')
      throw new ApiError(0, '网络连接失败，请检查连接后重试')
    } finally {
      clearTimeout(timer)
      options.signal?.removeEventListener('abort', abort)
    }
  }
}
export const api = new Client()
