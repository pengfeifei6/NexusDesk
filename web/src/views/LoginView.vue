<script setup lang="ts">
import { useDesk } from '../composables/desk'
const { session, email, password, busy, login } = useDesk()
</script>
<template>
  <section v-if="!session.user" class="login panel">
    <p class="eyebrow">欢迎回来</p>
    <h1>登录工作台</h1>
    <p>使用已注册账号访问你的组织工单。</p>
    <form @submit.prevent="login">
      <label>邮箱<input v-model="email" type="email" required autocomplete="username" /></label
      ><label
        >密码<input
          v-model="password"
          type="password"
          required
          autocomplete="current-password" /></label
      ><button class="primary" :disabled="busy">登录</button>
    </form>
    <p>还没有账号？<RouterLink to="/register">注册账号</RouterLink></p>
  </section>
</template>
