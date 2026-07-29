import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { createPinia } from 'pinia'
import LibraryView from './LibraryView.vue'

const AppDialogStub = {
  props: ['open'],
  template: '<section v-if="open"><slot /></section>',
}

function response(data: unknown) {
  return Promise.resolve(new Response(JSON.stringify({ data }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  }))
}

function buttonByText(wrapper: VueWrapper, text: string) {
  const button = wrapper.findAll('button').find(item => item.text().includes(text))
  if (!button) throw new Error(`button not found: ${text}`)
  return button
}

afterEach(() => vi.unstubAllGlobals())

describe('metadata search', () => {
  it('renders every typed Bangumi result with its title and id', async () => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      const path = String(input)
      if (path.endsWith('/api/v1/library')) {
        return response({
          items: [{
            ID: 1,
            title: '测试番剧',
            title_cn: '测试番剧',
            title_jp: '',
            image: '',
            summary: '',
            air_date: '',
            bangumi_id: 0,
            tmdb_id: 0,
            anilist_id: 0,
            data_source: 'bangumi',
            is_subscribed: false,
            is_local: false,
            local_anime_id: 0,
          }],
        })
      }
      if (path.includes('/api/v1/metadata/search?')) {
        return response([
          { id: 101, name: 'First', name_cn: '第一部', images: { large: '', common: '', medium: '', small: '', grid: '' }, summary: '', air_date: '' },
          { id: 202, name: 'Second', name_cn: '第二部', images: { large: '', common: '', medium: '', small: '', grid: '' }, summary: '', air_date: '' },
        ])
      }
      throw new Error(`unexpected request: ${path}`)
    }))

    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
    const wrapper = mount(LibraryView, {
      global: {
        plugins: [createPinia(), [VueQueryPlugin, { queryClient }]],
        stubs: {
          AppDialog: AppDialogStub,
          RouterLink: { template: '<a><slot /></a>' },
        },
      },
    })

    await vi.waitFor(() => expect(wrapper.text()).toContain('测试番剧'))
    expect(wrapper.text()).not.toContain('详情与匹配')
    const poster = wrapper.get('[data-testid="poster-open"]')
    expect(poster.attributes('aria-label')).toBe('查看详情 测试番剧')
    await poster.trigger('click')
    const inputs = wrapper.findAll('input')
    await inputs[1].setValue('测试')
    await buttonByText(wrapper, '搜索').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('第一部')
    expect(wrapper.text()).toContain('#101')
    expect(wrapper.text()).toContain('第二部')
    expect(wrapper.text()).toContain('#202')
    expect(wrapper.text()).not.toContain('未命名条目')
    expect(wrapper.findAll('button').filter(button => button.text().includes('使用此匹配'))).toHaveLength(2)
  })
})
