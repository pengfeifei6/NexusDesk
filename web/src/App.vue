<script setup lang="ts">
import { provide } from 'vue'
import { createDesk, deskKey } from './composables/desk'
import { createKnowledge, knowledgeKey } from './composables/knowledge'
const desk = createDesk()
provide(deskKey, desk)
provide(knowledgeKey, createKnowledge(desk))
const {
  session,
  logout,
  orgID,
  selectOrg,
  orgs,
  selectedOrg,
  orgPage,
  busy,
  error,
  notice,
  retry,
  changePage,
} = desk
</script>
<template>
  <header>
    <RouterLink to="/desk" class="brand">NexusDesk</RouterLink><span>组织工单工作台</span>
    <div v-if="session.user" class="account">
      {{ session.user.display_name }} <button @click="logout">退出登录</button>
    </div>
  </header>
  <main>
    <section v-if="session.user" class="toolbar panel">
      <label
        >组织选择<select :value="orgID" @change="selectOrg">
          <option value="">请选择组织</option>
          <option
            v-if="selectedOrg && !orgs.some((o) => o.id === selectedOrg!.id)"
            :value="selectedOrg.id"
          >
            {{ selectedOrg.name }} · {{ selectedOrg.my_role }}
          </option>
          <option v-for="o in orgs" :key="o.id" :value="o.id">
            {{ o.name }} · {{ o.my_role }}
          </option>
        </select></label
      >
      <div>
        <button :disabled="orgPage === 1 || busy" @click="changePage('org', -1)">上一页组织</button
        ><span>第 {{ orgPage }} 页</span
        ><button :disabled="orgs.length < 20 || busy" @click="changePage('org', 1)">
          下一页组织
        </button>
      </div>
    </section>
    <nav v-if="session.user && orgID" aria-label="组织工作区" class="actions">
      <RouterLink :to="`/organizations/${orgID}/tickets`">工单</RouterLink>
      <RouterLink :to="`/organizations/${orgID}/knowledge`">知识库</RouterLink>
      <RouterLink :to="`/organizations/${orgID}/knowledge/search`">检索问答</RouterLink>
      <RouterLink to="/desk">创建组织</RouterLink>
    </nav>
    <p v-if="!session.user && session.expired" role="alert" class="error">登录已失效，请重新登录</p>
    <p v-if="busy" role="status">正在加载…</p>
    <div v-if="error" role="alert" class="error">
      {{ error }} <button v-if="session.user" @click="retry">重试</button>
    </div>
    <p v-if="notice" role="status" class="notice">{{ notice }}</p>
    <RouterView />
  </main>
  <footer>本地协作工作台 · 登录状态仅保存在内存，刷新后需重新登录</footer>
</template>
<style>
@import './assets/main.css';
</style>
