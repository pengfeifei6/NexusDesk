import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import App from '../../App.vue'
import { routes } from '../../router'
import { useSession } from '../../stores/session'
import { api, ApiError } from '../client'

const organizations = [
  { id: '10', name: '甲组织', my_role: 'member' },
  { id: '20', name: '乙组织', my_role: 'admin' },
]
const document = {
  id: '9223372036854775807',
  org_id: '10',
  uploaded_by: '2',
  original_filename: 'guide.md',
  media_type: 'text/markdown',
  size_bytes: 35,
  sha256_hex: 'a'.repeat(64),
  status: 'UPLOADED',
  failure_code: null,
  extracted_chars: 0,
  created_at: '2026-11-10T12:00:00Z',
  updated_at: '2026-11-10T12:00:00Z',
}
const text = {
  document_id: document.id,
  content_text: '<img src=x onerror=alert(1)>\n# 原始正文',
  content_sha256: 'b'.repeat(64),
  extractor_version: 'plain-v1',
  extracted_at: '2026-11-10T12:01:00Z',
}
const wrappers: ReturnType<typeof mount>[] = []
afterEach(() => {
  wrappers.forEach((w) => w.unmount())
  wrappers.length = 0
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})
async function open(path: string, authenticated = true) {
  const pinia = createPinia()
  setActivePinia(pinia)
  const session = useSession()
  if (authenticated) session.user = { id: '1', display_name: '测试', email: 'test@example.test' }
  const router = createRouter({ history: createMemoryHistory(), routes })
  await router.push(path)
  const wrapper = mount(App, { global: { plugins: [pinia, router] } })
  wrappers.push(wrapper)
  await flushPromises()
  return { wrapper, router, session }
}
function button(wrapper: ReturnType<typeof mount>, label: string) {
  const found = wrapper.findAll('button').find((b) => b.text() === label)
  expect(found, `button ${label}`).toBeDefined()
  return found!
}
function fakeKnowledge(role = 'member', uploadedBy = '2', status = 'UPLOADED') {
  return vi.spyOn(api, 'request').mockImplementation(async (path) => {
    if (path.startsWith('/organizations?'))
      return { items: [{ ...organizations[0], my_role: role }, organizations[1]] }
    if (path.endsWith('/text')) return text
    if (path.endsWith('/process'))
      return { ...document, uploaded_by: uploadedBy, status: 'READY', extracted_chars: 35 }
    if (path.includes('/knowledge/documents?')) return { items: [document], page: 1, page_size: 20 }
    return { ...document, uploaded_by: uploadedBy, status }
  })
}

describe('new user onboarding', () => {
  it('registers three fields, returns to login with notice, and creates no organization or session', async () => {
    const calls: { path: string; body: unknown }[] = []
    vi.spyOn(api, 'request').mockImplementation(async (path, options) => {
      calls.push({ path, body: options?.body })
      return { id: '1', email: 'new@example.test', display_name: '新用户' }
    })
    const { wrapper, router, session } = await open('/register', false)
    await wrapper.get('input[name=display_name]').setValue('新用户')
    await wrapper.get('input[type=email]').setValue('new@example.test')
    await wrapper.get('input[type=password]').setValue('fixture-long-password')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(calls).toEqual([
      {
        path: '/auth/register',
        body: {
          display_name: '新用户',
          email: 'new@example.test',
          password: 'fixture-long-password',
        },
      },
    ])
    expect(router.currentRoute.value.path).toBe('/login')
    expect(wrapper.text()).toContain('注册成功，请登录')
    expect(session.user).toBeNull()
    expect((wrapper.get('input[type=password]').element as HTMLInputElement).value).toBe('')
  })
  it('creates the first organization with only its name and enters the server-returned organization', async () => {
    const bodies: unknown[] = []
    vi.spyOn(api, 'request').mockImplementation(async (path, options) => {
      if (path === '/organizations') {
        bodies.push(options?.body)
        return { id: '9007199254740993', name: '首个组织', my_role: 'admin' }
      }
      if (path.startsWith('/organizations?'))
        return {
          items: bodies.length
            ? [{ id: '9007199254740993', name: '首个组织', my_role: 'admin' }]
            : [],
        }
      return { items: [] }
    })
    const { wrapper, router } = await open('/desk')
    await wrapper.get('input[name=organization_name]').setValue('首个组织')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(bodies).toEqual([{ name: '首个组织' }])
    expect(router.currentRoute.value.path).toBe('/organizations/9007199254740993/tickets')
    expect(wrapper.text()).toContain('首个组织 · 管理员')
    expect(wrapper.text()).toContain('暂无工单')
    await router.push('/desk')
    expect(wrapper.find('input[name=organization_name]').exists()).toBe(true)
  })
})

