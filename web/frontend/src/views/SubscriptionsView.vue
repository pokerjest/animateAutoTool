<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { useQuery, useQueryClient } from '@tanstack/vue-query'
import { useRouter } from 'vue-router'
import { Plus, Sparkles, Upload } from '@lucide/vue'
import { api } from '../api/client'
import type {
  MikanSubscriptionSelection,
  ResolutionFilter,
  Subscription,
  SubtitleLanguage,
  TaskAccepted,
} from '../api/types'
import AppDialog from '../components/AppDialog.vue'
import AsyncButton from '../components/AsyncButton.vue'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import MikanDiscoveryDialog from '../components/MikanDiscoveryDialog.vue'
import PageHeader from '../components/PageHeader.vue'
import StateBlock from '../components/StateBlock.vue'
import SubscriptionBatchDialog from '../components/subscriptions/SubscriptionBatchDialog.vue'
import SubscriptionCard from '../components/subscriptions/SubscriptionCard.vue'
import SubscriptionHistoryDialog from '../components/subscriptions/SubscriptionHistoryDialog.vue'
import SubscriptionOverview from '../components/subscriptions/SubscriptionOverview.vue'
import { useAsyncActions } from '../composables/useAsyncActions'
import { useUIStore } from '../stores/ui'
import { regexRuleError, switchToMikanAggregate } from '../utils/mikanSubscription'

interface SubscriptionPayload {
  items: Subscription[]
  trend: {
    checked_count: number
    success_count: number
    warning_count: number
    error_count: number
    active_issue_count: number
  }
  scheduler: {
    is_running?: boolean
    last_summary?: string
  }
}

interface ValidationResult {
  primary_count: number
  backup_count?: number
  matching_count: number
  warnings?: string[]
  preview_titles?: string[]
  using_backup_hint?: string
}

interface HistoryData {
  Subscription: Subscription
  Runs: Array<Record<string, unknown>>
  Logs: Array<Record<string, unknown>>
}

type ViewMode = 'form' | 'batch' | null
type SubscriptionFilter = 'all' | 'active' | 'paused' | 'issues'

function createEmptyForm() {
  return {
    title: '',
    rss_url: '',
    backup_rss_url: '',
    mikan_id: '',
    image: '',
    subtitle_group: '',
    season: '',
    filter_rule: '',
    exclude_rule: '',
    resolution_filter: '' as ResolutionFilter,
    subtitle_language: '' as SubtitleLanguage,
    expected_episodes: 0,
    allow_multi_subgroup: false,
    auto_disable_on_done: false,
    stale_after_hours: 168,
  }
}

const ui = useUIStore()
const router = useRouter()
const queryClient = useQueryClient()
const actions = useAsyncActions()
const search = ref('')
const filter = ref<SubscriptionFilter>('all')
const mode = ref<ViewMode>(null)
const discoveryOpen = ref(false)
const resumeFormAfterDiscovery = ref(false)
const deleteTarget = ref<Subscription | null>(null)
const detailTarget = ref<Subscription | null>(null)
const editing = ref<Subscription | null>(null)
const form = reactive(createEmptyForm())
const validation = ref<ValidationResult | null>(null)
const batchText = ref('')
const batchPreview = ref<Array<Record<string, unknown>>>([])

const query = useQuery({
  queryKey: ['subscriptions'],
  queryFn: () => api<SubscriptionPayload>('/subscriptions'),
  refetchInterval: 30_000,
})

const history = useQuery({
  queryKey: computed(() => ['subscription-history', detailTarget.value?.ID]),
  queryFn: () => api<HistoryData>(`/subscriptions/${detailTarget.value!.ID}/history`),
  enabled: computed(() => Boolean(detailTarget.value)),
})

const items = computed(() => {
  const list = query.data.value?.items || []
  const searchText = search.value.toLowerCase()

  return list.filter((item) => {
    const text = `${item.title} ${item.subtitle_group || ''}`.toLowerCase()
    const matchesSearch = text.includes(searchText)
    const matchesFilter = filter.value === 'all'
      || (filter.value === 'active' && item.is_active)
      || (filter.value === 'paused' && !item.is_active)
      || (filter.value === 'issues' && (Boolean(item.last_error_display) || item.has_repair_actions))
    return matchesSearch && matchesFilter
  })
})

