<script setup lang="ts">
import { focusSection } from '../lib/focus-section'
import { computed } from 'vue'
import { useDesk } from '../composables/desk'
import TicketForm from '../components/TicketForm.vue'
import UiIcon from '../components/UiIcon.vue'
import StatusBadge from '../components/StatusBadge.vue'
import { dateTime } from '../lib/presentation'
const { busy, tickets, page, status, priority, base, load, filter, changePage } = useDesk()
const overview = computed(() => [
  { key: 'loaded', label: '本页工单', value: tickets.value.length, icon: 'stack', tone: 'neutral' },
  {
    key: 'open',
    label: '待处理',
    value: tickets.value.filter((t) => t.status === 'OPEN').length,
    icon: 'circle',
    tone: 'blue',
  },
  {
    key: 'working',
    label: '处理中',
    value: tickets.value.filter((t) => t.status === 'IN_PROGRESS').length,
    icon: 'activity',
    tone: 'amber',
  },
  {
    key: 'urgent',
    label: '高优先级',
    value: tickets.value.filter((t) => t.priority === 3).length,
    icon: 'flag',
    tone: 'rose',
  },
])
</script>
<template>
  <div class="page-heading">
    <div>
      <p class="eyebrow">TICKET MANAGEMENT</p>
      <h1>工单</h1>
      <p>跟进团队的每一个问题，让协作清晰有序。</p>
    </div>
    <div class="heading-actions">
      <button :disabled="busy" @click="load"><UiIcon name="refresh" :size="15" />刷新列表</button>
      <a
        class="primary create-shortcut"
        href="#ticket-create"
        @click.prevent="focusSection('ticket-create')"
        ><UiIcon name="plus" :size="16" />新建工单</a
      >
    </div>
  </div>
  <section class="ticket-overview" aria-label="当前页工单概览">
    <div class="overview-caption">
      <span>当前页概览</span><small>仅统计当前页 · 随筛选结果更新</small>
    </div>
    <div class="overview-grid">
      <article
        v-for="metric in overview"
        :key="metric.key"
        :data-metric="metric.key"
        :data-tone="metric.tone"
        class="metric-card"
      >
        <div>
          <span>{{ metric.label }}</span
          ><strong>{{ metric.value }}</strong>
        </div>
        <span class="metric-symbol"><UiIcon :name="metric.icon" :size="20" /></span>
      </article>
    </div>
  </section>
  <section class="panel table-panel" aria-label="工单列表">
    <div class="list-heading">
      <div>
        <UiIcon name="ticket" :size="18" />
        <h2>团队工单</h2>
        <span class="count-pill">当前页 {{ tickets.length }}</span>
      </div>
      <span class="meta">第 {{ page }} 页</span>
    </div>
    <div class="filters">
      <label
        >状态筛选<select v-model="status" @change="filter">
          <option value="">全部状态</option>
          <option value="OPEN">待处理</option>
          <option value="IN_PROGRESS">处理中</option>
          <option value="RESOLVED">已解决</option>
          <option value="CLOSED">已关闭</option>
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
    <div v-if="!busy && !tickets.length" class="empty-state">
      <UiIcon name="ticket" /><strong>暂无工单</strong>
      <p>试试调整筛选条件，或在下方创建第一张工单。</p>
    </div>
    <div v-if="tickets.length" class="table-scroll">
      <table class="data-table">
        <thead>
          <tr>
            <th scope="col">工单标题</th>
            <th scope="col">状态</th>
            <th scope="col">优先级</th>
            <th scope="col">受理人</th>
            <th scope="col">更新时间</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="t in tickets" :key="t.id">
            <td class="title-cell">
              <RouterLink
                class="table-title"
                :to="`${base}/${t.id}`"
                :aria-label="t.title"
                :title="t.title"
                ><UiIcon name="ticket" :size="17" /><span class="ticket-title-copy"
                  ><span class="ticket-title-text">{{ t.title }}</span
                  ><small>#{{ t.id }}</small></span
                ></RouterLink
              >
            </td>
            <td><StatusBadge :status="t.status" /></td>
            <td>
              <span class="priority" :data-priority="t.priority">{{
                ['', '低', '普通', '高'][t.priority]
              }}</span>
            </td>
            <td>{{ t.assignee_id ? `成员 ${t.assignee_id}` : '未指派' }}</td>
            <td>
              <time :datetime="t.updated_at" :title="t.updated_at">{{
                dateTime(t.updated_at)
              }}</time>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
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
