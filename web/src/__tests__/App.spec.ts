import { it, expect } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import { routes } from '../router'
import App from '../App.vue'
it('requires a fresh login after opening a protected URL without a memory session', async () => {
  const router = createRouter({ history: createMemoryHistory(), routes })
  await router.push('/organizations/10/tickets/5')
  const wrapper = mount(App, { global: { plugins: [createPinia(), router] } })
  await flushPromises()
  expect(router.currentRoute.value.path).toBe('/login')
  expect(wrapper.find('input[type=password]').exists()).toBe(true)
  wrapper.unmount()
})
