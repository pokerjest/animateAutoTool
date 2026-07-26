import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { createPinia, setActivePinia } from 'pinia'
import LocalAnimeView from './LocalAnimeView.vue'

interface TestAnime {
  ID: number
  title: string
  image: string
  path: string
  file_count: number
  total_size: number
  season: number
  summary: string
  has_repair_actions: boolean
}

const anime = (id: number, title: string): TestAnime => ({
  ID: id,
  title,
  image: '',
  path: `/library/${id}`,
  file_count: 1,
  total_size: 1,
  season: 1,
  summary: '',
  has_repair_actions: false,
})

function response(items: TestAnime[], page: number, pageSize: number, total: number) {
  return Promise.resolve(new Response(JSON.stringify({
    data: { directories: [], items, scan_status: {}, diagnostics: [] },
    meta: { page, page_size: pageSize, total },
  }), { headers: { 'Content-Type': 'application/json' } }))
}

function mountView() {
  const pinia = createPinia()
  setActivePinia(pinia)
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
  return mount(LocalAnimeView, {
    global: {
      plugins: [pinia, [VueQueryPlugin, { queryClient }]],
      stubs: {
        RouterLink: { template: '<a><slot /></a>' },
        AutoLoadSentinel: { template: '<button data-testid="load-more" @click="$emit(\'load\')">load</button>' },
      },
    },
  })
}

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

describe('LocalAnimeView pagination', () => {
  it('loads the next server page automatically and displays the server total', async () => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      const requestURL = new URL(String(input), 'http://localhost')
      const page = Number(requestURL.searchParams.get('page'))
      if (page === 1) return response([anime(1, '第一页 A'), anime(2, '第一页 B')], 1, 2, 3)
      if (page === 2) return response([anime(3, '第二页 C')], 2, 2, 3)
      throw new Error(`unexpected request: ${requestURL}`)
    }))

    const wrapper = mountView()
    await vi.waitFor(() => expect(wrapper.text()).toContain('3 部本地番剧'))
    expect(wrapper.text()).not.toContain('第二页 C')

    await wrapper.get('[data-testid="load-more"]').trigger('click')
    await vi.waitFor(() => expect(wrapper.text()).toContain('第二页 C'))
    expect(wrapper.find('[data-testid="load-more"]').exists()).toBe(false)
  })

  it('searches on the server and starts again from page one', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const requestURL = new URL(String(input), 'http://localhost')
      const query = requestURL.searchParams.get('q') || ''
      if (query === '跨页命中') return response([anime(99, '跨页命中')], 1, 48, 1)
      return response([anime(1, '首页番剧')], 1, 48, 198)
    })
    vi.stubGlobal('fetch', fetchMock)

    const wrapper = mountView()
    await vi.waitFor(() => expect(wrapper.text()).toContain('198 部本地番剧'))
    await wrapper.get('input[aria-label="搜索本地番剧"]').setValue('跨页命中')
    await new Promise(resolve => setTimeout(resolve, 300))
    await flushPromises()

    await vi.waitFor(() => expect(wrapper.text()).toContain('1 部本地番剧'))
    expect(wrapper.text()).toContain('跨页命中')
    expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining('page=1&page_size=48&q=%E8%B7%A8%E9%A1%B5%E5%91%BD%E4%B8%AD'), expect.anything())
  })
})
