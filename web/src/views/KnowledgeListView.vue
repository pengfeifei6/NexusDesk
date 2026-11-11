<script setup lang="ts">
import { useKnowledge } from '../composables/knowledge'
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
  <p v-if="error" role="alert" class="error">{{ error }}</p>
  <section class="panel">
    <div class="row">
      <h1>知识库文档</h1>
      <button :disabled="busy" @click="loadList">刷新文档列表</button>
    </div>
    <label
      >文档状态筛选<select v-model="status" aria-label="文档状态筛选" @change="filter">
        <option value="">全部状态</option>
        <option>UPLOADED</option>
        <option>PROCESSING</option>
        <option>READY</option>
        <option>FAILED</option>
      </select></label
    >
    <p v-if="busy" role="status">正在加载文档…</p>
    <p v-else-if="!items.length">暂无文档</p>
    <ul class="tickets">
      <li v-for="item in items" :key="item.id">
        <RouterLink :to="`${base}/${item.id}`">{{ item.original_filename }}</RouterLink>
        <span>
          {{ item.status }} · 上传者 {{ item.uploaded_by }} · {{ item.size_bytes }} 字节 · 更新
          {{ item.updated_at }}
        </span>
      </li>
    </ul>
    <div class="paging">
      <button :disabled="page === 1 || busy" @click="changePage(-1)">上一页文档</button
      ><span>第 {{ page }} 页</span
      ><button :disabled="items.length < 20 || busy" @click="changePage(1)">下一页文档</button>
    </div>
  </section>
  <section class="panel">
    <h2>上传文档</h2>
    <p>支持 TXT、MD 和 PDF，最大 5 MiB。PDF 可保存和下载，暂不支持文本提取。</p>
    <form @submit.prevent="upload(false)">
      <label
        >选择文件<input
          :key="fileInputKey"
          type="file"
          accept=".txt,.md,.pdf"
          required
          :disabled="busy"
          @change="chooseFile" /></label
      ><button class="primary" :disabled="busy || !file">上传文档</button>
      <button type="button" :disabled="busy || !file" @click="upload(true)">上传并异步处理</button>
    </form>
  </section>
</template>
