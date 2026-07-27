import { defineStore } from 'pinia'
import { api } from '../api/client'
import type { ContinueWatchingItem, JellyfinPlayInfo, PlaybackProgressInput } from '../api/types'
import { useUIStore } from './ui'

export type PlaybackSourceMode = 'proxy' | 'direct' | 'netbird'

export type ActivePlaybackInfo = {
  provider?: string
  item_id?: string
  stream_url: string
  direct_stream_url?: string
  netbird_stream_url?: string
  resume_ticks: number
  runtime_ticks: number
  played: boolean
  favorite?: boolean
  episode_favorite?: boolean
  series_favorite?: boolean
  media: JellyfinPlayInfo['media']
}

export interface PlaybackSelection {
  provider: string
  itemId?: string
  localAnimeId?: number
  localEpisodeId?: number
  title: string
  episodeTitle?: string
  image?: string
  season?: number
  episode?: number
}

interface StartOptions {
  autoplay?: boolean
  resumeTicks?: number
  streamURL?: string
  skipCurrentReport?: boolean
}

let prepareRequest = 0
let stallTimer: ReturnType<typeof setTimeout> | null = null
let lastProgressReport = 0

function storedSourceMode(): PlaybackSourceMode {
  const preferred = localStorage.getItem('player.preferredSource')
  const legacy = localStorage.getItem('player.sourceMode')
  const stored = preferred || legacy
  const result = stored === 'direct' || stored === 'netbird' ? stored : 'proxy'
  if (preferred === null && legacy) localStorage.setItem('player.preferredSource', result)
  return result
}

function clearStallTimer() {
  if (stallTimer) clearTimeout(stallTimer)
  stallTimer = null
}

function selectionKey(selection?: PlaybackSelection | null) {
  if (!selection) return ''
  if (selection.provider !== 'local' && selection.itemId) return `${selection.provider}:${selection.itemId}`
  return selection.localEpisodeId ? `local:${selection.localEpisodeId}` : ''
}

