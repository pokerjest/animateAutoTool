<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useQuery } from '@tanstack/vue-query'
import { BookmarkCheck, CheckCircle2, Clock3, Heart, PlayCircle, RotateCcw } from '@lucide/vue'
import { api, handlePosterError, posterURL } from '../api/client'
import AsyncButton from '../components/AsyncButton.vue'
import StateBlock from '../components/StateBlock.vue'
import { useAsyncActions } from '../composables/useAsyncActions'
import { usePlaybackStore, type PlaybackSelection } from '../stores/playback'
import { useUIStore } from '../stores/ui'
import type { ContinueWatchingResponse } from '../api/types'

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
  anime: { ID: number; title: string; summary: string; image: string; metadata?: { title?: string; title_cn?: string; image?: string; summary?: string; bangumi_id?: number } }
  episodes: Episode[]
  collection_status?: { bangumi_watched_count: number; anilist_watched_count: number }
}

const route = useRoute()
const router = useRouter()
const ui = useUIStore()
const actions = useAsyncActions()
const playback = usePlaybackStore()
const progress = ref(0)
const watchedOverrides = ref<Record<number, boolean>>({})
const continueQuery = useQuery({ queryKey: ['continue-watching'], queryFn: () => api<ContinueWatchingResponse>('/playback/continue?limit=10'), staleTime: 20_000 })
const latest = computed(() => continueQuery.data.value?.items[0])
const routeAnimeID = computed(() => Number(route.query.anime || 0))
const animeId = computed(() => routeAnimeID.value || playback.current?.localAnimeId || latest.value?.anime_id || 0)

watch([latest, () => playback.current], ([item, current]) => {
  if (routeAnimeID.value) return
  const animeID = current?.localAnimeId || item?.anime_id
  const episodeID = current?.localEpisodeId || item?.episode_id
  if (animeID && episodeID) void router.replace(`/player?anime=${animeID}&episode=${episodeID}`)
}, { immediate: true })

const query = useQuery({
  queryKey: ['episodes', animeId],
  queryFn: () => api<Payload>(`/local-anime/${animeId.value}/episodes`),
  enabled: computed(() => animeId.value > 0),
})

const animeTitle = computed(() => query.data.value?.anime.metadata?.title_cn || query.data.value?.anime.metadata?.title || query.data.value?.anime.title || '')
const animeImage = computed(() => query.data.value?.anime.metadata?.image || query.data.value?.anime.image || '')

function selectionFor(episode: Episode): PlaybackSelection {
  return {
    provider: 'local',
    itemId: String(episode.id),
    localAnimeId: animeId.value,
    localEpisodeId: episode.id,
    title: animeTitle.value,
    episodeTitle: episode.name,
    image: animeImage.value,
    season: episode.season,
    episode: episode.episode,
  }
}

watch(() => query.data.value?.episodes, episodes => {
  if (!episodes?.length || !query.data.value) return
  watchedOverrides.value = {}
  progress.value = query.data.value.collection_status?.bangumi_watched_count || 0
  const playable = episodes.filter(item => item.playable)
  const queue = playable.map(selectionFor)
  playback.setQueue(queue)
  const requestedID = Number(route.query.episode || 0)
  const currentID = playback.current?.localAnimeId === animeId.value ? playback.current.localEpisodeId : 0
  const target = playable.find(item => item.id === requestedID)
    || playable.find(item => item.id === currentID)
    || playable.find(item => (item.resume_ticks || 0) > 0 && !item.watched)
    || playable.find(item => !item.watched)
    || playable[0]
  if (!target) return
  const saved = continueQuery.data.value?.items.find(item => item.episode_id === target.id)
  void playback.start(selectionFor(target), {
    autoplay: route.query.autoplay === '1',
    resumeTicks: saved?.position_ticks ?? target.resume_ticks,
    streamURL: saved?.stream_url,
  })
}, { immediate: true })

watch(() => playback.current?.localEpisodeId, episodeID => {
  if (!episodeID || route.path !== '/player' || playback.current?.localAnimeId !== animeId.value) return
  if (Number(route.query.episode || 0) === episodeID && Number(route.query.anime || 0) === animeId.value) return
  void router.replace({ path: '/player', query: { anime: animeId.value, episode: episodeID } })
})

const selected = computed(() => query.data.value?.episodes.find(item => item.id === playback.current?.localEpisodeId) || null)
const playInfo = computed(() => playback.playInfo)
const hasMediaInfo = computed(() => {
  const media = playInfo.value?.media
  return Boolean(media && (media.container || media.video_codec || media.audio_codec || media.width || media.bitrate || media.size))
})

function isEpisodeWatched(episode: Episode) {
  return watchedOverrides.value[episode.id] ?? Boolean(episode.watched)
}

function setEpisodeWatched(episodeID: number, watched: boolean) {
  watchedOverrides.value = { ...watchedOverrides.value, [episodeID]: watched }
}

