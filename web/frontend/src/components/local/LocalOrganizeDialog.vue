<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { AlertTriangle, FolderTree, RefreshCw, ShieldCheck, Sparkles, WandSparkles } from '@lucide/vue'
import { api } from '../../api/client'
import type { AIAnalysisAccepted, LocalOrganizePreview, LocalOrganizeSelection, TaskAccepted } from '../../api/types'
import { useAsyncActions } from '../../composables/useAsyncActions'
import { useUIStore } from '../../stores/ui'
import AIProposalPanel from '../AIProposalPanel.vue'
import AppDialog from '../AppDialog.vue'
import AsyncButton from '../AsyncButton.vue'
import StateBlock from '../StateBlock.vue'

const props = defineProps<{ open: boolean; selection: LocalOrganizeSelection | null }>()
const emit = defineEmits<{ 'update:open': [value: boolean]; applied: [task: TaskAccepted] }>()
const actions = useAsyncActions()
const ui = useUIStore()
const preview = ref<LocalOrganizePreview | null>(null)
const seriesTemplate = ref('')
const episodeTemplate = ref('')
const includedIDs = ref(new Set<number>())
const error = ref('')
const aiProposalID = ref('')
const aiTargetPath = ref('')

const includedCount = computed(() => includedIDs.value.size)
const executableCount = computed(() => preview.value?.items
  .filter(item => includedIDs.value.has(item.anime_id))
  .reduce((total, item) => total + item.changes.filter(change => change.status === 'ready').length, 0) || 0)

watch(() => props.open, open => {
  if (open) void generatePreview(true)
  else reset()
})

function reset() {
  preview.value = null
  seriesTemplate.value = ''
  episodeTemplate.value = ''
  includedIDs.value = new Set()
  error.value = ''
  aiProposalID.value = ''
  aiTargetPath.value = ''
}

async function analyzeFilename(animeID: number, path: string) {
  try {
    const accepted = await actions.runTask(
      `ai-filename-${path}`,
      () => api<AIAnalysisAccepted>('/ai/filename-resolutions', {
        method: 'POST',
        body: JSON.stringify({ local_anime_id: animeID, path }),
      }),
      'AI 文件名识别',
      'ai-analysis',
      `正在识别 ${path.split(/[\\/]/).pop() || path}`,
    )
    aiProposalID.value = accepted.proposal_id
    aiTargetPath.value = path
  } catch (cause) {
    ui.toast(cause instanceof Error ? cause.message : '启动 AI 文件名识别失败', 'error')
  }
}

async function generatePreview(initial = false) {
  if (!props.selection) return
  error.value = ''
  try {
    await actions.run('local-organize-preview', async () => {
      const result = await api<LocalOrganizePreview>('/local-anime/organize/preview', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          selection: props.selection,
          series_template: initial ? '' : seriesTemplate.value,
          episode_template: initial ? '' : episodeTemplate.value,
        }),
      })
      preview.value = result
      seriesTemplate.value = result.series_template
      episodeTemplate.value = result.episode_template
      includedIDs.value = new Set(result.items.map(item => item.anime_id))
    })
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : '生成整理预览失败'
  }
}

function toggleItem(id: number) {
  const next = new Set(includedIDs.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  includedIDs.value = next
}

async function apply() {
  if (!preview.value || !includedIDs.value.size || !executableCount.value) return
  try {
    const task = await actions.runTask('local-organize-apply', () => api<TaskAccepted>('/local-anime/organize', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ plan_id: preview.value!.plan_id, include_anime_ids: [...includedIDs.value] }),
    }), '整理本地番剧', 'organize', '正在复核并整理现有媒体文件')
    ui.toast('整理任务已经启动')
    emit('applied', task)
    emit('update:open', false)
  } catch (cause) {
    ui.toast(cause instanceof Error ? cause.message : '启动整理失败', 'error')
  }
}

function statusLabel(status: string) {
  return ({ ready: '将整理', unchanged: '无需修改', conflict: '存在冲突', skipped: '已跳过' } as Record<string, string>)[status] || status
}
</script>

