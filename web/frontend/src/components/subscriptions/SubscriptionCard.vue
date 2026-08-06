<script setup lang="ts">
import { computed } from 'vue'
import { CircleAlert, FilePenLine, Pause, Play, PlayCircle, RefreshCw, Sparkles, Trash2 } from '@lucide/vue'
import { handlePosterError, posterURL, subscriptionPosterURL } from '../../api/client'
import type { Subscription } from '../../api/types'
import AsyncButton from '../AsyncButton.vue'

interface RepairAction {
  name: string
  label: string
  visible: boolean
}

const props = defineProps<{
  item: Subscription
  isBusy: (...keys: string[]) => boolean
}>()

const emit = defineEmits<{
  open: []
  play: []
  toggle: []
  check: []
  repair: [name: string]
  aiRules: []
  edit: []
  remove: []
}>()

const displayTitle = computed(() => (
  props.item.metadata?.title_cn || props.item.metadata?.title || props.item.title
))

const downloadProgressLabel = computed(() => {
  const total = props.item.expected_episodes
  return total > 0
    ? `已加入下载 ${props.item.downloaded_count} / ${total} 集`
    : `已加入下载 ${props.item.downloaded_count} 集`
})

const releaseFilterLabel = computed(() => {
  const parts: string[] = []
  if (props.item.resolution_filter) parts.push(props.item.resolution_filter.toUpperCase())
  if (props.item.subtitle_language === 'chs') parts.push('简中')
  if (props.item.subtitle_language === 'cht') parts.push('繁中')
  if (props.item.subtitle_language === 'chs_cht') parts.push('简繁')
  return parts.join(' · ')
})

// Local playback uses the local episode table; Jellyfin association is
// optional and must not hide a playable local series from the subscription UI.
const canPlay = computed(() => Boolean(
  props.item.local_anime_id && (props.item.library_episode_count || 0) > 0,
))
const usesResourceLedger = computed(() => props.item.rss_count !== undefined)
const showIssueBadge = computed(() => usesResourceLedger.value
  ? Boolean(props.item.needs_attention)
  : Boolean(props.item.last_error_display || props.item.has_repair_actions))
const resourceCountLabel = computed(() => {
  if (!usesResourceLedger.value || !props.item.rss_count) return ''
  return `RSS ${props.item.rss_count} · 规范 ${props.item.canonical_episode_count || 0}`
})

const repairActions = computed<RepairAction[]>(() => [
  { name: 'use-base-rss', label: '改用主 RSS', visible: Boolean(props.item.can_use_base_rss) },
  { name: 'clear-filter', label: '清空过滤', visible: Boolean(props.item.can_clear_filter) },
  { name: 'reset-logs', label: '清理阻塞记录', visible: Boolean(props.item.can_reset_stale_logs) },
  { name: 'retry-missing', label: '重检缺集', visible: Boolean(props.item.can_retry_missing) },
  { name: 'recheck-stale', label: '重检停滞', visible: Boolean(props.item.can_retry_stale) },
  { name: 'retry-upgrade', label: '重试升级', visible: Boolean(props.item.can_retry_upgrade) },
  { name: 'refresh-library', label: '同步 Jellyfin', visible: Boolean(props.item.can_refresh_library) },
].filter(action => action.visible))

function isRepairBusy(name: string) {
  return props.isBusy(
    `repair-${props.item.ID}-${name}`,
    `subscription-${props.item.ID}-${name}`,
  )
}
</script>

<template>
  <article class="panel group relative isolate grid gap-4 p-4 transition hover:ring-2 hover:ring-[var(--brand)] md:grid-cols-[72px_1fr_auto] md:items-center">
    <button
      type="button"
      class="absolute inset-0 z-0 cursor-pointer rounded-[inherit] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--brand)] focus-visible:ring-offset-2"
      :aria-label="`查看 ${displayTitle} 订阅详情`"
      data-testid="subscription-open"
      @click="emit('open')"
    />
    <img
      :src="posterURL(item.metadata || { image: item.image }, { width: 160 })"
      :alt="`${item.title} 海报`"
      loading="lazy"
      decoding="async"
      fetchpriority="low"
      class="pointer-events-none relative z-10 h-24 w-16 rounded-xl object-cover md:h-20 md:w-14"
      @error="handlePosterError($event, subscriptionPosterURL(item.ID, 'mikan', 160), subscriptionPosterURL(item.ID, 'local', 160))"
    />

    <div class="pointer-events-none relative z-10 min-w-0">
      <div class="flex flex-wrap items-center gap-2">
        <h3 class="truncate font-extrabold">{{ displayTitle }}</h3>
        <span class="badge" :class="item.is_active ? 'badge-success' : ''">
          {{ item.is_active ? '运行中' : '已暂停' }}
        </span>
        <span v-if="showIssueBadge" class="badge badge-danger">
          <CircleAlert :size="13" />
          需处理
        </span>
        <span v-if="resourceCountLabel" class="badge">
          {{ resourceCountLabel }}
        </span>
      </div>
      <p class="muted mt-1 truncate text-sm">
        {{ item.subtitle_group || '未指定字幕组' }}<template v-if="releaseFilterLabel"> · {{ releaseFilterLabel }}</template> · {{ downloadProgressLabel }}
      </p>
      <p class="mt-2 text-xs" :class="showIssueBadge && item.last_error_display ? 'text-[var(--danger)]' : 'muted'">
        {{ item.last_error_display || item.last_run_summary || '等待首次检查' }}
      </p>
      <p v-if="item.library_hint" class="muted mt-1 text-xs">
        {{ item.library_hint }}
      </p>

      <div v-if="repairActions.length" class="pointer-events-auto mt-3 flex flex-wrap gap-2">
        <AsyncButton
          v-for="action in repairActions"
          :key="action.name"
          class="badge badge-warning"
          :loading="isRepairBusy(action.name)"
          loading-label="处理中…"
          @click="emit('repair', action.name)"
        >
          {{ action.label }}
        </AsyncButton>
      </div>
    </div>

    <div class="relative z-10 flex flex-wrap gap-2 md:justify-end">
      <button v-if="canPlay" class="btn btn-primary" aria-label="打开播放器" @click="emit('play')">
        <PlayCircle :size="16" />
        播放
      </button>
      <AsyncButton
        class="btn btn-secondary"
        :loading="isBusy(`toggle-${item.ID}`)"
        loading-label="处理中…"
        @click="emit('toggle')"
      >
        <Pause v-if="item.is_active" :size="16" />
        <Play v-else :size="16" />
        {{ item.is_active ? '暂停' : '启用' }}
      </AsyncButton>
      <AsyncButton
        class="btn btn-secondary"
        :loading="isBusy(`run-${item.ID}`, `subscription-${item.ID}`)"
        loading-label="检查中…"
        @click="emit('check')"
      >
        <RefreshCw :size="16" />
        检查
      </AsyncButton>
      <AsyncButton class="btn btn-secondary" :loading="isBusy(`ai-rules-${item.ID}`)" loading-label="分析中…" @click="emit('aiRules')">
        <Sparkles :size="16" />
        AI 规则
      </AsyncButton>
      <button class="btn btn-quiet h-11 w-11 p-0" aria-label="编辑订阅" @click="emit('edit')">
        <FilePenLine :size="17" />
      </button>
      <button class="btn btn-danger h-11 w-11 p-0" aria-label="删除订阅" @click="emit('remove')">
        <Trash2 :size="16" />
      </button>
    </div>
  </article>
</template>
