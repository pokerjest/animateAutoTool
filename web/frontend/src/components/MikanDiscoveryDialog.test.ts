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

function mountDialog(props: { initialSearch?: string; initialSearchAliases?: string[]; initialBangumiSubjectId?: string } = {}) {
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
  it('uses the Bangumi subject ID to open the exact Mikan match', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const path = String(input)
      if (path.includes('/mikan/resolve?') && path.includes('bangumi_subject_id=598058')) {
        return response({ items: [{ mikan_id: '3997', bangumi_subject_id: '598058', title: '精确匹配番剧', image: 'poster.jpg' }] })
      }
      if (path.includes('/mikan/subgroups')) return response({ items: [{ id: '', name: '全部字幕组', is_all: true }, { id: '583', name: 'ANi', is_all: false }] })
      if (path.includes('/mikan/episodes')) return response({ mikan_id: '3997', total: 0, items: [] })
      throw new Error(`unexpected request: ${path}`)
    })
    vi.stubGlobal('fetch', fetchMock)

    const wrapper = mountDialog({ initialSearch: '可能重名的译名', initialBangumiSubjectId: '598058' })
    await waitForText(wrapper, '精确匹配番剧')
    await waitForText(wrapper, 'ANi')

    expect(wrapper.text()).toContain('MIKAN #3997')
    expect(fetchMock.mock.calls.some(call => String(call[0]).includes('/subscriptions/search'))).toBe(false)
  })

  it('starts in Mikan search when an initial calendar title is provided', async () => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      const path = String(input)
      if (path.includes('/subscriptions/search?q=%E6%B5%8B%E8%AF%95%E7%95%AA%E5%89%A7')) {
        return response({ items: [{ mikan_id: '3141', title: '测试番剧 Mikan', image: 'https://mikanani.me/images/Bangumi/poster.jpg' }] })
      }
      throw new Error(`unexpected request: ${path}`)
    }))

    const wrapper = mountDialog({ initialSearch: '测试番剧' })
    await waitForText(wrapper, '测试番剧 Mikan')
    expect((wrapper.get('#mikan-search').element as HTMLInputElement).value).toBe('测试番剧')
    expect(wrapper.get('img[alt="测试番剧 Mikan 海报"]').attributes('src')).toContain('/api/v1/subscriptions/mikan/poster?')
  })

  it('searches, previews a subgroup and emits the complete subscription preset', async () => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      const path = String(input)
      if (path.includes('/mikan/dashboard')) return response({ season: '2026 夏季番组', days: { '1': [] } })
      if (path.includes('/subscriptions/search')) return response({ items: [{ mikan_id: '3141', title: '测试番剧', image: 'poster.jpg', is_subscribed: true, is_local: true }] })
      if (path.includes('/mikan/subgroups')) return response({ items: [{ id: '', name: '全部字幕组', is_all: true }, { id: '583', name: 'ANi', is_all: false }] })
      if (path.includes('/mikan/episodes')) return response({
        mikan_id: '3141',
        total: 2,
        items: [
          { title: '[ANi] 测试番剧 01 [1080P][CHS]', episode_num: '01', sub_group: 'ANi', resolution: '1080p', pub_date: '2026-07-23T00:00:00Z' },
          { title: '[ANi] 测试番剧 01 [720P][CHT]', episode_num: '01', sub_group: 'ANi', resolution: '720p', pub_date: '2026-07-23T00:00:00Z' },
        ],
      })
      throw new Error(`unexpected request: ${path}`)
    }))

    const wrapper = mountDialog()
    await buttonByText(wrapper, '搜索').trigger('click')
    await wrapper.get('#mikan-search').setValue('测试')
    await wrapper.get('form').trigger('submit')
    await waitForText(wrapper, '测试番剧')
    const searchResults = wrapper.get('[data-testid="mikan-search-results"]')
    expect(searchResults.text()).toContain('已订阅')
    expect(searchResults.text()).toContain('本地已有')
    await buttonByText(wrapper, '测试番剧').trigger('click')
    await waitForText(wrapper, 'ANi')
    expect(wrapper.get('[data-testid="mikan-selected-status"]').text()).toContain('已订阅')
    expect(wrapper.get('[data-testid="mikan-selected-status"]').text()).toContain('本地已有')
    await buttonByText(wrapper, 'ANi').trigger('click')
    await waitForText(wrapper, '[ANi] 测试番剧 01 [1080P][CHS]')
    expect(wrapper.get('[data-testid="mikan-episode-preview"]').classes()).toEqual(expect.arrayContaining(['mikan-preview-list', 'overflow-y-scroll']))
    await wrapper.get('#mikan-resolution-filter').setValue('1080p')
    await wrapper.get('#mikan-subtitle-language').setValue('chs')
    await wrapper.get('#mikan-include-rule').setValue('1080[Pp].*(CHS|简中)')
    await wrapper.get('#mikan-exclude-rule').setValue('(合集|NCOP)')
    expect(wrapper.text()).toContain('预览命中 1 / 2')
    expect(wrapper.text()).not.toContain('[720P][CHT]')
    await wrapper.get('[data-testid="confirm-mikan-selection"]').trigger('click')

    expect(wrapper.emitted('select')?.[0]?.[0]).toMatchObject({
      mikan_id: '3141',
      title: '测试番剧',
      subtitle_group: 'ANi',
      rss_url: 'https://mikanani.me/RSS/Bangumi?bangumiId=3141&subgroupid=583',
      backup_rss_url: '',
      filter_rule: '1080[Pp].*(CHS|简中)',
      exclude_rule: '(合集|NCOP)',
      resolution_filter: '1080p',
      subtitle_language: 'chs',
      allow_multi_subgroup: false,
    })
  })

  it('rejects an invalid custom regex before confirming the Mikan source', async () => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      const path = String(input)
      if (path.includes('/mikan/dashboard')) return response({ season: '2026 夏季番组', days: { '1': [{ mikan_id: '9', title: '测试番剧', image: '' }] } })
      if (path.includes('/mikan/subgroups')) return response({ items: [{ id: '8', name: '字幕组', is_all: false }] })
      if (path.includes('/mikan/episodes')) return response({ mikan_id: '9', total: 1, items: [{ title: '[字幕组] 测试番剧 01 [1080P]', episode_num: '01', sub_group: '字幕组', resolution: '1080p', pub_date: '' }] })
      throw new Error(`unexpected request: ${path}`)
    }))

    const wrapper = mountDialog()
    await waitForText(wrapper, '测试番剧')
    await buttonByText(wrapper, '测试番剧').trigger('click')
    await waitForText(wrapper, '必须包含（正则）')
    await wrapper.get('#mikan-include-rule').setValue('[未闭合')

    expect(wrapper.text()).toContain('正则错误')
    expect(wrapper.get('[data-testid="confirm-mikan-selection"]').attributes()).toHaveProperty('disabled')
  })

  it('shows a recoverable dashboard error and retries in place', async () => {
    let attempts = 0
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      const path = String(input)
      if (!path.includes('/mikan/dashboard')) throw new Error(`unexpected request: ${path}`)
      attempts += 1
      if (attempts === 1) return response('季度番组暂时不可用', 502)
      return response({ season: '2026 夏季番组', days: { '1': [{ mikan_id: '9', title: '恢复成功', image: '', is_subscribed: true, is_local: true }] } })
    }))

    const wrapper = mountDialog()
    await waitForText(wrapper, '季度番组加载失败')
    await buttonByText(wrapper, '重试').trigger('click')
    await waitForText(wrapper, '恢复成功')
    const dashboardResults = wrapper.get('[data-testid="mikan-dashboard-results"]')
    expect(dashboardResults.text()).toContain('已订阅')
    expect(dashboardResults.text()).toContain('本地已有')
    expect(attempts).toBe(2)
  })
})
