<script setup lang="ts">
import { useDesk } from '../composables/desk'
const { session, email, password, busy, login } = useDesk()
</script>
<template>
  <section v-if="!session.user" class="login">
    <p class="eyebrow">WELCOME BACK</p>
    <h1>登录工作台</h1>
    <p>欢迎回来，继续与你的团队高效协作。</p>
    <form @submit.prevent="login">
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
          autocomplete="current-password"
          placeholder="请输入密码"
      /></label>
      <button class="primary" :disabled="busy">登录</button>
    </form>
    <p class="auth-switch">还没有账号？<RouterLink to="/register">注册账号</RouterLink></p>
  </section>
</template>
