import { computed, inject, onBeforeUnmount, ref, watch } from 'vue'
import type { InjectionKey } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, ApiError, Scope } from '../lib/client'
import type { KnowledgeDocument, DocumentText, KnowledgeJob } from '../lib/types'
import type { Desk } from './desk'

export function createKnowledge(desk: Desk) {
  const route = useRoute()
  const router = useRouter()
  const scope = new Scope()
  let active = scope.begin()
  let listSequence = 0
  const refreshedJobs = new Set<string>()
  const items = ref<KnowledgeDocument[]>([])
  const detail = ref<KnowledgeDocument | null>(null)
  const preview = ref<DocumentText | null>(null)
  const page = ref(1)
  const status = ref('')
  const busy = ref(false)
  const error = ref('')
  const file = ref<File | null>(null)
  const fileInputKey = ref(0)
  const submissionNotice = ref<{ documentID: string; message: string } | null>(null)
  const base = computed(() => `/organizations/${desk.orgID.value}/knowledge`)
  const endpoint = computed(() => `${base.value}/documents`)
  const documentID = computed(() => String(route.params.documentID || ''))
  const member = computed(() => desk.role.value === 'admin' || desk.role.value === 'member')
  const canDelete = computed(
    () =>
      member.value &&
      !!detail.value &&
      (desk.role.value === 'admin' || detail.value.uploaded_by === desk.session.user?.id),
  )
  const canProcess = computed(
    () => member.value && !!detail.value && ['UPLOADED', 'FAILED'].includes(detail.value.status),
  )
  function invalidatePermission(e: ApiError) {
    scope.cancel()
    listSequence++
    items.value = []
    detail.value = null
    preview.value = null
    busy.value = false
    error.value = e.message
  }
  function fail(e: unknown) {
    if (e instanceof ApiError && (e.status === 403 || e.status === 404)) invalidatePermission(e)
    error.value = e instanceof Error ? e.message : '请求失败，请重试'
  }
  async function loadList() {
    if (!active.current()) active = scope.begin()
    const run = active
    const seq = ++listSequence
    const query = new URLSearchParams({ page: String(page.value), page_size: '20' })
    if (status.value) query.set('status', status.value)
    const path = `${endpoint.value}?${query}`
    items.value = []
    error.value = ''
    busy.value = true
    try {
      const data = await api.request<{ items: KnowledgeDocument[] }>(path, { signal: run.signal })
      if (run.current() && seq === listSequence) items.value = data.items
    } catch (e) {
      if (run.current() && seq === listSequence) fail(e)
    } finally {
      if (run.current() && seq === listSequence) busy.value = false
    }
  }
  async function loadText(run: typeof active, path: string) {
    preview.value = null
    const data = await api.request<DocumentText>(`${path}/text`, { signal: run.signal })
    if (run.current()) preview.value = data
  }
  async function loadDetail() {
    if (!active.current()) active = scope.begin()
    const run = active
    const path = `${endpoint.value}/${documentID.value}`
    detail.value = null
    preview.value = null
    error.value = ''
    busy.value = true
    try {
      const data = await api.request<KnowledgeDocument>(path, { signal: run.signal })
      if (!run.current()) return
      detail.value = data
      if (data.status === 'READY') await loadText(run, path)
    } catch (e) {
      if (run.current()) fail(e)
    } finally {
      if (run.current()) busy.value = false
    }
  }
  function refreshCompletedJob(job: KnowledgeJob) {
    if (
      job.status !== 'SUCCEEDED' ||
      !desk.session.user ||
      job.org_id !== desk.orgID.value ||
      job.document_id !== documentID.value
    )
      return
    const identity = JSON.stringify([job.org_id, job.document_id, job.id, job.run_version])
    if (refreshedJobs.has(identity)) return
    refreshedJobs.add(identity)
    void loadDetail()
  }
  function chooseFile(event: Event) {
    file.value = (event.target as HTMLInputElement).files?.[0] || null
    error.value = ''
    if (file.value && file.value.size > 5242880) {
      error.value = '文件不能超过 5 MiB；服务器仍会检查实际文件大小'
      file.value = null
    }
  }
  async function upload(asyncProcess = false) {
    if (!file.value || busy.value) return
    if (file.value.size > 5242880) {
      error.value = '文件不能超过 5 MiB'
      return
    }
    const run = active
    const target = base.value
    const form = new FormData()
    form.append('file', file.value)
    error.value = ''
    busy.value = true
    try {
      const data = await api.request<KnowledgeDocument>(endpoint.value, {
        method: 'POST',
        body: form,
        signal: run.signal,
      })
      if (!run.current()) return
      file.value = null
      fileInputKey.value++
      submissionNotice.value = null
      if (asyncProcess) {
        try {
          await api.request(`${endpoint.value}/${data.id}/jobs`, {
            method: 'POST',
            signal: run.signal,
          })
        } catch (e) {
          if (!run.current()) return
          if (e instanceof ApiError && [401, 403, 404].includes(e.status)) {
            fail(e)
            return
          }
          submissionNotice.value = {
            documentID: data.id,
            message: '文档上传成功，异步任务未确认提交。请在下方提交异步处理，无需重复上传。',
          }
        }
      }
      if (!run.current()) return
      await router.push(`${target}/${data.id}`)
    } catch (e) {
      if (run.current()) fail(e)
    } finally {
      if (run.current()) busy.value = false
    }
  }
  async function process() {
    if (!canProcess.value || busy.value) return
    const run = active
    const path = `${endpoint.value}/${documentID.value}`
    busy.value = true
    error.value = ''
    preview.value = null
    try {
      const data = await api.request<KnowledgeDocument>(`${path}/process`, {
        method: 'POST',
        signal: run.signal,
      })
      if (!run.current()) return
      detail.value = data
      if (data.status === 'READY') await loadText(run, path)
    } catch (e) {
      if (!run.current()) return
      if (e instanceof ApiError && [409, 422, 504].includes(e.status)) {
        await loadDetail()
        if (!run.current()) return
      }
      fail(e)
    } finally {
      if (run.current()) busy.value = false
    }
  }
  async function remove() {
    if (!canDelete.value || busy.value) return
    const run = active
    const target = base.value
    busy.value = true
    error.value = ''
    try {
      await api.request(`${endpoint.value}/${documentID.value}`, {
        method: 'DELETE',
        signal: run.signal,
      })
      if (!run.current()) return
      detail.value = null
      preview.value = null
      await router.push(target)
    } catch (e) {
      if (run.current()) fail(e)
    } finally {
      if (run.current()) busy.value = false
    }
  }
  async function download() {
    if (!detail.value || busy.value) return
    const run = active
    const filename = detail.value.original_filename
    busy.value = true
    error.value = ''
    try {
      const blob = await api.request<Blob>(`${endpoint.value}/${documentID.value}/download`, {
        responseType: 'blob',
        signal: run.signal,
      })
      if (!run.current()) return
      const url = URL.createObjectURL(blob)
      const link = document.createElement('a')
      try {
        link.href = url
        link.download = filename
        document.body.appendChild(link)
        link.click()
      } finally {
        link.remove()
        URL.revokeObjectURL(url)
      }
    } catch (e) {
      if (run.current()) fail(e)
    } finally {
      if (run.current()) busy.value = false
    }
  }
  function filter() {
    page.value = 1
    void loadList()
  }
  function changePage(delta: number) {
    page.value = Math.max(1, page.value + delta)
    void loadList()
  }
  watch(
    () => [route.fullPath, desk.orgID.value, desk.session.epoch],
    () => {
      active = scope.begin()
      refreshedJobs.clear()
      items.value = []
      detail.value = null
      preview.value = null
      file.value = null
      fileInputKey.value++
      error.value = ''
      busy.value = false
      page.value = 1
      status.value = ''
      if (!desk.session.user || !route.path.startsWith(`${base.value}`)) return
      if (submissionNotice.value?.documentID !== documentID.value) submissionNotice.value = null
      if (documentID.value) void loadDetail()
      else if (route.path === base.value) void loadList()
    },
    { immediate: true, flush: 'sync' },
  )
  onBeforeUnmount(() => scope.cancel())
  return {
    items,
    detail,
    preview,
    page,
    status,
    busy,
    error,
    file,
    fileInputKey,
    submissionNotice,
    base,
    canDelete,
    canProcess,
    loadList,
    loadDetail,
    refreshCompletedJob,
    invalidatePermission,
    chooseFile,
    upload,
    process,
    remove,
    download,
    filter,
    changePage,
  }
}
export type Knowledge = ReturnType<typeof createKnowledge>
export const knowledgeKey: InjectionKey<Knowledge> = Symbol('knowledge')
export function useKnowledge() {
  const knowledge = inject(knowledgeKey)
  if (!knowledge) throw new Error('knowledge context missing')
  return knowledge
}
