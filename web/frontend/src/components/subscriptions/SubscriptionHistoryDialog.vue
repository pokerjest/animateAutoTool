<script setup lang="ts">
import { Clock3 } from '@lucide/vue'
import AppDialog from '../AppDialog.vue'
import StateBlock from '../StateBlock.vue'

defineProps<{
  open: boolean
  title: string
  loading: boolean
  error: boolean
  retrying: boolean
  runs: Array<Record<string, unknown>>
  logs: Array<Record<string, unknown>>
}>()

const emit = defineEmits<{
  'update:open': [value: boolean]
  retry: []
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
    <div v-else class="grid gap-5 lg:grid-cols-2">
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
  </AppDialog>
</template>
