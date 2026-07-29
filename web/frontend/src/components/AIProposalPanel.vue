<script setup lang="ts">
import { computed } from 'vue'
import { AlertTriangle, CheckCircle2, CircleAlert, Clock3, LoaderCircle, ShieldCheck, Sparkles, X } from '@lucide/vue'
import { useQuery, useQueryClient } from '@tanstack/vue-query'
import { api } from '../api/client'
import type { AIProposal, MetadataMatchCandidate } from '../api/types'
import AsyncButton from './AsyncButton.vue'
import { useAsyncActions } from '../composables/useAsyncActions'
import { useUIStore } from '../stores/ui'
import { useTaskStore } from '../stores/tasks'

const props = defineProps<{ proposalId: string; compact?: boolean }>()
const emit = defineEmits<{ applied: [proposal: AIProposal]; dismissed: [] }>()
const actions = useAsyncActions()
const ui = useUIStore()
const tasks = useTaskStore()
const queryClient = useQueryClient()

const query = useQuery({
  queryKey: computed(() => ['ai-proposal', props.proposalId]),
  queryFn: () => api<AIProposal>(`/ai/proposals/${props.proposalId}`),
  enabled: computed(() => Boolean(props.proposalId)),
  refetchInterval: queryState => queryState.state.data?.status === 'analyzing' ? 1500 : false,
})

const proposal = computed(() => query.data.value)
const metadataCandidate = computed<MetadataMatchCandidate | null>(() => {
  const payload = proposal.value?.payload
  if (!payload || proposal.value?.type !== 'metadata_match') return null
  const candidate = payload.candidate
  return candidate && typeof candidate === 'object' ? candidate as MetadataMatchCandidate : null
})
const confidenceLabel = computed(() => {
  const value = proposal.value?.confidence || 0
  if (value >= .8) return '高置信度'
  if (value >= .6) return '中等置信度'
  return '低置信度'
})

async function apply() {
  if (!proposal.value?.actionable || proposal.value.status !== 'ready') return
  try {
    await actions.run(`ai-confirm-${props.proposalId}`, async () => {
      const confirmation = await api<{ confirmation_token: string }>(`/ai/proposals/${props.proposalId}/confirm`, { method: 'POST' })
      const result = await api<unknown>(`/ai/proposals/${props.proposalId}/apply`, {
        method: 'POST',
        body: JSON.stringify({ confirmation_token: confirmation.confirmation_token }),
      })
      if (result && typeof result === 'object' && 'task_id' in result && typeof result.task_id === 'string') {
        tasks.track({ task_id: result.task_id, status: 'running' }, 'AI 提案执行', 'ai-proposal', '正在执行已确认的 AI 提案')
      }
      void queryClient.invalidateQueries({ queryKey: ['ai-proposal', props.proposalId] })
      ui.toast('AI 提案已确认执行')
      emit('applied', { ...proposal.value!, status: 'applied', payload: (result || proposal.value!.payload) as Record<string, unknown> })
    })
  } catch (error) {
    ui.toast(error instanceof Error ? error.message : '执行 AI 提案失败', 'error')
    void query.refetch()
  }
}

async function dismiss() {
  try {
    await actions.run(`ai-dismiss-${props.proposalId}`, () => api(`/ai/proposals/${props.proposalId}/dismiss`, { method: 'POST' }))
    ui.toast('AI 提案已忽略')
    emit('dismissed')
  } catch (error) {
    ui.toast(error instanceof Error ? error.message : '忽略 AI 提案失败', 'error')
  }
}
</script>

