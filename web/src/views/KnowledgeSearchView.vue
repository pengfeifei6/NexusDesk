<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useDesk } from '../composables/desk'
import { api, Scope } from '../lib/client'
import type { AskResponse, SearchResponse } from '../lib/types'
import RAGSourceList from '../components/RAGSourceList.vue'
const desk = useDesk()
const route = useRoute()
const orgID = desk.orgID
const query = ref('')
const topK = ref(5)
const busy = ref(false)
const error = ref('')
const search = ref<SearchResponse | null>(null)
const answer = ref<AskResponse | null>(null)
const scope = new Scope()
const canUse = computed(() => desk.role.value === 'admin' || desk.role.value === 'member')
function clear() {
  scope.cancel()
  search.value = null
  answer.value = null
  error.value = ''
  busy.value = false
}
watch([query, topK], clear, { flush: 'sync' })
watch(
  () => [route.fullPath, desk.session.epoch],
  () => {
    clear()
    query.value = ''
    topK.value = 5
  },
  { flush: 'sync' },
)
onBeforeUnmount(() => scope.cancel())
async function submit(mode: 'search' | 'ask') {
  if (busy.value || !canUse.value) return
  const text = query.value.trim()
  if (
    !text ||
    [...text].length > 1000 ||
    new TextEncoder().encode(text).length > 4000 ||
    text.includes('\0') ||
    !Number.isInteger(topK.value) ||
    topK.value < 1 ||
    topK.value > 10
  ) {
    error.value = '请输入 1～1000 个字符的问题'
    return
  }
  clear()
  const run = scope.begin()
  busy.value = true
  try {
    const result = await api.request<SearchResponse | AskResponse>(
      `/organizations/${orgID.value}/knowledge/${mode}`,
      {
        method: 'POST',
        body: { query: text, top_k: topK.value },
        signal: run.signal,
        timeoutMs: mode === 'search' ? 30000 : 70000,
      },
    )
    if (!run.current()) return
    if (mode === 'search') search.value = result as SearchResponse
    else answer.value = result as AskResponse
  } catch (e) {
    if (run.current()) error.value = e instanceof Error ? e.message : '请求失败，请重试'
  } finally {
    if (run.current()) busy.value = false
  }
}
</script>
<template>
  <section class="panel">
    <h1>知识库检索与问答</h1>
    <p>仅检索当前组织已建立索引的文档。回答附带可核对的来源；依据不足时会明确提示。</p>
    <label
      >检索问题<textarea
        v-model="query"
        aria-label="检索问题"
        rows="4"
        placeholder="例如：数据库连接超时应该先检查什么？"
      ></textarea>
    </label>
    <label
      >返回片段数<select v-model.number="topK" aria-label="返回片段数">
        <option v-for="n in 10" :key="n" :value="n">{{ n }}</option>
      </select></label
    >
    <div class="actions">
      <button :disabled="busy || !canUse" @click="submit('search')">检索文档</button
      ><button class="primary" :disabled="busy || !canUse" @click="submit('ask')">
        基于知识库回答
      </button>
    </div>
    <p v-if="busy" role="status">正在查询，请稍候…</p>
    <p v-if="error" role="alert" class="error">{{ error }}</p>
  </section>
  <section v-if="answer" class="panel">
    <h2>{{ answer.insufficient_evidence ? '依据不足' : '知识库回答' }}</h2>
    <pre class="body">{{ answer.answer }}</pre>
    <p class="meta">请求 {{ answer.request_id }} · {{ answer.generation_model }}</p>
    <p v-if="answer.sources.length">请结合下面的原文核对回答。</p>
  </section>
  <p v-if="search" role="status">
    找到 {{ search.items.length }} 个片段 · 向量生成 {{ search.embedding_ms }} ms · 数据库检索
    {{ search.search_ms }} ms
  </p>
  <p v-if="search && !search.items.length">未找到足够相似的文档，请调整问题或先建立索引。</p>
  <p v-if="search?.items.length || answer?.sources.length" class="meta">
    相似度用于排序，不代表回答正确的概率。
  </p>
  <RAGSourceList :items="answer?.sources || search?.items || []" :org-i-d="orgID" />
</template>
