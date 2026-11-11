<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useDesk } from '../composables/desk'
import { api, Scope } from '../lib/client'
import type { AskResponse, SearchResponse, RetrievalMode } from '../lib/types'
import RAGSourceList from '../components/RAGSourceList.vue'
import UiIcon from '../components/UiIcon.vue'
const desk = useDesk()
const route = useRoute()
const orgID = desk.orgID
const query = ref('')
const topK = ref(5)
const retrievalMode = ref<RetrievalMode>('vector')
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
watch([query, topK, retrievalMode], clear, { flush: 'sync' })
watch(
  () => [route.fullPath, desk.session.epoch],
  () => {
    clear()
    query.value = ''
    topK.value = 5
    retrievalMode.value = 'vector'
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
        body: { query: text, top_k: topK.value, retrieval_mode: retrievalMode.value },
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
  <div class="page-heading">
    <div>
      <p class="eyebrow">KNOWLEDGE ASSISTANT</p>
      <h1>知识库检索与问答</h1>
      <p>找到相关知识，获得有来源、可核对的回答。</p>
    </div>
  </div>
  <div class="rag-workspace">
    <section class="panel rag-composer">
      <div class="panel-title">
        <UiIcon name="search" :size="18" />
        <h2>你想了解什么？</h2>
      </div>
      <p class="meta">检索当前组织已建立索引的文档。</p>
      <form @submit.prevent="submit('ask')">
        <label
          >检索问题<textarea
            v-model="query"
            aria-label="检索问题"
            rows="5"
            placeholder="例如：数据库连接超时应该先检查什么？"
          ></textarea>
        </label>
        <div class="rag-form-options">
          <label
            >检索策略<select v-model="retrievalMode" aria-label="检索策略">
              <option value="vector">精确向量</option>
              <option value="hybrid">混合检索</option>
            </select></label
          >
          <label
            >返回片段数<select v-model.number="topK" aria-label="返回片段数">
              <option v-for="n in 10" :key="n" :value="n">{{ n }}</option>
            </select></label
          >
        </div>
        <p class="meta">
          {{
            retrievalMode === 'vector'
              ? '根据问题含义匹配相关文档。'
              : '结合语义与关键词，查找相关文档。'
          }}
        </p>
        <div class="actions">
          <button type="button" :disabled="busy || !canUse" @click="submit('search')">
            检索文档</button
          ><button type="button" class="primary" :disabled="busy || !canUse" @click="submit('ask')">
            基于知识库回答
          </button>
        </div>
      </form>
      <p v-if="busy" role="status" class="loading-line">正在查询，请稍候…</p>
      <p v-if="error" role="alert" class="error">{{ error }}</p>
    </section>
    <div class="rag-results" aria-live="polite">
      <section v-if="answer" class="panel answer-panel">
        <div class="result-heading">
          <h2>{{ answer.insufficient_evidence ? '依据不足' : '知识库回答' }}</h2>
          <UiIcon name="spark" :size="19" />
        </div>
        <pre class="body">{{ answer.answer }}</pre>
        <div v-if="answer.timings" class="result-metrics">
          <div>
            总耗时<strong>{{ answer.timings.total_ms }} ms</strong>
          </div>
          <div>
            向量生成<strong>{{ answer.timings.embedding_ms }} ms</strong>
          </div>
          <div>
            检索<strong>{{ answer.timings.search_ms }} ms</strong>
          </div>
          <div>
            回答生成<strong>{{ answer.timings.generation_ms }} ms</strong>
          </div>
        </div>
        <div class="usage-detail">
          <template v-if="!answer.usage || !answer.usage.known"
            >用量未知 · 生成服务未提供可信的 token 用量</template
          ><template v-else-if="!answer.usage.model_called">未调用生成模型 · 0 tokens</template
          ><template v-else
            ><strong>{{ answer.usage.total_tokens }} tokens</strong> · 输入
            {{ answer.usage.prompt_tokens }} / 输出 {{ answer.usage.completion_tokens
            }}<template
              v-if="
                answer.usage.cache_hit_tokens !== null &&
                answer.usage.cache_hit_tokens !== undefined
              "
            >
              · 缓存命中 {{ answer.usage.cache_hit_tokens }}</template
            ><template
              v-if="
                answer.usage.cache_miss_tokens !== null &&
                answer.usage.cache_miss_tokens !== undefined
              "
            >
              / 未命中 {{ answer.usage.cache_miss_tokens }}</template
            ></template
          >
        </div>
        <p class="meta">
          {{ answer.retrieval_mode === 'hybrid' ? '混合检索' : '精确向量' }} ·
          {{ answer.generation_model }}<br />请求 {{ answer.request_id }}
        </p>
      </section>
      <section v-if="search" class="panel">
        <div class="result-heading">
          <h2>检索结果</h2>
          <span class="badge">{{
            search.retrieval_mode === 'hybrid' ? '混合检索' : '精确向量'
          }}</span>
        </div>
        <p role="status" class="meta">
          找到 {{ search.items.length }} 个片段 · 向量生成 {{ search.embedding_ms }} ms · 检索
          {{ search.search_ms }} ms
        </p>
        <p v-if="!search.items.length" class="meta">
          未找到足够相似的文档，请调整问题或先建立索引。
        </p>
      </section>
      <template v-if="search?.items.length || answer?.sources.length"
        ><div class="result-heading">
          <h2>参考来源</h2>
          <span class="meta">请结合原文核对回答</span>
        </div>
        <p class="meta">余弦相似度衡量内容接近程度，不代表回答正确的概率。</p></template
      >
      <RAGSourceList :items="answer?.sources || search?.items || []" :org-i-d="orgID" />
      <section v-if="!answer && !search && !busy" class="panel ai-empty">
        <div class="empty-state">
          <UiIcon name="spark" /><strong>让团队知识，成为你的答案</strong>
          <p>输入问题开始检索。回答附带原文来源，<br />没有足够依据时会明确提示。</p>
        </div>
      </section>
    </div>
  </div>
</template>
