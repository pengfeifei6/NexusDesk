<script setup lang="ts">
import { createConversation } from '../composables/conversation'
import { useDesk } from '../composables/desk'
import RAGSourceList from '../components/RAGSourceList.vue'
import UiIcon from '../components/UiIcon.vue'
import { dateTime } from '../lib/presentation'
const desk = useDesk()
const {
  conversations,
  selected,
  messages,
  actions,
  title,
  content,
  error,
  notice,
  loading,
  creating,
  generating,
  historyLoading,
  actionBusy,
  canUse,
  hasUnfinished,
  loadList,
  history,
  select,
  create,
  send,
  stop,
  decide,
} = createConversation(desk)
const statuses = {
  STREAMING: '生成中',
  COMPLETED: '已完成',
  FAILED: '生成失败',
  INTERRUPTED: '已中断',
  PROPOSED: '待确认',
  EXECUTED: '已执行',
  REJECTED: '已拒绝',
  EXPIRED: '已过期',
}
const priorities = { 1: '低', 2: '普通', 3: '高' }
</script>
<template>
  <template v-if="desk.session.user">
    <div class="page-heading">
      <div>
        <p class="eyebrow">AI WORKSPACE</p>
        <h1>AI 对话</h1>
        <p>与知识对话，让问题更快走向解决。</p>
      </div>
      <span class="badge"><UiIcon name="shield" :size="12" />操作由你确认</span>
    </div>
    <div class="ai-workspace">
      <aside class="panel conversation-sidebar" aria-label="对话管理">
        <div class="panel-title">
          <UiIcon name="chat" :size="18" />
          <h2>我的对话</h2>
        </div>
        <form @submit.prevent="create">
          <label
            >对话标题<input
              v-model="title"
              aria-label="对话标题"
              placeholder="开启一个新的话题" /></label
          ><button class="primary" :disabled="creating || !canUse" type="submit">
            <UiIcon name="plus" :size="15" />新建对话
          </button>
        </form>
        <p v-if="loading" role="status" class="loading-line">正在加载对话…</p>
        <p v-else-if="!conversations.length" class="meta conversation-note">
          暂无对话，请新建对话。
        </p>
        <ul aria-label="对话列表" class="conversation-list">
          <li v-for="conversation in conversations" :key="conversation.id">
            <button :aria-pressed="selected?.id === conversation.id" @click="select(conversation)">
              <UiIcon name="chat" :size="15" /><span>{{ conversation.title }}</span>
            </button>
          </li>
        </ul>
        <button class="conversation-refresh" :disabled="loading || creating" @click="loadList">
          刷新对话列表
        </button>
        <p class="conversation-note">
          仅显示最近 50 个对话，没有分页入口。当前组织内的对话仅自己可见。
        </p>
      </aside>
      <div class="conversation-thread">
        <p v-if="error" role="alert" class="error">{{ error }}</p>
        <p v-if="notice" role="status" class="notice">{{ notice }}</p>
        <template v-if="selected">
          <section class="thread-header">
            <div class="row">
              <h2>{{ selected.title }}</h2>
              <button :disabled="generating || historyLoading || !!actionBusy" @click="history()">
                重新读取历史
              </button>
            </div>
            <p class="meta">
              仅显示最近 100 条消息及最近 100
              个提议。历史引用保留当时的内容，请核对来源当前是否有效。
            </p>
            <p v-if="historyLoading" role="status" class="loading-line">正在读取历史…</p>
            <p v-if="hasUnfinished && !generating" class="notice">
              历史中有未完成生成，请稍后重新读取历史；未完成状态将在 120 秒后更新为已中断。
            </p>
          </section>
          <div class="thread-messages">
            <div v-if="!messages.length && !actions.length" class="empty-state">
              <UiIcon name="spark" /><strong>开始你的第一条消息</strong>
              <p>
                询问团队知识，或让 AI 为你整理工单建议。<br />所有工单提议都需要你确认后才会创建。
              </p>
            </div>
            <article
              v-for="message in messages"
              :key="message.id"
              class="message"
              :data-role="message.role"
            >
              <span class="message-avatar"
                ><UiIcon v-if="message.role !== 'USER'" name="spark" :size="16" /><template v-else
                  >你</template
                ></span
              >
              <div class="message-content">
                <h3>
                  {{ message.role === 'USER' ? '你' : 'Nexus AI'
                  }}<span>{{ statuses[message.status] }}</span>
                </h3>
                <pre class="body">{{ message.content }}</pre>
                <p v-if="message.model_name" class="meta">{{ message.model_name }}</p>
                <RAGSourceList :items="message.citations || []" :org-i-d="desk.orgID.value" />
              </div>
            </article>
            <article
              v-for="action in actions"
              :key="action.id"
              class="proposal-card"
              aria-label="工单提议"
            >
              <div class="row">
                <h3><UiIcon name="ticket" :size="17" />工单提议</h3>
                <span class="badge">{{ statuses[action.status] }}</span>
              </div>
              <p class="proposal-title">{{ action.arguments.title }}</p>
              <pre class="body">{{ action.arguments.body }}</pre>
              <p>
                <span class="priority" :data-priority="action.arguments.priority"
                  >优先级：{{ priorities[action.arguments.priority] }}</span
                >
              </p>
              <p class="meta">
                组织 {{ action.org_id }} · 提出者 {{ action.proposed_by_user_id }}<br />有效期至
                <time :datetime="action.expires_at" :title="action.expires_at">{{
                  dateTime(action.expires_at, true)
                }}</time>
              </p>
              <p class="meta">请核对提议内容。确认后创建工单，停止生成不会撤销已有提议。</p>
              <div v-if="action.status === 'PROPOSED'" class="actions">
                <button
                  class="primary"
                  :disabled="!!actionBusy || !canUse || historyLoading"
                  @click="decide(action, 'confirm')"
                >
                  确认创建工单</button
                ><button
                  :disabled="!!actionBusy || !canUse || historyLoading"
                  @click="decide(action, 'reject')"
                >
                  拒绝
                </button>
              </div>
              <RouterLink
                v-if="action.ticket_id"
                :to="`/organizations/${action.org_id}/tickets/${action.ticket_id}`"
                >查看工单 #{{ action.ticket_id }} →</RouterLink
              >
            </article>
          </div>
          <section class="message-composer">
            <form @submit.prevent="send">
              <label
                >消息<textarea
                  v-model="content"
                  aria-label="消息"
                  rows="3"
                  :disabled="generating"
                  placeholder="输入问题，或描述需要协作处理的事项…"
                ></textarea>
              </label>
              <div class="actions">
                <button v-if="generating" type="button" @click="stop">停止生成</button
                ><button
                  type="button"
                  @click="send"
                  class="primary"
                  :disabled="generating || historyLoading || hasUnfinished || !canUse"
                >
                  发送<UiIcon name="arrow" :size="14" />
                </button>
              </div>
            </form>
            <p v-if="generating" role="status" aria-live="polite" class="loading-line">正在生成…</p>
            <p class="meta">
              AI 的回答请结合来源核对。断流或停止后，请重新读取历史；不会自动重发消息。
            </p>
          </section>
        </template>
        <section v-else class="panel ai-empty">
          <div class="empty-state">
            <UiIcon name="spark" /><strong>你的团队知识助手</strong>
            <p>新建或选择一个对话，开始提问。<br />AI 可以提出工单建议，创建始终由你确认。</p>
          </div>
        </section>
      </div>
    </div>
  </template>
</template>
