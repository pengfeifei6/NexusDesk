import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
const target = process.env.NEXUSDESK_API_TARGET || 'http://127.0.0.1:8080'
const url = new URL(target)
if (
  !['127.0.0.1', '[::1]'].includes(url.hostname) ||
  url.username ||
  url.password ||
  !['http:', 'https:'].includes(url.protocol)
)
  throw new Error('API target must be loopback HTTP(S)')
export default defineConfig({
  envDir: false,
  plugins: [vue()],
  server: { host: '127.0.0.1', proxy: { '/api': { target }, '/healthz': { target } } },
  preview: { host: '127.0.0.1' },
  resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
})
