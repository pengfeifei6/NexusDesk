<script setup lang="ts">
import { useDesk } from '../composables/desk'
import UiIcon from '../components/UiIcon.vue'
const { organizationName, busy, createOrganization, session, orgs } = useDesk()
</script>
<template>
  <div class="organization-welcome">
    <div class="welcome-hero">
      <div class="welcome-copy">
        <p class="eyebrow">A SPACE FOR GOOD WORK</p>
        <h1>你好，{{ session.user?.display_name }}。<br />让协作，从容发生。</h1>
        <p>连接团队的问题、经验与每一次新想法。<br />选择一个组织，开始今天的工作。</p>
        <span class="welcome-label"
          ><UiIcon name="shield" :size="14" />独立组织空间 · 清晰协作边界</span
        >
      </div>
      <div class="workspace-art" aria-hidden="true">
        <div class="art-orbit"></div>
        <span class="art-node art-node-ticket"><UiIcon name="ticket" :size="25" /></span
        ><span class="art-node art-node-book"><UiIcon name="book" :size="25" /></span
        ><span class="art-node art-node-spark"><UiIcon name="spark" :size="30" /></span
        ><span class="art-center">N<span>.</span></span>
      </div>
    </div>
    <section v-if="orgs.length" class="organization-directory" aria-label="当前页组织">
      <div class="section-heading">
        <h2>你的工作空间</h2>
        <span class="meta">当前页组织 · 更多组织可通过左侧分页查看</span>
      </div>
      <div class="organization-grid">
        <RouterLink
          v-for="org in orgs"
          :key="org.id"
          :to="`/organizations/${org.id}/tickets`"
          class="organization-card"
          ><span class="org-avatar">{{ org.name.slice(0, 1) }}</span
          ><span class="org-card-copy"
            ><strong>{{ org.name }}</strong
            ><small>{{ org.my_role === 'admin' ? '组织管理员' : '组织成员' }}</small></span
          ><UiIcon name="arrow" :size="18"
        /></RouterLink>
      </div>
    </section>
    <p class="account-id">
      我的账号编号：<strong>{{ session.user?.id }}</strong> · 请将此编号提供给组织管理员添加成员。
    </p>
    <section class="panel org-create-panel">
      <div>
        <span class="intro-icon"><UiIcon name="plus" :size="24" /></span>
        <h2>创建一个组织</h2>
        <p>为团队建立独立的协作空间。<br />创建后，你将成为组织管理员。</p>
      </div>
      <form @submit.prevent="createOrganization">
        <label
          >组织名称<input
            v-model="organizationName"
            name="organization_name"
            required
            placeholder="例如：产品与客户支持团队" /></label
        ><button class="primary" :disabled="busy">
          创建组织<UiIcon name="arrow" :size="15" />
        </button>
      </form>
    </section>
    <p class="meta">
      {{
        orgs.length
          ? '已有组织？通过左侧组织选择，进入对应工作空间。'
          : '尚未加入组织？创建组织，或联系管理员添加你的账号。'
      }}
    </p>
  </div>
</template>
