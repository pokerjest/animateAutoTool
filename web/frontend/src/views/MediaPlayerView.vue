<script setup lang="ts">
import { computed, watch } from 'vue'
import { useQuery } from '@tanstack/vue-query'
import { ArrowLeft, CheckCircle2, Clock3 } from '@lucide/vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '../api/client'
import type { MediaItem, MediaPage } from '../api/types'
import { usePlaybackStore, type PlaybackSelection } from '../stores/playback'
import StateBlock from '../components/StateBlock.vue'

const route = useRoute()
const router = useRouter()
const playback = usePlaybackStore()
const provider = computed(() => String(route.params.provider))
const itemID = computed(() => String(route.params.itemId))
const itemQuery = useQuery({ queryKey: computed(() => ['media-player-item', provider.value, itemID.value]), queryFn: () => api<MediaItem>(`/media/providers/${encodeURIComponent(provider.value)}/items/${encodeURIComponent(itemID.value)}`) })
const siblingsQuery = useQuery({ queryKey: computed(() => ['media-player-siblings', provider.value, itemQuery.data.value?.parent_id]), queryFn: () => api<MediaPage>(`/media/providers/${encodeURIComponent(provider.value)}/items/${encodeURIComponent(itemQuery.data.value!.parent_id)}/children?type=episode`), enabled: computed(() => Boolean(itemQuery.data.value?.parent_id)) })
const item = computed(() => itemQuery.data.value)
const siblingItems = computed(() => siblingsQuery.data.value?.items || [])

function selectionFor(value: MediaItem): PlaybackSelection {
  return {
    provider: value.provider,
    itemId: value.id,
    title: value.series_name || value.name,
    episodeTitle: value.series_name ? `第 ${value.episode || '?'} 集 · ${value.name}` : value.name,
    image: value.poster_url,
    season: value.season || 1,
    episode: value.episode || 0,
  }
}

watch([item, siblingItems], ([current, siblings]) => {
  if (!current) return
  const list = siblings.length ? siblings : [current]
  playback.setQueue(list.map(selectionFor))
  if (playback.current?.itemId === current.id) return
  const autoplay = route.query.autoplay === '1'
  void playback.start(selectionFor(current), { autoplay, resumeTicks: current.resume_ticks })
  if (autoplay) void router.replace({ path: route.path })
}, { immediate: true })

function goTo(itemIDValue: string) {
  void router.push(`/media/play/${provider.value}/${encodeURIComponent(itemIDValue)}?autoplay=1`)
}
</script>

<template>
  <div class="page-grid">
    <div class="flex flex-wrap items-center justify-between gap-3"><button class="btn btn-quiet" type="button" @click="router.back()"><ArrowLeft :size="17" />返回详情</button><span v-if="item" class="badge">当前线路由系统设置决定</span></div>
    <StateBlock v-if="itemQuery.isLoading.value" state="loading" scene="diagnosing" title="正在准备播放" />
    <StateBlock v-else-if="itemQuery.isError.value" state="error" scene="error" title="播放项目读取失败" :retrying="itemQuery.isFetching.value" @retry="itemQuery.refetch()" />
    <template v-else-if="item">
      <section class="panel p-5">
        <div class="flex flex-wrap items-start justify-between gap-3"><div><p class="eyebrow">NOW PLAYING</p><h1 class="mt-2 text-2xl font-black">{{ item.series_name || item.name }}</h1><p class="muted mt-1">{{ item.series_name ? `第 ${item.season || '?'} 季 · 第 ${item.episode || '?'} 集 · ${item.name}` : item.name }}</p></div><div class="flex items-center gap-2"><span v-if="item.played" class="badge badge-success"><CheckCircle2 :size="13" />已看</span><span v-if="item.runtime_ticks" class="badge"><Clock3 :size="13" />{{ Math.round(item.runtime_ticks / 600000000) }} 分钟</span></div></div>
        <div v-if="siblingItems.length" class="mt-5 flex gap-2 overflow-x-auto pb-1"><button v-for="episode in siblingItems" :key="episode.id" class="btn whitespace-nowrap" :class="episode.id === item.id ? 'btn-primary' : 'btn-secondary'" type="button" @click="goTo(episode.id)">E{{ episode.episode || '?' }} {{ episode.name }}</button></div>
      </section>
    </template>
  </div>
</template>
