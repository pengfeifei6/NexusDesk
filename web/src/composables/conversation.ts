import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { api, ApiError, Scope } from '../lib/client'
import { aiMessages, validAction, validSource } from '../lib/ai-stream'
import type { AIConversation, AIMessage, AIToolAction, RAGSource } from '../lib/types'
import type { Desk } from './desk'

export function createConversation(desk: Desk) {
  const route = useRoute()
  const context = new Scope(),
    conversationScope = new Scope(),
    streamScope = new Scope()
  let contextRun = context.begin(),
    conversationRun = conversationScope.begin()
  let historySequence = 0,
    listSequence = 0
  const conversations = ref<AIConversation[]>([]),
    selected = ref<AIConversation | null>(null)
  const messages = ref<AIMessage[]>([]),
    actions = ref<AIToolAction[]>([])
  const title = ref(''),
    content = ref(''),
    error = ref(''),
    notice = ref('')
  const loading = ref(false),
    creating = ref(false),
    generating = ref(false),
    historyLoading = ref(false)
  const actionBusy = ref('')
  const endpoint = computed(() => `/organizations/${desk.orgID.value}/ai`)
  const canUse = computed(
    () => !!desk.session.user && ['admin', 'member'].includes(desk.role.value || ''),
  )
  const hasUnfinished = computed(() => messages.value.some((m) => m.status === 'STREAMING'))
  function clearConversation() {
    conversationRun = conversationScope.begin()
    streamScope.cancel()
    historySequence++
    selected.value = null
    messages.value = []
    actions.value = []
    generating.value = false
    historyLoading.value = false
    actionBusy.value = ''
    content.value = ''
  }
  function fail(e: unknown) {
    if (
      e instanceof ApiError &&
      ([401, 403, 404].includes(e.status) ||
        ['UNAUTHENTICATED', 'FORBIDDEN', 'NOT_FOUND'].includes(e.code || ''))
    ) {
      context.cancel()
      clearConversation()
      conversations.value = []
      loading.value = false
      creating.value = false
    }
    error.value =
      e instanceof ApiError
        ? e.code &&
          !['RATE_LIMITED', 'RATE_LIMIT_UNAVAILABLE'].includes(e.code) &&
          Object.prototype.hasOwnProperty.call(aiMessages, e.code)
          ? aiMessages[e.code]!
          : e.message
        : '请求失败，请重新读取历史'
  }
  async function loadList() {
    const run = contextRun,
      seq = ++listSequence,
      path = `${endpoint.value}/conversations`
    loading.value = true
    try {
      const data = await api.request<AIConversation[]>(path, { signal: run.signal })
      if (run.current() && seq === listSequence) conversations.value = data
    } catch (e) {
      if (run.current() && seq === listSequence) fail(e)
    } finally {
      if (run.current() && seq === listSequence) loading.value = false
    }
  }
  async function history(preserveError = false) {
    if (!selected.value || generating.value) return
    const run = conversationRun,
      seq = ++historySequence
    const id = selected.value.id,
      org = desk.orgID.value
    if (!preserveError) {
      error.value = ''
      notice.value = ''
    }
    historyLoading.value = true
    try {
      const data = await api.request<{ messages: AIMessage[]; actions: AIToolAction[] }>(
        `${endpoint.value}/conversations/${id}/messages`,
        { signal: run.signal },
      )
      if (!run.current() || seq !== historySequence) return
      if (
        !Array.isArray(data.messages) ||
        !Array.isArray(data.actions) ||
        data.messages.length > 100 ||
        data.actions.length > 100 ||
        data.messages.some(
          (m) =>
            m.conversation_id !== id ||
            m.org_id !== org ||
            typeof m.id !== 'string' ||
            typeof m.content !== 'string' ||
            !['USER', 'ASSISTANT'].includes(m.role) ||
            !['STREAMING', 'COMPLETED', 'FAILED', 'INTERRUPTED'].includes(m.status) ||
            !Array.isArray(m.citations) ||
            m.citations.some((source) => !validSource(source)),
        ) ||
        data.actions.some((a) => !validAction(a) || a.conversation_id !== id || a.org_id !== org)
      )
        throw new ApiError(0, '历史响应格式异常，请重新读取历史')
      messages.value = data.messages
      actions.value = data.actions
    } catch (e) {
      if (run.current() && seq === historySequence) fail(e)
    } finally {
      if (run.current() && seq === historySequence) historyLoading.value = false
    }
  }
  function select(value: AIConversation) {
    clearConversation()
    error.value = ''
    notice.value = ''
    selected.value = value
    void history()
  }
  async function create() {
    if (creating.value || !canUse.value) return
    const value = title.value.trim()
    if (!validText(value, 100, 16000)) {
      error.value = '对话标题需 1～100 个字符'
      return
    }
    const run = contextRun
    creating.value = true
    error.value = ''
    try {
      const data = await api.request<AIConversation>(`${endpoint.value}/conversations`, {
        method: 'POST',
        body: { title: value },
        signal: run.signal,
      })
      if (!run.current()) return
      conversations.value = [data, ...conversations.value.filter((c) => c.id !== data.id)].slice(
        0,
        50,
      )
      title.value = ''
      select(data)
    } catch (e) {
      if (run.current()) fail(e)
    } finally {
      if (run.current()) creating.value = false
    }
  }
  async function send() {
    if (
      !selected.value ||
      !canUse.value ||
      generating.value ||
      historyLoading.value ||
      hasUnfinished.value
    )
      return
    const draft = content.value
    const text = draft.trim()
    if (!validText(text, 4000, 16384)) {
      error.value = '消息需 1～4000 个字符且不超过 16 KiB'
      return
    }
    const run = streamScope.begin(),
      parent = conversationRun
    const id = selected.value.id,
      org = desk.orgID.value,
      request = crypto.randomUUID()
    const current = () => run.current() && parent.current()
    let messageID = ''
    generating.value = true
    error.value = ''
    notice.value = ''
    content.value = ''
    try {
      await api.stream(`${endpoint.value}/conversations/${id}/messages`, {
        body: { content: text, client_request_id: request },
        signal: run.signal,
        onEvent(name, raw) {
          if (!current()) return
          const v = raw as Record<string, unknown>
          if (name === 'message_start') {
            if (messageID || v.conversation_id !== id || v.client_request_id !== request)
              throw new ApiError(0, '响应会话不匹配，请重新读取历史')
            messageID = String(v.message_id)
            const base = {
              conversation_id: id,
              client_request_id: request,
              org_id: org,
              model_name: '',
              input_tokens: 0,
              output_tokens: 0,
              created_at: new Date().toISOString(),
              completed_at: null,
            }
            messages.value.push(
              {
                ...base,
                id: String(v.user_message_id),
                role: 'USER',
                content: text,
                citations: [],
                status: 'COMPLETED',
              },
              {
                ...base,
                id: messageID,
                role: 'ASSISTANT',
                content: '',
                status: 'STREAMING',
                citations: [],
              },
            )
            return
          }
          if (!messageID || v.message_id !== messageID)
            throw new ApiError(0, '响应消息不匹配，请重新读取历史')
          const message = messages.value.find((m) => m.id === messageID)!
          if (name === 'content_delta') {
            if (typeof v.delta !== 'string' || message.content.length + v.delta.length > 65536)
              throw new ApiError(0, '生成内容超过限制，请重新读取历史')
            message.content += v.delta
          } else if (name === 'citation') message.citations.push(v.source as RAGSource)
          else if (name === 'tool_approval_required') {
            const action = v.action as AIToolAction
            if (
              action.conversation_id !== id ||
              action.org_id !== org ||
              action.assistant_message_id !== messageID ||
              action.proposed_by_user_id !== desk.session.user?.id
            )
              throw new ApiError(0, '提议范围不匹配，请重新读取历史')
            actions.value = [...actions.value.filter((a) => a.id !== action.id), action]
          } else if (name === 'message_end') {
            message.status = 'COMPLETED'
            message.completed_at = new Date().toISOString()
          }
        },
      })
      if (current()) notice.value = '生成已完成'
    } catch (e) {
      if (current()) {
        if (
          !messageID &&
          !content.value &&
          e instanceof ApiError &&
          ((e.status === 429 && e.code === 'RATE_LIMITED') ||
            (e.status === 503 && e.code === 'RATE_LIMIT_UNAVAILABLE'))
        )
          content.value = draft
        fail(e)
      }
    } finally {
      if (current()) {
        generating.value = false
        await history(true)
      }
    }
  }
  function stop() {
    if (!generating.value) return
    streamScope.cancel()
    generating.value = false
    notice.value = '已停止生成；已保存的提议仍可确认或拒绝。请重新读取历史核对状态。'
    void history(true)
  }
  async function decide(action: AIToolAction, decision: 'confirm' | 'reject') {
    if (actionBusy.value || !canUse.value || action.status !== 'PROPOSED') return
    const run = conversationRun
    // A history request started before this mutation may contain an older action snapshot.
    historySequence++
    historyLoading.value = false
    actionBusy.value = action.id
    error.value = ''
    try {
      const data = await api.request<AIToolAction>(
        `${endpoint.value}/tool-actions/${action.id}/${decision}`,
        { method: 'POST', signal: run.signal },
      )
      if (!run.current()) return
      if (
        !validAction(data) ||
        data.id !== action.id ||
        data.org_id !== desk.orgID.value ||
        data.conversation_id !== selected.value?.id
      )
        throw new ApiError(0, '提议响应格式异常，请重新读取历史')
      // Generation completion can start another history read while confirmation is in flight.
      // Its snapshot also predates this committed result, so invalidate it before applying data.
      historySequence++
      historyLoading.value = false
      actions.value = actions.value.map((a) => (a.id === data.id ? data : a))
      notice.value = decision === 'confirm' ? '工单已创建' : '提议已拒绝'
    } catch (e) {
      if (!run.current()) return
      fail(e)
      if (run.current()) await history(true)
    } finally {
      if (run.current()) actionBusy.value = ''
    }
  }
  watch(
    () => [route.fullPath, desk.orgID.value, desk.session.epoch, desk.session.user?.id],
    () => {
      contextRun = context.begin()
      clearConversation()
      listSequence++
      conversations.value = []
      title.value = ''
      error.value = ''
      notice.value = ''
      loading.value = false
      creating.value = false
      if (desk.session.user && route.path === `/organizations/${desk.orgID.value}/ai`)
        void loadList()
    },
    { immediate: true, flush: 'sync' },
  )
  onBeforeUnmount(() => {
    context.cancel()
    conversationScope.cancel()
    streamScope.cancel()
  })
  return {
    conversations,
    selected,
    messages,
    actions,
    title,
    content,
    error,
    notice,
    loading,
    creating,
    generating,
    historyLoading,
    actionBusy,
    canUse,
    hasUnfinished,
    loadList,
    history,
    select,
    create,
    send,
    stop,
    decide,
  }
}
function validText(value: string, maxChars: number, maxBytes: number) {
  return (
    !!value &&
    [...value].length <= maxChars &&
    new TextEncoder().encode(value).length <= maxBytes &&
    !/[\u0000\uD800-\uDFFF]/u.test(value)
  )
}
