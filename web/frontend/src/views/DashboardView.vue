<script setup lang="ts">
import { useQuery } from '@tanstack/vue-query'
import { useRouter } from 'vue-router'
import { AlertCircle, ArrowRight, CheckCircle2, Download, Library, Play, PlayCircle, RefreshCw, Sparkles, Tv } from '@lucide/vue'
import { api, posterThumbnailURL } from '../api/client'
import type { ContinueWatchingItem, ContinueWatchingResponse, Dashboard, TaskAccepted } from '../api/types'
import AsyncButton from '../components/AsyncButton.vue'
import PageHeader from '../components/PageHeader.vue'
import StateBlock from '../components/StateBlock.vue'
import { useAsyncActions } from '../composables/useAsyncActions'
import { usePlaybackStore } from '../stores/playback'
import { useUIStore } from '../stores/ui'

const router = useRouter()
const ui = useUIStore()
const actions = useAsyncActions()
const playback = usePlaybackStore()
const query = useQuery({ queryKey: ['dashboard'], queryFn: () => api<Dashboard>('/dashboard') })
const continueQuery = useQuery({ queryKey: ['continue-watching'], queryFn: () => api<ContinueWatchingResponse>('/playback/continue?limit=10'), staleTime: 20_000 })

async function sync() {
  try {
    await actions.runTask('sync', () => api<TaskAccepted>('/tasks/sync', { method: 'POST' }), '立即同步', 'sync', '正在同步订阅、本地媒体和下载状态')
    ui.toast('同步任务已经启动')
  } catch (error) {
    ui.toast(error instanceof Error ? error.message : '同步失败', 'error')
  }
}

async function resume(item: ContinueWatchingItem) {
  const started = playback.resume(item, true)
  await router.push(`/player?anime=${item.anime_id}&episode=${item.episode_id}`)
  await started
}

function remainingLabel(seconds: number) {
  if (!seconds) return '可继续播放'
  const minutes = Math.max(1, Math.round(seconds / 60))
  return `剩余约 ${minutes} 分钟`
}
</script>

