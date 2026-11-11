<script setup lang="ts">
import { useDesk } from '../composables/desk'
const { organizationName, busy, createOrganization } = useDesk()
</script>
<template>
  <section class="panel">
    <h1>选择组织，开始协作</h1>
    <p>选择已加入的组织，或创建自己的组织。创建者将成为该组织的管理员。</p>
    <form @submit.prevent="createOrganization">
      <label>组织名称<input v-model="organizationName" name="organization_name" required /></label>
      <button class="primary" :disabled="busy">创建组织</button>
    </form>
  </section>
</template>
