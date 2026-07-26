<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useQuery } from '@tanstack/vue-query'
import { BookmarkCheck, CheckCircle2, Clock3, Heart, Network, PlayCircle, RotateCcw, Server, SkipBack, SkipForward } from '@lucide/vue'
import { api, ApiError, handlePosterError, posterURL } from '../api/client'
import AsyncButton from '../components/AsyncButton.vue'
import PageHeader from '../components/PageHeader.vue'
import StateBlock from '../components/StateBlock.vue'
import { useAsyncActions } from '../composables/useAsyncActions'
import { useUIStore } from '../stores/ui'
import type { JellyfinPlayInfo, PlaybackDiagnostic, PlaybackProgressInput } from '../api/types'

interface Episode {
  id: number
  name: string
  episode: number
  season: number
  playable: boolean
  thumbnail: string
  overview: string
  duration: string
  watched?: boolean
  resume_ticks?: number
  runtime_ticks?: number
  progress_percent?: number
}

interface Payload {
  anime: { ID: number; title: string; summary: string; image: string; metadata?: { title_cn?: string; image?: string; summary?: string; bangumi_id?: number } }
  episodes: Episode[]
  collection_status?: { bangumi_watched_count: number; anilist_watched_count: number }
}

const route = useRoute()
const ui = useUIStore()
const actions = useAsyncActions()
const animeId = computed(() => Number(route.query.anime || 0))
const selected = ref<Episode | null>(null)
const progress = ref(0)
const playInfo = ref<JellyfinPlayInfo | null>(null)
const playSource = ref('')
const playBusy = ref(false)
const playError = ref('')
const video = ref<HTMLVideoElement | null>(null)
const playbackEpisodeID = ref(0)
const autoNext = ref(localStorage.getItem('player.autoNext') !== 'false')
const watchedOverrides = ref<Record<number, boolean>>({})
let playRequest = 0
let autoPlayPending = false

const query = useQuery({
  queryKey: ['episodes', animeId],
  queryFn: () => api<Payload>(`/local-anime/${animeId.value}/episodes`),
  enabled: computed(() => animeId.value > 0),
})

watch(() => query.data.value?.episodes, episodes => {
  watchedOverrides.value = {}
  const initial = episodes?.find(item => item.playable) || episodes?.[0]
  selected.value = initial ? { ...initial } : null
  progress.value = query.data.value?.collection_status?.bangumi_watched_count || 0
}, { immediate: true })

watch(autoNext, value => localStorage.setItem('player.autoNext', String(value)))

const playableEpisodes = computed(() => query.data.value?.episodes.filter(item => item.playable) || [])
const selectedIndex = computed(() => playableEpisodes.value.findIndex(item => item.id === selected.value?.id))
const previousEpisode = computed(() => selectedIndex.value > 0 ? playableEpisodes.value[selectedIndex.value - 1] : null)
const nextEpisode = computed(() => selectedIndex.value >= 0 && selectedIndex.value < playableEpisodes.value.length - 1 ? playableEpisodes.value[selectedIndex.value + 1] : null)
const hasMediaInfo = computed(() => {
  const media = playInfo.value?.media
  return Boolean(media && (media.container || media.video_codec || media.audio_codec || media.width || media.bitrate || media.size))
})

function playbackErrorMessage(error: unknown) {
  if (error instanceof ApiError && error.details && typeof error.details === 'object') {
    const diagnostic = (error.details as { diagnostic?: PlaybackDiagnostic }).diagnostic
    const detail = [diagnostic?.summary, diagnostic?.hint].filter(Boolean).join('；')
    if (detail) return detail
  }
  return error instanceof Error ? error.message : '无法准备播放'
}