export const usePlaybackStore = defineStore('playback', {
  state: () => ({
    current: null as PlaybackSelection | null,
    queue: [] as PlaybackSelection[],
    playInfo: null as ActivePlaybackInfo | null,
    source: '',
    activeSource: storedSourceMode() as PlaybackSourceMode,
    preferredSource: storedSourceMode() as PlaybackSourceMode,
    video: null as HTMLVideoElement | null,
    preparing: false,
    stalled: false,
    seeking: false,
    switchingSource: false,
    error: '',
    position: 0,
    duration: 0,
    playing: false,
    pendingResumeSeconds: 0,
    pendingAutoplay: false,
    restoreVolume: 1,
    restoreMuted: false,
    restorePlaybackRate: 1,
    autoNext: localStorage.getItem('player.autoNext') !== 'false',
  }),
  getters: {
    active: state => Boolean(state.current && state.source),
    sourceMode: state => state.activeSource,
    usingDirect: state => Boolean(state.playInfo?.direct_stream_url) && state.activeSource === 'direct' && state.source === state.playInfo?.direct_stream_url,
    usingNetBird: state => Boolean(state.playInfo?.netbird_stream_url) && state.activeSource === 'netbird' && state.source === state.playInfo?.netbird_stream_url,
    currentIndex: state => state.queue.findIndex(item => selectionKey(item) === selectionKey(state.current)),
    previousSelection(): PlaybackSelection | null {
      return this.currentIndex > 0 ? this.queue[this.currentIndex - 1] : null
    },
    nextSelection(): PlaybackSelection | null {
      return this.currentIndex >= 0 && this.currentIndex < this.queue.length - 1 ? this.queue[this.currentIndex + 1] : null
    },
  },
  actions: {
    attachVideo(video: HTMLVideoElement | null) {
      this.video = video
    },
    setQueue(queue: PlaybackSelection[]) {
      this.queue = queue
    },
    setAutoNext(value: boolean) {
      this.autoNext = value
      localStorage.setItem('player.autoNext', String(value))
    },
    selectionFromContinue(item: ContinueWatchingItem): PlaybackSelection {
      return {
        provider: 'local',
        itemId: String(item.episode_id),
        localAnimeId: item.anime_id,
        localEpisodeId: item.episode_id,
        title: item.title,
        episodeTitle: item.episode_title,
        image: item.image,
        season: item.season,
        episode: item.episode,
      }
    },
    captureVideoSettings() {
      if (!this.video) return
      this.restoreVolume = this.video.volume
      this.restoreMuted = this.video.muted
      this.restorePlaybackRate = this.video.playbackRate
    },
    pauseForWorkspaceSwitch() {
      if (!this.current) return
      this.captureVideoSettings()
      this.video?.pause()
      this.playing = false
      void this.report('pause')
    },
    async start(selection: PlaybackSelection, options: StartOptions = {}) {
      if (selectionKey(this.current) === selectionKey(selection) && this.source) {
        if (options.autoplay) {
          try { await this.video?.play() } catch { /* browser controls remain available */ }
        }
        return
      }
      if (this.current && !options.skipCurrentReport) void this.report('pause')
      this.captureVideoSettings()
      this.switchingSource = true
      const request = ++prepareRequest
      this.current = selection
      this.playInfo = null
      this.error = ''
      this.stalled = false
      this.preparing = true
      this.position = Math.max(0, (options.resumeTicks || 0) / 10_000_000)
      this.duration = 0
      this.pendingResumeSeconds = this.position
      this.pendingAutoplay = Boolean(options.autoplay)
      this.preferredSource = storedSourceMode()
      this.activeSource = this.preferredSource
      this.source = ''
      if (options.streamURL && this.activeSource === 'proxy') this.source = options.streamURL

      try {
        const external = selection.provider !== 'local' && selection.itemId
        if (!external && !selection.localEpisodeId) throw new Error('本地剧集缺少可播放的剧集 ID')
        const info = await api<ActivePlaybackInfo>(external
          ? `/media/providers/${encodeURIComponent(selection.provider)}/items/${encodeURIComponent(selection.itemId!)}/play`
          : `/jellyfin/play/${selection.localEpisodeId}`)
        if (request !== prepareRequest || selectionKey(this.current) !== selectionKey(selection)) return
        this.playInfo = info
        const resumeTicks = options.resumeTicks ?? info.resume_ticks
        this.pendingResumeSeconds = Math.max(0, resumeTicks / 10_000_000)
        this.position = this.pendingResumeSeconds
        this.duration = Math.max(0, info.runtime_ticks / 10_000_000)
        const preferred = this.preferredSource === 'direct' && info.direct_stream_url
          ? info.direct_stream_url
          : this.preferredSource === 'netbird' && info.netbird_stream_url
            ? info.netbird_stream_url
            : info.stream_url
        this.activeSource = preferred === info.direct_stream_url ? 'direct' : preferred === info.netbird_stream_url ? 'netbird' : 'proxy'
        if (!this.source || this.source !== preferred) this.source = preferred
      } catch (error) {
        if (request === prepareRequest) {
          this.error = error instanceof Error ? error.message : '无法准备播放'
          if (!this.source) this.pendingAutoplay = false
        }
      } finally {
        if (request === prepareRequest) {
          this.preparing = false
          if (!this.source) this.switchingSource = false
        }
      }
    },
    async resume(item: ContinueWatchingItem, autoplay = true) {
      await this.start(this.selectionFromContinue(item), {
        autoplay,
        resumeTicks: item.position_ticks,
        streamURL: item.stream_url,
      })
    },
    setPreferredSource(mode: PlaybackSourceMode) {
      this.preferredSource = mode
      localStorage.setItem('player.preferredSource', mode)
      localStorage.setItem('player.sourceMode', mode)
    },
    async switchSource(mode: PlaybackSourceMode, remember = true) {
      const next = mode === 'direct'
        ? this.playInfo?.direct_stream_url
        : mode === 'netbird'
          ? this.playInfo?.netbird_stream_url
          : this.playInfo?.stream_url
      if (!next || next === this.source) return
      this.captureVideoSettings()
      this.pendingResumeSeconds = this.video?.currentTime || this.position
      this.pendingAutoplay = Boolean(this.video && !this.video.paused && !this.video.ended)
      this.switchingSource = true
      this.activeSource = mode
      this.source = next
      this.error = ''
      this.stalled = false
      if (remember) this.setPreferredSource(mode)
    },
    async report(event: PlaybackProgressInput['event'], beacon = false) {
      if (!this.current) return
      const position = this.video?.currentTime ?? this.position
      const duration = Number.isFinite(this.video?.duration) ? this.video?.duration || this.duration : this.duration
      const body = { event, ticks: Math.max(0, Math.round(position * 10_000_000)), duration_ticks: Math.max(0, Math.round(duration * 10_000_000)) }
      const external = this.current.provider !== 'local' && this.current.itemId
      const path = external
        ? `/media/providers/${encodeURIComponent(this.current.provider)}/items/${encodeURIComponent(this.current.itemId!)}/progress`
        : '/playback/progress'
      if (!external && !this.current.localEpisodeId) return
      const payload = external ? body : { episode_id: this.current.localEpisodeId!, ...body } satisfies PlaybackProgressInput
      if (beacon && navigator.sendBeacon) {
        navigator.sendBeacon(`/api/v1${path}`, new Blob([JSON.stringify(payload)], { type: 'application/json' }))
        return
      }
      try {
        await api(path, { method: 'POST', body: JSON.stringify(payload) })
      } catch {
        // Playback remains usable; the next periodic update retries persistence.
      }
    },
    onLoadedMetadata() {
      const video = this.video
      if (!video) return
      video.volume = this.restoreVolume
      video.muted = this.restoreMuted
      video.playbackRate = this.restorePlaybackRate
      const upperBound = Number.isFinite(video.duration) ? Math.max(0, video.duration - 0.25) : this.pendingResumeSeconds
      if (this.pendingResumeSeconds > 0) video.currentTime = Math.min(this.pendingResumeSeconds, upperBound)
      this.duration = Number.isFinite(video.duration) ? video.duration : this.duration
      this.position = video.currentTime
      this.switchingSource = false
      if (this.pendingAutoplay) {
        this.pendingAutoplay = false
        void video.play().catch(() => useUIStore().toast('播放已准备好，请点击播放继续观看', 'info'))
      }
    },
    onTimeUpdate() {
      if (!this.video) return
      this.position = this.video.currentTime
      if (Number.isFinite(this.video.duration)) this.duration = this.video.duration
      const now = Date.now()
      if (now - lastProgressReport >= 10_000) {
        lastProgressReport = now
        void this.report('timeupdate')
      }
    },
    onPlaying() {
      clearStallTimer()
      this.playing = true
      this.switchingSource = false
      this.stalled = false
      this.preparing = false
      void this.report('playing')
    },
    onPause() {
      clearStallTimer()
      this.playing = false
      if (this.video && !this.video.ended && !this.switchingSource) void this.report('pause')
    },
    onWaiting() {
      this.stalled = true
      if ((!this.usingDirect && !this.usingNetBird) || this.seeking || this.video?.paused || stallTimer) return
      stallTimer = setTimeout(() => {
        stallTimer = null
        if ((!this.usingDirect && !this.usingNetBird) || !this.stalled || this.seeking || this.video?.paused) return
        const sourceLabel = this.usingNetBird ? 'NetBird 代理' : 'Jellyfin 直连'
        void this.switchSource('proxy', false).then(() => {
          useUIStore().toast(`${sourceLabel}持续卡顿，已从当前位置切换到 AnimateTool 代理`, 'info')
        })
      }, 8_000)
    },
    onCanPlay() {
      clearStallTimer()
      this.stalled = false
      this.preparing = false
    },
    onSeeking(value: boolean) {
      this.seeking = value
      if (value) clearStallTimer()
      else void this.report('seeked')
    },
    onError() {
      clearStallTimer()
      if ((this.usingDirect || this.usingNetBird) && this.playInfo?.stream_url) {
        const sourceLabel = this.usingNetBird ? 'NetBird 代理' : 'Jellyfin 直连'
        void this.switchSource('proxy', false).then(() => useUIStore().toast(`${sourceLabel}不可用，已切换到 AnimateTool 代理`, 'info'))
        return
      }
      this.error = '视频流加载失败，请检查 Jellyfin 连接和浏览器编码支持。'
      this.playing = false
    },
    onEnded() {
      this.playing = false
      this.position = this.duration
      void this.report('ended')
      if (this.playInfo) this.playInfo.played = true
      const next = this.nextSelection
      if (this.autoNext && next) {
        void this.start(next, { autoplay: true, resumeTicks: 0, skipCurrentReport: true })
      }
    },
    async restart() {
      this.pendingResumeSeconds = 0
      this.position = 0
      if (this.video) this.video.currentTime = 0
      await this.report('restart')
      try { await this.video?.play() } catch { /* native control remains */ }
    },
    async stop() {
      clearStallTimer()
      await this.report('stop')
      this.switchingSource = true
      this.video?.pause()
      this.video?.removeAttribute('src')
      this.video?.load()
      prepareRequest += 1
      this.current = null
      this.queue = []
      this.playInfo = null
      this.source = ''
      this.playing = false
      this.stalled = false
      this.preparing = false
      this.switchingSource = false
      this.error = ''
    },
    destroyForUnload() {
      if (this.current) void this.report('destroy', true)
      clearStallTimer()
    },
  },
})
