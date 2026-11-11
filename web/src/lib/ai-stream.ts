// A bounded SSE decoder: network chunks may split UTF-8, lines, or events.
export async function consumeSSE(
  body: ReadableStream<Uint8Array>,
  emit: (name: string, data: string) => void,
  signal: AbortSignal,
) {
  const reader = body.getReader()
  const decoder = new TextDecoder('utf-8', { fatal: true })
  let pending = ''
  let name = ''
  let data: string[] = []
  let eventSize = 0
  const abort = () => {
    void reader.cancel().catch(() => {})
  }
  signal.addEventListener('abort', abort, { once: true })
  const line = (text: string) => {
    if (!text) {
      if (data.length) emit(name || 'message', data.join('\n'))
      name = ''
      data = []
      eventSize = 0
      return
    }
    eventSize += text.length
    if (eventSize > 262144) throw new Error('oversize event')
    if (text.startsWith(':')) return
    const colon = text.indexOf(':')
    const field = colon < 0 ? text : text.slice(0, colon)
    let value = colon < 0 ? '' : text.slice(colon + 1)
    if (value.startsWith(' ')) value = value.slice(1)
    if (field === 'event') name = value
    if (field === 'data') data.push(value)
  }
  try {
    while (true) {
      if (signal.aborted) throw new Error('aborted')
      const next = await reader.read()
      pending += next.done ? decoder.decode() : decoder.decode(next.value, { stream: true })
      let position: number
      while ((position = pending.indexOf('\n')) >= 0) {
        line(pending.slice(0, position).replace(/\r$/, ''))
        pending = pending.slice(position + 1)
      }
      if (pending.length + eventSize > 262144) throw new Error('oversize buffer')
      if (next.done) {
        if (pending || data.length) throw new Error('incomplete event')
        return
      }
    }
  } finally {
    signal.removeEventListener('abort', abort)
    await reader.cancel().catch(() => {})
    reader.releaseLock()
  }
}

export const aiMessages: Record<string, string> = {
  INVALID_REQUEST: '输入不符合要求',
  UNAUTHENTICATED: '登录已失效，请重新登录',
  FORBIDDEN: '没有执行此操作的权限',
  NOT_FOUND: '资源不存在或无法访问',
  AI_CONFLICT: '对话或提议状态已变化，请重新读取历史',
  AI_EXPIRED: '提议已过期，请重新读取历史',
  AI_LIMIT: '对话已达到上下文或消息限制，请新建对话',
  AI_BUSY: '生成请求繁忙，请稍后重试',
  RATE_LIMITED: '发送过于频繁，请稍后重试',
  RATE_LIMIT_UNAVAILABLE: '限流服务暂时不可用，请稍后重试',
  MODEL_INVALID_RESPONSE: '模型返回格式异常，请重新读取历史',
  MODEL_UNAVAILABLE: '模型暂时不可用，请稍后重试',
  MODEL_TIMEOUT: '模型响应超时，请重新读取历史',
  INTERNAL_ERROR: '服务暂时不可用，请重新读取历史',
}

export function validAIEvent(name: string, data: unknown): boolean {
  if (!data || typeof data !== 'object') return false
  const v = data as Record<string, unknown>
  const string = (key: string) => typeof v[key] === 'string' && !!v[key]
  if (name === 'error') return string('code') && typeof v.retryable === 'boolean'
  if (!string('message_id')) return false
  switch (name) {
    case 'message_start':
      return ['user_message_id', 'conversation_id', 'client_request_id'].every(string)
    case 'content_delta':
      return typeof v.delta === 'string'
    case 'citation':
      return validSource(v.source)
    case 'tool_approval_required':
      return validAction(v.action)
    case 'message_end': {
      const usage = v.usage as Record<string, unknown> | undefined
      return (
        v.status === 'COMPLETED' &&
        !!usage &&
        ['input_tokens', 'output_tokens'].every(
          (k) => Number.isSafeInteger(usage[k]) && Number(usage[k]) >= 0,
        )
      )
    }
    default:
      return false
  }
}
export function validSource(value: unknown): boolean {
  if (!value || typeof value !== 'object') return false
  const v = value as Record<string, unknown>
  return (
    ['document_id', 'chunk_id', 'source_name', 'excerpt'].every((k) => typeof v[k] === 'string') &&
    typeof v.score === 'number' &&
    Number.isFinite(v.score) &&
    Math.abs(v.score) <= 1 &&
    ['chunk_index', 'start_rune', 'end_rune'].every(
      (k) => Number.isSafeInteger(v[k]) && Number(v[k]) >= 0,
    )
  )
}
export function validAction(value: unknown): boolean {
  if (!value || typeof value !== 'object') return false
  const v = value as Record<string, unknown>
  const args = v.arguments as Record<string, unknown> | undefined
  return (
    [
      'id',
      'conversation_id',
      'assistant_message_id',
      'org_id',
      'proposed_by_user_id',
      'expires_at',
    ].every((k) => typeof v[k] === 'string' && !!v[k]) &&
    v.tool_name === 'propose_create_ticket' &&
    ['PROPOSED', 'EXECUTED', 'REJECTED', 'EXPIRED'].includes(String(v.status)) &&
    !!args &&
    typeof args.title === 'string' &&
    typeof args.body === 'string' &&
    [1, 2, 3].includes(Number(args.priority)) &&
    typeof args.priority === 'number'
  )
}
