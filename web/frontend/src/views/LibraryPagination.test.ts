import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { createPinia } from 'pinia'
import LibraryView from './LibraryView.vue'

function item(id: number) {
  return {
    ID: id,
    title: `番剧 ${id}`,
    title_cn: `番剧 ${id}`,
    title_jp: '',
    title_en: '',
    image: '',
    summary: '',
    air_date: '',
    bangumi_id: id,
    tmdb_id: 0,
    anilist_id: 0,
    data_source: 'bangumi',
    is_subscribed: false,
    is_local: false,
    local_anime_id: 0,
  }
}

function response(items: ReturnType<typeof item>[], page: number, total: number) {
  return Promise.resolve(new Response(JSON.stringify({
    data: { items },
    meta: { page, page_size: 48, total },
  }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
}

afterEach(() => vi.unstubAllGlobals())

describe('library pagination', () => {
  it('loads the next server page and keeps the total count', async () => {
    vi.stubGlobal('IntersectionObserver', undefined)
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      const url = new URL(String(input), 'http://localhost')
      if (url.pathname !== '/api/v1/library') throw new Error(`unexpected request: ${url}`)
      const page = Number(url.searchParams.get('page') || '1')
      return page === 1
        ? response(Array.from({ length: 48 }, (_, index) => item(index + 1)), 1, 49)
        : response([item(49)], 2, 49)
    }))

    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
    const wrapper = mount(LibraryView, {
      global: {
        plugins: [createPinia(), [VueQueryPlugin, { queryClient }]],
        stubs: {
          AppDialog: { props: ['open'], template: '<section v-if="open"><slot /></section>' },
          AutoLoadSentinel: { emits: ['load'], template: '<button data-testid="load-more" @click="$emit(\'load\')">load</button>' },
          RouterLink: { template: '<a><slot /></a>' },
        },
      },
    })

    await vi.waitFor(() => expect(wrapper.text()).toContain('49 部番剧'))
    expect(wrapper.text()).toContain('番剧 48')
    expect(wrapper.text()).not.toContain('番剧 49')

    await wrapper.get('[data-testid="load-more"]').trigger('click')
    await flushPromises()
    await vi.waitFor(() => expect(wrapper.text()).toContain('番剧 49'))
    expect(wrapper.findAll('[data-testid="poster-open"]')).toHaveLength(49)
  })
})
