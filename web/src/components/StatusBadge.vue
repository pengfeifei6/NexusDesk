<script setup lang="ts">
import { statusLabels } from '../lib/presentation'
withDefaults(defineProps<{ status?: string }>(), { status: '—' })
</script>
<template>
  <span class="status-badge" :data-status="status"
    ><span class="status-dot" />{{ statusLabels[status] || status }}</span
  >
</template>