const batchItems = computed(() => batchText.value
  .split('\n')
  .map(line => line.trim())
  .filter(Boolean)
  .map((line) => {
    const [title, rss_url, filter_rule = ''] = line.split('|').map(value => value.trim())
    return { title, rss_url, filter_rule }
  })
  .filter(item => item.title && item.rss_url))

const formTitle = computed(() => editing.value ? '编辑订阅' : '添加订阅')
const historyTitle = computed(() => detailTarget.value?.title || '订阅历史')
const deleteDescription = computed(() => `会同时删除 ${deleteTarget.value?.title || ''} 的下载记录，且无法撤销。`)
const includeRuleError = computed(() => regexRuleError(form.filter_rule))
const excludeRuleError = computed(() => regexRuleError(form.exclude_rule))
const hasRegexError = computed(() => Boolean(includeRuleError.value || excludeRuleError.value))

function resetForm() {
  Object.assign(form, createEmptyForm())
  editing.value = null
  validation.value = null
}

function openCreate() {
  resetForm()
  mode.value = 'form'
}

function openBatch() {
  mode.value = 'batch'
}

function openHistory(item: Subscription) {
  detailTarget.value = item
}

function playSubscription(item: Subscription) {
  if (!item.playable || !item.local_anime_id) return
  detailTarget.value = null
  void router.push({ path: '/player', query: { anime: String(item.local_anime_id) } })
}

function confirmDelete(item: Subscription) {
  deleteTarget.value = item
}

function openEdit(item: Subscription) {
  editing.value = item
  Object.assign(form, {
    title: item.title,
    rss_url: item.rss_url,
    backup_rss_url: item.backup_rss_url || '',
    mikan_id: item.mikan_id || '',
    image: item.image || '',
    subtitle_group: item.subtitle_group || '',
    season: item.season || '',
    filter_rule: item.filter_rule || '',
    exclude_rule: item.exclude_rule || '',
    resolution_filter: item.resolution_filter || '',
    subtitle_language: item.subtitle_language || '',
    expected_episodes: item.expected_episodes || 0,
    allow_multi_subgroup: Boolean(item.allow_multi_subgroup),
    auto_disable_on_done: Boolean(item.auto_disable_on_done),
    stale_after_hours: item.stale_after_hours || 168,
  })
  validation.value = null
  mode.value = 'form'
}

function openDiscovery(fromForm = false) {
  if (!fromForm) resetForm()
  resumeFormAfterDiscovery.value = fromForm
  mode.value = null
  discoveryOpen.value = true
}

function setDiscoveryOpen(value: boolean) {
  discoveryOpen.value = value
  if (!value && resumeFormAfterDiscovery.value) {
    resumeFormAfterDiscovery.value = false
    mode.value = 'form'
  }
}

function applyMikanSelection(selection: MikanSubscriptionSelection) {
  Object.assign(form, {
    mikan_id: selection.mikan_id,
    title: selection.title,
    image: selection.image,
    season: selection.season,
    subtitle_group: selection.subtitle_group,
    rss_url: selection.rss_url,
    backup_rss_url: selection.backup_rss_url,
    filter_rule: selection.filter_rule,
    exclude_rule: selection.exclude_rule,
    resolution_filter: selection.resolution_filter,
    subtitle_language: selection.subtitle_language,
    allow_multi_subgroup: selection.allow_multi_subgroup,
  })
  validation.value = null
  resumeFormAfterDiscovery.value = false
  discoveryOpen.value = false
  mode.value = 'form'
}

function handleMultiSubgroupChange() {
  switchToMikanAggregate(form)
  validation.value = null
}

function setFormOpen(value: boolean) {
  if (value) return
  mode.value = null
  resetForm()
}

function setBatchOpen(value: boolean) {
  if (!value) mode.value = null
}

function setHistoryOpen(value: boolean) {
  if (!value) detailTarget.value = null
}

function setDeleteOpen(value: boolean) {
  if (!value) deleteTarget.value = null
}

