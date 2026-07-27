<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useQuery } from '@tanstack/vue-query'
import { ArrowLeft, CheckCircle2, Clock3, Heart, PlayCircle } from '@lucide/vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '../api/client'
import type { MediaItem, MediaPage } from '../api/types'
import AsyncButton from '../components/AsyncButton.vue'
import StateBlock from '../components/StateBlock.vue'
import { useAsyncActions } from '../composables/useAsyncActions'

const route = useRoute()
const router = useRouter()
const actions = useAsyncActions()
const provider = computed(() => String(route.params.provider))
const itemID = computed(() => String(route.params.itemId))
const itemQuery = useQuery({ queryKey: computed(() => ['media-item', provider.value, itemID.value]), queryFn: () => api<MediaItem>(`/media/providers/${encodeURIComponent(provider.value)}/items/${encodeURIComponent(itemID.value)}`) })
const seasonsQuery = useQuery({ queryKey: computed(() => ['media-children', provider.value, itemID.value, 'season']), queryFn: () => api<MediaPage>(`/media/providers/${encodeURIComponent(provider.value)}/items/${encodeURIComponent(itemID.value)}/children?type=season`), enabled: computed(() => itemQuery.data.value?.type === 'Series') })
const selectedSeason = ref('')
const episodesQuery = useQuery({ queryKey: computed(() => ['media-children', provider.value, selectedSeason.value, 'episode']), queryFn: () => api<MediaPage>(`/media/providers/${encodeURIComponent(provider.value)}/items/${encodeURIComponent(selectedSeason.value)}/children?type=episode`), enabled: computed(() => Boolean(selectedSeason.value)) })
const item = computed(() => itemQuery.data.value)
const seasons = computed(() => seasonsQuery.data.value?.items || [])
const episodes = computed(() => episodesQuery.data.value?.items || [])
const playTarget = computed(() => {
  if (!item.value) return null
  if (item.value.type !== 'Series') return item.value
  return episodes.value.find(episode => episode.resume_ticks > 0 && !episode.played)
    || episodes.value.find(episode => !episode.played)
    || episodes.value[0]
    || null
})
watch(seasons, value => {
  if (!selectedSeason.value && value.length) {
    selectedSeason.value = (value.find(season => season.season > 0) || value[0]).id
  }
}, { immediate: true })

async function updateState(kind: 'played' | 'favorite') {
  if (!item.value) return
  const value = kind === 'played' ? !item.value.played : !item.value.favorite
  await actions.run(`media-state-${kind}`, async () => {
    const result = await api<{ played: boolean; favorite: boolean }>(`/media/providers/${provider.value}/items/${itemID.value}/user-state`, { method: 'PUT', body: JSON.stringify({ [kind === 'played' ? 'played' : 'favorite']: value }) })
    item.value!.played = result.played
    item.value!.favorite = result.favorite
  })
}
</script>

<template>
  <div class="page-grid">
    <button class="btn btn-quiet w-fit" type="button" @click="router.back()"><ArrowLeft :size="17" />返回</button>
    <StateBlock v-if="itemQuery.isLoading.value" state="loading" title="正在读取媒体详情" />
    <StateBlock v-else-if="itemQuery.isError.value" state="error" title="媒体详情读取失败" :retrying="itemQuery.isFetching.value" @retry="itemQuery.refetch()" />
    <template v-else-if="item">
      <section class="panel overflow-hidden">
        <div class="grid gap-6 p-5 lg:grid-cols-[240px_1fr]">
          <img :src="item.poster_url" :alt="item.name" class="aspect-[2/3] w-full max-w-60 rounded-2xl object-cover" />
          <div class="min-w-0">
            <div class="flex flex-wrap items-start justify-between gap-3"><div class="min-w-0"><p class="eyebrow">{{ item.type }}</p><h1 class="mt-2 break-words text-3xl font-black">{{ item.name }}</h1><p v-if="item.series_name" class="muted mt-1 break-words">{{ item.series_name }}</p></div><span v-if="item.community_rating" class="badge badge-success shrink-0">★ {{ item.community_rating.toFixed(1) }}</span></div>
            <p class="muted mt-4 max-w-3xl whitespace-pre-line text-sm leading-7">{{ item.overview || '暂无简介' }}</p>
            <div class="mt-5 flex flex-wrap gap-2"><span v-if="item.production_year" class="badge">{{ item.production_year }}</span><span v-for="genre in item.genres" :key="genre" class="badge">{{ genre }}</span></div>
            <div class="mt-6 flex flex-wrap gap-2">
              <RouterLink v-if="playTarget" class="btn btn-primary w-full justify-center sm:w-auto" :to="`/media/play/${provider}/${encodeURIComponent(playTarget.id)}?autoplay=1`"><PlayCircle :size="17" />{{ playTarget.resume_ticks ? '继续播放' : '播放' }}</RouterLink>
              <button v-else class="btn btn-primary w-full justify-center sm:w-auto" type="button" disabled><PlayCircle :size="17" />正在读取剧集</button>
              <AsyncButton class="btn btn-secondary w-full justify-center sm:w-auto" :loading="actions.isBusy('media-state-played')" @click="updateState('played')"><CheckCircle2 :size="16" />{{ item.played ? '标为未看' : '标记已看' }}</AsyncButton>
              <AsyncButton class="btn btn-secondary w-full justify-center sm:w-auto" :loading="actions.isBusy('media-state-favorite')" @click="updateState('favorite')"><Heart :size="16" :fill="item.favorite ? 'currentColor' : 'none'" />{{ item.favorite ? '取消收藏' : '收藏' }}</AsyncButton>
            </div>
          </div>
        </div>
      </section>
      <section v-if="item.type === 'Series'" class="panel p-5">
        <div class="flex flex-wrap items-center justify-between gap-3"><h2 class="text-xl font-black">季度与剧集</h2><select v-if="seasons.length" v-model="selectedSeason" class="field max-w-52"><option v-for="season in seasons" :key="season.id" :value="season.id">{{ season.name }}</option></select></div>
        <StateBlock v-if="!seasons.length && seasonsQuery.isLoading.value" state="loading" title="正在读取季度" />
        <StateBlock v-else-if="!episodes.length" state="empty" title="当前季度还没有剧集" />
        <div v-else class="mt-5 space-y-3">
          <article v-for="episode in episodes" :key="episode.id" class="panel-muted grid gap-4 p-4 sm:grid-cols-[1fr_auto] sm:items-center">
            <div><div class="flex flex-wrap items-center gap-2"><h3 class="font-black">第 {{ episode.episode || '?' }} 集 · {{ episode.name }}</h3><span v-if="episode.played" class="badge badge-success">已看</span></div><p class="muted mt-1 line-clamp-2 text-sm">{{ episode.overview || '暂无简介' }}</p><p v-if="episode.runtime_ticks" class="muted mt-2 flex items-center gap-1 text-xs"><Clock3 :size="13" />{{ Math.round(episode.runtime_ticks / 600000000) }} 分钟</p><div v-if="episode.progress_percent && !episode.played" class="mt-2 h-1.5 max-w-80 overflow-hidden rounded-full bg-[var(--surface-muted)]"><div class="h-full rounded-full bg-[var(--brand)]" :style="{ width: `${episode.progress_percent}%` }"></div></div></div>
            <RouterLink class="btn btn-primary w-full justify-center sm:w-auto" :to="`/media/play/${provider}/${encodeURIComponent(episode.id)}?autoplay=1`"><PlayCircle :size="16" />{{ episode.resume_ticks ? '继续' : '播放' }}</RouterLink>
          </article>
        </div>
      </section>
    </template>
  </div>
</template>
