<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useQuery } from '@tanstack/vue-query'
import { BookmarkCheck, Clock3, Network, PlayCircle, Server } from '@lucide/vue'
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
let playRequest = 0

const query = useQuery({
  queryKey: ['episodes', animeId],
  queryFn: () => api<Payload>(`/local-anime/${animeId.value}/episodes`),
  enabled: computed(() => animeId.value > 0),
})

watch(() => query.data.value?.episodes, episodes => {
  selected.value = episodes?.find(item => item.playable) || episodes?.[0] || null
  progress.value = query.data.value?.collection_status?.bangumi_watched_count || 0
}, { immediate: true })

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
    if (request === playRequest) playError.value = playbackErrorMessage(error)
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
    <PageHeader eyebrow="WATCH" title="播放工作区" description="从本地媒体库选择剧集，在一个工作区内播放并同步观看进度。" />
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
          @loadedmetadata="restorePosition"
          @error="handlePlaybackError"
          @timeupdate="reportTimeUpdate"
          @pause="reportPlayback('pause')"
          @ended="reportPlayback('ended')"
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
      </section>

      <section class="grid gap-3">
        <article v-for="ep in query.data.value.episodes" :key="ep.id || ep.name" class="panel grid gap-4 p-4 sm:grid-cols-[120px_1fr_auto] sm:items-center" :class="selected?.id === ep.id ? 'ring-2 ring-[var(--brand)]' : ''">
          <div class="grid aspect-video place-items-center overflow-hidden rounded-xl bg-[var(--surface-muted)]">
            <img v-if="ep.thumbnail" :src="ep.thumbnail" alt="" loading="lazy" decoding="async" fetchpriority="low" class="h-full w-full object-cover" />
            <PlayCircle v-else class="muted" />
          </div>
          <div>
            <h3 class="font-extrabold">第 {{ ep.episode || '?' }} 集 · {{ ep.name }}</h3>
            <p class="muted mt-1 line-clamp-2 text-sm">{{ ep.overview || '本地媒体文件' }}</p>
            <p v-if="ep.duration" class="muted mt-2 flex items-center gap-1 text-xs"><Clock3 :size="13" />{{ ep.duration }}</p>
          </div>
          <button v-if="ep.playable" class="btn btn-primary" @click="selected = ep"><PlayCircle :size="17" />{{ selected?.id === ep.id ? '播放中' : '播放' }}</button>
          <span v-else class="badge">不可播放</span>
        </article>
      </section>
    </template>
  </div>
</template>
