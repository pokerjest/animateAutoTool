<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useInfiniteQuery, useQuery } from '@tanstack/vue-query'
import { Search, SlidersHorizontal } from '@lucide/vue'
import { useRoute, useRouter } from 'vue-router'
import { api, apiEnvelope } from '../api/client'
import type { MediaItem, MediaLibrary, MediaPage } from '../api/types'
import AutoLoadSentinel from '../components/AutoLoadSentinel.vue'
import PageHeader from '../components/PageHeader.vue'
import PosterCard from '../components/PosterCard.vue'
import StateBlock from '../components/StateBlock.vue'

const route = useRoute()
const router = useRouter()
const search = ref(String(route.query.q || ''))
const libraryID = computed(() => String(route.params.libraryId || 'all'))
const appliedSearch = computed(() => String(route.query.q || ''))
const sort = computed(() => String(route.query.sort || 'name'))
const sortOptions: Record<string, { by: string; order: string }> = {
  name: { by: 'SortName', order: 'Ascending' },
  recent: { by: 'DateCreated', order: 'Descending' },
  year: { by: 'ProductionYear', order: 'Descending' },
  rating: { by: 'CommunityRating', order: 'Descending' },
}
const query = useInfiniteQuery({
  queryKey: computed(() => ['media-items', libraryID.value, appliedSearch.value, sort.value]),
  initialPageParam: 1,
  queryFn: ({ pageParam }) => {
    const selectedSort = sortOptions[sort.value] || sortOptions.name
    const params = new URLSearchParams({
      library_id: libraryID.value,
      q: appliedSearch.value,
      sort_by: selectedSort.by,
      sort_order: selectedSort.order,
      page: String(pageParam),
      page_size: '48',
    })
    return apiEnvelope<MediaPage>(`/media/providers/jellyfin/items?${params}`)
  },
  getNextPageParam: lastPage => {
    const page = lastPage.meta?.page ?? 1
    const pageSize = lastPage.meta?.page_size ?? lastPage.data.items.length
    const total = lastPage.meta?.total ?? lastPage.data.items.length
    return page * pageSize < total ? page + 1 : undefined
  },
})
const libraries = useQuery({ queryKey: ['media-libraries', 'jellyfin'], queryFn: () => api<{ items: MediaLibrary[] }>('/media/providers/jellyfin/libraries') })
const pages = computed(() => query.data.value?.pages || [])
const items = computed<MediaItem[]>(() => pages.value.flatMap(page => page.data.items))
const total = computed(() => pages.value[0]?.meta?.total ?? items.value.length)
const remaining = computed(() => Math.max(0, total.value - items.value.length))
watch(() => route.query.q, value => { search.value = String(value || '') })

function submitSearch() {
  void router.replace({ query: { ...route.query, q: search.value.trim() || undefined } })
}

function selectLibrary(value: string) {
  void router.push({ path: `/media/library/${encodeURIComponent(value)}`, query: { ...route.query, q: search.value.trim() || undefined } })
}

function selectSort(value: string) {
  void router.replace({ query: { ...route.query, sort: value === 'name' ? undefined : value } })
}

async function loadMore() {
  if (query.hasNextPage.value && !query.isFetchingNextPage.value) await query.fetchNextPage()
}
</script>

<template>
  <div class="page-grid">
    <PageHeader eyebrow="LIBRARY" title="媒体库" description="浏览 Jellyfin 中已选择的媒体库，支持标题搜索和基础筛选。">
      <RouterLink class="btn btn-secondary" to="/media">媒体首页</RouterLink>
    </PageHeader>
    <section class="panel grid gap-3 p-4 lg:grid-cols-[220px_1fr_220px_auto]">
      <select class="field" :value="libraryID" aria-label="选择媒体库" @change="selectLibrary(($event.target as HTMLSelectElement).value)">
        <option value="all">全部媒体库</option>
        <option v-for="library in libraries.data.value?.items.filter(item => item.selected)" :key="library.id" :value="library.id">{{ library.name }}</option>
      </select>
      <form class="search-field" @submit.prevent="submitSearch"><Search :size="18" /><input v-model="search" class="field field-leading-icon" placeholder="搜索标题" aria-label="搜索媒体标题" /></form>
      <select class="field" :value="sort" aria-label="媒体排序" @change="selectSort(($event.target as HTMLSelectElement).value)">
        <option value="name">按标题</option>
        <option value="recent">最近加入</option>
        <option value="year">发行年份</option>
        <option value="rating">社区评分</option>
      </select>
      <button class="btn btn-primary" type="button" @click="submitSearch"><SlidersHorizontal :size="16" />筛选</button>
    </section>
    <StateBlock v-if="query.isLoading.value" state="loading" title="正在读取媒体库" />
    <StateBlock v-else-if="query.isError.value" state="error" title="媒体库读取失败" :retrying="query.isFetching.value" @retry="query.refetch()" />
    <StateBlock v-else-if="!items.length" state="empty" title="没有找到媒体" description="检查 Jellyfin 媒体库扫描状态，或换一个搜索词。" />
    <section v-else class="poster-grid">
      <PosterCard v-for="item in items" :key="`${item.provider}:${item.id}`" openable :title="item.name" :image="item.poster_url" :meta="item.production_year ? String(item.production_year) : item.type" :badges="[item.favorite ? '收藏' : '', item.played ? '已看' : ''].filter(Boolean)" @open="router.push(`/media/item/${item.provider}/${encodeURIComponent(item.id)}`)" />
    </section>
    <AutoLoadSentinel v-if="query.hasNextPage.value" :remaining="remaining" @load="loadMore" />
    <p v-if="query.isFetchingNextPage.value" class="muted py-3 text-center text-sm" role="status" aria-live="polite">正在加载更多媒体…</p>
  </div>
</template>
