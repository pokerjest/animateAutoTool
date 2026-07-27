<script setup lang="ts">
import { computed, ref } from 'vue'
import { useQuery } from '@tanstack/vue-query'
import { Film, RefreshCw, Search } from '@lucide/vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '../api/client'
import type { MediaLibrary, MediaPage, MediaProvider } from '../api/types'
import AsyncButton from '../components/AsyncButton.vue'
import PageHeader from '../components/PageHeader.vue'
import PosterCard from '../components/PosterCard.vue'
import StateBlock from '../components/StateBlock.vue'
import { useAsyncActions } from '../composables/useAsyncActions'

const actions = useAsyncActions()
const route = useRoute()
const router = useRouter()
const search = ref('')
const providers = useQuery({ queryKey: ['media-providers'], queryFn: () => api<{ providers: MediaProvider[] }>('/media/providers') })
const libraries = useQuery({ queryKey: ['media-libraries', 'jellyfin'], queryFn: () => api<{ items: MediaLibrary[] }>('/media/providers/jellyfin/libraries'), enabled: computed(() => Boolean(providers.data.value?.providers?.some(item => item.connected))) })
const continueQuery = useQuery({ queryKey: ['media-continue'], queryFn: () => api<MediaPage>('/media/providers/jellyfin/items?section=continue'), enabled: computed(() => Boolean(providers.data.value?.providers?.some(item => item.connected))) })
const favoritesQuery = useQuery({ queryKey: ['media-favorites'], queryFn: () => api<MediaPage>('/media/providers/jellyfin/items?section=favorites'), enabled: computed(() => Boolean(providers.data.value?.providers?.some(item => item.connected))) })
const recentQuery = useQuery({ queryKey: ['media-recent'], queryFn: () => api<MediaPage>('/media/providers/jellyfin/items?library_id=all&sort_by=DateCreated&sort_order=Descending&page_size=12'), enabled: computed(() => Boolean(providers.data.value?.providers?.some(item => item.connected))) })
const jellyfin = computed(() => providers.data.value?.providers?.find(item => item.id === 'jellyfin'))
const configured = computed(() => Boolean(jellyfin.value?.configured))
const connected = computed(() => providers.data.value?.providers?.find(item => item.id === 'jellyfin')?.connected ?? false)
const section = computed(() => String(route.query.section || ''))
const shelves = computed(() => [
  { key: 'continue', title: '继续观看', items: continueQuery.data.value?.items || [] },
  { key: 'recent', title: '最近加入', items: recentQuery.data.value?.items || [] },
  { key: 'favorites', title: '我的收藏', items: favoritesQuery.data.value?.items || [] },
].filter(shelf => shelf.items.length && (!section.value || shelf.key === section.value)))

async function refresh() {
  await Promise.all([providers.refetch(), libraries.refetch(), continueQuery.refetch(), favoritesQuery.refetch(), recentQuery.refetch()])
}

function submitSearch() {
  void router.push({ path: '/media/library/all', query: { q: search.value || undefined } })
}
</script>

<template>
  <div class="page-grid">
    <PageHeader eyebrow="MEDIA CENTER" title="媒体首页" description="从 Jellyfin 读取媒体库、继续观看和收藏内容。播放线路在播放器视频下方选择。">
      <RouterLink class="btn btn-secondary" to="/media/library/all"><Film :size="17" />浏览媒体库</RouterLink>
      <AsyncButton class="btn btn-primary" :loading="actions.isBusy('media-refresh')" loading-label="刷新中…" @click="actions.run('media-refresh', refresh)"><RefreshCw :size="17" />刷新媒体库</AsyncButton>
    </PageHeader>

    <section v-if="providers.isLoading.value" class="panel p-6"><StateBlock state="loading" title="正在连接媒体服务" /></section>
    <section v-else-if="!configured" class="panel p-7">
      <StateBlock state="empty" title="请先配置 Jellyfin" description="媒体模式需要保存 Jellyfin 地址和 API Key。配置完成后即可回来浏览媒体库。" />
      <div class="mt-5 flex justify-center"><RouterLink class="btn btn-primary" to="/settings?focus=media">打开媒体服务设置</RouterLink></div>
    </section>
    <section v-else-if="!connected" class="panel p-7">
      <StateBlock state="error" title="Jellyfin 已配置但当前不可达" :description="jellyfin?.detail || '请检查 AnimateTool 连接地址、API Key 和网络代理设置。'" />
      <div class="mt-5 flex flex-wrap justify-center gap-2"><AsyncButton class="btn btn-primary" :loading="actions.isBusy('media-refresh')" loading-label="刷新中…" @click="actions.run('media-refresh', refresh)"><RefreshCw :size="17" />重新检查</AsyncButton><RouterLink class="btn btn-secondary" to="/settings?focus=media">检查媒体服务设置</RouterLink></div>
    </section>
    <template v-else>
      <section v-if="libraries.data.value?.items?.length" class="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <RouterLink v-for="library in libraries.data.value.items.filter(item => item.selected)" :key="library.id" :to="`/media/library/${encodeURIComponent(library.id)}`" class="panel flex items-center gap-4 p-5 transition hover:-translate-y-0.5 hover:border-[var(--brand)]">
          <span class="grid h-12 w-12 place-items-center rounded-2xl bg-[var(--brand-soft)] text-[var(--brand)]"><Film :size="22" /></span>
          <span class="min-w-0"><strong class="block truncate">{{ library.name }}</strong><small class="muted mt-1 block">{{ library.collection_type || '媒体库' }}</small></span>
        </RouterLink>
      </section>
      <form class="panel flex flex-col gap-3 p-4 sm:flex-row sm:items-center" @submit.prevent="submitSearch">
        <label class="search-field min-w-0 flex-1"><Search :size="18" /><input v-model="search" class="field field-leading-icon min-w-0" placeholder="搜索媒体标题、剧集或电影" aria-label="搜索媒体" /></label>
        <button class="btn btn-primary w-full justify-center sm:w-auto" type="submit">搜索</button>
      </form>
      <StateBlock v-if="!shelves.length" state="empty" title="媒体库暂时没有可展示内容" description="可以在 Jellyfin 中扫描媒体库，或者打开媒体库页面查看全部条目。" />
      <section v-for="shelf in shelves" :key="shelf.key" class="space-y-3">
        <div class="flex items-center justify-between"><h2 class="text-xl font-black">{{ shelf.title }}</h2><RouterLink class="muted text-sm font-bold hover:text-[var(--brand)]" :to="shelf.key === 'continue' ? '/media?section=continue' : shelf.key === 'favorites' ? '/media?section=favorites' : '/media/library/all'">查看全部</RouterLink></div>
        <div class="poster-grid">
          <PosterCard v-for="item in shelf.items.slice(0, 8)" :key="`${item.provider}:${item.id}`" openable :title="item.name" :image="item.poster_url" :meta="item.type === 'Episode' ? `${item.series_name || '剧集'} · 第 ${item.season || '?'} 季第 ${item.episode || '?'} 集` : item.production_year ? String(item.production_year) : item.type" :badges="[item.progress_percent > 0 && !item.played ? `${Math.round(item.progress_percent)}%` : '', item.favorite ? '收藏' : ''].filter(Boolean)" @open="router.push(`/media/item/${item.provider}/${encodeURIComponent(item.id)}`)" />
        </div>
      </section>
    </template>
  </div>
</template>