describe('knowledge workflow', () => {
  it('lists documents and sends filter and pagination parameters', async () => {
    const paths: string[] = []
    vi.spyOn(api, 'request').mockImplementation(async (path) => {
      paths.push(path)
      if (path.startsWith('/organizations?')) return { items: organizations }
      return {
        items: Array.from({ length: 20 }, (_, i) => ({ ...document, id: String(i + 1) })),
        page: 1,
        page_size: 20,
      }
    })
    const { wrapper } = await open('/organizations/10/knowledge')
    expect(wrapper.text()).toContain('guide.md')
    await wrapper.get('select[aria-label="文档状态筛选"]').setValue('FAILED')
    await flushPromises()
    await button(wrapper, '下一页文档').trigger('click')
    await flushPromises()
    expect(paths).toContain(
      '/organizations/10/knowledge/documents?page=1&page_size=20&status=FAILED',
    )
    expect(paths).toContain(
      '/organizations/10/knowledge/documents?page=2&page_size=20&status=FAILED',
    )
  })
  it('uploads a file as FormData and opens the returned detail', async () => {
    let sent: FormData | undefined
    vi.spyOn(api, 'request').mockImplementation(async (path, options) => {
      if (path.startsWith('/organizations?')) return { items: organizations }
      if (options?.method === 'POST') {
        sent = options.body as FormData
        return document
      }
      if (path.includes('documents?')) return { items: [] }
      return document
    })
    const { wrapper, router } = await open('/organizations/10/knowledge')
    const file = new File(['# 测试'], 'guide.md', { type: 'text/markdown' })
    Object.defineProperty(wrapper.get('input[type=file]').element, 'files', {
      value: [file],
      configurable: true,
    })
    await wrapper.get('input[type=file]').trigger('change')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(sent?.get('file')).toBe(file)
    expect(Array.from(sent!.keys())).toEqual(['file'])
    expect(router.currentRoute.value.path).toBe(`/organizations/10/knowledge/${document.id}`)
    expect(wrapper.text()).toContain('guide.md')
  })
  it('blocks files above 5 MiB before sending them', async () => {
    let uploads = 0
    vi.spyOn(api, 'request').mockImplementation(async (path, options) => {
      if (options?.method === 'POST') uploads++
      return path.startsWith('/organizations?') ? { items: organizations } : { items: [] }
    })
    const { wrapper } = await open('/organizations/10/knowledge')
    const file = new File(['x'], 'large.txt')
    Object.defineProperty(file, 'size', { value: 5242881 })
    Object.defineProperty(wrapper.get('input[type=file]').element, 'files', { value: [file] })
    await wrapper.get('input[type=file]').trigger('change')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(uploads).toBe(0)
    expect(wrapper.text()).toContain('5 MiB')
  })
  it('processes as an active member and displays escaped plain text', async () => {
    fakeKnowledge()
    const { wrapper } = await open(`/organizations/10/knowledge/${document.id}`)
    expect(wrapper.text()).not.toContain('删除文档')
    await button(wrapper, '提取正文').trigger('click')
    await flushPromises()
    expect(wrapper.get('pre').text()).toBe(text.content_text)
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.text()).toContain('已就绪')
  })
  it.each([
    ['admin', '2'],
    ['member', '1'],
  ])('allows delete button for %s uploader %s and returns to list', async (role, uploader) => {
    fakeKnowledge(role, uploader)
    const { wrapper, router } = await open(`/organizations/10/knowledge/${document.id}`)
    await button(wrapper, '删除文档').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/organizations/10/knowledge')
    expect(wrapper.text()).not.toContain('提取正文')
  })
  it('clears old detail and preview when refresh returns 404', async () => {
    let missing = false
    vi.spyOn(api, 'request').mockImplementation(async (path) => {
      if (path.startsWith('/organizations?')) return { items: organizations }
      if (missing) throw new ApiError(404, '资源不存在或无法访问')
      return path.endsWith('/text') ? text : { ...document, status: 'READY' }
    })
    const { wrapper } = await open(`/organizations/10/knowledge/${document.id}`)
    expect(wrapper.text()).toContain(text.content_text)
    missing = true
    await button(wrapper, '刷新文档').trigger('click')
    await flushPromises()
    expect(wrapper.text()).not.toContain('guide.md')
    expect(wrapper.find('pre').exists()).toBe(false)
    expect(wrapper.text()).toContain('资源不存在')
  })
  it('explains unsupported PDF extraction and keeps download available', async () => {
    vi.spyOn(api, 'request').mockImplementation(async (path) => {
      if (path.startsWith('/organizations?')) return { items: organizations }
      if (path.endsWith('/process'))
        throw new ApiError(422, 'PDF 暂不支持文本提取，可下载原文件', 'PDF_UNSUPPORTED')
      return {
        ...document,
        original_filename: 'guide.pdf',
        media_type: 'application/pdf',
        status: 'FAILED',
        failure_code: 'PDF_UNSUPPORTED',
      }
    })
    const { wrapper } = await open(`/organizations/10/knowledge/${document.id}`)
    expect(wrapper.text()).toContain('PDF 暂不支持文本提取')
    expect(button(wrapper, '下载原文件').exists()).toBe(true)
  })
})

