<script setup lang="ts">
import { Clock3, PlayCircle } from '@lucide/vue'
import { handlePosterError, posterURL } from '../../api/client'
import type { Subscription } from '../../api/types'
import AppDialog from '../AppDialog.vue'
import StateBlock from '../StateBlock.vue'

defineProps<{
  open: boolean
  title: string
  loading: boolean
  error: boolean
  retrying: boolean
  item?: Subscription | null
  runs: Array<Record<string, unknown>>
  logs: Array<Record<string, unknown>>
}>()

const emit = defineEmits<{
  'update:open': [value: boolean]
  retry: []
  play: [item: Subscription]
}>()

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

function numberField(row: Record<string, unknown>, primary: string, fallback: string) {
  const value = Number(row[primary] ?? row[fallback] ?? 0)
  return Number.isFinite(value) ? value : 0
}

function hasLiveProgress(row: Record<string, unknown>) {
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
          @error="handlePosterError($event, item.image)"
        />
        <div class="min-w-0">
          <div class="flex flex-wrap items-center gap-2">
            <strong class="truncate text-lg">{{ title }}</strong>
            <span v-if="item.library_stage" class="badge" :class="item.playable ? 'badge-success' : ''">{{ item.library_stage }}</span>
          </div>
          <p class="muted mt-1 text-sm">{{ item.subtitle_group || '未指定字幕组' }} · 已加入下载 {{ item.downloaded_count }} 集</p>
          <p v-if="item.library_hint" class="muted mt-2 text-xs">{{ item.library_hint }}</p>
        </div>
        <button v-if="item.playable && item.local_anime_id" class="btn btn-primary" @click="emit('play', item)">
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
    </div>
  </AppDialog>
</template>