async function save() {
  if (hasRegexError.value) {
    ui.toast('请先修正包含或排除正则', 'error')
    return
  }
  try {
    await actions.run('save', async () => {
      const path = editing.value ? `/subscriptions/${editing.value.ID}` : '/subscriptions'
      await api(path, {
        method: editing.value ? 'PUT' : 'POST',
        body: JSON.stringify(form),
        headers: { 'Content-Type': 'application/json' },
      })
      ui.toast(editing.value ? '订阅已保存' : '订阅已添加')
      mode.value = null
      resetForm()
      queryClient.invalidateQueries({ queryKey: ['subscriptions'] })
    })
  } catch (error) {
    ui.toast(error instanceof Error ? error.message : '保存失败', 'error')
  }
}

async function validate() {
  if (!form.rss_url) return
  if (hasRegexError.value) {
    ui.toast('请先修正包含或排除正则', 'error')
    return
  }
  try {
    await actions.run('validate', async () => {
      const params = new URLSearchParams({
        title: form.title,
        rss: form.rss_url,
        backup_rss: form.backup_rss_url,
        filter: form.filter_rule,
        exclude: form.exclude_rule,
        resolution_filter: form.resolution_filter,
        subtitle_language: form.subtitle_language,
        subtitle_group: form.subtitle_group,
        allow_multi_subgroup: String(form.allow_multi_subgroup),
      })
      validation.value = await api<ValidationResult>(`/subscriptions/validate-rss?${params}`)
    })
  } catch (error) {
    ui.toast(error instanceof Error ? error.message : 'RSS 校验失败', 'error')
  }
}

async function operate(item: Subscription, name: 'run' | 'toggle') {
  try {
    if (name === 'run') {
      await actions.runTask(
        `run-${item.ID}`,
        () => api<TaskAccepted>(`/subscriptions/${item.ID}/run`, { method: 'POST' }),
        '订阅检查',
        'subscription',
        `正在检查 ${item.title}`,
      )
    } else {
      await actions.run(`toggle-${item.ID}`, () => (
        api(`/subscriptions/${item.ID}/toggle`, { method: 'POST' })
      ))
    }
    ui.toast(name === 'run' ? '订阅检查已经启动' : '订阅状态已更新')
    queryClient.invalidateQueries({ queryKey: ['subscriptions'] })
  } catch (error) {
    ui.toast(error instanceof Error ? error.message : '操作失败', 'error')
  }
}

async function repair(item: Subscription, name: string) {
  try {
    const syncingJellyfin = name === 'refresh-library'
    await actions.runTask(
      `repair-${item.ID}-${name}`,
      () => api<TaskAccepted>(`/subscriptions/${item.ID}/repair/${name}`, { method: 'POST' }),
      syncingJellyfin ? '同步 Jellyfin' : '订阅修复',
      'subscription-repair',
      syncingJellyfin ? `正在请求 Jellyfin 扫描并识别 ${item.title}` : `正在修复 ${item.title}`,
    )
    ui.toast(syncingJellyfin ? '已请求 Jellyfin 扫描，识别完成后会自动更新播放状态' : '修复任务已经启动')
  } catch (error) {
    ui.toast(error instanceof Error ? error.message : '修复失败', 'error')
  }
}

async function remove() {
  if (!deleteTarget.value) return
  const id = deleteTarget.value.ID
  try {
    await actions.run(`remove-${id}`, async () => {
      await api(`/subscriptions/${id}`, { method: 'DELETE' })
      ui.toast('订阅已删除')
      deleteTarget.value = null
      queryClient.invalidateQueries({ queryKey: ['subscriptions'] })
    })
  } catch (error) {
    ui.toast(error instanceof Error ? error.message : '删除失败', 'error')
  }
}

async function previewBatch() {
  if (!batchItems.value.length) return
  try {
    await actions.run('batch-preview', async () => {
      batchPreview.value = await api<Array<Record<string, unknown>>>('/subscriptions/batch-preview', {
        method: 'POST',
        body: JSON.stringify(batchItems.value),
        headers: { 'Content-Type': 'application/json' },
      })
    })
  } catch (error) {
    ui.toast(error instanceof Error ? error.message : '预览失败', 'error')
  }
}

async function importBatch() {
  try {
    await actions.run('batch-import', async () => {
      const result = await api<{ added: number; failed: number }>('/subscriptions/batch', {
        method: 'POST',
        body: JSON.stringify(batchItems.value),
        headers: { 'Content-Type': 'application/json' },
      })
      ui.toast(
        `已添加 ${result.added} 条，失败 ${result.failed} 条`,
        result.failed ? 'info' : 'success',
      )
      mode.value = null
      batchText.value = ''
      batchPreview.value = []
      queryClient.invalidateQueries({ queryKey: ['subscriptions'] })
    })
  } catch (error) {
    ui.toast(error instanceof Error ? error.message : '导入失败', 'error')
  }
}
</script>

