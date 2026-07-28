import { afterEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, nextTick } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import PlaybackHost from '../components/PlaybackHost.vue'
import { usePlaybackStore } from '../stores/playback'
import { useSessionStore } from '../stores/session'
import { useWorkspaceStore } from '../stores/workspace'
import PlayerView from './PlayerView.vue'

function envelope(data: unknown) {
  return Promise.resolve(new Response(JSON.stringify({ data }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
}

function raw(data: unknown) {
  return Promise.resolve(new Response(JSON.stringify(data), { status: 200, headers: { 'Content-Type': 'application/json' } }))
}

function playerFetch(direct = false, netbird = false) {
  return vi.fn((input: RequestInfo | URL) => {
    const path = String(input)
    if (path.includes('/api/v1/playback/continue')) return envelope({ items: [] })
    if (path.endsWith('/api/v1/local-anime/1/episodes')) {
      return envelope({
        anime: { ID: 1, title: '测试番剧', summary: '', image: '', metadata: { title_cn: '测试番剧', bangumi_id: 99 } },
        episodes: [
          { id: 11, name: '01.mkv', episode: 1, season: 1, playable: true, thumbnail: '', overview: '', duration: '24m', progress_percent: 50, resume_ticks: 500_000_000 },
          { id: 12, name: '02.mkv', episode: 2, season: 1, playable: true, thumbnail: '', overview: '', duration: '24m' },
        ],
        collection_status: { bangumi_watched_count: 0, anilist_watched_count: 0 },
      })
    }
    if (path.includes('/api/v1/jellyfin/play/')) {
      const episodeID = path.endsWith('/12') ? 12 : 11
      return raw({
        stream_url: `/api/v1/jellyfin/stream/${episodeID}`,
        direct_stream_url: direct ? `https://media.example-tailnet.ts.net/Videos/${episodeID}/stream` : '',
        netbird_stream_url: netbird ? `https://media.netbird.example/api/v1/netbird/jellyfin/stream/${episodeID}?token=signed` : '',
        resume_ticks: episodeID === 11 ? 500_000_000 : 0,
        runtime_ticks: 14_400_000_000,
        played: false,
        episode_favorite: false,
        series_favorite: false,
        media: { container: 'mkv', size: 1_073_741_824, bitrate: 8_000_000, width: 1920, height: 1080, video_codec: 'hevc', audio_codec: 'aac', audio_channels: 2, subtitle_count: 3 },
        poster_url: '', title: '测试番剧', episode_title: `Episode ${episodeID}`,
      })
    }
    if (path.endsWith('/api/v1/playback/progress')) return envelope({ position_ticks: 0 })
    throw new Error(`unexpected request: ${path}`)
  })
}

async function mountPlayer(fetchMock: ReturnType<typeof vi.fn>, path = '/player') {
  vi.stubGlobal('fetch', fetchMock)
  const sendBeacon = vi.fn(() => true)
  Object.defineProperty(navigator, 'sendBeacon', { value: sendBeacon, configurable: true })
  vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined)
  vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => undefined)
  vi.spyOn(HTMLMediaElement.prototype, 'load').mockImplementation(() => undefined)
  const Other = defineComponent({ template: '<div>其他页面</div>' })
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/player', component: PlayerView }, { path: '/media/local-player', component: PlayerView }, { path: '/other', component: Other }],
  })
  await router.push(`${path}?anime=1&episode=11`)
  await router.isReady()
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
  const pinia = createPinia()
  const workspace = useWorkspaceStore(pinia)
  workspace.setMode('media')
  const Root = defineComponent({ components: { PlaybackHost, RouterView }, template: '<PlaybackHost/><RouterView/>' })
  const wrapper = mount(Root, {
    attachTo: document.body,
    global: { plugins: [pinia, router, [VueQueryPlugin, { queryClient }]] },
  })
  await vi.waitFor(() => expect(wrapper.find('video').exists()).toBe(true))
  return { wrapper, router, queryClient, playback: usePlaybackStore(pinia), workspace, sendBeacon }
}

afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  localStorage.removeItem('player.sourceMode')
  localStorage.removeItem('player.preferredSource')
  localStorage.removeItem('animate.workspace.mode')
  document.body.innerHTML = ''
})

describe('Plex-style global playback', () => {
  it('renders local playback as a full media-workspace player', async () => {
    const mounted = await mountPlayer(playerFetch(), '/media/local-player')
    await vi.waitFor(() => expect(mounted.wrapper.get('video').attributes('src')).toBe('/api/v1/jellyfin/stream/11'))

    expect(mounted.router.currentRoute.value.path).toBe('/media/local-player')
    expect(mounted.wrapper.find('[aria-label="全局播放器"]').classes()).toContain('panel')
    expect(mounted.playback.current?.localEpisodeId).toBe(11)

    mounted.wrapper.unmount()
    mounted.queryClient.clear()
  })

  it('waits for first-run setup before loading continue watching', async () => {
    const fetchMock = vi.fn(() => envelope({ items: [] }))
    vi.stubGlobal('fetch', fetchMock)
    const Other = defineComponent({ template: '<div>初始化页面</div>' })
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/other', component: Other }] })
    await router.push('/other')
    await router.isReady()
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
    const pinia = createPinia()
    const session = useSessionStore(pinia)
    session.state = {
      authenticated: true,
      setup_pending: true,
      local_setup_available: false,
      local_recovery_available: false,
      version: 'test',
      recovery_local_only: true,
    }
    const wrapper = mount(PlaybackHost, {
      global: { plugins: [pinia, router, [VueQueryPlugin, { queryClient }]] },
    })

    await flushPromises()
    expect(fetchMock).not.toHaveBeenCalled()

    session.state.setup_pending = false
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledWith('/api/v1/playback/continue?limit=10', expect.any(Object)))
    wrapper.unmount()
    queryClient.clear()
  })

  it('keeps the same video element and source while navigating to another page', async () => {
    const mounted = await mountPlayer(playerFetch())
    await vi.waitFor(() => expect(mounted.wrapper.get('video').attributes('src')).toBe('/api/v1/jellyfin/stream/11'))
    const originalVideo = mounted.wrapper.get('video').element

    await mounted.router.push('/other')
    await flushPromises()

    expect(mounted.wrapper.get('video').element).toBe(originalVideo)
    expect(mounted.wrapper.get('video').attributes('src')).toBe('/api/v1/jellyfin/stream/11')
    expect(mounted.wrapper.text()).toContain('其他页面')
    expect(mounted.wrapper.text()).toContain('测试番剧')

    window.dispatchEvent(new PageTransitionEvent('pagehide'))
    expect(mounted.sendBeacon).toHaveBeenCalledWith('/api/v1/playback/progress', expect.any(Blob))
    mounted.wrapper.unmount()
    mounted.queryClient.clear()
  })

  it('pauses and hides playback in management mode without discarding the current media', async () => {
    const mounted = await mountPlayer(playerFetch())
    await vi.waitFor(() => expect(mounted.playback.current?.localEpisodeId).toBe(11))
    const originalSource = mounted.playback.source

    mounted.playback.position = 50
    mounted.playback.pauseForWorkspaceSwitch()
    mounted.workspace.setMode('manage')
    await nextTick()

    expect(mounted.wrapper.find('video').exists()).toBe(false)
    expect(mounted.playback.current?.localEpisodeId).toBe(11)
    expect(mounted.playback.source).toBe(originalSource)
    expect(mounted.playback.position).toBe(50)

    mounted.workspace.setMode('media')
    await nextTick()
    expect(mounted.wrapper.find('video').exists()).toBe(true)
    expect(mounted.playback.playing).toBe(false)

    mounted.wrapper.unmount()
    mounted.queryClient.clear()
  })

  it('keeps a stalled remembered direct stream on the selected route', async () => {
    vi.useFakeTimers()
    localStorage.setItem('player.sourceMode', 'direct')
    const mounted = await mountPlayer(playerFetch(true))
    await vi.waitFor(() => expect(mounted.wrapper.get('video').attributes('src')).toContain('example-tailnet.ts.net'))
    const video = mounted.wrapper.get('video')
    Object.defineProperty(video.element, 'paused', { value: false, configurable: true })

    await video.trigger('waiting')
    vi.advanceTimersByTime(8_000)
    await flushPromises()
    await nextTick()

    expect(mounted.wrapper.get('video').attributes('src')).toContain('example-tailnet.ts.net')
    expect(mounted.playback.sourceMode).toBe('direct')
    expect(localStorage.getItem('player.preferredSource')).toBe('direct')
    await video.trigger('error')
    expect(mounted.wrapper.get('video').attributes('src')).toContain('example-tailnet.ts.net')
    expect(mounted.wrapper.text()).toContain('Jellyfin 直连视频流加载失败')
    expect(mounted.playback.switchingSource).toBe(false)
    expect(mounted.playback.stalled).toBe(false)

    const proxyButton = mounted.wrapper.findAll('button').find(button => button.text().includes('AnimateTool 代理'))
    expect(proxyButton?.attributes('disabled')).toBeUndefined()
    await proxyButton!.trigger('click')
    expect(mounted.playback.activeSource).toBe('proxy')
    expect(mounted.wrapper.get('video').attributes('src')).toBe('/api/v1/jellyfin/stream/11')
    mounted.wrapper.unmount()
    mounted.queryClient.clear()
  })

  it('migrates a remembered NetBird preference to the AnimateTool proxy', async () => {
    localStorage.setItem('player.sourceMode', 'netbird')
    const mounted = await mountPlayer(playerFetch(false, true))
    expect(mounted.wrapper.get('video').attributes('src')).toBe('/api/v1/jellyfin/stream/11')
    expect(mounted.playback.sourceMode).toBe('proxy')
    expect(localStorage.getItem('player.preferredSource')).toBe('proxy')
    expect(localStorage.getItem('player.sourceMode')).toBe('proxy')
    mounted.wrapper.unmount()
    mounted.queryClient.clear()
  })

  it('shows only the two current source choices in the full player', async () => {
    localStorage.setItem('player.preferredSource', 'netbird')
    const mounted = await mountPlayer(playerFetch(true, true))
    await vi.waitFor(() => expect(mounted.wrapper.get('video').attributes('src')).toBe('/api/v1/jellyfin/stream/11'))
    const sourceGroup = mounted.wrapper.find('[role="group"][aria-label="播放线路"]')
    expect(sourceGroup.exists()).toBe(true)
    expect(sourceGroup.text()).toContain('AnimateTool 代理')
    expect(sourceGroup.text()).toContain('Jellyfin 直连')
    expect(mounted.wrapper.text()).not.toContain('NetBird')
    expect(localStorage.getItem('player.preferredSource')).toBe('proxy')
    mounted.playback.onLoadedMetadata()
    await mounted.playback.switchSource('direct')
    await vi.waitFor(() => expect(mounted.wrapper.get('video').attributes('src')).toContain('media.example-tailnet.ts.net'))
    const proxyButton = mounted.wrapper.findAll('button').find(button => button.text().includes('AnimateTool 代理'))
    expect(proxyButton?.attributes('disabled')).toBeUndefined()
    expect(localStorage.getItem('player.preferredSource')).toBe('direct')

    mounted.wrapper.unmount()
    mounted.queryClient.clear()
  })

  it('advances to the next episode only after the ended event', async () => {
    const mounted = await mountPlayer(playerFetch())
    await vi.waitFor(() => expect(mounted.playback.current?.localEpisodeId).toBe(11))
    const video = mounted.wrapper.get('video')
    Object.defineProperty(video.element, 'currentTime', { value: 1_300, writable: true, configurable: true })
    Object.defineProperty(video.element, 'duration', { value: 1_440, configurable: true })

    await video.trigger('timeupdate')
    expect(mounted.playback.current?.localEpisodeId).toBe(11)
    await video.trigger('ended')
    await vi.waitFor(() => expect(mounted.playback.current?.localEpisodeId).toBe(12))
    await vi.waitFor(() => expect(mounted.wrapper.get('video').attributes('src')).toBe('/api/v1/jellyfin/stream/12'))

    mounted.wrapper.unmount()
    mounted.queryClient.clear()
  })
})
