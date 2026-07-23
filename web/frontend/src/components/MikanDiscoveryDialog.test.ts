import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import MikanDiscoveryDialog from './MikanDiscoveryDialog.vue'

const AppDialogStub = {
  props: ['open'],
  emits: ['update:open'],
  template: '<div v-if="open"><slot /></div>',
}

function response(data: unknown, status = 200) {
  const payload = status >= 400 ? { error: { code: 'failed', message: String(data) } } : { data }
  return Promise.resolve(new Response(JSON.stringify(payload), { status, headers: { 'Content-Type': 'application/json' } }))
}

function mountDialog(props: { initialSearch?: string } = {}) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
  return mount(MikanDiscoveryDialog, {
    attachTo: document.body,
    props: { open: true, ...props },
    global: {
      plugins: [[VueQueryPlugin, { queryClient }]],
      stubs: { AppDialog: AppDialogStub },
    },
  })
}

async function waitForText(wrapper: ReturnType<typeof mountDialog>, text: string) {
  await vi.waitFor(() => expect(wrapper.text()).toContain(text))
  await flushPromises()
}

function buttonByText(wrapper: ReturnType<typeof mountDialog>, text: string) {
  const button = wrapper.findAll('button').find(item => item.text().includes(text))
  if (!button) throw new Error(`button not found: ${text}`)
  return button
}

afterEach(() => {
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('MikanDiscoveryDialog', () => {
  it('starts in Mikan search when an initial calendar title is provided', async () => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      const path = String(input)
      if (path.includes('/subscriptions/search?q=%E6%B5%8B%E8%AF%95%E7%95%AA%E5%89%A7')) {
        return response({ items: [{ mikan_id: '3141', title: '测试番剧 Mikan', image: 'poster.jpg' }] })
      }
      throw new Error(`unexpected request: ${path}`)
    }))

    const wrapper = mountDialog({ initialSearch: '测试番剧' })
    await waitForText(wrapper, '测试番剧 Mikan')
    expect((wrapper.get('#mikan-search').element as HTMLInputElement).value).toBe('测试番剧')
  })

  it('searches, previews a subgroup and emits the complete subscription preset', async () => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      const path = String(input)
      if (path.includes('/mikan/dashboard')) return response({ season: '2026 夏季番组', days: { '1': [] } })
      if (path.includes('/subscriptions/search')) return response({ items: [{ mikan_id: '3141', title: '测试番剧', image: 'poster.jpg' }] })
      if (path.includes('/mikan/subgroups')) return response({ items: [{ id: '', name: '全部字幕组', is_all: true }, { id: '583', name: 'ANi', is_all: false }] })
      if (path.includes('/mikan/episodes')) return response({ mikan_id: '3141', total: 1, items: [{ title: '[ANi] 测试番剧 01', episode_num: '01', sub_group: 'ANi', resolution: '1080p', pub_date: '2026-07-23T00:00:00Z' }] })
      throw new Error(`unexpected request: ${path}`)
    }))

    const wrapper = mountDialog()
    await buttonByText(wrapper, '搜索').trigger('click')
    await wrapper.get('#mikan-search').setValue('测试')
    await wrapper.get('form').trigger('submit')
    await waitForText(wrapper, '测试番剧')
    await buttonByText(wrapper, '测试番剧').trigger('click')
    await waitForText(wrapper, 'ANi')
    await buttonByText(wrapper, 'ANi').trigger('click')
    await waitForText(wrapper, '[ANi] 测试番剧 01')
    await wrapper.get('[data-testid="confirm-mikan-selection"]').trigger('click')

    expect(wrapper.emitted('select')?.[0]?.[0]).toMatchObject({
      mikan_id: '3141',
      title: '测试番剧',
      subtitle_group: 'ANi',
      rss_url: 'https://mikanani.me/RSS/Bangumi?bangumiId=3141&subgroupid=583',
      backup_rss_url: 'https://mikanani.me/RSS/Bangumi?bangumiId=3141',
      filter_rule: 'ANi',
      allow_multi_subgroup: false,
    })
  })

  it('shows a recoverable dashboard error and retries in place', async () => {
    let attempts = 0
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      const path = String(input)
      if (!path.includes('/mikan/dashboard')) throw new Error(`unexpected request: ${path}`)
      attempts += 1
      if (attempts === 1) return response('季度番组暂时不可用', 502)
      return response({ season: '2026 夏季番组', days: { '1': [{ mikan_id: '9', title: '恢复成功', image: '' }] } })
    }))

    const wrapper = mountDialog()
    await waitForText(wrapper, '季度番组加载失败')
    await buttonByText(wrapper, '重试').trigger('click')
    await waitForText(wrapper, '恢复成功')
    expect(attempts).toBe(2)
  })
})
