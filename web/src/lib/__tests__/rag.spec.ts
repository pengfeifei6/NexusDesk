import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import App from '../../App.vue'
import { routes } from '../../router'
import { useSession } from '../../stores/session'
import { api, ApiError, decode } from '../client'

const source = {
  document_id: '12',
  chunk_id: '9223372036854775807',
  chunk_index: 0,
  source_name: 'guide.md',
  score: 0.81,
  excerpt: '<script>fixture</script>',
  start_rune: 0,
  end_rune: 24,
}
const doc = {
  id: '12',
  org_id: '10',
  uploaded_by: '1',
  original_filename: 'guide.md',
  media_type: 'text/markdown',
  size_bytes: 20,
  sha256_hex: 'a'.repeat(64),
  status: 'READY',
  failure_code: null,
  extracted_chars: 20,
  created_at: 'now',
  updated_at: 'now',
}
const index = {
  document_id: '12',
  status: 'ABSENT',
  index_version: '0',
  embedding_model: 'bge-m3:567m',
  embedding_dimension: 1024,
  chunker_version: 'rune-500-80-v1',
  source_content_sha256: '',
  chunk_count: 0,
  failure_code: null,
  started_at: null,
  indexed_at: null,
  updated_at: null,
}
const wrappers: ReturnType<typeof mount>[] = []
afterEach(() => {
  wrappers.forEach((w) => w.unmount())
  wrappers.length = 0
  vi.restoreAllMocks()
})
async function open(path: string) {
  const pinia = createPinia()
  setActivePinia(pinia)
  const session = useSession()
  session.user = { id: '1', display_name: 'fixture', email: 'fixture@example.test' }
  const router = createRouter({ history: createMemoryHistory(), routes })
  await router.push(path)
  const wrapper = mount(App, { global: { plugins: [pinia, router] } })
  wrappers.push(wrapper)
  await flushPromises()
  return { wrapper, router, session }
}
function setup(extra?: (path: string, options: Parameters<typeof api.request>[1]) => unknown) {
  return vi.spyOn(api, 'request').mockImplementation(async (path, options) => {
    if (path.startsWith('/organizations?'))
      return {
        items: [
          { id: '10', name: 'A', my_role: 'admin' },
          { id: '20', name: 'B', my_role: 'member' },
        ],
      }
    const result = extra?.(path, options)
    if (result !== undefined) return result
    if (path.endsWith('/index')) return index
    if (path.endsWith('/text'))
      return {
        document_id: '12',
        content_text: 'fixture body',
        extractor_version: 'utf8-v1',
        extracted_at: 'now',
      }
    if (path.includes('/documents?')) return { items: [] }
    return doc
  })
}
function button(w: ReturnType<typeof mount>, name: string) {
  const b = w.findAll('button').find((x) => x.text() === name)
  expect(b).toBeDefined()
  return b!
}
describe('RAG front-end contracts', () => {
  it('preserves chunk/index IDs while decoding finite fractional scores and timings', () => {
    const v = decode<{
      chunk_id: string
      index_version: string
      score: number
      embedding_ms: number
    }>(
      '{"chunk_id":9223372036854775807,"index_version":9223372036854775806,"score":0.81,"embedding_ms":1.25}',
    )
    expect(v.chunk_id).toBe('9223372036854775807')
    expect(v.index_version).toBe('9223372036854775806')
    expect(v.score).toBe(0.81)
    expect(v.embedding_ms).toBe(1.25)
    expect(() => decode('{"size_bytes":1.25}')).toThrow()
  })
  it('shows extraction READY separately from absent index, then builds the index without a request body', async () => {
    let sent: unknown
    setup((path, options) => {
      if (path.endsWith('/index') && options?.method === 'POST') {
        sent = options
        return { ...index, status: 'READY', index_version: '1', chunk_count: 2 }
      }
    })
    const { wrapper } = await open('/organizations/10/knowledge/12')
    expect(wrapper.text()).toContain('尚未索引')
    await button(wrapper, '建立索引').trigger('click')
    await flushPromises()
    expect(sent).toEqual(expect.objectContaining({ method: 'POST', timeoutMs: 70000 }))
    expect((sent as { body?: unknown }).body).toBeUndefined()
    expect(wrapper.text()).toContain('已索引')
    expect(wrapper.text()).toContain('2 块')
  })
  it('searches within the current organization and renders escaped source excerpts', async () => {
    let sent: unknown
    setup((path, options) => {
      if (path.endsWith('/search')) {
        sent = { path, options }
        return {
          items: [source],
          embedding_model: 'bge-m3:567m',
          embedding_ms: 1.2,
          search_ms: 0.5,
        }
      }
    })
    const { wrapper } = await open('/organizations/10/knowledge/search')
    await wrapper.get('textarea[aria-label="检索问题"]').setValue('数据库连接超时')
    await button(wrapper, '检索文档').trigger('click')
    await flushPromises()
    expect(sent).toEqual(
      expect.objectContaining({
        path: '/organizations/10/knowledge/search',
        options: expect.objectContaining({
          body: { query: '数据库连接超时', top_k: 5, retrieval_mode: 'vector' },
          timeoutMs: 30000,
        }),
      }),
    )
    expect(wrapper.text()).toContain('<script>fixture</script>')
    expect(wrapper.find('script').exists()).toBe(false)
    expect(wrapper.get('a[href="/organizations/10/knowledge/12"]').text()).toContain('guide.md')
  })
  it('asks and displays only returned sources and the answer as plain text', async () => {
    setup((path) =>
      path.endsWith('/ask')
        ? {
            answer: '<img src=x>先检查网络',
            sources: [source],
            insufficient_evidence: false,
            request_id: 'fixture',
            embedding_model: 'bge-m3:567m',
            generation_model: 'qwen3.5:35b',
          }
        : undefined,
    )
    const { wrapper } = await open('/organizations/10/knowledge/search')
    await wrapper.get('textarea').setValue('怎样检查网络')
    await button(wrapper, '基于知识库回答').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('<img src=x>先检查网络')
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.text()).toContain('guide.md')
  })
  it('shows insufficient evidence without an invented source list', async () => {
    setup((path) =>
      path.endsWith('/ask')
        ? {
            answer: '知识库未检索到足够依据。',
            sources: [],
            insufficient_evidence: true,
            request_id: 'fixture',
            embedding_model: 'bge-m3:567m',
            generation_model: 'qwen3.5:35b',
          }
        : undefined,
    )
    const { wrapper } = await open('/organizations/10/knowledge/search')
    await wrapper.get('textarea').setValue('无关问题')
    await button(wrapper, '基于知识库回答').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('知识库未检索到足够依据')
    expect(wrapper.find('a[href$="/knowledge/12"]').exists()).toBe(false)
  })
  it.each(['organization', 'session', 'navigation'])(
    'ignores a late answer after %s change',
    async (change) => {
      let resolve!: (v: unknown) => void
      setup((path) =>
        path.endsWith('/ask')
          ? new Promise((r) => {
              resolve = r
            })
          : undefined,
      )
      const { wrapper, router, session } = await open('/organizations/10/knowledge/search')
      await wrapper.get('textarea').setValue('旧问题')
      await button(wrapper, '基于知识库回答').trigger('click')
      await flushPromises()
      if (change === 'session') session.clear()
      else
        await router.push(
          change === 'organization' ? '/organizations/20/knowledge/search' : '/desk',
        )
      resolve({ answer: '禁止显示的旧答案', sources: [source], insufficient_evidence: false })
      await flushPromises()
      expect(wrapper.text()).not.toContain('禁止显示的旧答案')
      expect(wrapper.text()).not.toContain(source.excerpt)
    },
  )
  it('clears earlier results when the question changes and blocks invalid input', async () => {
    let count = 0
    setup((path) => {
      if (path.endsWith('/search')) {
        count++
        return { items: [source], embedding_model: 'bge-m3:567m', embedding_ms: 1, search_ms: 1 }
      }
    })
    const { wrapper } = await open('/organizations/10/knowledge/search')
    await wrapper.get('textarea').setValue('有效问题')
    await button(wrapper, '检索文档').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('guide.md')
    await wrapper.get('textarea').setValue('新问题')
    expect(wrapper.text()).not.toContain(source.excerpt)
    await wrapper.get('textarea').setValue('🧪'.repeat(1001))
    await button(wrapper, '检索文档').trigger('click')
    await flushPromises()
    expect(count).toBe(1)
  })
  it('defaults to exact vector retrieval and submits the selected hybrid strategy', async () => {
    const requests: unknown[] = []
    setup((path, options) => {
      if (path.endsWith('/search')) {
        requests.push(options?.body)
        return { items: [], retrieval_mode: 'hybrid', embedding_ms: 1, search_ms: 2 }
      }
    })
    const { wrapper } = await open('/organizations/10/knowledge/search')
    const strategy = wrapper.get('select[aria-label="检索策略"]')
    expect((strategy.element as HTMLSelectElement).value).toBe('vector')
    await wrapper.get('textarea').setValue('数据库连接')
    await strategy.setValue('hybrid')
    await button(wrapper, '检索文档').trigger('click')
    await flushPromises()
    expect(requests).toEqual([{ query: '数据库连接', top_k: 5, retrieval_mode: 'hybrid' }])
  })
  it('cancels an old request and clears existing evidence when the retrieval strategy changes', async () => {
    let resolve!: (value: unknown) => void
    let signal: AbortSignal | undefined
    setup((path, options) => {
      if (path.endsWith('/ask')) {
        signal = options?.signal
        return new Promise((r) => {
          resolve = r
        })
      }
      if (path.endsWith('/search')) return { items: [source], embedding_ms: 1, search_ms: 1 }
    })
    const { wrapper } = await open('/organizations/10/knowledge/search')
    await wrapper.get('textarea').setValue('数据库连接')
    await button(wrapper, '检索文档').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('guide.md')
    await wrapper.get('select[aria-label="检索策略"]').setValue('hybrid')
    expect(wrapper.text()).not.toContain('guide.md')
    await button(wrapper, '基于知识库回答').trigger('click')
    await wrapper.get('select[aria-label="检索策略"]').setValue('vector')
    expect(signal?.aborted).toBe(true)
    resolve({ answer: '过期策略的答案', sources: [source], insufficient_evidence: false })
    await flushPromises()
    expect(wrapper.text()).not.toContain('过期策略的答案')
    expect(wrapper.text()).not.toContain(source.excerpt)
  })
  it.each([
    {
      known: false,
      model_called: true,
      prompt_tokens: null,
      completion_tokens: null,
      total_tokens: null,
      label: '用量未知',
    },
    {
      known: true,
      model_called: false,
      prompt_tokens: 0,
      completion_tokens: 0,
      total_tokens: 0,
      label: '未调用生成模型',
    },
    {
      known: true,
      model_called: true,
      prompt_tokens: 10,
      completion_tokens: 4,
      total_tokens: 14,
      label: '14 tokens',
    },
  ])('shows measured or unknown usage without invented counts: $label', async (usage) => {
    setup((path) =>
      path.endsWith('/ask')
        ? {
            answer: '回答',
            sources: [],
            insufficient_evidence: false,
            request_id: 'fixture',
            generation_model: 'fixture-model',
            retrieval_mode: 'vector',
            usage,
            timings: { embedding_ms: 12, search_ms: 3, generation_ms: 85, total_ms: 105 },
          }
        : undefined,
    )
    const { wrapper } = await open('/organizations/10/knowledge/search')
    await wrapper.get('textarea').setValue('数据库连接')
    await button(wrapper, '基于知识库回答').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain(usage.label)
    expect(wrapper.text()).toContain('105 ms')
    expect(!usage.known && wrapper.text().includes('0 tokens')).toBe(false)
  })
  it('decodes fractional fusion scores separately from cosine scores', () => {
    expect(decode('{"fusion_score":0.03279,"score":0.81}')).toEqual({
      fusion_score: 0.03279,
      score: 0.81,
    })
  })
  it('refreshes index metadata after a model failure and preserves a safe error', async () => {
    let attempted = false
    setup((path, options) => {
      if (path.endsWith('/index')) {
        if (options?.method === 'POST') {
          attempted = true
          return Promise.reject(new ApiError(503, '模型暂时不可用', 'MODEL_UNAVAILABLE'))
        }
        return {
          ...index,
          status: attempted ? 'FAILED' : 'ABSENT',
          failure_code: attempted ? 'MODEL_UNAVAILABLE' : null,
        }
      }
    })
    const { wrapper } = await open('/organizations/10/knowledge/12')
    await button(wrapper, '建立索引').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('索引失败')
    expect(wrapper.text()).toContain('模型暂时不可用')
  })
  it('clears protected preview when failed indexing refresh reports revoked access', async () => {
    let attempted = false
    setup((path, options) => {
      if (path.endsWith('/index')) {
        if (options?.method === 'POST') {
          attempted = true
          return Promise.reject(new ApiError(503, '模型暂时不可用'))
        }
        if (attempted) return Promise.reject(new ApiError(404, '资源不存在或无法访问'))
      }
    })
    const { wrapper } = await open('/organizations/10/knowledge/12')
    expect(wrapper.text()).toContain('fixture body')
    await button(wrapper, '建立索引').trigger('click')
    await flushPromises()
    expect(wrapper.text()).not.toContain('fixture body')
    expect(wrapper.text()).not.toContain('guide.md')
    expect(wrapper.text()).toContain('资源不存在或无法访问')
  })
  it('clears protected preview immediately on denied POST without waiting for another GET', async () => {
    let attempted = false
    setup((path, options) => {
      if (path.endsWith('/index')) {
        if (options?.method === 'POST') {
          attempted = true
          return Promise.reject(new ApiError(404, '资源不存在或无法访问'))
        }
        if (attempted) return new Promise(() => {})
      }
    })
    const { wrapper } = await open('/organizations/10/knowledge/12')
    await button(wrapper, '建立索引').trigger('click')
    await flushPromises()
    expect(wrapper.text()).not.toContain('fixture body')
    expect(wrapper.text()).toContain('资源不存在或无法访问')
  })
  it('ignores a late denied metadata refresh after navigating to another organization', async () => {
    let attempted = false,
      reject!: (e: unknown) => void
    setup((path, options) => {
      if (path.startsWith('/organizations/10/') && path.endsWith('/index')) {
        if (options?.method === 'POST') {
          attempted = true
          return Promise.reject(new ApiError(503, '模型暂时不可用'))
        }
        if (attempted)
          return new Promise((_resolve, r) => {
            reject = r
          })
      }
    })
    const { wrapper, router } = await open('/organizations/10/knowledge/12')
    await button(wrapper, '建立索引').trigger('click')
    await flushPromises()
    await router.push('/organizations/20/knowledge/12')
    await flushPromises()
    reject(new ApiError(404, '旧组织无权访问'))
    await flushPromises()
    expect(wrapper.text()).toContain('fixture body')
    expect(wrapper.text()).not.toContain('旧组织无权访问')
  })
})
