<script setup lang="ts">
import { useDesk } from '../composables/desk'
import UiIcon from './UiIcon.vue'
const { title, body, newPriority, busy, create } = useDesk()
</script>
<template>
  <section id="ticket-create" class="panel form-section ticket-create-panel" tabindex="-1">
    <div class="panel-title">
      <UiIcon name="plus" :size="18" />
      <h2>创建工单</h2>
    </div>
    <p class="section-description">把问题交给团队，让每一步处理都清晰可见。</p>
    <form @submit.prevent="create">
      <div class="form-grid">
        <label
          >工单标题<input
            v-model="title"
            required
            maxlength="200"
            placeholder="用一句话描述需要解决的问题" /></label
        ><label
          >优先级<select v-model="newPriority">
            <option :value="1">低</option>
            <option :value="2">普通</option>
            <option :value="3">高</option>
          </select></label
        ><label class="full"
          >工单描述<textarea
            v-model="body"
            required
            maxlength="10000"
            placeholder="补充问题背景、影响范围与复现步骤…"
          />
        </label>
      </div>
      <div class="form-footer">
        <p class="meta">清楚的描述，有助于团队更快响应。</p>
        <button class="primary" :disabled="busy"><UiIcon name="plus" :size="15" />创建工单</button>
      </div>
    </form>
  </section>
</template>
