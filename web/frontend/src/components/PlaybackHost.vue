<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useQuery, useQueryClient } from '@tanstack/vue-query'
import { LoaderCircle, Maximize2, Network, Pause, Play, RotateCcw, Server, SkipBack, SkipForward, X } from '@lucide/vue'
import { api, normalizePosterURL } from '../api/client'
import type { ContinueWatchingResponse } from '../api/types'
import { usePlaybackStore } from '../stores/playback'
import { useSessionStore } from '../stores/session'
import { useWorkspaceStore } from '../stores/workspace'
import { localPlayerLocation, localPlayerPath } from '../utils/playerRoutes'

const route = useRoute()
const router = useRouter()
const queryClient = useQueryClient()
const playback = usePlaybackStore()
const session = useSessionStore()
const workspace = useWorkspaceStore()
const video = ref<HTMLVideoElement | null>(null)
const full = computed(() => route.path === '/player' || route.path === localPlayerPath || route.path.startsWith('/media/play'))
const continueQuery = useQuery({
  queryKey: ['continue-watching'],
  queryFn: () => api<ContinueWatchingResponse>('/playback/continue?limit=10'),
  enabled: computed(() => session.authenticated && !session.setupPending),
  staleTime: 20_000,
})
const latest = computed(() => continueQuery.data.value?.items[0])
const progress = computed(() => playback.duration > 0 ? Math.min(100, playback.position / playback.duration * 100) : 0)
const sourceLabel = computed(() => playback.usingDirect ? 'Jellyfin 直连' : 'AnimateTool 代理')
const sourceOptions = computed(() => [
  { value: 'proxy' as const, label: 'AnimateTool 代理', available: Boolean(playback.playInfo?.stream_url) },
  { value: 'direct' as const, label: 'Jellyfin 直连', available: Boolean(playback.playInfo?.direct_stream_url) },
])

watch(video, element => playback.attachVideo(element), { immediate: true })

function pageHide() {
  playback.destroyForUnload()
}

onMounted(() => window.addEventListener('pagehide', pageHide))
onBeforeUnmount(() => {
  window.removeEventListener('pagehide', pageHide)
  playback.destroyForUnload()
  playback.attachVideo(null)
})

async function openFullPlayer() {
  const current = playback.current
  if (!current) return
  if (current.provider !== 'local' && current.itemId) {
    await router.push(`/media/play/${encodeURIComponent(current.provider)}/${encodeURIComponent(current.itemId)}`)
    return
  }
  await router.push(localPlayerLocation(current.localAnimeId!, current.localEpisodeId, true))
}

async function resumeLatest() {
  if (!latest.value) return
  const item = latest.value
  const started = playback.resume(item, true)
  await router.push(localPlayerLocation(item.anime_id, item.episode_id, true))
  await started
}

function togglePlayback() {
  const element = video.value
  if (!element) return
  if (element.paused) void element.play()
  else element.pause()
}

function seek(event: Event) {
  const target = event.currentTarget as HTMLInputElement
  if (video.value && playback.duration > 0) video.value.currentTime = Number(target.value) / 100 * playback.duration
}

async function stop() {
  await playback.stop()
  await queryClient.invalidateQueries({ queryKey: ['continue-watching'] })
}

function ended() {
  playback.onEnded()
  setTimeout(() => void queryClient.invalidateQueries({ queryKey: ['continue-watching'] }), 400)
}

async function chooseSource(value: 'proxy' | 'direct') {
  if (value === playback.activeSource) return
  await playback.switchSource(value)
}
</script>

