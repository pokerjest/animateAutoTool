import { afterEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { createPinia } from 'pinia'
import CalendarView from './CalendarView.vue'
import type { MikanSubscriptionSelection } from '../api/types'

const AppDialogStub = {
  props: ['open'],
  emits: ['update:open'],
  template: '<div v-if="open"><slot /></div>',
}

const selection: MikanSubscriptionSelection = {
  mikan_id: '3141',
  title: '测试番剧 Mikan',
  image: 'poster.jpg',
  season: '',
  subgroup_id: '583',
  subtitle_group: 'ANi',
  rss_url: 'https://mikanani.me/RSS/Bangumi?bangumiId=3141&subgroupid=583',
  backup_rss_url: 'https://mikanani.me/RSS/Bangumi?bangumiId=3141',
  filter_rule: 'ANi',
  allow_multi_subgroup: false,
}

const MikanDiscoveryDialogStub = defineComponent({
  props: { open: Boolean, initialSearch: String },
  emits: ['update:open', 'select'],
  setup(_props, { emit }) {
    return { choose: () => emit('select', selection) }
  },
  template: '<div v-if="open" data-testid="mikan-dialog"><span>{{ initialSearch }}</span><button data-testid="choose-mikan" @click="choose">确认 Mikan 源</button></div>',
})

function response(data: unknown) {
  return Promise.resolve(new Response(JSON.stringify({ data }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
}

function buttonByText(wrapper: ReturnType<typeof mount>, text: string) {
  const button = wrapper.findAll('button').find(item => item.text().includes(text))
  if (!button) throw new Error(`button not found: ${text}`)
  return button
}

afterEach(() => {
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('CalendarView', () => {
  it('opens details from the poster and saves the selected Mikan source', async () => {
    let subscriptionBody: Record<string, unknown> | undefined
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      if (path.endsWith('/api/v1/calendar')) return response({
        today: 1,
        days: [{
          weekday: { id: 1, cn: '星期一', en: 'Mon' },
          items: [{ id: 99, name: 'Test Anime', name_cn: '测试番剧', images: { large: 'https://lain.bgm.tv/pic/cover/l/poster.jpg' }, air_date: '2026-07-23', summary: '简介' }],
        }],
      })
      if (path.endsWith('/api/v1/subscriptions')) {
        subscriptionBody = JSON.parse(String(init?.body)) as Record<string, unknown>
        return response({ ID: 1 })
      }
      throw new Error(`unexpected request: ${path}`)
    })
    vi.stubGlobal('fetch', fetchMock)

    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
    const wrapper = mount(CalendarView, {
      attachTo: document.body,
      global: {
        plugins: [createPinia(), [VueQueryPlugin, { queryClient }]],
        stubs: {
          AppDialog: AppDialogStub,
          MikanDiscoveryDialog: MikanDiscoveryDialogStub,
          RouterLink: { template: '<a><slot /></a>' },
        },
      },
    })

    await vi.waitFor(() => expect(wrapper.text()).toContain('测试番剧'))
    expect(wrapper.text()).not.toContain('查看详情')
    expect(wrapper.get('img').attributes('src')).toBe('/api/v1/calendar/posters/99?width=360')

    await wrapper.get('[data-testid="poster-open"]').trigger('click')
    expect(wrapper.text()).toContain('从 Mikan 添加订阅')
    expect(wrapper.findAll('img').some(image => image.attributes('src') === '/api/v1/calendar/posters/99?width=720')).toBe(true)
    await buttonByText(wrapper, '从 Mikan 添加订阅').trigger('click')
    expect(wrapper.get('[data-testid="mikan-dialog"]').text()).toContain('测试番剧')

    await wrapper.get('[data-testid="choose-mikan"]').trigger('click')
    await flushPromises()
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))

    expect(subscriptionBody).toMatchObject({
      mikan_id: '3141',
      subtitle_group: 'ANi',
      rss_url: 'https://mikanani.me/RSS/Bangumi?bangumiId=3141&subgroupid=583',
      backup_rss_url: 'https://mikanani.me/RSS/Bangumi?bangumiId=3141',
      filter_rule: 'ANi',
    })
  })
})
