<script setup lang="ts">
import AppDialog from '../AppDialog.vue'
import AsyncButton from '../AsyncButton.vue'

defineProps<{
  open: boolean
  text: string
  preview: Array<Record<string, unknown>>
  itemCount: number
  previewLoading: boolean
  importLoading: boolean
}>()

const emit = defineEmits<{
  'update:open': [value: boolean]
  'update:text': [value: string]
  preview: []
  import: []
}>()

function field(row: Record<string, unknown>, primary: string, fallback: string) {
  return String(row[primary] ?? row[fallback] ?? '')
}

function listLength(row: Record<string, unknown>, primary: string, fallback: string) {
  const value = row[primary] ?? row[fallback]
  return Array.isArray(value) ? value.length : 0
}

function updateText(event: Event) {
  emit('update:text', (event.target as HTMLTextAreaElement).value)
}
</script>

<template>
  <AppDialog
    :open="open"
    title="批量导入订阅"
    description="每行使用“番剧名 | RSS 地址 | 可选过滤规则”。系统会先预览，不会直接写入。"
    wide
    @update:open="emit('update:open', $event)"
  >
    <textarea
      :value="text"
      class="field min-h-48 font-mono text-sm"
      placeholder="示例番剧 | https://example.com/rss | 1080p"
      @input="updateText"
    />

    <div v-if="preview.length" class="mt-5 grid gap-2">
      <div v-for="(item, index) in preview" :key="index" class="panel-muted p-3 text-sm">
        <strong>{{ field(item, 'Title', 'title') }}</strong>
        <p class="muted mt-1">
          {{ item.Error || item.error || `${listLength(item, 'Episodes', 'episodes')} 项预览` }}
        </p>
      </div>
    </div>

    <div class="mt-6 flex justify-end gap-2">
      <AsyncButton
        class="btn btn-secondary"
        :disabled="!itemCount"
        :loading="previewLoading"
        loading-label="生成中…"
        @click="emit('preview')"
      >
        生成预览
      </AsyncButton>
      <AsyncButton
        class="btn btn-primary"
        :disabled="!preview.length"
        :loading="importLoading"
        loading-label="导入中…"
        @click="emit('import')"
      >
        确认导入 {{ itemCount }} 条
      </AsyncButton>
    </div>
  </AppDialog>
</template>
