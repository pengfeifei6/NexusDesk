<script setup lang="ts">
import RAGIndexPanel from '../components/RAGIndexPanel.vue'
import AsyncJobPanel from '../components/AsyncJobPanel.vue'
import { useKnowledge } from '../composables/knowledge'
const {
  detail,
  preview,
  submissionNotice,
  busy,
  error,
  base,
  canDelete,
  canProcess,
  loadDetail,
  refreshCompletedJob,
  process,
  remove,
  download,
} = useKnowledge()
const failures: Record<string, string> = {
  INVALID_TEXT: '文档正文不是有效文本',
  EMPTY_TEXT: '文档没有可提取的正文',
  TEXT_TOO_LARGE: '提取正文超过大小限制',
  PDF_UNSUPPORTED: 'PDF 暂不支持文本提取，可下载原文件',
  PROCESS_TIMEOUT: '文档处理超时，可以重试',
  SOURCE_UNAVAILABLE: '原文件暂时不可用',
  SOURCE_CHANGED: '原文件校验失败',
  PROCESS_CANCELED: '文档处理已取消，可以重试',
  PROCESS_FAILED: '文档处理失败，可以重试',
}
</script>
<template>
  <RouterLink :to="base">← 返回知识库列表</RouterLink>
  <p v-if="error" role="alert" class="error">{{ error }}</p>
  <p v-if="busy" role="status">正在处理文档请求…</p>
  <section v-if="detail" class="panel detail">
    <div class="row">
      <h1>{{ detail.original_filename }}</h1>
      <span class="badge">{{ detail.status }}</span>
    </div>
    <p class="meta">
      文档 #{{ detail.id }} · 上传者 {{ detail.uploaded_by }} · {{ detail.media_type }} ·
      {{ detail.size_bytes }} 字节
    </p>
    <p class="meta">
      上传 {{ detail.created_at }} · 更新 {{ detail.updated_at }} · 已提取
      {{ detail.extracted_chars }} 字符
    </p>
    <p v-if="detail.failure_code" class="notice">
      {{
        Object.prototype.hasOwnProperty.call(failures, detail.failure_code)
          ? failures[detail.failure_code]
          : '文档处理失败，请重试'
      }}
    </p>
    <p v-else-if="detail.media_type === 'application/pdf'">PDF 暂不支持文本提取，可下载原文件。</p>
    <p v-if="detail.status === 'PROCESSING'">
      文档正在处理中，请稍后刷新。若长时间未完成，请联系管理员。
    </p>
    <div class="actions">
      <button v-if="canProcess" class="primary" :disabled="busy" @click="process">提取正文</button
      ><button :disabled="busy" @click="download">下载原文件</button
      ><button v-if="canDelete" :disabled="busy" @click="remove">删除文档</button
      ><button :disabled="busy" @click="loadDetail">刷新文档</button>
    </div>
  </section>
  <p v-if="detail && submissionNotice?.documentID === detail.id" class="notice">
    {{ submissionNotice.message }}
  </p>
  <RAGIndexPanel v-if="detail" :document="detail" />
  <AsyncJobPanel v-if="detail" :document="detail" @completed="refreshCompletedJob" />
  <section v-if="detail && preview" class="panel">
    <h2>正文预览</h2>
    <p class="meta">{{ preview.extractor_version }} · {{ preview.extracted_at }}</p>
    <pre class="body">{{ preview.content_text }}</pre>
  </section>
  <button v-if="!detail && !busy" @click="loadDetail">重试文档</button>
</template>