describe('knowledge request isolation', () => {
  it('ignores late detail and text from the previous organization and preserves knowledge on organization switch', async () => {
    let finish!: (v: unknown) => void
    vi.spyOn(api, 'request').mockImplementation(async (path) => {
      if (path.startsWith('/organizations?')) return { items: organizations }
      if (path.endsWith('/text'))
        return new Promise((resolve) => {
          finish = resolve
        })
      if (path.includes('/10/knowledge/')) return { ...document, status: 'READY' }
      return {
        items: [{ ...document, id: '7', original_filename: '乙组织文档.txt', org_id: '20' }],
      }
    })
    const { wrapper, router } = await open(`/organizations/10/knowledge/${document.id}`)
    await wrapper.get('select').setValue('20')
    await flushPromises()
    finish(text)
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/organizations/20/knowledge')
    expect(wrapper.text()).toContain('乙组织文档.txt')
    expect(wrapper.text()).not.toContain(text.content_text)
    expect(wrapper.text()).not.toContain('guide.md')
  })
  it('ignores a late same-organization filter response', async () => {
    let finish!: (v: unknown) => void
    vi.spyOn(api, 'request').mockImplementation(async (path) => {
      if (path.startsWith('/organizations?')) return { items: organizations }
      if (path.includes('status=UPLOADED'))
        return new Promise((resolve) => {
          finish = resolve
        })
      return { items: [{ ...document, original_filename: '最新文档.txt' }] }
    })
    const { wrapper } = await open('/organizations/10/knowledge')
    const filter = wrapper.get('select[aria-label="文档状态筛选"]')
    await filter.setValue('UPLOADED')
    await filter.setValue('READY')
    await flushPromises()
    finish({ items: [{ ...document, original_filename: '过时文档.txt' }] })
    await flushPromises()
    expect(wrapper.text()).toContain('最新文档.txt')
    expect(wrapper.text()).not.toContain('过时文档.txt')
  })
  it.each(['organization', 'logout', 'navigation'])(
    'does not save a late download after %s change',
    async (change) => {
      let finish!: (v: unknown) => void
      const createURL = vi.fn(() => 'blob:fixture')
      vi.stubGlobal(
        'URL',
        class extends URL {
          static createObjectURL = createURL
          static revokeObjectURL = vi.fn()
        },
      )
      vi.spyOn(api, 'request').mockImplementation(async (path) => {
        if (path.startsWith('/organizations?')) return { items: organizations }
        if (path.endsWith('/download'))
          return new Promise((resolve) => {
            finish = resolve
          })
        if (path.includes('documents?') || path.includes('/tickets?')) return { items: [] }
        return document
      })
      const { wrapper, router } = await open(`/organizations/10/knowledge/${document.id}`)
      await button(wrapper, '下载原文件').trigger('click')
      if (change === 'organization') await router.push('/organizations/20/knowledge')
      if (change === 'navigation') await router.push('/organizations/10/tickets')
      if (change === 'logout') await button(wrapper, '退出登录').trigger('click')
      await flushPromises()
      finish(new Blob(['private']))
      await flushPromises()
      expect(createURL).not.toHaveBeenCalled()
    },
  )
  it('releases a successful download ObjectURL and uses its safe original filename', async () => {
    const revokeURL = vi.fn()
    const createURL = vi.fn(() => 'blob:fixture')
    vi.stubGlobal(
      'URL',
      class extends URL {
        static createObjectURL = createURL
        static revokeObjectURL = revokeURL
      },
    )
    let saved = ''
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (
      this: HTMLAnchorElement,
    ) {
      saved = this.download
    })
    vi.spyOn(api, 'request').mockImplementation(async (path) => {
      if (path.startsWith('/organizations?')) return { items: organizations }
      return path.endsWith('/download') ? new Blob(['hello']) : document
    })
    const { wrapper } = await open(`/organizations/10/knowledge/${document.id}`)
    await button(wrapper, '下载原文件').trigger('click')
    await flushPromises()
    expect(saved).toBe('guide.md')
    expect(createURL).toHaveBeenCalledOnce()
    expect(revokeURL).toHaveBeenCalledWith('blob:fixture')
  })
})

