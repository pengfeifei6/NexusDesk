<script setup lang="ts">
import { useDesk } from '../composables/desk'
import TicketManagementPanel from '../components/TicketManagementPanel.vue'
import UiIcon from '../components/UiIcon.vue'
import StatusBadge from '../components/StatusBadge.vue'
import { dateTime, eventLabels } from '../lib/presentation'
const {
  detail,
  canWork,
  busy,
  role,
  base,
  comments,
  events,
  commentPage,
  eventPage,
  content,
  action,
  load,
  comment,
  changePage,
} = useDesk()
</script>
<template>
  <RouterLink :to="base" class="back-link">← 返回工单列表</RouterLink>
  <section v-if="detail" class="panel detail">
    <p class="eyebrow">工单 #{{ detail.id }}</p>
    <div class="row">
      <h1 class="detail-title">{{ detail.title }}</h1>
      <StatusBadge :status="detail.status" />
    </div>
    <dl class="detail-meta">
      <div>
        <dt>受理人</dt>
        <dd>{{ detail.assignee_id ? `成员 ${detail.assignee_id}` : '未指派' }}</dd>
      </div>
      <div>
        <dt>优先级</dt>
        <dd>
          <span class="priority" :data-priority="detail.priority">{{
            ['', '低', '普通', '高'][detail.priority]
          }}</span>
        </dd>
      </div>
      <div>
        <dt>最后更新</dt>
        <dd>
          <time :datetime="detail.updated_at" :title="detail.updated_at">{{
            dateTime(detail.updated_at)
          }}</time>
        </dd>
      </div>
      <div>
        <dt>当前版本</dt>
        <dd>{{ detail.version }}</dd>
      </div>
    </dl>
    <p class="body">{{ detail.body }}</p>
    <div class="actions">
      <button
        v-if="detail.status === 'OPEN' && !detail.assignee_id"
        class="primary"
        :disabled="busy"
        @click="action()"
      >
        领取工单</button
      ><button
        v-if="detail.status === 'OPEN' && detail.assignee_id && canWork"
        class="primary"
        :disabled="busy"
        @click="action('IN_PROGRESS')"
      >
        开始处理</button
      ><button
        v-if="detail.status === 'IN_PROGRESS' && canWork"
        class="primary"
        :disabled="busy"
        @click="action('RESOLVED')"
      >
        解决工单</button
      ><button
        v-if="detail.status === 'RESOLVED' && role === 'admin'"
        :disabled="busy"
        @click="action('CLOSED')"
      >
        关闭工单</button
      ><button
        v-if="detail.status === 'RESOLVED' && role === 'admin'"
        :disabled="busy"
        @click="action('IN_PROGRESS')"
      >
        重新打开</button
      ><button :disabled="busy" @click="load">刷新详情</button>
    </div>
    <TicketManagementPanel />
  </section>
  <div v-if="detail" class="detail-columns">
    <section class="panel">
      <div class="panel-title">
        <UiIcon name="chat" :size="18" />
        <h2>评论</h2>
      </div>
      <div v-if="!comments.length" class="empty-state">
        <strong>本页暂无评论</strong>
        <p>记录处理进展，让团队及时了解情况。</p>
      </div>
      <article v-for="c in comments" :key="c.id" class="comment">
        <small
          >用户 {{ c.author_id }} ·
          <time :datetime="c.created_at" :title="c.created_at">{{
            dateTime(c.created_at)
          }}</time></small
        >
        <p class="body">{{ c.content }}</p>
      </article>
      <div class="paging">
        <button :disabled="commentPage === 1 || busy" @click="changePage('comment', -1)">
          上一页评论</button
        ><span>第 {{ commentPage }} 页</span
        ><button :disabled="comments.length < 20 || busy" @click="changePage('comment', 1)">
          下一页评论
        </button>
      </div>
      <form v-if="detail.status !== 'CLOSED'" class="comment-form" @submit.prevent="comment">
        <label
          >评论内容<textarea
            v-model="content"
            required
            maxlength="5000"
            placeholder="写下处理进展或补充信息…"
          /></label
        ><button class="primary" :disabled="busy">发表评论</button>
      </form>
      <p v-else class="meta">已关闭工单不能新增评论。</p>
    </section>
    <section class="panel">
      <h2>事件记录</h2>
      <p v-if="!events.length" class="meta">本页暂无事件</p>
      <article v-for="e in events" :key="e.id" class="timeline-entry">
        <strong>{{ eventLabels[e.event_type] || e.event_type }}</strong>
        <p class="meta">
          操作者 {{ e.actor_id }} · 版本 {{ e.ticket_version }}<br /><time
            :datetime="e.created_at"
            :title="e.created_at"
            >{{ dateTime(e.created_at) }}</time
          >
        </p>
        <details>
          <summary>查看变更详情</summary>
          <pre>{{ JSON.stringify(e.after_data, null, 2) }}</pre>
        </details>
      </article>
      <div class="paging">
        <button :disabled="eventPage === 1 || busy" @click="changePage('event', -1)">
          上一页事件</button
        ><span>第 {{ eventPage }} 页</span
        ><button :disabled="events.length < 20 || busy" @click="changePage('event', 1)">
          下一页事件
        </button>
      </div>
    </section>
  </div>
</template>
