<script setup lang="ts">
import { computed, nextTick, provide, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { createDesk, deskKey } from './composables/desk'
import { createKnowledge, knowledgeKey } from './composables/knowledge'
import UiIcon from './components/UiIcon.vue'
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
const route = useRoute()
const navigationOpen = ref(false)
const navigationTrigger = ref<HTMLButtonElement | null>(null)
async function closeNavigation() {
  if (!navigationOpen.value) return
  navigationOpen.value = false
  await nextTick()
  navigationTrigger.value?.focus()
}
watch(
  () => route.fullPath,
  () => {
    navigationOpen.value = false
  },
)
const pageName = computed(() => {
  if (route.path.includes('/knowledge/search')) return '检索问答'
  if (route.path.includes('/knowledge')) return route.params.documentID ? '文档详情' : '知识库'
  if (route.path.includes('/tickets')) return route.params.ticketID ? '工单详情' : '工单'
  if (route.path.endsWith('/members')) return '组织成员'
  if (route.path.includes('/ai')) return 'AI 对话'
  return '组织工作台'
})
const links = computed(() =>
  orgID.value
    ? [
        { label: '工单', icon: 'ticket', path: `/organizations/${orgID.value}/tickets` },
        { label: '知识库', icon: 'book', path: `/organizations/${orgID.value}/knowledge` },
        {
          label: '检索问答',
          icon: 'search',
          path: `/organizations/${orgID.value}/knowledge/search`,
        },
        { label: '组织成员', icon: 'members', path: `/organizations/${orgID.value}/members` },
        { label: 'AI 对话', icon: 'spark', path: `/organizations/${orgID.value}/ai` },
      ]
    : [],
)
function active(path: string) {
  return (
    route.path === path ||
    (!path.endsWith('/knowledge') && route.path.startsWith(path + '/')) ||
    (path.endsWith('/knowledge') && !!route.params.documentID)
  )
}
</script>
<template>
  <a class="skip-link" href="#main-content">跳至主要内容</a>
  <div v-if="session.user" class="app-shell" @keydown.esc="closeNavigation">
    <button
      v-if="navigationOpen"
      class="nav-scrim"
      aria-label="关闭导航"
      @click="closeNavigation"
    />
    <aside id="workspace-navigation" class="sidebar" :class="{ 'is-open': navigationOpen }">
      <RouterLink to="/desk" class="brand"
        ><span class="brand-mark"><UiIcon name="grid" :size="19" /></span
        ><span class="brand-copy"
          >NexusDesk<span class="brand-dot">.</span><small>TEAM WORKSPACE</small></span
        ></RouterLink
      >
      <div class="workspace-selector">
        <div class="workspace-selector-heading">
          <span class="workspace-monogram">{{ (selectedOrg?.name || 'N').slice(0, 1) }}</span
          ><span>团队工作空间<small>ORGANIZATION</small></span>
        </div>
        <label
          >组织选择<select :value="orgID" aria-label="组织选择" @change="selectOrg">
            <option value="">请选择组织</option>
            <option
              v-if="selectedOrg && !orgs.some((o) => o.id === selectedOrg!.id)"
              :value="selectedOrg.id"
            >
              {{ selectedOrg.name }} · {{ selectedOrg.my_role === 'admin' ? '管理员' : '成员' }}
            </option>
            <option v-for="o in orgs" :key="o.id" :value="o.id">
              {{ o.name }} · {{ o.my_role === 'admin' ? '管理员' : '成员' }}
            </option>
          </select></label
        >
        <div class="org-paging">
          <button :disabled="orgPage === 1 || busy" @click="changePage('org', -1)">
            上一页组织</button
          ><span>第 {{ orgPage }} 页</span
          ><button :disabled="orgs.length < 20 || busy" @click="changePage('org', 1)">
            下一页组织
          </button>
        </div>
      </div>
      <p class="nav-caption">工作空间</p>
      <nav aria-label="组织工作区" class="workspace-nav">
        <RouterLink
          v-for="link in links"
          :key="link.path"
          :to="link.path"
          :class="{ 'is-active': active(link.path) }"
          :aria-current="active(link.path) ? 'page' : undefined"
          ><UiIcon :name="link.icon" :size="18" /><span>{{ link.label }}</span></RouterLink
        >
        <RouterLink to="/desk" :class="{ 'is-active': route.path === '/desk' }"
          ><UiIcon name="plus" /><span>创建组织</span></RouterLink
        >
      </nav>
      <div class="sidebar-bottom">
        <span class="sidebar-note-icon"><UiIcon name="spark" :size="18" /></span>
        <div><strong>让知识持续生长</strong><span>每一次协作，都有据可循</span></div>
        <small>NEXUSDESK · WORK TOGETHER</small>
      </div>
    </aside>
    <div class="workspace-main">
      <header class="app-header">
        <button
          ref="navigationTrigger"
          class="mobile-menu icon-button"
          :aria-expanded="navigationOpen"
          aria-label="打开导航"
          aria-controls="workspace-navigation"
          @click="navigationOpen = !navigationOpen"
        >
          <UiIcon name="menu" />
        </button>
        <div class="breadcrumb">
          <UiIcon name="layers" :size="17" /> <span>{{ selectedOrg?.name || 'NexusDesk' }}</span
          ><UiIcon name="chevron" :size="14" /><strong>{{ pageName }}</strong>
        </div>
        <div class="account">
          <span class="avatar">{{ session.user.display_name.slice(0, 1) }}</span
          ><span class="account-name"
            >{{ session.user.display_name
            }}<small>{{ desk.role.value === 'admin' ? '组织管理员' : '工作台账户' }}</small></span
          ><button class="logout" @click="logout">退出登录</button>
        </div>
      </header>
      <main id="main-content" class="workspace-content" tabindex="-1">
        <p v-if="busy" role="status" class="loading-line">正在加载…</p>
        <div v-if="error" role="alert" class="error">
          {{ error }} <button @click="retry">重试</button>
        </div>
        <p v-if="notice" role="status" class="notice">{{ notice }}</p>
        <RouterView />
      </main>
      <footer class="workspace-footer">NexusDesk <span>·</span> 为专注的团队而设计</footer>
    </div>
  </div>
  <div v-else class="auth-shell">
    <aside class="auth-story">
      <RouterLink to="/login" class="brand"
        ><span class="brand-mark"><UiIcon name="grid" :size="19" /></span
        ><span class="brand-copy"
          >NexusDesk<span class="brand-dot">.</span><small>TEAM WORKSPACE</small></span
        ></RouterLink
      >
      <div class="auth-story-content">
        <span class="auth-kicker"><span></span> YOUR TEAM, CONNECTED</span>
        <h2>每一次协作，<br />都成为下一次<br /><em>更好的开始。</em></h2>
        <p>把问题、知识与团队连接在一起。<br />让专注的工作，自然发生。</p>
        <div class="auth-preview" aria-hidden="true">
          <div class="preview-topline">
            <span class="preview-symbol"><UiIcon name="ticket" :size="17" /></span
            ><span>从问题，到解决</span><span class="preview-dots">···</span>
          </div>
          <div class="preview-ticket">
            <span class="preview-check"><UiIcon name="check" :size="14" /></span>
            <div><strong>把团队经验，留在团队里</strong><small>清晰流转 · 有序沉淀</small></div>
            <span class="preview-avatar">N</span>
          </div>
          <div class="preview-flow">
            <span><UiIcon name="ticket" :size="14" />工单协作</span
            ><UiIcon name="arrow" :size="14" /><span><UiIcon name="book" :size="14" />知识积累</span
            ><UiIcon name="arrow" :size="14" /><span
              ><UiIcon name="spark" :size="14" />智能解答</span
            >
          </div>
        </div>
        <div class="auth-features">
          <span><UiIcon name="shield" :size="14" />组织隔离</span
          ><span><UiIcon name="book" :size="14" />经验沉淀</span
          ><span><UiIcon name="spark" :size="14" />受控 AI</span>
        </div>
      </div>
      <p class="auth-story-footer">Designed for clarity. Built for your team.</p>
    </aside>
    <main id="main-content" class="auth-main" tabindex="-1">
      <div class="auth-form-area">
        <span class="auth-form-symbol" aria-hidden="true"
          ><UiIcon :name="route.path === '/register' ? 'plus' : 'arrow'" :size="23"
        /></span>
        <p v-if="session.expired" role="alert" class="error">登录已失效，请重新登录</p>
        <p v-if="busy" role="status" class="loading-line">正在加载…</p>
        <p v-if="error" role="alert" class="error">{{ error }}</p>
        <p v-if="notice" role="status" class="notice">{{ notice }}</p>
        <RouterView />
      </div>
      <footer>你的团队，连接于此。</footer>
    </main>
  </div>
</template>
<style>
@import './assets/main.css';
@import './assets/workspace-theme.css';
</style>
