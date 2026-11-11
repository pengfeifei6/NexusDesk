<script setup lang="ts">
import type { RAGSource } from '../lib/types'
defineProps<{ items: RAGSource[]; orgID: string }>()
</script>
<template>
  <article v-for="source in items" :key="source.chunk_id" class="panel">
    <RouterLink :to="`/organizations/${orgID}/knowledge/${source.document_id}`">{{
      source.source_name
    }}</RouterLink>
    <p class="meta">
      片段 #{{ source.chunk_index }} · 相似度 {{ source.score.toFixed(3) }} · 字符
      {{ source.start_rune }}～{{ source.end_rune }}（不含末尾）
    </p>
    <pre class="body">{{ source.excerpt }}</pre>
  </article>
</template>