<template>
  <div class="page-grid">
    <PageHeader
      eyebrow="FOLLOW"
      title="订阅管理"
      description="从异常和新更新开始处理，让每一条 RSS 都保持可解释、可恢复。"
    >
      <button class="btn btn-secondary" @click="openDiscovery()">
        <Sparkles :size="17" />
        发现番剧
      </button>
      <button class="btn btn-secondary" @click="openBatch">
        <Upload :size="17" />
        批量导入
      </button>
      <button class="btn btn-primary" @click="openCreate">
        <Plus :size="17" />
        添加订阅
      </button>
    </PageHeader>

    <SubscriptionOverview
      v-model:search="search"
      v-model:filter="filter"
      :trend="query.data.value?.trend"
    />

    <StateBlock v-if="query.isLoading.value" state="loading" />
    <StateBlock
      v-else-if="query.isError.value"
      state="error"
      title="订阅加载失败"
      :retrying="query.isFetching.value"
      @retry="query.refetch()"
    />
    <StateBlock
      v-else-if="!items.length"
      state="empty"
      title="没有符合条件的订阅"
      description="调整筛选条件，或添加第一条 RSS 订阅。"
    />
    <section v-else class="grid gap-3">
      <SubscriptionCard
        v-for="item in items"
        :key="item.ID"
        :item="item"
        :is-busy="actions.isBusy"
        @open="openHistory(item)"
        @play="playSubscription(item)"
        @toggle="operate(item, 'toggle')"
        @check="operate(item, 'run')"
        @repair="repair(item, $event)"
        @edit="openEdit(item)"
        @remove="confirmDelete(item)"
      />
    </section>

    <AppDialog
      :open="mode === 'form'"
      :title="formTitle"
      description="先校验 RSS 与过滤规则，再保存。"
      wide
      @update:open="setFormOpen"
    >
      <form @submit.prevent="save">
        <div class="panel-muted mb-5 flex flex-wrap items-center justify-between gap-3 p-4">
          <div>
            <p class="eyebrow">MIKAN ASSOCIATION</p>
            <strong class="mt-1 block">
              {{ form.mikan_id ? `已关联 Mikan #${form.mikan_id}` : '尚未关联 Mikan' }}
            </strong>
            <p class="muted mt-1 text-xs">
              {{ form.subtitle_group || (form.mikan_id ? '全部字幕组' : '可继续手动填写 RSS') }}
              <span v-if="form.season"> · {{ form.season }}</span>
            </p>
          </div>
          <button type="button" class="btn btn-secondary" @click="openDiscovery(true)">
            <Sparkles :size="16" />
            {{ form.mikan_id ? '重新关联' : '从 Mikan 选择' }}
          </button>
        </div>

        <div class="grid gap-4">
          <label class="label">番剧名称<input v-model="form.title" class="field" required /></label>
          <label class="label">主 RSS 地址<input v-model="form.rss_url" class="field" type="url" required /></label>
          <label class="label">备用 RSS 地址<input v-model="form.backup_rss_url" class="field" type="url" /></label>
          <div class="grid gap-4 sm:grid-cols-2">
            <label class="label">必须包含（正则）<input v-model="form.filter_rule" class="field font-mono" placeholder="例如：1080[Pp].*(CHS|简中)" spellcheck="false" /><span v-if="includeRuleError" class="text-xs text-[var(--danger)]" role="alert">正则错误：{{ includeRuleError }}</span></label>
            <label class="label">必须不含（正则）<input v-model="form.exclude_rule" class="field font-mono" placeholder="例如：(720[Pp]|合集|NCOP)" spellcheck="false" /><span v-if="excludeRuleError" class="text-xs text-[var(--danger)]" role="alert">正则错误：{{ excludeRuleError }}</span></label>
          </div>
          <p class="muted -mt-2 text-xs">规则使用正则表达式匹配 RSS 资源标题；留空表示不限制。字幕组专属 Mikan RSS 不需要重复填写字幕组名称。</p>
          <div class="grid gap-4 sm:grid-cols-2">
            <label class="label">
              清晰度
              <select v-model="form.resolution_filter" class="field">
                <option value="">不限清晰度</option>
                <option value="2160p">2160P / 4K</option>
                <option value="1080p">1080P</option>
                <option value="720p">720P</option>
              </select>
            </label>
            <label class="label">
              字幕语言
              <select v-model="form.subtitle_language" class="field">
                <option value="">不限字幕</option>
                <option value="chs">简体中文（含简繁）</option>
                <option value="cht">繁体中文（含简繁）</option>
                <option value="chs_cht">简繁双语</option>
              </select>
            </label>
          </div>
          <div class="grid gap-4 sm:grid-cols-2">
            <label class="label">预期集数<input v-model.number="form.expected_episodes" class="field" type="number" min="0" /></label>
            <label class="label">无更新提醒（小时）<input v-model.number="form.stale_after_hours" class="field" type="number" min="1" /></label>
          </div>
          <label class="flex min-h-11 items-center gap-3 font-bold">
            <input
              v-model="form.allow_multi_subgroup"
              type="checkbox"
              class="h-4 w-4 accent-[var(--brand)]"
              @change="handleMultiSubgroupChange"
            />
            允许多字幕组共存
          </label>
          <p v-if="form.allow_multi_subgroup && form.mikan_id" class="muted -mt-2 text-xs">
            已使用聚合 RSS；字幕组自动过滤已关闭，自定义规则会保留。
          </p>
          <label class="flex min-h-11 items-center gap-3 font-bold">
            <input
              v-model="form.auto_disable_on_done"
              type="checkbox"
              class="h-4 w-4 accent-[var(--brand)]"
            />
            完结后自动暂停
          </label>
        </div>

        <div v-if="validation" class="panel-muted mt-5 p-4 text-sm">
          <strong>
            RSS 中 {{ validation.primary_count }} 项，规则命中 {{ validation.matching_count }} 项
          </strong>
          <ul
            v-if="validation.warnings?.length"
            class="mt-2 list-disc space-y-1 pl-5 text-[var(--warning)]"
          >
            <li v-for="warning in validation.warnings" :key="warning">{{ warning }}</li>
          </ul>
          <p v-if="validation.preview_titles?.length" class="muted mt-2 line-clamp-3">
            {{ validation.preview_titles.join(' · ') }}
          </p>
        </div>

        <div class="mt-7 flex flex-wrap justify-end gap-2">
          <AsyncButton
            class="btn btn-secondary"
            :disabled="hasRegexError"
            :loading="actions.isBusy('validate')"
            loading-label="校验中…"
            @click="validate"
          >
            校验 RSS
          </AsyncButton>
          <AsyncButton
            type="submit"
            class="btn btn-primary"
            :disabled="hasRegexError"
            :loading="actions.isBusy('save')"
            loading-label="正在保存…"
          >
            保存订阅
          </AsyncButton>
        </div>
      </form>
    </AppDialog>

    <SubscriptionBatchDialog
      v-model:text="batchText"
      :open="mode === 'batch'"
      :preview="batchPreview"
      :item-count="batchItems.length"
      :preview-loading="actions.isBusy('batch-preview')"
      :import-loading="actions.isBusy('batch-import')"
      @update:open="setBatchOpen"
      @preview="previewBatch"
      @import="importBatch"
    />

    <MikanDiscoveryDialog
      :open="discoveryOpen"
      @update:open="setDiscoveryOpen"
      @select="applyMikanSelection"
    />

    <SubscriptionHistoryDialog
      :open="Boolean(detailTarget)"
      :title="historyTitle"
      :loading="history.isLoading.value"
      :error="history.isError.value"
      :retrying="history.isFetching.value"
      :item="history.data.value?.Subscription || detailTarget"
      :runs="history.data.value?.Runs || []"
      :logs="history.data.value?.Logs || []"
      @update:open="setHistoryOpen"
      @retry="history.refetch()"
      @play="playSubscription"
    />

    <ConfirmDialog
      :open="Boolean(deleteTarget)"
      danger
      :loading="Boolean(deleteTarget && actions.isBusy(`remove-${deleteTarget.ID}`))"
      loading-label="删除中…"
      title="删除这条订阅？"
      :description="deleteDescription"
      confirm-label="确认删除"
      @update:open="setDeleteOpen"
      @confirm="remove"
    />
  </div>
</template>
