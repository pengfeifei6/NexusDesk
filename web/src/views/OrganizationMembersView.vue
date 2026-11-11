<script setup lang="ts">
import { useDesk } from '../composables/desk'
import { createMembers } from '../composables/members'
import UiIcon from '../components/UiIcon.vue'
import { dateTime } from '../lib/presentation'
const desk = useDesk()
const { items, page, busy, error, notice, userID, newRole, canAdd, load, add, changePage } =
  createMembers(desk)
</script>
<template>
  <div class="page-heading">
    <div>
      <p class="eyebrow">TEAM MEMBERS</p>
      <h1>组织成员</h1>
      <p>查看团队成员，为已注册的同事开通组织访问。</p>
    </div>
    <button :disabled="busy" @click="load">刷新成员</button>
  </div>
  <p v-if="error" role="alert" class="error">{{ error }}</p>
  <p v-if="notice" role="status" class="notice">{{ notice }}</p>
  <section class="panel">
    <div class="list-heading">
      <div>
        <span class="section-icon"><UiIcon name="members" :size="18" /></span>
        <h2>团队成员</h2>
        <span class="count-pill">当前页 {{ items.length }}</span>
      </div>
      <span class="meta">共同协作，有序前行</span>
    </div>
    <p v-if="busy" role="status">正在加载成员…</p>
    <p v-else-if="!items.length" class="meta">本页暂无可显示的成员。</p>
    <div class="table-scroll">
      <table v-if="items.length" class="member-table">
        <thead>
          <tr>
            <th>成员</th>
            <th>角色</th>
            <th>状态</th>
            <th>加入时间</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in items" :key="item.user_id">
            <td>
              <strong>{{ item.display_name }}</strong
              ><small class="meta member-id">账号 #{{ item.user_id }}</small>
            </td>
            <td>{{ item.role === 'admin' ? '管理员' : '成员' }}</td>
            <td>{{ item.status === 'active' ? '正常' : '已停用' }}</td>
            <td>{{ dateTime(item.created_at) }}</td>
          </tr>
        </tbody>
      </table>
    </div>
    <div class="paging">
      <button :disabled="busy || page === 1" @click="changePage(-1)">上一页成员</button
      ><span>第 {{ page }} 页</span
      ><button :disabled="busy || items.length < 20" @click="changePage(1)">下一页成员</button>
    </div>
  </section>
  <section v-if="canAdd" class="panel form-section">
    <h2>添加已注册成员</h2>
    <p class="meta">请同事注册并在组织工作台查看自己的账号编号，再将编号提供给你。</p>
    <form class="member-form" @submit.prevent="add">
      <div class="form-grid">
        <label
          >账号编号<input
            v-model="userID"
            name="member_user_id"
            inputmode="numeric"
            required
            maxlength="19"
            autocomplete="off"
            placeholder="同事的账号编号" /></label
        ><label
          >初始角色<select v-model="newRole" name="member_role">
            <option value="member">成员</option>
            <option value="admin">管理员</option>
          </select></label
        >
      </div>
      <p v-if="newRole === 'admin'" class="meta">
        管理员可以添加成员、指派和删除工单，请确认授权范围。
      </p>
      <button class="primary" :disabled="busy">添加成员</button>
    </form>
  </section>
</template>