<template>
  <div class="page-grid">
    <PageHeader eyebrow="Today" title="今天也在安心追番" description="更新、下载和媒体库状态已经汇总到这里。">
      <AsyncButton class="btn btn-primary" :loading="actions.isBusy('sync', 'manual-sync')" loading-label="同步中…" @click="sync"><RefreshCw :size="17" />立即同步</AsyncButton>
    </PageHeader>
    <StateBlock v-if="query.isLoading.value" state="loading" />
    <StateBlock v-else-if="query.isError.value" state="error" title="概览加载失败" :retrying="query.isFetching.value" @retry="query.refetch()" />
    <template v-else-if="query.data.value">
      <section v-if="continueQuery.data.value?.items.length" class="panel overflow-hidden p-5 sm:p-6">
        <div class="mb-5 flex items-center justify-between gap-3">
          <div><p class="eyebrow">CONTINUE WATCHING</p><h2 class="mt-1 text-2xl font-black">继续观看</h2></div>
          <RouterLink to="/player" class="btn btn-quiet">打开播放器<ArrowRight :size="16" /></RouterLink>
        </div>
        <div class="-mx-1 flex snap-x gap-4 overflow-x-auto px-1 pb-2">
          <button v-for="item in continueQuery.data.value.items" :key="`${item.anime_id}:${item.episode_id}`" type="button" class="group w-[210px] shrink-0 snap-start text-left sm:w-[250px]" @click="resume(item)">
            <span class="relative block aspect-video overflow-hidden rounded-2xl bg-[var(--surface-muted)]">
              <img :src="posterThumbnailURL(item.image, 480)" :alt="item.title" loading="lazy" decoding="async" class="h-full w-full object-cover transition duration-200 group-hover:scale-[1.03]" />
              <span class="absolute inset-0 bg-gradient-to-t from-black/75 via-black/5 to-transparent"></span>
              <span class="absolute bottom-3 left-3 grid h-11 w-11 place-items-center rounded-full bg-white text-black shadow-lg"><Play :size="19" fill="currentColor" /></span>
              <span class="absolute bottom-0 left-0 h-1.5 bg-[var(--brand)]" :style="{ width: `${item.progress_percent}%` }"></span>
            </span>
            <strong class="mt-3 block truncate">{{ item.title }}</strong>
            <span class="muted mt-1 block truncate text-xs">第 {{ item.episode || '?' }} 集 · {{ remainingLabel(item.remaining_seconds) }}</span>
          </button>
        </div>
      </section>

      <section class="grid gap-4 sm:grid-cols-2 xl:grid-cols-5">
        <article v-for="item in [{ label: '活跃订阅', value: query.data.value.active_subscriptions, icon: Tv, tone: 'brand' }, { label: '下载记录', value: query.data.value.downloads, icon: Download, tone: 'sky' }, { label: '番剧图鉴', value: query.data.value.library_items, icon: Library, tone: 'brand' }, { label: '本地剧集', value: query.data.value.local_series, icon: PlayCircle, tone: 'sky' }, { label: '待处理问题', value: query.data.value.open_issues, icon: AlertCircle, tone: 'danger' }]" :key="item.label" class="panel p-5">
          <component :is="item.icon" :size="20" :class="item.tone === 'danger' ? 'text-[var(--danger)]' : item.tone === 'sky' ? 'text-[var(--sky)]' : 'text-[var(--brand)]'" />
          <p class="muted mt-5 text-xs font-extrabold uppercase tracking-wider">{{ item.label }}</p><strong class="mt-1 block text-3xl font-black">{{ item.value }}</strong>
        </article>
      </section>

      <section class="grid gap-5 xl:grid-cols-[1.4fr_.8fr]">
        <div class="panel p-5 sm:p-6">
          <div class="mb-5 flex items-center justify-between"><div><p class="eyebrow">AUTOMATION</p><h3 class="mt-1 text-xl font-black">自动化任务</h3></div><RouterLink to="/health" class="btn btn-quiet">查看健康状态<ArrowRight :size="16" /></RouterLink></div>
          <div class="grid gap-3 sm:grid-cols-2">
            <article v-for="task in query.data.value.tasks" :key="task.title" class="panel-muted p-4">
              <div class="flex items-start justify-between gap-3"><span class="grid h-9 w-9 place-items-center rounded-xl" :class="task.status_tone === 'rose' ? 'bg-red-100 text-red-700' : task.status_tone === 'amber' ? 'bg-amber-100 text-amber-700' : 'bg-emerald-100 text-emerald-700'"><AlertCircle v-if="task.status_tone === 'rose'" :size="18" /><CheckCircle2 v-else :size="18" /></span><span class="badge">{{ task.status_label }}</span></div>
              <h4 class="mt-4 font-extrabold">{{ task.title }}</h4><p class="muted mt-1 text-sm leading-5">{{ task.summary }}</p><p v-if="task.progress_text" class="mt-2 text-xs font-bold text-[var(--sky)]">{{ task.progress_text }}</p>
            </article>
          </div>
        </div>
        <aside class="panel p-5 sm:p-6">
          <div class="flex items-center gap-3"><span class="grid h-10 w-10 place-items-center rounded-xl bg-[var(--brand-soft)] text-[var(--brand)]"><Sparkles :size="19" /></span><div><p class="eyebrow">RECENT</p><h3 class="text-xl font-black">最近下载</h3></div></div>
          <div v-if="query.data.value.recent_downloads.length" class="mt-5 divide-y divide-[var(--line)]"><div v-for="item in query.data.value.recent_downloads" :key="item.ID" class="py-3"><p class="line-clamp-2 text-sm font-bold">{{ item.Title }}</p><p class="muted mt-1 text-xs">{{ item.Episode || '集数待识别' }} · {{ item.Status }}</p></div></div>
          <div v-else class="muted mt-10 text-center text-sm">还没有下载记录</div>
        </aside>
      </section>
    </template>
  </div>
</template>