async function preparePlayback() {
  const episodeID = selected.value?.playable ? selected.value.id : 0
  if (!episodeID) {
    playRequest += 1
    playBusy.value = false
    playError.value = ''
    playInfo.value = null
    playSource.value = ''
    return
  }
  const request = ++playRequest
  playBusy.value = true
  playError.value = ''
  playInfo.value = null
  playSource.value = ''
  try {
    const info = await api<JellyfinPlayInfo>(`/jellyfin/play/${episodeID}`)
    if (request !== playRequest) return
    playInfo.value = info
    playbackEpisodeID.value = episodeID
    playSource.value = info.direct_stream_url || info.stream_url
  } catch (error) {
    if (request === playRequest) {
      autoPlayPending = false
      playError.value = playbackErrorMessage(error)
    }
  } finally {
    if (request === playRequest) playBusy.value = false
  }
}

watch(() => selected.value?.id, () => { void preparePlayback() })

const usingDirect = computed(() => Boolean(playInfo.value?.direct_stream_url) && playSource.value === playInfo.value?.direct_stream_url)

function selectSource(mode: 'direct' | 'proxy') {
  const next = mode === 'direct' ? playInfo.value?.direct_stream_url : playInfo.value?.stream_url
  if (!next) return
  playError.value = ''
  playSource.value = next
}

function handlePlaybackError() {
  const proxy = playInfo.value?.stream_url
  if (usingDirect.value && proxy && proxy !== playSource.value) {
    playSource.value = proxy
    ui.toast('Tailscale 直连不可用，已自动切换到 AnimateTool 代理', 'info')
    return
  }
  playError.value = '视频流加载失败，请检查 Jellyfin、Tailscale 连接及浏览器编码支持。'
}

function restorePosition() {
  const element = video.value
  const seconds = (playInfo.value?.resume_ticks || 0) / 10_000_000
  if (!element || seconds <= 0) return
  const upperBound = Number.isFinite(element.duration) ? Math.max(0, element.duration - 0.5) : seconds
  element.currentTime = Math.min(seconds, upperBound)
}

async function handleLoadedMetadata() {
  restorePosition()
  if (!autoPlayPending || !video.value) return
  autoPlayPending = false
  try {
    await video.value.play()
  } catch {
    ui.toast('已切换到下一集，请点击播放继续观看', 'info')
  }
}

async function reportPlayback(event: 'timeupdate' | 'pause' | 'ended' | 'destroy') {
  const episodeID = playbackEpisodeID.value
  const element = video.value
  if (!episodeID || !element) return
  const ticks = Math.max(0, Math.round(element.currentTime * 10_000_000))
  try {
    const body: PlaybackProgressInput = { episode_id: episodeID, event, ticks }
    await api('/jellyfin/progress', {
      method: 'POST',
      body: JSON.stringify(body),
      headers: { 'Content-Type': 'application/json' },
    })
  } catch {
    // Playback must continue even when progress synchronization is unavailable.
  }
}

function handleEnded() {
  void reportPlayback('ended')
  if (selected.value) setEpisodeWatched(selected.value.id, true)
  if (playInfo.value) playInfo.value.played = true
  if (autoNext.value && nextEpisode.value) {
    selectEpisode(nextEpisode.value, true)
  }
}

function selectAdjacent(direction: 'previous' | 'next') {
  const target = direction === 'previous' ? previousEpisode.value : nextEpisode.value
  if (target) selectEpisode(target)
}

function selectEpisode(episode: Episode, autoPlay = false) {
  autoPlayPending = autoPlay
  selected.value = { ...episode, watched: isEpisodeWatched(episode) }
}

function isEpisodeWatched(episode: Episode) {
  return watchedOverrides.value[episode.id] ?? Boolean(episode.watched)
}

function setEpisodeWatched(episodeID: number, watched: boolean) {
  watchedOverrides.value = { ...watchedOverrides.value, [episodeID]: watched }
  if (selected.value?.id === episodeID) selected.value = { ...selected.value, watched }
}