async function updateEpisodeState(kind: 'played' | 'favorite') {
  if (!selected.value || !playInfo.value) return
  const value = kind === 'played' ? !playInfo.value.played : !playInfo.value.episode_favorite
  try {
    await actions.run(`jellyfin-episode-${kind}`, async () => {
      const result = await api<{ played: boolean; favorite: boolean }>(`/jellyfin/episodes/${selected.value!.id}/user-state`, {
        method: 'PUT', body: JSON.stringify({ [kind]: value }),
      })
      if (kind === 'played') {
        playInfo.value!.played = result.played
        setEpisodeWatched(selected.value!.id, result.played)
      } else playInfo.value!.episode_favorite = result.favorite
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
        method: 'PUT', body: JSON.stringify({ favorite }),
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

async function syncProgress() {
  const bangumiID = query.data.value?.anime.metadata?.bangumi_id
  if (!bangumiID) return
  try {
    await actions.run('sync-progress', async () => {
      await api(`/bangumi/subject/${bangumiID}/progress`, { method: 'POST', body: JSON.stringify({ episode_count: progress.value }) })
      ui.toast(`已同步至第 ${progress.value} 集`)
    })
  } catch (error) {
    ui.toast(error instanceof Error ? error.message : '同步失败', 'error')
  }
}
</script>

<template>
  <div class="page-grid">
    <StateBlock v-if="!animeId" state="empty" title="还没有可继续观看的内容" description="请先在本地番剧中选择一部作品开始播放。" />
    <StateBlock v-else-if="query.isLoading.value" state="loading" title="正在读取剧集" />
    <StateBlock v-else-if="query.isError.value" state="error" title="无法读取剧集" :retrying="query.isFetching.value" @retry="query.refetch()" />
    <template v-else-if="query.data.value">
      <section class="panel overflow-hidden">
        <div class="grid gap-6 p-5 lg:grid-cols-[220px_1fr]">
          <img :src="posterURL(query.data.value.anime.metadata || { image: query.data.value.anime.image }, { width: 720 })" :alt="query.data.value.anime.title" decoding="async" class="aspect-[2/3] w-full max-w-56 rounded-2xl object-cover" @error="handlePosterError($event, query.data.value.anime.image)" />
          <div class="min-w-0">
            <div class="flex flex-wrap items-start justify-between gap-3">
              <div><p class="eyebrow">WATCH</p><h1 class="mt-2 text-3xl font-black">{{ animeTitle }}</h1></div>
              <AsyncButton v-if="playInfo" class="btn btn-secondary" :loading="actions.isBusy('jellyfin-series-favorite')" loading-label="同步中…" @click="updateSeriesFavorite">
                <Heart :size="17" :fill="playInfo.series_favorite ? 'currentColor' : 'none'" />{{ playInfo.series_favorite ? '已收藏整部' : '收藏到 Jellyfin' }}
              </AsyncButton>
            </div>
            <p class="muted mt-3 line-clamp-4 max-w-3xl text-sm leading-6">{{ query.data.value.anime.metadata?.summary || query.data.value.anime.summary || '暂无简介' }}</p>
            <div v-if="query.data.value.anime.metadata?.bangumi_id" class="panel-muted mt-5 flex flex-wrap items-center gap-3 p-3">
              <BookmarkCheck class="text-[var(--brand)]" :size="18" />
              <label class="text-sm font-bold">Bangumi 看到第 <input v-model.number="progress" class="field mx-1 inline-block h-10 min-h-10 w-20" type="number" min="0" /> 集</label>
              <AsyncButton class="btn btn-secondary ml-auto" :loading="actions.isBusy('sync-progress')" loading-label="同步中…" @click="syncProgress">同步进度</AsyncButton>
            </div>
          </div>
        </div>
      </section>

      <section v-if="playInfo" class="panel flex flex-wrap items-center gap-3 p-4">
        <AsyncButton class="btn btn-secondary" :loading="actions.isBusy('jellyfin-episode-played')" loading-label="同步中…" @click="updateEpisodeState('played')">
          <RotateCcw v-if="playInfo.played" :size="16" /><CheckCircle2 v-else :size="16" />{{ playInfo.played ? '设为未看' : '标记已看' }}
        </AsyncButton>
        <AsyncButton class="btn btn-secondary" :loading="actions.isBusy('jellyfin-episode-favorite')" loading-label="同步中…" @click="updateEpisodeState('favorite')">
          <Heart :size="16" :fill="playInfo.episode_favorite ? 'currentColor' : 'none'" />{{ playInfo.episode_favorite ? '已收藏本集' : '收藏本集' }}
        </AsyncButton>
        <template v-if="hasMediaInfo">
          <span v-if="playInfo.media.width" class="badge">{{ playInfo.media.width }}×{{ playInfo.media.height }}</span>
          <span v-if="playInfo.media.video_codec" class="badge">{{ playInfo.media.video_codec.toUpperCase() }}</span>
          <span v-if="playInfo.media.audio_codec" class="badge">{{ playInfo.media.audio_codec.toUpperCase() }}<template v-if="playInfo.media.audio_channels"> · {{ playInfo.media.audio_channels }} 声道</template></span>
          <span v-if="playInfo.media.bitrate" class="badge">{{ formatBitrate(playInfo.media.bitrate) }}</span>
          <span v-if="playInfo.media.size" class="badge">{{ formatBytes(playInfo.media.size) }}</span>
          <span v-if="playInfo.media.subtitle_count" class="badge">{{ playInfo.media.subtitle_count }} 条字幕</span>
        </template>
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
            <div v-if="ep.progress_percent && !isEpisodeWatched(ep)" class="mt-2 max-w-72"><div class="h-1.5 overflow-hidden rounded-full bg-[var(--surface-muted)]"><div class="h-full rounded-full bg-[var(--brand)]" :style="{ width: `${Math.min(100, ep.progress_percent)}%` }"></div></div><p class="muted mt-1 text-xs">Jellyfin 已播放 {{ Math.round(ep.progress_percent) }}%</p></div>
          </div>
          <button v-if="ep.playable" class="btn btn-primary" @click="playback.start(selectionFor(ep), { autoplay: true, resumeTicks: ep.resume_ticks })"><PlayCircle :size="17" />{{ selected?.id === ep.id ? '播放中' : (ep.resume_ticks ? '继续播放' : '播放') }}</button>
          <span v-else class="badge">不可播放</span>
        </article>
      </section>
    </template>
  </div>
</template>
