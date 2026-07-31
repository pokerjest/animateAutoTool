<script setup lang="ts">
import { ref } from 'vue'
import { Activity, AlertTriangle, Bug, CheckCircle2, Cpu, Database, Download, HardDrive, RefreshCw, Sparkles } from '@lucide/vue'
import { useQuery } from '@tanstack/vue-query'
import { api } from '../api/client'
import type { AIAnalysisAccepted } from '../api/types'
import AIProposalPanel from '../components/AIProposalPanel.vue'
import AsyncButton from '../components/AsyncButton.vue'
import PageHeader from '../components/PageHeader.vue'
import StateBlock from '../components/StateBlock.vue'
import { useAsyncActions } from '../composables/useAsyncActions'
import { useUIStore } from '../stores/ui'

interface Health {
  generated_at: string
  configs: Record<string, boolean>
  subscription_total: number
  subscription_active: number
  download_completed: number
  download_downloading: number
  download_stale: number
  download_failed: number
  local_anime_count: number
  local_episode_count: number
  open_library_issues: number
  stale_subscriptions_72h: number
  health_tone: string
  summary: string
  recommendations: string[]
}
interface Runtime {
  uptime_seconds: number
  go: { goroutines: number; gomaxprocs: number; num_cpu: number }
  memory: { heap_alloc_bytes: number; sys_bytes: number }
  gc: { num_gc: number }
}

const actions = useAsyncActions()
const ui = useUIStore()
const aiProposalID = ref('')
const query = useQuery({
  queryKey: ['health'],
  queryFn: async () => ({ health: await api<Health>('/health'), runtime: await api<Runtime>('/runtime') }),
  refetchInterval: 30_000,
})
const size = (n: number) => `${(n / 1024 / 1024).toFixed(1)} MB`
const duration = (s: number) => s > 86400 ? `${Math.floor(s / 86400)} 天` : s > 3600 ? `${Math.floor(s / 3600)} 小时` : `${Math.floor(s / 60)} 分钟`

async function analyzeHealth() {
  try {
    const accepted = await actions.runTask(
      'ai-health-analysis',
      () => api<AIAnalysisAccepted>('/ai/health/analyze', { method: 'POST' }),
      'AI 健康分析',
      'ai-analysis',
      '正在读取健康报告和脱敏日志',
    )
    aiProposalID.value = accepted.proposal_id
  } catch (error) {
    ui.toast(error instanceof Error ? error.message : '启动 AI 健康分析失败', 'error')
  }
}
</script>

<template>
  <div class="page-grid">
    <PageHeader eyebrow="SYSTEM PULSE" title="系统健康" description="把下载、扫描、媒体库和运行时状态放在一起看，优先处理真正影响追番的异常。">
      <AsyncButton class="btn btn-primary" :loading="actions.isBusy('ai-health-analysis')" loading-label="分析中…" @click="analyzeHealth"><Sparkles :size="17"/>AI 分析当前问题</AsyncButton>
      <a class="btn btn-secondary" href="/api/v1/diagnostics/health/export" download title="导出仅含异常事件和系统快照的开发者诊断包"><Bug :size="17"/>导出健康诊断</a>
      <a class="btn btn-secondary" href="/api/v1/diagnostics/logs/export" download title="打包下载最新三个小时的完整服务日志"><Download :size="17"/>导出普通日志</a>
      <AsyncButton class="btn btn-secondary" :loading="query.isFetching.value" loading-label="刷新中…" @click="query.refetch()"><RefreshCw :size="17"/>刷新</AsyncButton>
    </PageHeader>

    <AIProposalPanel v-if="aiProposalID" :proposal-id="aiProposalID" @applied="query.refetch()" @dismissed="aiProposalID = ''" />
    <StateBlock v-if="query.isLoading.value" state="loading" scene="diagnosing"/>
    <StateBlock v-else-if="query.isError.value" state="error" scene="diagnosing" title="健康报告加载失败" :retrying="query.isFetching.value" @retry="query.refetch()"/>
    <template v-else-if="query.data.value">
      <section class="rounded-[1.5rem] border p-6" :class="query.data.value.health.health_tone === 'rose' ? 'border-red-200 bg-red-50 dark:border-red-900 dark:bg-red-950/40' : query.data.value.health.health_tone === 'amber' ? 'border-amber-200 bg-amber-50 dark:border-amber-900 dark:bg-amber-950/40' : 'border-emerald-200 bg-emerald-50 dark:border-emerald-900 dark:bg-emerald-950/40'">
        <div class="flex items-start gap-4">
          <CheckCircle2 v-if="query.data.value.health.health_tone === 'emerald'" class="text-[var(--success)]" :size="28"/>
          <AlertTriangle v-else class="text-[var(--warning)]" :size="28"/>
          <div><p class="eyebrow">综合判断</p><h3 class="mt-1 text-xl font-black">{{ query.data.value.health.summary }}</h3><p v-if="query.data.value.health.health_tone !== 'emerald'" class="muted mt-2 text-sm">若界面修复无效或异常持续出现，请导出健康诊断交给开发者；其中不包含配置密钥和普通运行流水。</p></div>
        </div>
      </section>
      <section class="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <article v-for="item in [{ label: '活跃订阅', value: `${query.data.value.health.subscription_active}/${query.data.value.health.subscription_total}`, icon: Activity }, { label: '完成下载', value: query.data.value.health.download_completed, icon: HardDrive }, { label: '本地剧集', value: query.data.value.health.local_episode_count, icon: Database }, { label: '待处理问题', value: query.data.value.health.open_library_issues, icon: AlertTriangle }]" :key="item.label" class="panel p-5"><component :is="item.icon" class="text-[var(--brand)]" :size="20"/><p class="muted mt-5 text-xs font-bold">{{ item.label }}</p><strong class="mt-1 block text-3xl font-black">{{ item.value }}</strong></article>
      </section>
      <section class="grid gap-5 xl:grid-cols-[1fr_1fr]">
        <article class="panel p-5 sm:p-6"><p class="eyebrow">RUNTIME</p><h3 class="mt-1 text-xl font-black">运行时状态</h3><div class="mt-5 grid gap-3 sm:grid-cols-2"><div v-for="item in [{ label: '运行时间', value: duration(query.data.value.runtime.uptime_seconds) }, { label: 'Goroutines', value: query.data.value.runtime.go.goroutines }, { label: '堆内存', value: size(query.data.value.runtime.memory.heap_alloc_bytes) }, { label: 'GC 次数', value: query.data.value.runtime.gc.num_gc }]" :key="item.label" class="panel-muted p-4"><Cpu :size="16" class="text-[var(--sky)]"/><p class="muted mt-3 text-xs">{{ item.label }}</p><strong class="mt-1 block text-lg">{{ item.value }}</strong></div></div></article>
        <article class="panel p-5 sm:p-6"><p class="eyebrow">NEXT ACTIONS</p><h3 class="mt-1 text-xl font-black">建议下一步</h3><ul v-if="query.data.value.health.recommendations.length" class="mt-5 space-y-3"><li v-for="item in query.data.value.health.recommendations" :key="item" class="panel-muted flex gap-3 p-4 text-sm leading-6"><CheckCircle2 class="mt-0.5 shrink-0 text-[var(--brand)]" :size="17"/>{{ item }}</li></ul><p v-else class="muted mt-8 text-center">暂无需要处理的建议</p></article>
      </section>
    </template>
  </div>
</template>