async function updateEpisodeState(kind: 'played' | 'favorite') {
  if (!selected.value || !playInfo.value) return
  const value = kind === 'played' ? !playInfo.value.played : !playInfo.value.episode_favorite
  try {
    await actions.run(`jellyfin-episode-${kind}`, async () => {
      const result = await api<{ played: boolean; favorite: boolean }>(`/jellyfin/episodes/${selected.value!.id}/user-state`, {
        method: 'PUT',
        body: JSON.stringify({ [kind]: value }),
        headers: { 'Content-Type': 'application/json' },
      })
      if (kind === 'played') {
        playInfo.value!.played = result.played
        setEpisodeWatched(selected.value!.id, result.played)
      } else {
        playInfo.value!.episode_favorite = result.favorite
      }
    })
    ui.toast(kind === 'played' ? (value ? '已在 Jellyfin 标记为已看' : '已在 Jellyfin 设为未看') : (value ? '已收藏本集' : '已取消收藏本集'))
  } catch (error) {
    ui.toast(error instanceof Error ? error.message : '更新 Jellyfin 状态失败', 'error')
  }
}

async function updateSeriesFavorite() {
  if (!playInfo.value || !animeId.value) return
  const favorite = !playInfo.value.series_favorite
  try {
    await actions.run('jellyfin-series-favorite', async () => {
      const result = await api<{ favorite: boolean }>(`/jellyfin/series/${animeId.value}/user-state`, {
        method: 'PUT',
        body: JSON.stringify({ favorite }),
        headers: { 'Content-Type': 'application/json' },
      })
      playInfo.value!.series_favorite = result.favorite
    })
    ui.toast(favorite ? '已在 Jellyfin 收藏整部番剧' : '已取消收藏整部番剧')
  } catch (error) {
    ui.toast(error instanceof Error ? error.message : '更新 Jellyfin 收藏失败', 'error')
  }
}

function formatBytes(value = 0) {
  if (!value) return ''
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const index = Math.min(units.length - 1, Math.floor(Math.log(value) / Math.log(1024)))
  return `${(value / 1024 ** index).toFixed(index > 2 ? 1 : 0)} ${units[index]}`
}

function formatBitrate(value = 0) {
  return value > 0 ? `${(value / 1_000_000).toFixed(1)} Mbps` : ''
}

let lastProgressReport = 0
function reportTimeUpdate() {
  const now = Date.now()
  if (now - lastProgressReport < 15_000) return
  lastProgressReport = now
  void reportPlayback('timeupdate')
}

onBeforeUnmount(() => { void reportPlayback('destroy') })

async function syncProgress() {
  const bangumiID = query.data.value?.anime.metadata?.bangumi_id
  if (!bangumiID) return
  try {
    await actions.run('sync-progress', async () => {
      await api(`/bangumi/subject/${bangumiID}/progress`, {
        method: 'POST',
        body: JSON.stringify({ episode_count: progress.value }),
        headers: { 'Content-Type': 'application/json' },
      })
      ui.toast(`已同步至第 ${progress.value} 集`)
    })
  } catch (error) {
    ui.toast(error instanceof Error ? error.message : '同步失败', 'error')
  }
}
</script>