<template>
  <AppDialog :open="open" title="整理现有番剧" description="先核对文件与文件夹的新位置；只有标记为“将整理”的项目才会执行，目标文件绝不会被覆盖。" wide @update:open="emit('update:open',$event)">
    <StateBlock v-if="actions.isBusy('local-organize-preview') && !preview" state="loading" title="正在检查文件与做种状态" />
    <StateBlock v-else-if="error && !preview" state="error" title="无法生成整理预览" :description="error" :retrying="actions.isBusy('local-organize-preview')" @retry="generatePreview(true)" />
    <template v-else-if="preview">
      <section class="grid gap-3 sm:grid-cols-2">
        <label class="label">系列文件夹模板<input v-model="seriesTemplate" class="field font-mono" /></label>
        <label class="label">剧集文件模板<input v-model="episodeTemplate" class="field font-mono" /></label>
      </section>
      <p class="muted mt-2 text-xs leading-5">变量：<code>{title}</code>、<code>{season}</code>、<code>{episode}</code>、<code>{episode_end}</code>、<code>{episode_type}</code>、<code>{absolute_episode}</code>、<code>{group}</code>、<code>{resolution}</code>、<code>{version}</code>、<code>{language}</code>、<code>{year}</code>、<code>{original}</code>、<code>{ext}</code>。临时修改只作用于本次整理。</p>

      <section class="mt-5 grid grid-cols-2 gap-2 sm:grid-cols-4">
        <div class="panel-muted p-3"><small class="muted">选中番剧</small><strong class="mt-1 block text-xl">{{ includedCount }}</strong></div>
        <div class="panel-muted p-3"><small class="muted">将整理</small><strong class="mt-1 block text-xl text-[var(--success)]">{{ executableCount }}</strong></div>
        <div class="panel-muted p-3"><small class="muted">冲突</small><strong class="mt-1 block text-xl text-[var(--warning)]">{{ preview.conflict_count }}</strong></div>
        <div class="panel-muted p-3"><small class="muted">跳过/不变</small><strong class="mt-1 block text-xl">{{ preview.skipped_count + preview.unchanged_count }}</strong></div>
      </section>

      <p v-if="error" class="mt-4 rounded-xl bg-red-50 p-3 text-sm font-bold text-red-700 dark:bg-red-950/40 dark:text-red-300" role="alert">{{ error }}</p>
      <AIProposalPanel
        v-if="aiProposalID"
        class="mt-4"
        :proposal-id="aiProposalID"
        compact
        @applied="emit('update:open', false)"
        @dismissed="aiProposalID = ''; aiTargetPath = ''"
      />
      <div class="mt-5 max-h-[46vh] space-y-3 overflow-y-auto pr-1">
        <article v-for="item in preview.items" :key="item.anime_id" class="panel-muted p-4" :class="includedIDs.has(item.anime_id)?'':'opacity-60'">
          <div class="flex items-start gap-3">
            <input class="mt-1 h-5 w-5 shrink-0 accent-[var(--brand)]" type="checkbox" :checked="includedIDs.has(item.anime_id)" :aria-label="`整理 ${item.title}`" @change="toggleItem(item.anime_id)" />
            <div class="min-w-0 flex-1">
              <div class="flex flex-wrap items-center gap-2"><strong>{{ item.title }}</strong><span v-if="!item.metadata_matched" class="badge border-amber-300 text-amber-700 dark:text-amber-300"><AlertTriangle :size="13" />未匹配元数据</span></div>
              <p class="muted mt-1 break-all text-xs">{{ item.source_path }}</p>
              <p class="mt-1 flex items-start gap-1 break-all text-xs text-[var(--success)]"><FolderTree class="mt-0.5 shrink-0" :size="13" />{{ item.target_path }}</p>
              <p v-for="warning in item.warnings" :key="warning" class="mt-2 text-xs font-bold text-amber-700 dark:text-amber-300">{{ warning }}</p>
            </div>
          </div>
          <div class="mt-3 space-y-2">
            <div v-for="change in item.changes" :key="`${change.original}-${change.target}`" class="rounded-xl border border-[var(--line)] bg-[var(--surface-solid)] p-3 text-xs">
              <div class="flex flex-wrap items-center justify-between gap-2"><span class="badge" :class="change.status==='ready'?'badge-success':change.status==='conflict'?'border-amber-300 text-amber-700 dark:text-amber-300':''">{{ statusLabel(change.status) }}</span><span v-if="change.managed_by_qb" class="flex items-center gap-1 font-bold text-[var(--sky)]"><ShieldCheck :size="13" />qB 安全移动</span></div>
              <p class="muted mt-2 break-all line-through">{{ change.original }}</p>
              <p v-if="change.target!==change.original" class="mt-1 break-all font-bold">{{ change.target }}</p>
              <div v-if="change.parse_source" class="mt-2 flex flex-wrap gap-2">
                <span class="badge">{{ change.parse_source }}</span>
                <span class="badge">{{ Math.round((change.parse_confidence||0)*100) }}% 置信度</span>
                <span v-if="change.episode_type&&change.episode_type!=='episode'" class="badge">{{ change.episode_type }}</span>
                <span v-if="change.episode_end" class="badge">结束集 {{ change.episode_end }}</span>
                <span v-if="change.version" class="badge">版本 {{ change.version }}</span>
              </div>
              <p v-if="change.reason" class="mt-1 font-bold text-amber-700 dark:text-amber-300">{{ change.reason }}</p>
              <AsyncButton
                v-if="change.status === 'skipped' && change.reason?.includes('无法识别剧集编号')"
                class="btn btn-secondary mt-3"
                :loading="actions.isBusy(`ai-filename-${change.original}`)"
                loading-label="AI 识别中…"
                @click="analyzeFilename(item.anime_id, change.original)"
              >
                <Sparkles :size="15"/>{{ aiTargetPath === change.original ? '重新分析' : 'AI 协助识别' }}
              </AsyncButton>
            </div>
          </div>
        </article>
      </div>

      <div class="mt-6 flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
        <AsyncButton class="btn btn-secondary" :loading="actions.isBusy('local-organize-preview')" loading-label="重新生成中…" @click="generatePreview(false)"><RefreshCw :size="16" />按当前模板重新预览</AsyncButton>
        <AsyncButton class="btn btn-primary" :disabled="!includedCount||!executableCount||Boolean(error)" :loading="actions.isBusy('local-organize-apply')" loading-label="启动中…" @click="apply"><WandSparkles :size="16" />整理 {{ executableCount }} 项</AsyncButton>
      </div>
    </template>
  </AppDialog>
</template>