<template>
  <section v-if="proposal" class="rounded-2xl border border-[var(--brand)]/30 bg-[var(--brand-soft)] p-4" :class="compact ? 'text-sm' : 'p-5'">
    <div class="flex items-start gap-3">
      <span class="grid h-9 w-9 shrink-0 place-items-center rounded-xl bg-[var(--surface-solid)] text-[var(--brand)]">
        <LoaderCircle v-if="proposal.status === 'analyzing'" class="animate-spin" :size="18"/>
        <CheckCircle2 v-else-if="proposal.status === 'applied'" :size="18"/>
        <CircleAlert v-else-if="['failed','expired','stale'].includes(proposal.status)" :size="18"/>
        <Sparkles v-else :size="18"/>
      </span>
      <div class="min-w-0 flex-1">
        <div class="flex flex-wrap items-center gap-2">
          <strong>AI 运维提案</strong>
          <span v-if="proposal.status === 'analyzing'" class="badge">分析中</span>
          <span v-else-if="proposal.status === 'ready'" class="badge badge-success">待确认</span>
          <span v-else-if="proposal.status === 'applied'" class="badge badge-success">已执行</span>
          <span v-else class="badge badge-danger">{{ proposal.status }}</span>
          <span v-if="proposal.status === 'ready'" class="badge" :class="proposal.confidence < .6 ? 'border-amber-300 text-amber-700' : ''">{{ confidenceLabel }} · {{ Math.round(proposal.confidence * 100) }}%</span>
        </div>
        <p v-if="proposal.status === 'analyzing'" class="muted mt-2">正在通过只读工具收集上下文，页面不会被修改。</p>
        <p v-else class="mt-2 leading-6">{{ proposal.summary || proposal.error || 'AI 没有提供摘要' }}</p>
        <div v-if="metadataCandidate" class="mt-3 rounded-xl border border-[var(--line)] bg-[var(--surface-solid)] p-3">
          <p class="text-xs font-black">三源候选快照（确认前可核对）</p>
          <div class="mt-2 grid gap-2 text-xs sm:grid-cols-3">
            <span :class="metadataCandidate.bangumi ? '' : 'muted'">Bangumi：{{ metadataCandidate.bangumi ? `${metadataCandidate.bangumi.name_cn || metadataCandidate.bangumi.name} (#${metadataCandidate.bangumi.id})` : '未找到' }}</span>
            <span :class="metadataCandidate.tmdb ? '' : 'muted'">TMDB：{{ metadataCandidate.tmdb ? `${metadataCandidate.tmdb.name_cn || metadataCandidate.tmdb.name} (#${metadataCandidate.tmdb.id})` : '未找到/未配置' }}</span>
            <span :class="metadataCandidate.anilist ? '' : 'muted'">AniList：{{ metadataCandidate.anilist ? `${metadataCandidate.anilist.name_cn || metadataCandidate.anilist.name} (#${metadataCandidate.anilist.id})` : '未找到/未配置' }}</span>
          </div>
          <p v-if="metadataCandidate.evidence?.length" class="muted mt-2 text-xs">{{ metadataCandidate.evidence.join(' · ') }}</p>
        </div>
        <div v-if="proposal.evidence?.length" class="mt-3 space-y-1">
          <p v-for="item in proposal.evidence" :key="item" class="flex gap-2 text-xs leading-5"><ShieldCheck class="mt-0.5 shrink-0 text-[var(--success)]" :size="14"/>{{ item }}</p>
        </div>
        <div v-if="proposal.warnings?.length" class="mt-3 space-y-1">
          <p v-for="item in proposal.warnings" :key="item" class="flex gap-2 text-xs leading-5 text-amber-700 dark:text-amber-300"><AlertTriangle class="mt-0.5 shrink-0" :size="14"/>{{ item }}</p>
        </div>
        <p v-if="proposal.status === 'ready' && proposal.actionable" class="mt-3 flex gap-2 text-xs leading-5 text-[var(--success)]"><ShieldCheck class="mt-0.5 shrink-0" :size="14"/>执行时仍会复用 AnimateTool 原有校验、预览和审计流程。</p>
        <div v-if="proposal.status === 'ready' && proposal.actionable" class="mt-4 flex flex-wrap gap-2">
          <AsyncButton class="btn btn-primary" :loading="actions.isBusy(`ai-confirm-${proposal.id}`)" loading-label="确认中…" @click="apply"><CheckCircle2 :size="16"/>确认并执行</AsyncButton>
          <AsyncButton class="btn btn-secondary" :loading="actions.isBusy(`ai-dismiss-${proposal.id}`)" loading-label="忽略中…" @click="dismiss"><X :size="16"/>忽略</AsyncButton>
        </div>
        <p v-else-if="proposal.status === 'ready'" class="muted mt-3 text-xs">这次分析没有安全的自动执行动作，请按建议人工处理。</p>
        <p v-if="proposal.expires_at && proposal.status === 'ready'" class="muted mt-3 flex items-center gap-1 text-xs"><Clock3 :size="13"/>提案会在 {{ new Date(proposal.expires_at).toLocaleString() }} 失效</p>
      </div>
    </div>
  </section>
  <section v-else-if="query.isError.value" class="rounded-2xl border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-900 dark:bg-red-950/30 dark:text-red-300">
    AI 提案加载失败，请刷新页面后重试。
  </section>
</template>
