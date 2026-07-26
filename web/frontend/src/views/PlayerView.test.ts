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
  vi.restoreAllMocks()
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

  it('controls Jellyfin user state, displays media details, and continues with the next episode', async () => {
    const playMock = vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined)
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      if (path.endsWith('/api/v1/local-anime/1/episodes')) {
        return envelope({
          anime: { ID: 1, title: '测试番剧', summary: '', image: '', metadata: { title_cn: '测试番剧', bangumi_id: 99 } },
          episodes: [
            { id: 11, name: '01.mkv', episode: 1, season: 1, playable: true, thumbnail: '', overview: '', duration: '24m', progress_percent: 50 },
            { id: 12, name: '02.mkv', episode: 2, season: 1, playable: true, thumbnail: '', overview: '', duration: '24m' },
          ],
          collection_status: { bangumi_watched_count: 0, anilist_watched_count: 0 },
        })
      }
      if (path.includes('/api/v1/jellyfin/play/')) {
        const episodeID = path.endsWith('/12') ? 12 : 11
        return raw({
          stream_url: `/api/v1/jellyfin/stream/${episodeID}`,
          direct_stream_url: '',
          resume_ticks: 0,
          runtime_ticks: 14_400_000_000,
          played: false,
          episode_favorite: false,
          series_favorite: false,
          media: {
            container: 'mkv', size: 1_073_741_824, bitrate: 8_000_000,
            width: 1920, height: 1080, video_codec: 'hevc', audio_codec: 'aac', audio_channels: 2, subtitle_count: 3,
          },
          poster_url: '', title: '测试番剧', episode_title: `Episode ${episodeID}`,
        })
      }
      if (path.endsWith('/api/v1/jellyfin/episodes/11/user-state')) {
        const body = JSON.parse(String(init?.body || '{}')) as { played?: boolean; favorite?: boolean }
        return envelope({ played: body.played ?? true, favorite: body.favorite ?? false })
      }
      if (path.endsWith('/api/v1/jellyfin/series/1/user-state')) return envelope({ favorite: true })
      if (path.endsWith('/api/v1/jellyfin/progress')) return raw({ ok: true })
      throw new Error(`unexpected request: ${path}`)
    })
    vi.stubGlobal('fetch', fetchMock)

    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/player', component: PlayerView }] })
    await router.push('/player?anime=1')
    await router.isReady()
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
    const wrapper = mount(PlayerView, {
      attachTo: document.body,
      global: { plugins: [createPinia(), router, [VueQueryPlugin, { queryClient }]] },
    })
    const button = (label: string) => {
      const match = wrapper.findAll('button').find(item => item.text().includes(label))
      if (!match) throw new Error(`missing button: ${label}`)
      return match
    }

    await vi.waitFor(() => expect(wrapper.text()).toContain('1920×1080'))
    expect(wrapper.text()).toContain('HEVC')
    expect(wrapper.text()).toContain('AAC · 2 声道')
    expect(wrapper.text()).toContain('8.0 Mbps')
    expect(wrapper.text()).toContain('1.0 GB')
    expect(wrapper.text()).toContain('3 条字幕')
    expect(wrapper.text()).toContain('Jellyfin 已播放 50%')

    await button('标记已看').trigger('click')
    await vi.waitFor(() => expect(wrapper.text()).toContain('设为未看'))
    await button('收藏本集').trigger('click')
    await vi.waitFor(() => expect(wrapper.text()).toContain('已收藏本集'))
    await button('收藏到 Jellyfin').trigger('click')
    await vi.waitFor(() => expect(wrapper.text()).toContain('已收藏整部'))

    await button('下一集').trigger('click')
    await vi.waitFor(() => expect(wrapper.get('video').attributes('src')).toBe('/api/v1/jellyfin/stream/12'))
    await button('上一集').trigger('click')
    await vi.waitFor(() => expect(wrapper.get('video').attributes('src')).toBe('/api/v1/jellyfin/stream/11'))
    await wrapper.get('video').trigger('ended')
    await vi.waitFor(() => expect(wrapper.get('video').attributes('src')).toBe('/api/v1/jellyfin/stream/12'))
    await wrapper.get('video').trigger('loadedmetadata')
    expect(playMock).toHaveBeenCalledOnce()

    expect(fetchMock).toHaveBeenCalledWith('/api/v1/jellyfin/episodes/11/user-state', expect.objectContaining({ method: 'PUT' }))
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/jellyfin/series/1/user-state', expect.objectContaining({ method: 'PUT' }))
    wrapper.unmount()
    queryClient.clear()
    playMock.mockRestore()
  })
})
