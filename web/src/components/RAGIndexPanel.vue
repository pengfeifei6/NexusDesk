<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useDesk } from '../composables/desk'
import { useKnowledge } from '../composables/knowledge'
import { api, ApiError, Scope } from '../lib/client'
import type { IndexInfo, KnowledgeDocument } from '../lib/types'
const props = defineProps<{ document: KnowledgeDocument }>()
const desk = useDesk()
const knowledge = useKnowledge()
const route = useRoute()
const index = ref<IndexInfo | null>(null)
const error = ref('')
const busy = ref(false)
const scope = new Scope()
const endpoint = computed(
  () => `/organizations/${desk.orgID.value}/knowledge/documents/${props.document.id}/index`,
)
const member = computed(() => desk.role.value === 'admin' || desk.role.value === 'member')
const labels = { ABSENT: '尚未索引', INDEXING: '索引中', READY: '已索引', FAILED: '索引失败' }
function fail(e: unknown) {
  if (e instanceof ApiError && [403, 404].includes(e.status)) {
    index.value = null
    knowledge.invalidatePermission(e)
  }
  error.value = e instanceof Error ? e.message : '索引请求失败'
}
async function load() {
  const run = scope.begin()
  index.value = null
  error.value = ''
  busy.value = true
  try {
    const data = await api.request<IndexInfo>(endpoint.value, { signal: run.signal })
    if (run.current()) index.value = data
  } catch (e) {
    if (run.current()) fail(e)
  } finally {
    if (run.current()) busy.value = false
  }
}
async function build() {
  if (busy.value || !member.value || props.document.status !== 'READY') return
  const run = scope.begin(),
    path = endpoint.value
  busy.value = true
  error.value = ''
  try {
    const data = await api.request<IndexInfo>(path, {
      method: 'POST',
      signal: run.signal,
      timeoutMs: 70000,
    })
    if (run.current()) index.value = data
  } catch (e) {
    if (!run.current()) return
    if (e instanceof ApiError && [403, 404].includes(e.status)) {
      fail(e)
      return
    }
    try {
      const data = await api.request<IndexInfo>(path, { signal: run.signal })
      if (run.current()) index.value = data
    } catch (refreshError) {
      if (!run.current()) return
      if (refreshError instanceof ApiError && [403, 404].includes(refreshError.status)) {
        fail(refreshError)
        return
      }
    }
    if (run.current()) fail(e)
  } finally {
    if (run.current()) busy.value = false
  }
}
watch(
  () => [route.fullPath, desk.session.epoch, props.document.id],
  () => {
    scope.cancel()
    if (desk.session.user) void load()
    else {
      index.value = null
      error.value = ''
      busy.value = false
    }
  },
  { immediate: true, flush: 'sync' },
)
onBeforeUnmount(() => scope.cancel())
</script>
<template>
  <section class="panel" aria-label="检索索引">
    <h2>检索索引</h2>
    <p>正文提取完成后，还需建立索引才能参与检索和问答。</p>
    <p v-if="index">
      {{ labels[index.status] }} · {{ index.chunk_count }} 块 · 版本 {{ index.index_version }}
    </p>
    <p v-if="index" class="meta">
      {{ index.embedding_model }} · {{ index.embedding_dimension }} 维
    </p>
    <p v-if="index?.failure_code" class="notice">
      上次索引失败，可检查模型服务后重试。错误码：{{ index.failure_code }}
    </p>
    <p v-if="index?.status === 'INDEXING'">
      索引正在处理中，完成后即可检索。若长时间未完成，请联系管理员。
    </p>
    <p v-if="document.status !== 'READY'">请先完成正文提取。</p>
    <p v-if="error" role="alert" class="error">{{ error }}</p>
    <p v-if="busy" role="status">正在处理索引请求…</p>
    <div class="actions">
      <button
        v-if="member"
        :disabled="busy || document.status !== 'READY' || index?.status === 'INDEXING'"
        @click="build"
      >
        建立索引</button
      ><button :disabled="busy" @click="load">刷新索引</button>
    </div>
  </section>
</template>
