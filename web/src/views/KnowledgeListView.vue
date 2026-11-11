<script setup lang="ts">
import { focusSection } from '../lib/focus-section'
import { useKnowledge } from '../composables/knowledge'
import UiIcon from '../components/UiIcon.vue'
import StatusBadge from '../components/StatusBadge.vue'
import { dateTime, fileSize } from '../lib/presentation'
const {
  items,
  page,
  status,
  busy,
  error,
  file,
  fileInputKey,
  base,
  loadList,
  chooseFile,
  upload,
  filter,
  changePage,
} = useKnowledge()
</script>
<template>
  <div class="page-heading">
    <div>
      <p class="eyebrow">KNOWLEDGE LIBRARY</p>
      <h1>知识库文档</h1>
      <p>沉淀团队经验，为每一次解答提供依据。</p>
    </div>
    <button :disabled="busy" @click="loadList">刷新文档列表</button>
  </div>
  <p v-if="error" role="alert" class="error">{{ error }}</p>
  <section class="library-intro">
    <span class="library-symbol"><UiIcon name="book" :size="24" /></span>
    <div>
      <strong>把团队经验，变成可用的知识</strong>
      <p>上传文档，交给异步任务处理，再通过检索找到可靠依据。</p>
    </div>
    <a href="#document-upload" @click.prevent="focusSection('document-upload')" class="text-action"
      >上传新文档 <UiIcon name="arrow" :size="15"
    /></a>
  </section>
  <section class="panel table-panel">
    <div class="list-heading">
      <div>
        <UiIcon name="file" :size="18" />
        <h2>文档资源</h2>
        <span class="count-pill">当前页 {{ items.length }}</span>
      </div>
      <span class="meta">第 {{ page }} 页</span>
    </div>
    <div class="filters">
      <label
        >文档状态筛选<select v-model="status" aria-label="文档状态筛选" @change="filter">
          <option value="">全部状态</option>
          <option value="UPLOADED">待提取</option>
          <option value="PROCESSING">提取中</option>
          <option value="READY">已就绪</option>
          <option value="FAILED">失败</option>
        </select></label
      >
    </div>
    <p v-if="busy" role="status" class="loading-line">正在加载文档…</p>
    <div v-else-if="!items.length" class="empty-state">
      <UiIcon name="book" /><strong>暂无文档</strong>
      <p>上传团队文档，开始积累你的组织知识库。</p>
    </div>
    <div v-if="items.length" class="table-scroll">
      <table class="data-table">
        <thead>
          <tr>
            <th scope="col">文档名称</th>
            <th scope="col">状态</th>
            <th scope="col">大小</th>
            <th scope="col">上传者</th>
            <th scope="col">更新时间</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in items" :key="item.id">
            <td class="title-cell">
              <RouterLink class="table-title" :to="`${base}/${item.id}`"
                ><UiIcon name="file" :size="20" /><span
                  >{{ item.original_filename }}<small>文档 #{{ item.id }}</small></span
                ></RouterLink
              >
            </td>
            <td><StatusBadge :status="item.status" /></td>
            <td>{{ fileSize(item.size_bytes) }}</td>
            <td>成员 {{ item.uploaded_by }}</td>
            <td>
              <time :datetime="item.updated_at" :title="item.updated_at">{{
                dateTime(item.updated_at)
              }}</time>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <div class="paging">
      <button :disabled="page === 1 || busy" @click="changePage(-1)">上一页文档</button
      ><span>第 {{ page }} 页</span
      ><button :disabled="items.length < 20 || busy" @click="changePage(1)">下一页文档</button>
    </div>
  </section>
  <section id="document-upload" class="panel upload-panel" tabindex="-1">
    <div>
      <div class="panel-title">
        <UiIcon name="file" :size="18" />
        <h2>上传文档</h2>
      </div>
      <p>支持 TXT、MD 和 PDF，最大 5 MiB。<br />PDF 可保存和下载，暂不支持文本提取。</p>
    </div>
    <form @submit.prevent="upload(false)">
      <label
        >选择文件<input
          :key="fileInputKey"
          type="file"
          accept=".txt,.md,.pdf"
          required
          :disabled="busy"
          @change="chooseFile"
      /></label>
      <div class="actions">
        <button :disabled="busy || !file">上传文档</button
        ><button type="button" class="primary" :disabled="busy || !file" @click="upload(true)">
          上传并异步处理<UiIcon name="arrow" :size="15" />
        </button>
      </div>
    </form>
  </section>
</template>
