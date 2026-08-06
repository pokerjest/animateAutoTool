<script setup lang="ts">
import { computed } from 'vue'
import { ArrowUpCircle, Clock3, PlayCircle, RotateCw } from '@lucide/vue'
import { handlePosterError, posterURL, subscriptionPosterURL } from '../../api/client'
import type { Subscription, SubscriptionResource } from '../../api/types'
import AppDialog from '../AppDialog.vue'
import AsyncButton from '../AsyncButton.vue'
import StateBlock from '../StateBlock.vue'

const props = defineProps<{
  open: boolean
  title: string
  loading: boolean
  error: boolean
  retrying: boolean
  item?: Subscription | null
  runs: Array<Record<string, unknown>>
  logs: Array<Record<string, unknown>>
  resources: SubscriptionResource[]
  isBusy: (...keys: string[]) => boolean
}>()

const emit = defineEmits<{
  'update:open': [value: boolean]
  retry: []
  play: [item: Subscription]
  resourceAction: [resource: SubscriptionResource, action: 'retry' | 'upgrade']
}>()

const groupedResources = computed(() => {
  const groups = new Map<string, { label: string; items: SubscriptionResource[] }>()
  for (const resource of props.resources) {
    const key = resource.canonical_key || `resource:${resource.ID}`
    const season = resource.season_val || 'S?'
    const episode = resource.episode || '?'
    const current = groups.get(key) || { label: `${season} · 第 ${episode} 集`, items: [] }
    current.items.push(resource)
    groups.set(key, current)
  }
  return [...groups.values()]
})

function field(row: Record<string, unknown>, primary: string, fallback: string) {
  return String(row[primary] ?? row[fallback] ?? '')
}

function statusLabel(value: string) {
  switch (value.trim().toLowerCase()) {
    case 'downloading':
      return '下载中'
    case 'completed':
      return '已完成'
    case 'renamed':
      return '已整理'
    case 'failed':
      return '失败'
    case 'archived':
      return '已归档'
    case 'seen':
      return '已发现'
    case 'filtered':
      return '已过滤'
    case 'pending':
      return '待提交'
    case 'unknown':
      return '待核对'
    case 'unresolved':
      return '无法识别'
    case 'superseded':
      return '候选版本'
    case 'success':
      return '成功'
    case 'warning':
      return '警告'
    case 'error':
      return '失败'
    case 'idle':
      return '无更新'
    default:
      return value
  }
}

function resourceTone(state: SubscriptionResource['state']) {
  if (state === 'completed') return 'badge-success'
  if (state === 'failed' || state === 'unknown' || state === 'unresolved') return 'badge-danger'
  if (state === 'downloading' || state === 'pending') return 'badge-warning'
  return ''
}

function canRetry(resource: SubscriptionResource) {
  return ['failed', 'unknown', 'unresolved'].includes(resource.state)
}

function canUpgrade(resource: SubscriptionResource) {
  return resource.state === 'superseded' && Number(resource.version_tag?.replace(/^V/i, '') || 1) > 1
}

function numberField(row: Record<string, unknown>, primary: string, fallback: string) {
  const value = Number(row[primary] ?? row[fallback] ?? 0)
  return Number.isFinite(value) ? value : 0
}

function hasLiveProgress(row: Record<string, unknown>) {
  if (field(row, 'Status', 'status').trim().toLowerCase() === 'archived') return false
  return numberField(row, 'total_bytes', 'TotalBytes') > 0
    || numberField(row, 'downloaded_bytes', 'DownloadedBytes') > 0
    || numberField(row, 'download_speed', 'DownloadSpeed') > 0
    || numberField(row, 'progress_percent', 'ProgressPercent') > 0
}

function progressPercent(row: Record<string, unknown>) {
  return Math.min(100, Math.max(0, numberField(row, 'progress_percent', 'ProgressPercent')))
}

