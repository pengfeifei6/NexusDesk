<script setup lang="ts">
import type { RAGSource } from '../lib/types'
import UiIcon from './UiIcon.vue'
defineProps<{ items: RAGSource[]; orgID: string }>()
</script>
<template>
  <article
    v-for="(source, i) in items"
    :key="source.chunk_id"
    class="panel source-card"
    :aria-label="`来源 ${i + 1}`"
  >
    <div class="source-heading">
      <span class="source-number">{{ i + 1 }}</span
      ><RouterLink :to="`/organizations/${orgID}/knowledge/${source.document_id}`">{{
        source.source_name
      }}</RouterLink
      ><UiIcon name="file" :size="16" />
    </div>
    <p class="meta">
      片段 #{{ source.chunk_index }} · 余弦相似度 {{ source.score.toFixed(3) }} · 字符
      {{ source.start_rune }}～{{ source.end_rune }}（不含末尾）<template
        v-if="source.fusion_score !== undefined"
      >
        · 融合分数 {{ source.fusion_score.toFixed(4) }}</template
      >
    </p>
    <pre class="body">{{ source.excerpt }}</pre>
  </article>
</template>
