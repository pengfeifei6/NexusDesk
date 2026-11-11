import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { Desk } from './desk'
import type { Member } from '../lib/types'
import { api, ApiError, Scope } from '../lib/client'

export function createTicketManagement(desk: Desk) {
  const route = useRoute(),
    router = useRouter(),
    scope = new Scope()
  let active = scope.begin(),
    memberSequence = 0
  const mode = ref<'edit' | 'assign' | 'delete' | ''>('')
  const title = ref(''),
    body = ref(''),
    priority = ref(2),
    version = ref('')
  const assignee = ref(''),
    members = ref<Member[]>([]),
    memberPage = ref(1),
    memberCount = ref(0)
  const busy = ref(false),
    membersBusy = ref(false),
    error = ref(''),
    notice = ref('')
  const isMember = computed(() => ['admin', 'member'].includes(desk.role.value || ''))
  const canEdit = computed(
    () =>
      isMember.value &&
      desk.detail.value?.status === 'OPEN' &&
      (desk.role.value === 'admin' || desk.detail.value.creator_id === desk.session.user?.id),
  )
  const canAssign = computed(
    () => desk.role.value === 'admin' && desk.detail.value?.status === 'OPEN',
  )
  const canDelete = computed(() => desk.role.value === 'admin' && !!desk.detail.value)
  function close() {
    mode.value = ''
    title.value = ''
    body.value = ''
    assignee.value = ''
    members.value = []
    memberSequence++
  }
  onBeforeUnmount(() => scope.cancel())
  watch(
    () => [route.fullPath, desk.session.epoch, desk.accessEpoch.value],
    () => {
      active = scope.begin()
      close()
      error.value = ''
      notice.value = ''
      busy.value = false
      membersBusy.value = false
    },
    { flush: 'sync' },
  )
  watch(
    () => [desk.detail.value, desk.role.value],
    () => close(),
    { flush: 'sync' },
  )
  async function loadMembers() {
    const run = active,
      seq = ++memberSequence
    membersBusy.value = true
    members.value = []
    assignee.value = ''
    error.value = ''
    try {
      const data = await api.request<{ items: Member[] }>(
        `/organizations/${desk.orgID.value}/members?page=${memberPage.value}&page_size=20`,
        { signal: run.signal },
      )
      if (run.current() && seq === memberSequence) {
        memberCount.value = data.items.length
        members.value = data.items.filter((m) => m.status === 'active')
      }
    } catch (e) {
      if (run.current() && seq === memberSequence) fail(e)
    } finally {
      if (run.current() && seq === memberSequence) membersBusy.value = false
    }
  }
  function open(next: 'edit' | 'assign' | 'delete') {
    if (
      busy.value ||
      desk.busy.value ||
      !desk.detail.value ||
      !(next === 'edit' ? canEdit.value : next === 'assign' ? canAssign.value : canDelete.value)
    )
      return
    const d = desk.detail.value
    close()
    mode.value = next
    version.value = d.version
    title.value = d.title
    body.value = d.body
    priority.value = d.priority
    error.value = ''
    notice.value = ''
    if (next === 'assign') {
      memberPage.value = 1
      void loadMembers()
    }
  }
  function fail(e: unknown) {
    if (e instanceof ApiError && [401, 403, 404].includes(e.status)) {
      close()
      members.value = []
      desk.invalidateAccess()
    }
    error.value = e instanceof Error ? e.message : '工单操作失败，请重试'
    // The panel unmounts when access is lost, so keep its safe message globally.
    if (!desk.detail.value) desk.error.value = error.value
  }
  async function submit() {
    const action = mode.value
    if (
      busy.value ||
      desk.busy.value ||
      !desk.detail.value ||
      !action ||
      !(action === 'edit' ? canEdit.value : action === 'assign' ? canAssign.value : canDelete.value)
    )
      return
    if (
      action === 'assign' &&
      (membersBusy.value ||
        !assignee.value ||
        (assignee.value !== 'unassigned' &&
          !members.value.some((m) => m.user_id === assignee.value)))
    )
      return
    const run = active,
      base = desk.base.value,
      endpoint = `${base}/${desk.ticketID.value}`
    busy.value = true
    error.value = ''
    notice.value = ''
    try {
      await api.request(endpoint, {
        method: action === 'delete' ? 'DELETE' : 'PATCH',
        signal: run.signal,
        ...(action === 'delete'
          ? {}
          : {
              body:
                action === 'edit'
                  ? {
                      expected_version: version.value,
                      title: title.value,
                      body: body.value,
                      priority: priority.value,
                    }
                  : {
                      expected_version: version.value,
                      assignee_id: assignee.value === 'unassigned' ? null : assignee.value,
                    },
            }),
      })
      if (!run.current()) return
      close()
      if (action === 'delete') {
        desk.detail.value = null
        await router.push(base)
      } else {
        await desk.load()
        if (run.current()) notice.value = '工单已更新。'
      }
    } catch (e) {
      if (!run.current()) return
      if (e instanceof ApiError && e.status === 409) {
        close()
        await desk.load()
        if (run.current())
          notice.value = desk.error.value
            ? '工单已发生变化；刷新失败，请手动刷新后重新确认。'
            : '工单已发生变化，已重新读取，请重新确认后操作。'
      } else fail(e)
    } finally {
      if (run.current()) busy.value = false
    }
  }
  function changePage(delta: number) {
    if (!busy.value && !membersBusy.value) {
      memberPage.value += delta
      void loadMembers()
    }
  }
  return {
    mode,
    title,
    body,
    priority,
    assignee,
    members,
    memberPage,
    memberCount,
    busy,
    membersBusy,
    error,
    notice,
    canEdit,
    canAssign,
    canDelete,
    open,
    close,
    submit,
    changePage,
  }
}
