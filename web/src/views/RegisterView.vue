<script setup lang="ts">
import { useDesk } from '../composables/desk'
const { session, displayName, email, password, busy, register } = useDesk()
</script>
<template>
  <section v-if="!session.user" class="login panel">
    <p class="eyebrow">开始使用 NexusDesk</p>
    <h1>注册账号</h1>
    <p>注册后请先登录，再创建或加入组织。</p>
    <form @submit.prevent="register">
      <label
        >显示名称<input v-model="displayName" name="display_name" required autocomplete="nickname"
      /></label>
      <label>邮箱<input v-model="email" type="email" required autocomplete="username" /></label>
      <label
        >密码<input v-model="password" type="password" required autocomplete="new-password"
      /></label>
      <p>密码需 15～128 个字符。</p>
      <button class="primary" :disabled="busy">注册</button>
    </form>
    <RouterLink to="/login">已有账号，返回登录</RouterLink>
  </section>
</template>