<template>
  <section
    v-if="playback.current && workspace.isMedia"
    aria-label="全局播放器"
    :class="full
      ? 'panel mb-6 overflow-hidden bg-black'
      : 'glass fixed bottom-[5.8rem] left-3 right-3 z-[45] overflow-hidden rounded-2xl shadow-2xl sm:left-auto sm:w-[390px] lg:bottom-6 lg:right-6'"
  >
    <div v-if="full" class="flex flex-wrap items-center justify-between gap-3 bg-[var(--surface-solid)] p-4">
      <div class="min-w-0">
        <p class="eyebrow">NOW PLAYING</p>
        <h2 class="truncate text-xl font-black">{{ playback.current.title }}</h2>
        <p class="muted mt-1 text-sm">第 {{ playback.current.episode || '?' }} 集 · {{ playback.current.episodeTitle }}</p>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <span class="badge" :class="playback.usingDirect ? 'badge-success' : ''"><Network v-if="playback.usingDirect" :size="13" /><Server v-else :size="13" />{{ sourceLabel }}</span>
        <button class="btn btn-secondary" @click="playback.restart"><RotateCcw :size="16" />从头播放</button>
      </div>
    </div>

    <div class="relative bg-black" :class="full ? 'aspect-video' : 'grid grid-cols-[132px_1fr] sm:grid-cols-[160px_1fr]'">
      <video
        v-show="playback.source"
        ref="video"
        :src="playback.source"
        :controls="full"
        playsinline
        preload="auto"
        class="bg-black"
        :class="full ? 'h-full w-full' : 'aspect-video h-full w-full object-contain'"
        @loadedmetadata="playback.onLoadedMetadata"
        @timeupdate="playback.onTimeUpdate"
        @playing="playback.onPlaying"
        @pause="playback.onPause"
        @waiting="playback.onWaiting"
        @stalled="playback.onWaiting"
        @canplay="playback.onCanPlay"
        @seeking="playback.onSeeking(true)"
        @seeked="playback.onSeeking(false)"
        @error="playback.onError"
        @ended="ended"
        @click="!full && openFullPlayer()"
      ></video>
      <div v-if="!full" class="min-w-0 bg-[var(--surface-solid)] p-3">
        <p class="truncate text-sm font-black">{{ playback.current.title }}</p>
        <p class="muted mt-1 truncate text-xs">第 {{ playback.current.episode || '?' }} 集 · {{ playback.current.episodeTitle }}</p>
        <span class="badge mt-2 inline-flex max-w-full truncate text-[.68rem]" :class="playback.usingDirect ? 'badge-success' : ''">{{ sourceLabel }}</span>
        <input class="mt-2 h-6 w-full accent-[var(--brand)]" type="range" min="0" max="100" :value="progress" aria-label="播放进度" @input="seek" />
        <div class="mt-1 flex items-center gap-1">
          <button class="btn btn-quiet h-11 min-h-11 w-11 p-0" :aria-label="playback.playing ? '暂停' : '播放'" @click="togglePlayback"><Pause v-if="playback.playing" :size="18" /><Play v-else :size="18" /></button>
          <button class="btn btn-quiet h-11 min-h-11 w-11 p-0" aria-label="返回完整播放器" @click="openFullPlayer"><Maximize2 :size="18" /></button>
          <button class="btn btn-quiet ml-auto h-11 min-h-11 w-11 p-0" aria-label="关闭播放器" @click="stop"><X :size="18" /></button>
        </div>
      </div>
      <div v-if="(playback.preparing || playback.stalled) && full" class="pointer-events-none absolute inset-0 grid place-items-center bg-black/35 text-white" role="status" aria-live="polite">
        <span class="rounded-full bg-black/65 px-4 py-2 text-sm font-bold"><LoaderCircle class="mr-2 inline animate-spin" :size="18" />{{ playback.stalled ? '正在等待视频数据…' : '正在准备播放…' }}</span>
      </div>
    </div>

    <div v-if="full" class="grid gap-4 bg-[var(--surface-solid)] p-4">
      <div class="grid gap-2 sm:flex sm:flex-wrap sm:items-center" role="group" aria-label="播放线路">
        <span class="muted text-sm font-bold sm:mr-1">播放线路</span>
        <button
          v-for="option in sourceOptions"
          :key="option.value"
          type="button"
          class="btn min-h-11 w-full justify-center sm:w-auto"
          :class="playback.activeSource === option.value ? 'btn-primary' : 'btn-secondary'"
          :disabled="!option.available || (playback.switchingSource && playback.activeSource === option.value)"
          :aria-pressed="playback.activeSource === option.value"
          @click="chooseSource(option.value)"
        >
          <Network v-if="option.value === 'direct'" :size="16" />
          <Server v-else :size="16" />
          {{ option.label }}
          <span v-if="!option.available" class="text-xs opacity-70">不可用</span>
        </button>
      </div>
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div class="flex flex-wrap gap-2">
        <button class="btn btn-secondary" :disabled="!playback.previousSelection" @click="playback.previousSelection && playback.start(playback.previousSelection, { autoplay: true })"><SkipBack :size="16" />上一集</button>
        <button class="btn btn-primary" :disabled="!playback.nextSelection" @click="playback.nextSelection && playback.start(playback.nextSelection, { autoplay: true })">下一集<SkipForward :size="16" /></button>
        </div>
        <label class="flex min-h-11 items-center gap-2 text-sm font-bold"><input :checked="playback.autoNext" type="checkbox" class="h-4 w-4 accent-[var(--brand)]" @change="playback.setAutoNext(($event.currentTarget as HTMLInputElement).checked)" />播完自动下一集</label>
      </div>
    </div>
    <div v-if="playback.error" class="bg-[var(--danger-soft)] p-3 text-sm font-bold text-[var(--danger)]" role="alert">{{ playback.error }}</div>
  </section>

  <button
    v-else-if="workspace.isMedia && latest && !full"
    type="button"
    class="glass fixed bottom-[5.8rem] left-3 right-3 z-[45] flex min-h-20 items-center gap-3 rounded-2xl p-3 text-left shadow-xl sm:left-auto sm:w-[390px] lg:bottom-6 lg:right-6"
    @click="resumeLatest"
  >
    <img :src="normalizePosterURL(latest.image)" alt="" class="h-14 w-10 rounded-lg object-cover" />
    <span class="min-w-0 flex-1"><strong class="block truncate text-sm">继续观看 · {{ latest.title }}</strong><small class="muted mt-1 block truncate">第 {{ latest.episode || '?' }} 集 · 已播放 {{ Math.round(latest.progress_percent) }}%</small></span>
    <span class="grid h-11 w-11 shrink-0 place-items-center rounded-full bg-[var(--brand)] text-white"><Play :size="18" /></span>
  </button>
</template>