it.each(['upload', 'process'])(
  'does not navigate or display late %s results after leaving knowledge',
  async (operation) => {
    let finish!: (v: unknown) => void
    vi.spyOn(api, 'request').mockImplementation(async (path, options) => {
      if (path.startsWith('/organizations?')) return { items: organizations }
      if (options?.method === 'POST')
        return new Promise((resolve) => {
          finish = resolve
        })
      if (path.includes('documents?') || path.includes('/tickets?')) return { items: [] }
      return document
    })
    const { wrapper, router } = await open(
      operation === 'process'
        ? `/organizations/10/knowledge/${document.id}`
        : '/organizations/10/knowledge',
    )
    if (operation === 'upload') {
      const file = new File(['hello'], 'guide.txt')
      Object.defineProperty(wrapper.get('input[type=file]').element, 'files', { value: [file] })
      await wrapper.get('input[type=file]').trigger('change')
      await wrapper.get('form').trigger('submit')
    } else await button(wrapper, '提取正文').trigger('click')
    await router.push('/organizations/20/tickets')
    await flushPromises()
    finish({ ...document, status: 'READY' })
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/organizations/20/tickets')
    expect(wrapper.text()).not.toContain('guide.md')
    expect(wrapper.find('pre').exists()).toBe(false)
  },
)
it('does not resurrect a late document detail after changing organization', async () => {
  let finish!: (v: unknown) => void
  vi.spyOn(api, 'request').mockImplementation(async (path) => {
    if (path.startsWith('/organizations?')) return { items: organizations }
    if (path.includes('/10/knowledge/'))
      return new Promise((resolve) => {
        finish = resolve
      })
    return { items: [] }
  })
  const { wrapper, router } = await open(`/organizations/10/knowledge/${document.id}`)
  await router.push('/organizations/20/knowledge')
  await flushPromises()
  finish({ ...document, status: 'READY' })
  await flushPromises()
  expect(wrapper.text()).not.toContain('guide.md')
  expect(wrapper.find('pre').exists()).toBe(false)
})
it('clears READY detail and text after a forbidden delete', async () => {
  vi.spyOn(api, 'request').mockImplementation(async (path, options) => {
    if (path.startsWith('/organizations?')) return { items: organizations }
    if (options?.method === 'DELETE') throw new ApiError(403, '没有执行此操作的权限')
    return path.endsWith('/text') ? text : { ...document, uploaded_by: '1', status: 'READY' }
  })
  const { wrapper } = await open(`/organizations/10/knowledge/${document.id}`)
  expect(wrapper.text()).toContain(text.content_text)
  await button(wrapper, '删除文档').trigger('click')
  await flushPromises()
  expect(wrapper.find('pre').exists()).toBe(false)
  expect(wrapper.text()).not.toContain('guide.md')
  expect(wrapper.text()).toContain('没有执行此操作的权限')
})
it('refreshes FAILED metadata after extraction rejection and shows only the safe message', async () => {
  let failed = false
  vi.spyOn(api, 'request').mockImplementation(async (path) => {
    if (path.startsWith('/organizations?')) return { items: organizations }
    if (path.endsWith('/process')) {
      failed = true
      throw new ApiError(422, '文档没有可提取的正文', 'EMPTY_TEXT')
    }
    return {
      ...document,
      status: failed ? 'FAILED' : 'UPLOADED',
      failure_code: failed ? 'EMPTY_TEXT' : null,
    }
  })
  const { wrapper } = await open(`/organizations/10/knowledge/${document.id}`)
  await button(wrapper, '提取正文').trigger('click')
  await flushPromises()
  expect(wrapper.text()).toContain('失败')
  expect(wrapper.text()).toContain('文档没有可提取的正文')
  expect(wrapper.find('pre').exists()).toBe(false)
})

