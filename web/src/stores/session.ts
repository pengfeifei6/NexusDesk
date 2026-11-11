import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { User } from '../lib/types'
import { api } from '../lib/client'
export const useSession = defineStore('session', () => {
  const user = ref<User | null>(null)
  const epoch = ref(0)
  const expired = ref(false)
  function clear() {
    api.token = ''
    user.value = null
    epoch.value++
  }
  api.onExpired = () => {
    clear()
    expired.value = true
  }
  return { user, epoch, expired, clear }
})
