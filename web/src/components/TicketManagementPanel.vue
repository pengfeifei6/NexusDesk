<script setup lang="ts">
import { useDesk } from '../composables/desk'
import { createTicketManagement } from '../composables/ticket-management'
const desk = useDesk()
const {
  mode,
  title,
  body,
  priority,
  assignee,
  members,
  memberPage,
  memberCount,
  busy,
  membersBusy,
  error,
  notice,
  canEdit,
  canAssign,
  canDelete,
  open,
  close,
  submit,
  changePage,
} = createTicketManagement(desk)
</script>
<template>
  <section v-if="canEdit || canAssign || canDelete" class="ticket-management">
    <div class="actions">
      <button v-if="canEdit" :disabled="busy || desk.busy.value" @click="open('edit')">
        编辑工单</button
      ><button v-if="canAssign" :disabled="busy || desk.busy.value" @click="open('assign')">
        指派受理人</button
      ><button
        v-if="canDelete"
        class="danger"
        :disabled="busy || desk.busy.value"
        @click="open('delete')"
      >
        删除工单
      </button>
    </div>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <p v-if="notice" class="notice" role="status">{{ notice }}</p>
    <form v-if="mode === 'edit'" class="ticket-edit-form management-form" @submit.prevent="submit">
      <h2>编辑工单</h2>
      <div class="form-grid">
        <label>工单标题<input v-model="title" name="edit_title" required maxlength="200" /></label
        ><label
          >优先级<select v-model="priority">
            <option :value="1">低</option>
            <option :value="2">普通</option>
            <option :value="3">高</option>
          </select></label
        ><label class="full"
          >工单描述<textarea v-model="body" name="edit_body" required maxlength="10000" />
        </label>
      </div>
      <div class="actions">
        <button class="primary" :disabled="busy">保存修改</button
        ><button type="button" :disabled="busy" @click="close">取消编辑</button>
      </div>
    </form>
    <form
      v-if="mode === 'assign'"
      class="ticket-assign-form management-form"
      @submit.prevent="submit"
    >
      <h2>指派受理人</h2>
      <p class="meta">候选成员按页展示，仅可选择本组织正常成员；翻页会清空当前选择。</p>
      <label
        >受理人<select
          v-model="assignee"
          name="assignee_id"
          :disabled="membersBusy || busy"
          required
        >
          <option value="" disabled>请选择受理人</option>
          <option value="unassigned">解除指派</option>
          <option v-for="m in members" :key="m.user_id" :value="m.user_id">
            {{ m.display_name }} · #{{ m.user_id }}
          </option>
        </select></label
      >
      <div class="paging">
        <button
          type="button"
          :disabled="busy || membersBusy || memberPage === 1"
          @click="changePage(-1)"
        >
          上一页候选成员</button
        ><span>第 {{ memberPage }} 页</span
        ><button
          type="button"
          :disabled="busy || membersBusy || memberCount < 20"
          @click="changePage(1)"
        >
          下一页候选成员
        </button>
      </div>
      <div class="actions">
        <button class="primary" :disabled="busy || membersBusy || !assignee">保存指派</button
        ><button type="button" :disabled="busy" @click="close">取消指派</button>
      </div>
    </form>
    <div
      v-if="mode === 'delete'"
      class="delete-confirm management-form"
      role="group"
      aria-labelledby="delete-ticket-heading"
    >
      <h2 id="delete-ticket-heading">确认删除此工单</h2>
      <p>#{{ desk.detail.value?.id }} · {{ desk.detail.value?.title }}</p>
      <p>删除后，工单及其评论将从工作区隐藏，审计记录保留。请确认选择正确。</p>
      <div class="actions">
        <button class="danger" :disabled="busy" @click="submit">确认删除</button
        ><button :disabled="busy" @click="close">取消删除</button>
      </div>
    </div>
  </section>
</template>
