<script setup lang="ts">
import { AlertTriangle, Inbox, LoaderCircle, RotateCcw } from '@lucide/vue'
import { computed } from 'vue'
import AsyncButton from './AsyncButton.vue'
import MascotArt from './MascotArt.vue'
import type { MascotScene } from '../utils/mascot'

const props = defineProps<{
  state: 'loading' | 'empty' | 'error'
  title?: string
  description?: string
  retrying?: boolean
  scene?: MascotScene
}>()

defineEmits<{ retry: [] }>()

const resolvedScene = computed<MascotScene | undefined>(() => {
  if (props.scene) return props.scene
  const title = props.title || ''
  if (props.state === 'loading') return title.includes('扫描') || title.includes('整理') ? 'organizing' : undefined
  if (props.state === 'error') return title.includes('健康') || title.includes('媒体') || title.includes('连接') || title.includes('图鉴') ? 'diagnosing' : 'error'
  if (title.includes('搜索') || title.includes('匹配') || title.includes('找到') || title.includes('符合条件')) return 'empty-search'
  if (title.includes('播放') || title.includes('剧集')) return 'empty-playback'
  if (title.includes('本地') || title.includes('备份') || title.includes('整理')) return 'organizing'
  return 'empty-library'
})
</script>

<template>
  <div class="panel state-block grid min-h-52 place-items-center p-8 text-center">
    <div>
      <MascotArt v-if="resolvedScene" :scene="resolvedScene" class="mascot-state-art" />
      <LoaderCircle v-if="state==='loading'" class="state-default-icon mx-auto mb-4 animate-spin text-[var(--brand)]" :size="30"/>
      <AlertTriangle v-else-if="state==='error'" class="state-default-icon mx-auto mb-4 text-[var(--danger)]" :size="30"/>
      <Inbox v-else class="state-default-icon muted mx-auto mb-4" :size="32"/>
      <h3 class="font-extrabold">{{ title || (state==='loading'?'正在加载':'这里还没有内容') }}</h3>
      <p v-if="description" class="muted mt-2 max-w-lg text-sm">{{ description }}</p>
      <AsyncButton v-if="state==='error'" class="btn btn-secondary mt-5" :loading="retrying" loading-label="重试中…" @click="$emit('retry')"><RotateCcw :size="16"/>重试</AsyncButton>
    </div>
  </div>
</template>
