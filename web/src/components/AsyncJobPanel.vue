<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useDesk } from '../composables/desk'
import { useKnowledge } from '../composables/knowledge'
import { api, ApiError, Scope } from '../lib/client'
import type { KnowledgeDocument, KnowledgeJob } from '../lib/types'

const props = defineProps<{ document: KnowledgeDocument }>()
const emit = defineEmits<{ completed: [job: KnowledgeJob] }>()
const desk = useDesk(),
  knowledge = useKnowledge(),
  route = useRoute()
const job = ref<KnowledgeJob | null>(null),
  busy = ref(false),
  error = ref(''),
  paused = ref(false)
const scope = new Scope()
let timer: ReturnType<typeof setTimeout> | undefined
let deadline = 0
const active = computed(
  () => !!job.value && ['PENDING', 'RUNNING', 'RETRYING'].includes(job.value.status),
)
const member = computed(() => ['admin', 'member'].includes(desk.role.value || ''))
const canControl = computed(
  () =>
    member.value &&
    !!job.value &&
    (desk.role.value === 'admin' || job.value.requested_by === desk.session.user?.id),
)
const base = computed(() => `/organizations/${desk.orgID.value}`)
const endpoint = computed(() => `${base.value}/knowledge/documents/${props.document.id}/jobs`)
const labels = {
  PENDING: '等待执行',
  RUNNING: '正在处理',
  RETRYING: '等待自动重试',
  SUCCEEDED: '处理完成',
  FAILED: '处理失败',
  CANCELED: '已取消',
}
const phases = {
  WAITING: '等待后台处理',
  EXTRACTING: '提取正文',
  INDEXING: '建立索引',
  DONE: '完成',
}
function stop() {
  clearTimeout(timer)
  timer = undefined
  scope.cancel()
}
function fail(e: unknown) {
  if (e instanceof ApiError && [403, 404].includes(e.status)) {
    job.value = null
    knowledge.invalidatePermission(e)
  }
  error.value = e instanceof Error ? e.message : '任务请求失败'
}
function schedule() {
  clearTimeout(timer)
  if (!active.value || document.hidden || error.value) return
  if (Date.now() >= deadline) {
    paused.value = true
    return
  }
  timer = setTimeout(() => void load(), 2000)
}
async function load(reset = false) {
  if (busy.value || !desk.session.user) return
  if (reset) {
    deadline = Date.now() + 300000
    paused.value = false
  }
  const run = scope.begin()
  busy.value = true
  error.value = ''
  try {
    const data = await api.request<{ items: KnowledgeJob[] }>(endpoint.value, {
      signal: run.signal,
    })
    if (!run.current()) return
    job.value = Array.isArray(data.items) ? data.items[0] || null : null
    if (job.value?.status === 'SUCCEEDED') emit('completed', job.value)
  } catch (e) {
    if (run.current()) fail(e)
  } finally {
    if (run.current()) {
      busy.value = false
      schedule()
    }
  }
}
async function mutate(action: 'submit' | 'retry' | 'cancel') {
  if (busy.value || !member.value || (action !== 'submit' && !canControl.value)) return
  clearTimeout(timer)
  const run = scope.begin()
  const path =
    action === 'submit' ? endpoint.value : `${base.value}/jobs/${job.value!.id}/${action}`
  busy.value = true
  error.value = ''
  paused.value = false
  deadline = Date.now() + 300000
  try {
    const data = await api.request<KnowledgeJob>(path, { method: 'POST', signal: run.signal })
    if (!run.current()) return
    job.value = data
    if (data.status === 'SUCCEEDED') emit('completed', data)
  } catch (e) {
    if (run.current()) fail(e)
  } finally {
    if (run.current()) {
      busy.value = false
      schedule()
    }
  }
}
function visibility() {
  if (document.hidden) {
    stop()
    busy.value = false
  } else if (!paused.value && Date.now() < deadline) void load()
  else if (active.value) paused.value = true
}
watch(
  () => [route.fullPath, desk.session.epoch, desk.orgID.value, props.document.id],
  () => {
    stop()
    job.value = null
    error.value = ''
    busy.value = false
    paused.value = false
    deadline = Date.now() + 300000
    if (desk.session.user && !document.hidden) void load()
  },
  { immediate: true, flush: 'sync' },
)
document.addEventListener('visibilitychange', visibility)
onBeforeUnmount(() => {
  stop()
  document.removeEventListener('visibilitychange', visibility)
})
</script>

<template>
  <section class="panel" aria-label="异步处理任务">
    <h2>异步处理任务</h2>
    <p>提交后由后台完成正文提取和索引。离开页面后任务仍会继续执行。</p>
    <template v-if="job">
      <p role="status">{{ labels[job.status] }} · {{ phases[job.phase] }}</p>
      <p class="meta">
        任务 #{{ job.id }} · 第 {{ job.run_version }} 轮 · 尝试 {{ job.attempts }} /
        {{ job.max_attempts }} · 累计 {{ job.total_attempts }} 次
      </p>
      <p v-if="job.failure_code" class="notice">
        错误码：{{ job.failure_code }}。{{
          job.status === 'RETRYING' ? '系统将在稍后自动重试。' : '可检查文档或本地服务后重试。'
        }}
      </p>
      <p v-if="job.last_dispatch_error" class="notice">
        后台服务暂时不可用，任务已保存，恢复后会继续处理。
      </p>
    </template>
    <p v-else>尚未提交异步任务。</p>
    <p v-if="error" role="alert" class="error">{{ error }}</p>
    <p v-if="paused" class="notice">自动刷新已暂停，可手动刷新任务。</p>
    <div class="actions">
      <button v-if="member && !job" :disabled="busy" @click="mutate('submit')">提交异步处理</button>
      <button
        v-if="canControl && job && ['FAILED', 'CANCELED'].includes(job.status)"
        :disabled="busy"
        @click="mutate('retry')"
      >
        重试任务
      </button>
      <button v-if="canControl && active" :disabled="busy" @click="mutate('cancel')">
        取消任务
      </button>
      <button :disabled="busy" @click="load(true)">刷新任务</button>
    </div>
  </section>
</template>
