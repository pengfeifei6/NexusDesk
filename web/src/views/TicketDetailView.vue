<script setup lang="ts">
import { useDesk } from '../composables/desk'
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
  <RouterLink :to="base">← 返回工单列表</RouterLink>
  <section v-if="detail" class="panel detail">
    <div class="row">
      <h1>{{ detail.title }}</h1>
      <span class="badge">{{ detail.status }}</span>
    </div>
    <p class="meta">
      工单 #{{ detail.id }} · 版本 {{ detail.version }} · 受理人
      {{ detail.assignee_id || '未指派' }} · 优先级
      {{ ['', '低', '普通', '高'][detail.priority] }} · 更新 {{ detail.updated_at }}
    </p>
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
        :disabled="busy"
        @click="action('IN_PROGRESS')"
      >
        开始处理</button
      ><button
        v-if="detail.status === 'IN_PROGRESS' && canWork"
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
  </section>
  <section v-if="detail" class="panel">
    <h2>评论</h2>
    <p v-if="!comments.length">本页暂无评论</p>
    <article v-for="c in comments" :key="c.id">
      <small>用户 {{ c.author_id }} · {{ c.created_at }}</small>
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
    <form v-if="detail.status !== 'CLOSED'" @submit.prevent="comment">
      <label>评论内容<textarea v-model="content" required maxlength="5000" /></label
      ><button class="primary" :disabled="busy">发表评论</button>
    </form>
    <p v-else>已关闭工单不能新增评论。</p>
  </section>
  <section v-if="detail" class="panel">
    <h2>事件记录</h2>
    <p v-if="!events.length">本页暂无事件</p>
    <article v-for="e in events" :key="e.id">
      <strong>{{ e.event_type }}</strong>
      <p>操作者 {{ e.actor_id }} · 版本 {{ e.ticket_version }} · {{ e.created_at }}</p>
      <pre>{{ JSON.stringify(e.after_data, null, 2) }}</pre>
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
</template>
