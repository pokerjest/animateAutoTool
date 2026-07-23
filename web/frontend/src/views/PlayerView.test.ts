import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import PlayerView from './PlayerView.vue'

function envelope(data: unknown) {
  return Promise.resolve(new Response(JSON.stringify({ data }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
}

function raw(data: unknown) {
  return Promise.resolve(new Response(JSON.stringify(data), { status: 200, headers: { 'Content-Type': 'application/json' } }))
}

afterEach(() => {
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('PlayerView Jellyfin direct playback', () => {
  it('prefers the configured Tailscale stream and falls back to the proxy on failure', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const path = String(input)
      if (path.endsWith('/api/v1/local-anime/1/episodes')) {
        return envelope({
          anime: { ID: 1, title: '测试番剧', summary: '', image: '', metadata: { title_cn: '测试番剧', bangumi_id: 99 } },
          episodes: [{ id: 11, name: '01.mkv', episode: 1, season: 1, playable: true, thumbnail: '', overview: '', duration: '24m' }],
          collection_status: { bangumi_watched_count: 0, anilist_watched_count: 0 },
        })
      }
      if (path.endsWith('/api/v1/jellyfin/play/11')) {
        return raw({
          stream_url: '/api/v1/jellyfin/stream/11',
          direct_stream_url: 'https://media.example-tailnet.ts.net/Videos/episode-1/stream?api_key=token&static=true',
          resume_ticks: 0,
        })
      }
      throw new Error(`unexpected request: ${path}`)
    })
    vi.stubGlobal('fetch', fetchMock)

    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/player', component: PlayerView }],
    })
    await router.push('/player?anime=1')
    await router.isReady()
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
    const wrapper = mount(PlayerView, {
      attachTo: document.body,
      global: { plugins: [createPinia(), router, [VueQueryPlugin, { queryClient }]] },
    })

    await vi.waitFor(() => expect(wrapper.find('video').exists()).toBe(true))
    expect(wrapper.get('video').attributes('src')).toContain('media.example-tailnet.ts.net')
    expect(wrapper.text()).toContain('Tailscale 直连')

    await wrapper.get('video').trigger('error')
    await flushPromises()
    expect(wrapper.get('video').attributes('src')).toBe('/api/v1/jellyfin/stream/11')
    expect(wrapper.text()).toContain('服务端代理')

    wrapper.unmount()
    queryClient.clear()
  })
})