<template>
  <div class="page-grid">
    <PageHeader eyebrow="WATCH" title="播放工作区" description="从本地媒体库选择剧集，在一个工作区内播放并同步观看进度。">
      <AsyncButton v-if="playInfo" class="btn btn-secondary" :loading="actions.isBusy('jellyfin-series-favorite')" loading-label="同步中…" @click="updateSeriesFavorite">
        <Heart :size="17" :fill="playInfo.series_favorite ? 'currentColor' : 'none'" />{{ playInfo.series_favorite ? '已收藏整部' : '收藏到 Jellyfin' }}
      </AsyncButton>
    </PageHeader>
    <StateBlock v-if="!animeId" state="empty" title="请先选择一部本地番剧" description="在本地番剧页点击“查看与播放”进入这里。" />
    <StateBlock v-else-if="query.isLoading.value" state="loading" />
    <StateBlock v-else-if="query.isError.value" state="error" title="无法读取剧集" :retrying="query.isFetching.value" @retry="query.refetch()" />
    <template v-else-if="query.data.value">
      <section class="panel overflow-hidden">
        <div class="grid gap-6 p-5 lg:grid-cols-[220px_1fr]">
          <img :src="posterURL(query.data.value.anime.metadata || { image: query.data.value.anime.image }, { width: 720 })" :alt="query.data.value.anime.title" decoding="async" class="aspect-[2/3] w-full max-w-56 rounded-2xl object-cover" @error="handlePosterError($event, query.data.value.anime.image)" />
          <div class="min-w-0">
            <p class="eyebrow">NOW AVAILABLE</p>
            <h2 class="mt-2 text-3xl font-black">{{ query.data.value.anime.metadata?.title_cn || query.data.value.anime.title }}</h2>
            <p class="muted mt-3 line-clamp-4 max-w-3xl text-sm leading-6">{{ query.data.value.anime.metadata?.summary || query.data.value.anime.summary || '暂无简介' }}</p>
            <div v-if="query.data.value.anime.metadata?.bangumi_id" class="panel-muted mt-5 flex flex-wrap items-center gap-3 p-3">
              <BookmarkCheck class="text-[var(--brand)]" :size="18" />
              <label class="text-sm font-bold">Bangumi 看到第 <input v-model.number="progress" class="field mx-1 inline-block h-10 min-h-10 w-20" type="number" min="0" /> 集</label>
              <AsyncButton class="btn btn-secondary ml-auto" :loading="actions.isBusy('sync-progress')" loading-label="同步中…" @click="syncProgress">同步进度</AsyncButton>
            </div>
          </div>
        </div>
      </section>

      <section v-if="selected?.playable" class="panel overflow-hidden bg-black">
        <div v-if="playBusy" class="bg-[var(--surface-solid)] p-5"><StateBlock state="loading" title="正在向 Jellyfin 准备播放地址" /></div>
        <div v-else-if="playError" class="bg-[var(--surface-solid)] p-5"><StateBlock state="error" title="无法播放这一集" :description="playError" @retry="preparePlayback" /></div>
        <video
          v-else-if="playSource"
          :key="`${selected.id}:${playSource}`"
          ref="video"
          controls
          playsinline
          preload="metadata"
          class="aspect-video w-full"
          :src="playSource"
          @loadedmetadata="handleLoadedMetadata"
          @error="handlePlaybackError"
          @timeupdate="reportTimeUpdate"
          @pause="reportPlayback('pause')"
          @ended="handleEnded"
        ></video>
        <div class="flex flex-wrap items-center justify-between gap-3 bg-[var(--surface-solid)] p-4">
          <div>
            <p class="font-black">第 {{ selected.episode || '?' }} 集 · {{ selected.name }}</p>
            <p class="muted mt-1 text-xs">{{ selected.overview || '本地媒体文件' }}</p>
          </div>
          <div v-if="playInfo" class="flex flex-wrap items-center justify-end gap-2">
            <span class="badge" :class="usingDirect ? 'badge-success' : ''"><Network v-if="usingDirect" :size="13" /><Server v-else :size="13" />{{ usingDirect ? 'Tailscale 直连' : '服务端代理' }}</span>
            <button v-if="playInfo.direct_stream_url" class="btn btn-quiet" :disabled="usingDirect" @click="selectSource('direct')">使用直连</button>
            <button class="btn btn-quiet" :disabled="!usingDirect" @click="selectSource('proxy')">使用代理</button>
            <a v-if="playSource" class="btn btn-secondary shrink-0" :href="playSource" target="_blank" rel="noreferrer">单独打开</a>
          </div>
        </div>
        <div v-if="playInfo" class="grid gap-4 border-t border-white/10 bg-[var(--surface-solid)] p-4 lg:grid-cols-[auto_1fr] lg:items-center">
          <div class="flex flex-wrap gap-2">
            <button class="btn btn-secondary" :disabled="!previousEpisode" @click="selectAdjacent('previous')"><SkipBack :size="16" />上一集</button>
            <AsyncButton class="btn btn-secondary" :loading="actions.isBusy('jellyfin-episode-played')" loading-label="同步中…" @click="updateEpisodeState('played')">
              <RotateCcw v-if="playInfo.played" :size="16" /><CheckCircle2 v-else :size="16" />{{ playInfo.played ? '设为未看' : '标记已看' }}
            </AsyncButton>
            <AsyncButton class="btn btn-secondary" :loading="actions.isBusy('jellyfin-episode-favorite')" loading-label="同步中…" @click="updateEpisodeState('favorite')">
              <Heart :size="16" :fill="playInfo.episode_favorite ? 'currentColor' : 'none'" />{{ playInfo.episode_favorite ? '已收藏本集' : '收藏本集' }}
            </AsyncButton>
            <button class="btn btn-primary" :disabled="!nextEpisode" @click="selectAdjacent('next')">下一集<SkipForward :size="16" /></button>
          </div>
          <div class="flex flex-wrap items-center gap-2 lg:justify-end">
            <label class="flex min-h-11 items-center gap-2 text-sm font-bold"><input v-model="autoNext" type="checkbox" class="h-4 w-4 accent-[var(--brand)]" />播完自动下一集</label>
            <template v-if="hasMediaInfo">
              <span v-if="playInfo.media.width" class="badge">{{ playInfo.media.width }}×{{ playInfo.media.height }}</span>
              <span v-if="playInfo.media.video_codec" class="badge">{{ playInfo.media.video_codec.toUpperCase() }}</span>
              <span v-if="playInfo.media.audio_codec" class="badge">{{ playInfo.media.audio_codec.toUpperCase() }}<template v-if="playInfo.media.audio_channels"> · {{ playInfo.media.audio_channels }} 声道</template></span>
              <span v-if="playInfo.media.bitrate" class="badge">{{ formatBitrate(playInfo.media.bitrate) }}</span>
              <span v-if="playInfo.media.size" class="badge">{{ formatBytes(playInfo.media.size) }}</span>
              <span v-if="playInfo.media.subtitle_count" class="badge">{{ playInfo.media.subtitle_count }} 条字幕</span>
            </template>
          </div>
        </div>
      </section>

      <section class="grid gap-3">
        <article v-for="ep in query.data.value.episodes" :key="ep.id || ep.name" class="panel grid gap-4 p-4 sm:grid-cols-[120px_1fr_auto] sm:items-center" :class="selected?.id === ep.id ? 'ring-2 ring-[var(--brand)]' : ''">
          <div class="grid aspect-video place-items-center overflow-hidden rounded-xl bg-[var(--surface-muted)]">
            <img v-if="ep.thumbnail" :src="ep.thumbnail" alt="" loading="lazy" decoding="async" fetchpriority="low" class="h-full w-full object-cover" />
            <PlayCircle v-else class="muted" />
          </div>
          <div>
            <div class="flex flex-wrap items-center gap-2"><h3 class="font-extrabold">第 {{ ep.episode || '?' }} 集 · {{ ep.name }}</h3><span v-if="isEpisodeWatched(ep)" class="badge badge-success"><CheckCircle2 :size="13" />已看</span></div>
            <p class="muted mt-1 line-clamp-2 text-sm">{{ ep.overview || '本地媒体文件' }}</p>
            <p v-if="ep.duration" class="muted mt-2 flex items-center gap-1 text-xs"><Clock3 :size="13" />{{ ep.duration }}</p>
            <div v-if="ep.progress_percent && !isEpisodeWatched(ep)" class="mt-2 max-w-72"><div class="h-1.5 overflow-hidden rounded-full bg-[var(--surface-muted)]"><div class="h-full rounded-full bg-[var(--brand)]" :style="{width:`${Math.min(100,ep.progress_percent)}%`}"></div></div><p class="muted mt-1 text-xs">Jellyfin 已播放 {{ Math.round(ep.progress_percent) }}%</p></div>
          </div>
          <button v-if="ep.playable" class="btn btn-primary" @click="selectEpisode(ep)"><PlayCircle :size="17" />{{ selected?.id === ep.id ? '播放中' : '播放' }}</button>
          <span v-else class="badge">不可播放</span>
        </article>
      </section>
    </template>
  </div>
</template>
