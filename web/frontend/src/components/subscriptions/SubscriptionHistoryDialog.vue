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
              <strong>{{ field(run, 'Status', 'status') || '已检查' }}</strong>
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
            <p class="muted mt-1">
              第 {{ field(log, 'Episode', 'episode') || '?' }} 集 · {{ field(log, 'Status', 'status') }}
            </p>
          </article>
        </div>
      </section>
      </div>
    </div>
  </AppDialog>
</template>
