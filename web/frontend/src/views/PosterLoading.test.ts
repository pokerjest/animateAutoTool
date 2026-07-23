import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { createPinia } from 'pinia'
import LibraryView from './LibraryView.vue'

afterEach(() => vi.unstubAllGlobals())

describe('poster loading', () => {
  it('renders a small first batch and automatically loads more near the end', async () => {
    let onIntersect: IntersectionObserverCallback | undefined
    vi.stubGlobal('IntersectionObserver', class {
      constructor(callback: IntersectionObserverCallback) { onIntersect = callback }
      observe() {}
      disconnect() {}
      unobserve() {}
      takeRecords() { return [] }
      root = null
      rootMargin = '320px 0px'
      thresholds = [0]
    })
    const items = Array.from({ length: 30 }, (_, index) => ({
      ID: index + 1,
      UpdatedAt: '2026-07-23T12:00:00Z',
      title: `番剧 ${index + 1}`,
      title_cn: `番剧 ${index + 1}`,
      title_jp: '',
      image: '',
      summary: '',
      air_date: '',
      bangumi_id: index + 1,
      tmdb_id: 0,
      anilist_id: 0,
      data_source: 'bangumi',
      is_subscribed: false,
      is_local: false,
      local_anime_id: 0,
    }))
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ data: { items } }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })))
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
    const wrapper = mount(LibraryView, {
      global: {
        plugins: [createPinia(), [VueQueryPlugin, { queryClient }]],
        stubs: { RouterLink: { template: '<a><slot /></a>' } },
      },
    })

    await vi.waitFor(() => expect(wrapper.findAll('img')).toHaveLength(24))
    const first = wrapper.get('img')
    expect(first.attributes('src')).toContain('/api/v1/posters/1?width=360&v=')
    expect(first.attributes('loading')).toBe('lazy')
    expect(first.attributes('decoding')).toBe('async')

    expect(wrapper.text()).not.toContain('继续加载')
    expect(wrapper.find('[data-testid="auto-load-sentinel"]').exists()).toBe(true)
    expect(onIntersect).toBeDefined()
    onIntersect!([{ isIntersecting: true } as IntersectionObserverEntry], {} as IntersectionObserver)
    await wrapper.vm.$nextTick()
    expect(wrapper.findAll('img')).toHaveLength(30)
    expect(wrapper.find('[data-testid="auto-load-sentinel"]').exists()).toBe(false)
  })
})
