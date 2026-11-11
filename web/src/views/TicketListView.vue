<script setup lang="ts">
import { useDesk } from '../composables/desk'
import TicketForm from '../components/TicketForm.vue'
const { busy, tickets, page, status, priority, base, load, filter, changePage } = useDesk()
</script>
<template>
  <section class="panel">
    <div class="row">
      <h1>工单</h1>
      <button :disabled="busy" @click="load">刷新列表</button>
    </div>
    <div class="filters">
      <label
        >状态筛选<select v-model="status" @change="filter">
          <option value="">全部状态</option>
          <option>OPEN</option>
          <option>IN_PROGRESS</option>
          <option>RESOLVED</option>
          <option>CLOSED</option>
        </select></label
      ><label
        >优先级筛选<select v-model="priority" @change="filter">
          <option value="">全部优先级</option>
          <option value="1">低</option>
          <option value="2">普通</option>
          <option value="3">高</option>
        </select></label
      >
    </div>
    <p v-if="!busy && !tickets.length">暂无工单</p>
    <ul class="tickets">
      <li v-for="t in tickets" :key="t.id">
        <RouterLink :to="`${base}/${t.id}`">{{ t.title }}</RouterLink
        ><span
          >{{ t.status }} · {{ ['', '低', '普通', '高'][t.priority] }} · 受理人
          {{ t.assignee_id || '未指派' }} · 更新 {{ t.updated_at }}</span
        >
      </li>
    </ul>
    <div class="paging">
      <button :disabled="page === 1 || busy" @click="changePage('ticket', -1)">上一页工单</button
      ><span>第 {{ page }} 页</span
      ><button :disabled="tickets.length < 20 || busy" @click="changePage('ticket', 1)">
        下一页工单
      </button>
    </div>
  </section>
  <TicketForm />
</template>
