export function dateTime(value?: string | null, full = false): string {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat('zh-CN', {
    year: full ? 'numeric' : undefined,
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  }).format(date)
}
export function fileSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}
export const statusLabels: Record<string, string> = {
  OPEN: '待处理',
  IN_PROGRESS: '处理中',
  RESOLVED: '已解决',
  CLOSED: '已关闭',
  UPLOADED: '待提取',
  PROCESSING: '提取中',
  READY: '已就绪',
  FAILED: '失败',
  ABSENT: '尚未索引',
  INDEXING: '索引中',
  PENDING: '待执行',
  RUNNING: '执行中',
  RETRYING: '等待重试',
  SUCCEEDED: '已完成',
  CANCELED: '已取消',
}
export const eventLabels: Record<string, string> = {
  TICKET_CREATED: '创建工单',
  TICKET_UPDATED: '更新工单',
  TICKET_ASSIGNED: '指派受理人',
  TICKET_STATUS_CHANGED: '变更状态',
  TICKET_DELETED: '删除工单',
  TICKET_COMMENTED: '添加评论',
}
