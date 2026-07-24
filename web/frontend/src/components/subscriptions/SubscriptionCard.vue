<script setup lang="ts">
import { computed } from 'vue'
import { CircleAlert, FilePenLine, History, Pause, Play, RefreshCw, Trash2 } from '@lucide/vue'
import { handlePosterError, posterURL } from '../../api/client'
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
  toggle: []
  check: []
  repair: [name: string]
  history: []
  edit: []
  remove: []
}>()

const displayTitle = computed(() => (
  props.item.metadata?.title_cn || props.item.metadata?.title || props.item.title
))

const repairActions = computed<RepairAction[]>(() => [
  { name: 'use-base-rss', label: '改用主 RSS', visible: Boolean(props.item.can_use_base_rss) },
  { name: 'clear-filter', label: '清空过滤', visible: Boolean(props.item.can_clear_filter) },
  { name: 'reset-logs', label: '清理阻塞记录', visible: Boolean(props.item.can_reset_stale_logs) },
  { name: 'retry-missing', label: '重检缺集', visible: Boolean(props.item.can_retry_missing) },
  { name: 'recheck-stale', label: '重检停滞', visible: Boolean(props.item.can_retry_stale) },
  { name: 'retry-upgrade', label: '重试升级', visible: Boolean(props.item.can_retry_upgrade) },
  { name: 'refresh-library', label: '刷新媒体库', visible: Boolean(props.item.can_refresh_library) },
].filter(action => action.visible))

function isRepairBusy(name: string) {
  return props.isBusy(
    `repair-${props.item.ID}-${name}`,
    `subscription-${props.item.ID}-${name}`,
  )
}
</script>

<template>
  <article class="panel grid gap-4 p-4 md:grid-cols-[72px_1fr_auto] md:items-center">
    <img
      :src="posterURL(item.metadata || { image: item.image }, { width: 160 })"
      :alt="`${item.title} 海报`"
      loading="lazy"
      decoding="async"
      fetchpriority="low"
      class="h-24 w-16 rounded-xl object-cover md:h-20 md:w-14"
      @error="handlePosterError($event, item.image)"
    />

    <div class="min-w-0">
      <div class="flex flex-wrap items-center gap-2">
        <h3 class="truncate font-extrabold">{{ displayTitle }}</h3>
        <span class="badge" :class="item.is_active ? 'badge-success' : ''">
          {{ item.is_active ? '运行中' : '已暂停' }}
        </span>
        <span v-if="item.last_error_display || item.has_repair_actions" class="badge badge-danger">
          <CircleAlert :size="13" />
          需处理
        </span>
      </div>
      <p class="muted mt-1 truncate text-sm">
        {{ item.subtitle_group || '未指定字幕组' }} · 已下载 {{ item.downloaded_count }}
        <span v-if="item.expected_episodes"> / {{ item.expected_episodes }}</span>
      </p>
      <p class="mt-2 text-xs" :class="item.last_error_display ? 'text-[var(--danger)]' : 'muted'">
        {{ item.last_error_display || item.last_run_summary || '等待首次检查' }}
      </p>

      <div v-if="item.has_repair_actions && repairActions.length" class="mt-3 flex flex-wrap gap-2">
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

    <div class="flex flex-wrap gap-2 md:justify-end">
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
      <button class="btn btn-quiet h-11 w-11 p-0" aria-label="查看历史" @click="emit('history')">
        <History :size="17" />
      </button>
      <button class="btn btn-quiet h-11 w-11 p-0" aria-label="编辑订阅" @click="emit('edit')">
        <FilePenLine :size="17" />
      </button>
      <button class="btn btn-danger h-11 w-11 p-0" aria-label="删除订阅" @click="emit('remove')">
        <Trash2 :size="16" />
      </button>
    </div>
  </article>
</template>
