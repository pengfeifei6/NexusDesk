<script setup lang="ts">
import { useDesk } from '../composables/desk'
const { title, body, newPriority, busy, create } = useDesk()
</script>
<template>
  <section class="panel">
    <h2>创建工单</h2>
    <form @submit.prevent="create">
      <label>工单标题<input v-model="title" required maxlength="200" /></label
      ><label>工单描述<textarea v-model="body" required maxlength="10000" /></label
      ><label
        >优先级<select v-model="newPriority">
          <option :value="1">低</option>
          <option :value="2">普通</option>
          <option :value="3">高</option>
        </select></label
      ><button class="primary" :disabled="busy">创建工单</button>
    </form>
  </section>
</template>