function formatBytes(value: number) {
  if (value < 1024) return `${Math.round(value)} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let size = value
  let unit = -1
  while (size >= 1024 && unit < units.length - 1) {
    size /= 1024
    unit += 1
  }
  return `${size.toFixed(size >= 100 ? 0 : size >= 10 ? 1 : 2)} ${units[unit]}`
}

function formatSpeed(value: number) {
  return value > 0 ? `${formatBytes(value)}/s` : ''
}
</script>

<template>
  <AppDialog
    :open="open"
    :title="title"
    description="最近的检查、下载和错误记录。"
    wide
    @update:open="emit('update:open', $event)"
  >
    <StateBlock v-if="loading" state="loading" />
    <StateBlock
      v-else-if="error"
      state="error"
      title="历史记录加载失败"
      :retrying="retrying"
      @retry="emit('retry')"
    />
    <div v-else class="grid gap-5">
      <section v-if="item" class="panel-muted grid gap-4 p-4 sm:grid-cols-[80px_1fr_auto] sm:items-center">
        <img
          :src="posterURL(item.metadata || { image: item.image }, { width: 160 })"
          :alt="`${title} 海报`"
          decoding="async"
          class="h-28 w-20 rounded-xl object-cover"
          @error="handlePosterError($event, subscriptionPosterURL(item.ID, 'mikan', 160, item.UpdatedAt || item.updated_at || item.metadata?.UpdatedAt || item.metadata?.updated_at || item.image), subscriptionPosterURL(item.ID, 'local', 160, item.UpdatedAt || item.updated_at || item.metadata?.UpdatedAt || item.metadata?.updated_at || item.image))"
        />
        <div class="min-w-0">
          <div class="flex flex-wrap items-center gap-2">
            <strong class="truncate text-lg">{{ title }}</strong>
            <span v-if="item.library_stage" class="badge" :class="item.playable ? 'badge-success' : ''">{{ item.library_stage }}</span>
          </div>
          <p class="muted mt-1 text-sm">{{ item.subtitle_group || '未指定字幕组' }} · 已加入下载 {{ item.downloaded_count }} 集</p>
          <p v-if="item.library_hint" class="muted mt-2 text-xs">{{ item.library_hint }}</p>
        </div>
        <button v-if="item.local_anime_id && (item.library_episode_count || 0) > 0" class="btn btn-primary" @click="emit('play', item)">
          <PlayCircle :size="17" />查看与播放
        </button>
      </section>

      <div class="grid gap-5 lg:grid-cols-2">
      <section>
        <h3 class="font-black">最近检查</h3>
        <div class="mt-3 space-y-2">
          <article v-for="run in runs" :key="field(run, 'ID', 'id')" class="panel-muted p-3 text-sm">
            <div class="flex items-center justify-between gap-2">
              <strong>{{ statusLabel(field(run, 'Status', 'status')) || '已检查' }}</strong>
              <span class="muted flex items-center gap-1 text-xs">
                <Clock3 :size="13" />
                {{ field(run, 'CheckedAt', 'checked_at') }}
              </span>
            </div>
            <p class="muted mt-1">{{ field(run, 'Summary', 'summary') || field(run, 'Error', 'error') }}</p>
          </article>
        </div>
      </section>

      <section>
        <h3 class="font-black">最近下载</h3>
        <div class="mt-3 space-y-2">
          <article v-for="log in logs" :key="field(log, 'ID', 'id')" class="panel-muted p-3 text-sm">
            <strong class="line-clamp-2">{{ field(log, 'Title', 'title') }}</strong>
            <div class="muted mt-1 flex items-center justify-between gap-2">
              <p>第 {{ field(log, 'Episode', 'episode') || '?' }} 集 · {{ statusLabel(field(log, 'Status', 'status')) }}</p>
              <strong v-if="hasLiveProgress(log)" class="shrink-0 text-[var(--brand)]">{{ progressPercent(log).toFixed(1) }}%</strong>
            </div>
            <template v-if="hasLiveProgress(log)">
              <div class="mt-2 h-1.5 overflow-hidden rounded-full bg-[var(--line)]" role="progressbar" :aria-valuenow="progressPercent(log)" aria-valuemin="0" aria-valuemax="100">
                <div class="h-full rounded-full bg-[var(--brand)] transition-[width] duration-500" :style="{ width: `${progressPercent(log)}%` }"></div>
              </div>
              <p class="muted mt-1 text-xs">
                {{ formatBytes(numberField(log, 'downloaded_bytes', 'DownloadedBytes')) }} /
                {{ formatBytes(numberField(log, 'total_bytes', 'TotalBytes')) }}
                <span v-if="formatSpeed(numberField(log, 'download_speed', 'DownloadSpeed'))"> · {{ formatSpeed(numberField(log, 'download_speed', 'DownloadSpeed')) }}</span>
              </p>
            </template>
            <p v-else-if="field(log, 'Status', 'status') === 'downloading'" class="muted mt-2 text-xs">
              正在等待下载器同步进度…
            </p>
          </article>
        </div>
      </section>
      </div>

      <section v-if="groupedResources.length">
        <div class="flex flex-wrap items-end justify-between gap-2">
          <div>
            <h3 class="font-black">资源对账</h3>
            <p class="muted mt-1 text-xs">V2/V3 默认仅保留为候选，不会自动替换已下载或下载中的版本。</p>
          </div>
          <span class="badge">RSS {{ resources.length }} 条</span>
        </div>
        <div class="mt-3 grid gap-3">
          <article v-for="group in groupedResources" :key="group.label" class="panel-muted overflow-hidden">
            <header class="flex flex-wrap items-center justify-between gap-2 border-b border-[var(--line)] px-4 py-3">
              <strong>{{ group.label }}</strong>
              <span class="muted text-xs">{{ group.items.length }} 个候选</span>
            </header>
            <div class="divide-y divide-[var(--line)]">
              <div v-for="resource in group.items" :key="resource.ID" class="grid gap-3 px-4 py-3 md:grid-cols-[1fr_auto] md:items-center">
                <div class="min-w-0">
                  <div class="flex flex-wrap items-center gap-2">
                    <span class="badge">{{ resource.version_tag || 'V1' }}</span>
                    <span class="badge" :class="resourceTone(resource.state)">{{ statusLabel(resource.state) }}</span>
                    <span v-if="resource.selected" class="badge badge-success">当前选择</span>
                  </div>
                  <p class="mt-2 line-clamp-2 text-sm font-semibold">{{ resource.title }}</p>
                  <p v-if="resource.state_reason || resource.last_error" class="muted mt-1 text-xs">
                    {{ resource.last_error || resource.state_reason }}
                  </p>
                </div>
                <div v-if="canRetry(resource) || canUpgrade(resource)" class="flex flex-wrap gap-2 md:justify-end">
                  <AsyncButton
                    v-if="canRetry(resource)"
                    class="btn btn-secondary"
                    :loading="isBusy(`resource-retry-${resource.ID}`)"
                    loading-label="重试中…"
                    @click="emit('resourceAction', resource, 'retry')"
                  >
                    <RotateCw :size="16" />重试
                  </AsyncButton>
                  <AsyncButton
                    v-if="canUpgrade(resource)"
                    class="btn btn-secondary"
                    :loading="isBusy(`resource-upgrade-${resource.ID}`)"
                    loading-label="切换中…"
                    @click="emit('resourceAction', resource, 'upgrade')"
                  >
                    <ArrowUpCircle :size="16" />使用此版本
                  </AsyncButton>
                </div>
              </div>
            </div>
          </article>
        </div>
      </section>
    </div>
  </AppDialog>
</template>