it('preserves typed login credentials while resetting the previous memory session', async () => {
  let credentials: unknown
  vi.spyOn(api, 'request').mockImplementation(async (path, options) => {
    if (path === '/auth/login') {
      credentials = options?.body
      return { access_token: 'test-memory-session' }
    }
    if (path === '/me') return { id: '1', display_name: '测试', email: 'test@example.test' }
    return { items: [] }
  })
  const { wrapper, router } = await open('/login', false)
  await wrapper.get('input[type=email]').setValue('test@example.test')
  await wrapper.get('input[type=password]').setValue('fixture-long-password')
  await wrapper.get('form').trigger('submit')
  await flushPromises()
  expect(credentials).toEqual({ email: 'test@example.test', password: 'fixture-long-password' })
  expect(router.currentRoute.value.path).toBe('/desk')
  expect(wrapper.find('input[type=password]').exists()).toBe(false)
  api.token = ''
})

it('uses a generic failure message for an inherited property name rather than rendering it', async () => {
  vi.spyOn(api, 'request').mockImplementation(async (path) =>
    path.startsWith('/organizations?')
      ? { items: organizations }
      : { ...document, status: 'FAILED', failure_code: 'constructor' },
  )
  const { wrapper } = await open(`/organizations/10/knowledge/${document.id}`)
  expect(wrapper.get('.notice').text()).toBe('文档处理失败，请重试')
  expect(wrapper.text()).not.toContain('function Object')
})

describe('onboarding Unicode length rules', () => {
  it('rejects a supplementary-plane password shorter than 15 code points before sending it', async () => {
    const request = vi.spyOn(api, 'request').mockResolvedValue({ id: '1' })
    const { wrapper } = await open('/register', false)
    await wrapper.get('input[name=display_name]').setValue('测试')
    await wrapper.get('input[type=email]').setValue('unicode@example.test')
    await wrapper.get('input[type=password]').setValue('🧪'.repeat(8))
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(request).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('密码需 15～128 个字符')
  })
  it('allows the API code-point limits without native UTF-16 truncation', async () => {
    const request = vi.spyOn(api, 'request').mockResolvedValue({ id: '1' })
    const { wrapper, router } = await open('/register', false)
    const name = '🧪'.repeat(80),
      password = '🧪'.repeat(65)
    expect(wrapper.get('input[name=display_name]').attributes('maxlength')).toBeUndefined()
    expect(wrapper.get('input[type=password]').attributes('maxlength')).toBeUndefined()
    await wrapper.get('input[name=display_name]').setValue(name)
    await wrapper.get('input[type=email]').setValue('unicode@example.test')
    await wrapper.get('input[type=password]').setValue(password)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(request).toHaveBeenCalledWith(
      '/auth/register',
      expect.objectContaining({
        body: { display_name: name, email: 'unicode@example.test', password },
      }),
    )
    expect(router.currentRoute.value.path).toBe('/login')
  })
  it('validates organization names by code points and sends the valid full name', async () => {
    const bodies: unknown[] = []
    vi.spyOn(api, 'request').mockImplementation(async (path, options) => {
      if (path === '/organizations') {
        bodies.push(options?.body)
        return { id: '10', name: '组织', my_role: 'admin' }
      }
      return { items: organizations }
    })
    const { wrapper } = await open('/desk')
    const input = wrapper.get('input[name=organization_name]')
    expect(input.attributes('maxlength')).toBeUndefined()
    await input.setValue('🧪'.repeat(101))
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(bodies).toEqual([])
    expect(wrapper.text()).toContain('组织名称需 1～100 个字符')
    await input.setValue('🧪'.repeat(100))
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(bodies).toEqual([{ name: '🧪'.repeat(100) }])
  })
})
