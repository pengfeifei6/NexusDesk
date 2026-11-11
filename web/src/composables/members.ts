import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { api, ApiError, Scope } from '../lib/client'
import { positiveID } from '../lib/identifiers'
import type { Member } from '../lib/types'
import type { Desk } from './desk'

export function createMembers(desk: Desk) {
  const route = useRoute(),
    scope = new Scope()
  let active = scope.begin()
  const items = ref<Member[]>([]),
    page = ref(1),
    busy = ref(false)
  const error = ref(''),
    notice = ref(''),
    userID = ref(''),
    newRole = ref<'member' | 'admin'>('member')
  const accessible = ref(true)
  const endpoint = computed(() => `/organizations/${desk.orgID.value}/members`)
  const canAdd = computed(() => accessible.value && desk.role.value === 'admin')
  onBeforeUnmount(() => scope.cancel())
  watch(
    () => desk.accessEpoch.value,
    () => {
      active = scope.begin()
      items.value = []
      accessible.value = false
      userID.value = ''
      notice.value = ''
      busy.value = false
    },
    { flush: 'sync' },
  )
  function fail(e: unknown, adding = false) {
    if (
      e instanceof ApiError &&
      [401, 403, 404].includes(e.status) &&
      !(adding && e.code === 'USER_NOT_FOUND')
    ) {
      items.value = []
      accessible.value = false
      userID.value = ''
      desk.invalidateAccess()
    }
    error.value = e instanceof Error ? e.message : '成员操作失败，请重试'
  }
  async function load() {
    const run = (active = scope.begin())
    busy.value = true
    error.value = ''
    items.value = []
    try {
      const data = await api.request<{ items: Member[] }>(
        `${endpoint.value}?page=${page.value}&page_size=20`,
        { signal: run.signal },
      )
      if (run.current()) {
        items.value = data.items
        accessible.value = true
      }
    } catch (e) {
      if (run.current()) fail(e)
    } finally {
      if (run.current()) busy.value = false
    }
  }
  async function add() {
    if (busy.value || !canAdd.value) return
    if (!positiveID(userID.value)) {
      error.value = '请输入有效的账号编号（正整数，最多为9223372036854775807）'
      return
    }
    const run = active,
      path = endpoint.value
    busy.value = true
    error.value = ''
    notice.value = ''
    try {
      await api.request<Member>(path, {
        method: 'POST',
        body: { user_id: userID.value, role: newRole.value },
        signal: run.signal,
      })
      if (!run.current()) return
      userID.value = ''
      page.value = 1
      notice.value = '成员已添加，可通过成员分页查看。'
      await load()
    } catch (e) {
      if (run.current()) fail(e, true)
    } finally {
      if (run.current()) busy.value = false
    }
  }
  function changePage(delta: number) {
    if (!busy.value) {
      page.value += delta
      void load()
    }
  }
  watch(
    () => [route.fullPath, desk.session.epoch],
    () => {
      active = scope.begin()
      items.value = []
      userID.value = ''
      page.value = 1
      newRole.value = 'member'
      error.value = ''
      notice.value = ''
      accessible.value = true
      busy.value = false
      if (desk.session.user && desk.orgID.value && route.path.endsWith('/members')) void load()
    },
    { immediate: true, flush: 'sync' },
  )
  return { items, page, busy, error, notice, userID, newRole, canAdd, load, add, changePage }
}
