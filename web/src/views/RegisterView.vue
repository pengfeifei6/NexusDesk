<script setup lang="ts">
import { useDesk } from '../composables/desk'
const { session, displayName, email, password, busy, register } = useDesk()
</script>
<template>
  <section v-if="!session.user" class="login">
    <p class="eyebrow">GET STARTED</p>
    <h1>注册账号</h1>
    <p>从这里开始，连接你的团队与知识。</p>
    <form @submit.prevent="register">
      <label
        >显示名称<input
          v-model="displayName"
          name="display_name"
          required
          autocomplete="nickname"
          placeholder="团队成员如何称呼你"
      /></label>
      <label
        >邮箱<input
          v-model="email"
          type="email"
          required
          autocomplete="username"
          placeholder="name@company.com"
      /></label>
      <label
        >密码<input
          v-model="password"
          type="password"
          required
          autocomplete="new-password"
          placeholder="设置登录密码"
      /></label>
      <p class="password-hint">密码需 15～128 个字符。</p>
      <button class="primary" :disabled="busy">注册</button>
    </form>
    <p class="auth-switch"><RouterLink to="/login">已有账号，返回登录</RouterLink></p>
  </section>
</template>
