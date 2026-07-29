<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useQuery } from '@tanstack/vue-query'
import { Download, FlaskConical, RefreshCw, RotateCcw, ShieldCheck } from '@lucide/vue'
import { api } from '../api/client'
import type { TaskAccepted, UpdaterSnapshot, UpdaterSnapshotList, UpdateRelease, UpdateReleaseCatalog } from '../api/types'
import { useAsyncActions } from '../composables/useAsyncActions'
import { useUIStore } from '../stores/ui'
import AsyncButton from './AsyncButton.vue'
import ConfirmDialog from './ConfirmDialog.vue'

type UpdateChannel = 'stable' | 'beta'

const channelStorageKey = 'animate.updater.channel'
const storedChannel = localStorage.getItem(channelStorageKey)
const channel = ref<UpdateChannel>(storedChannel === 'beta' ? 'beta' : 'stable')
const selectedVersion = ref('')
const confirmOpen = ref(false)
const rollbackSnapshot = ref<UpdaterSnapshot | null>(null)
const actions = useAsyncActions()
const ui = useUIStore()

const releases = useQuery({
  queryKey: computed(() => ['updater-releases', channel.value]),
  queryFn: () => api<UpdateReleaseCatalog>(`/settings/updater/releases?channel=${channel.value}`),
  staleTime: 60_000,
})
const snapshots = useQuery({
  queryKey: ['updater-snapshots'],
  queryFn: () => api<UpdaterSnapshotList>('/settings/updater/snapshots'),
  staleTime: 60_000,
})

const installableReleases = computed(() =>
  (releases.data.value?.items || []).filter(item => item.installable ?? (item.newer_than_current && item.asset_available)),
)
const selectedRelease = computed<UpdateRelease | undefined>(() =>
  installableReleases.value.find(item => item.version === selectedVersion.value),
)

watch(
  [channel, () => releases.data.value],
  () => {
    if (!installableReleases.value.some(item => item.version === selectedVersion.value)) {
      selectedVersion.value = installableReleases.value[0]?.version || ''
    }
  },
  { immediate: true },
)

function setChannel(next: UpdateChannel) {
  channel.value = next
  localStorage.setItem(channelStorageKey, next)
}

function formatPublishedAt(value?: string) {
  if (!value) return ''
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return ''
  return parsed.toLocaleDateString()
}

function requestUpdate() {
  if (!selectedRelease.value) return
  confirmOpen.value = true
}

async function applySelectedVersion() {
  const version = selectedRelease.value?.version
  if (!version) return
  try {
    await actions.runTask(
      'dashboard-update',
      () => api<TaskAccepted>('/settings/updater/apply', {
        method: 'POST',
        body: JSON.stringify({ version }),
        headers: { 'Content-Type': 'application/json' },
      }),
      `更新到 ${version}`,
      'updater',
      `正在重新校验并下载 ${version}`,
    )
    confirmOpen.value = false
    ui.toast(`${version} 更新任务已经启动，完成后应用会自动重启`)
  } catch (error) {
    ui.toast(error instanceof Error ? error.message : '启动更新失败', 'error')
  }
}

async function restoreSnapshot() {
  const snapshot = rollbackSnapshot.value
  if (!snapshot) return
  try {
    const result = await actions.run(
      `rollback-${snapshot.id}`,
      () => api<{ snapshot_id: string; restarting?: boolean }>('/settings/updater/rollback', {
        method: 'POST',
        body: JSON.stringify({ id: snapshot.id }),
        headers: { 'Content-Type': 'application/json' },
      }),
    )
    rollbackSnapshot.value = null
    ui.toast(result.restarting ? '更新快照已恢复，应用正在重启' : '更新快照已恢复，请重启应用使运行状态完全同步')
    void snapshots.refetch()
  } catch (error) {
    ui.toast(error instanceof Error ? error.message : '恢复更新快照失败', 'error')
  }
}
</script>

<template>
  <section class="panel overflow-hidden p-5 sm:p-6" data-testid="dashboard-updater">
    <div class="flex flex-wrap items-start justify-between gap-4">
      <div class="flex min-w-0 items-start gap-3">
        <span class="grid h-11 w-11 shrink-0 place-items-center rounded-xl bg-[var(--brand-soft)] text-[var(--brand)]">
          <Download :size="20" />
        </span>
        <div class="min-w-0">
          <p class="eyebrow">APPLICATION UPDATE</p>
          <h2 class="mt-1 text-xl font-black">选择要更新的版本</h2>
          <p class="muted mt-1 text-sm leading-6">稳定版用于日常使用；测试版会额外显示 GitHub prerelease，方便快速验证新功能。</p>
        </div>
      </div>
      <span class="badge">当前 {{ releases.data.value?.current_version || '读取中' }}</span>
    </div>

    <div class="mt-5 grid gap-4 lg:grid-cols-[minmax(220px,.7fr)_minmax(260px,1.3fr)_auto] lg:items-end">
      <div>
        <span class="label mb-2">更新通道</span>
        <div class="grid grid-cols-2 gap-2 rounded-2xl bg-[var(--surface-muted)] p-1.5" role="group" aria-label="更新通道">
          <button
            type="button"
            class="flex min-h-11 items-center justify-center gap-2 rounded-xl px-3 text-sm font-extrabold transition"
            :class="channel==='stable'?'bg-[var(--surface-solid)] text-[var(--brand-strong)] shadow-sm':'muted'"
            :aria-pressed="channel==='stable'"
            @click="setChannel('stable')"
          >
            <ShieldCheck :size="17" />稳定版
          </button>
          <button
            type="button"
            class="flex min-h-11 items-center justify-center gap-2 rounded-xl px-3 text-sm font-extrabold transition"
            :class="channel==='beta'?'bg-amber-100 text-amber-800 shadow-sm dark:bg-amber-950/60 dark:text-amber-200':'muted'"
            :aria-pressed="channel==='beta'"
            @click="setChannel('beta')"
          >
            <FlaskConical :size="17" />测试版
          </button>
        </div>
      </div>

      <label class="label min-w-0">
        目标版本
        <select v-model="selectedVersion" class="field" :disabled="releases.isLoading.value || !installableReleases.length">
          <option v-if="!installableReleases.length" value="">
            {{ releases.isLoading.value ? '正在读取版本列表…' : '当前通道没有可安全切换版本' }}
          </option>
          <option v-for="item in installableReleases" :key="item.version" :value="item.version">
            {{ item.version }}{{ item.prerelease ? ' · 测试版' : ' · 稳定版' }}{{ item.switchable ? ' · 可回切' : '' }}{{ formatPublishedAt(item.published_at) ? ` · ${formatPublishedAt(item.published_at)}` : '' }}
          </option>
        </select>
      </label>

      <div class="flex flex-wrap gap-2 lg:justify-end">
        <AsyncButton class="btn btn-secondary" :loading="releases.isFetching.value" loading-label="刷新中…" @click="releases.refetch()">
          <RefreshCw :size="16" />刷新版本
        </AsyncButton>
        <AsyncButton
          class="btn btn-primary"
          :disabled="!selectedRelease"
          :loading="actions.isBusy('dashboard-update','repo-update-apply')"
          loading-label="更新中…"
          @click="requestUpdate"
        >
          <Download :size="16" />更新到所选版本
        </AsyncButton>
      </div>
    </div>

    <div v-if="channel==='beta'" class="mt-4 flex items-start gap-3 rounded-xl border border-amber-300/70 bg-amber-50 p-4 text-sm leading-6 text-amber-900 dark:border-amber-800 dark:bg-amber-950/40 dark:text-amber-100">
      <FlaskConical class="mt-0.5 shrink-0" :size="18" />
      <span>测试版可能包含未完成改动。它适合快速调试，但不建议开启无人值守自动更新；需要发布测试包时使用类似 <code>v0.9.9-beta.2</code> 的标签。</span>
    </div>
    <p v-else-if="releases.isError.value" class="mt-4 text-sm text-[var(--danger)]">版本列表读取失败，请检查 GitHub 网络或更新代理设置后重试。</p>
    <p v-else class="muted mt-4 text-xs leading-5">自动更新只向前升级；手动切换到旧稳定版必须通过 Release 兼容清单、数据库 schema 和安装包校验。</p>

    <section v-if="snapshots.data.value?.items?.length" class="mt-5 rounded-2xl border border-[var(--line)] p-4">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h3 class="font-black">可恢复的更新快照</h3>
          <p class="muted mt-1 text-xs leading-5">更新前自动保存数据库和配置；恢复前请先停止正在运行的下载或播放任务。</p>
        </div>
        <RotateCcw :size="18" class="muted" />
      </div>
      <div class="mt-3 grid gap-2">
        <button
          v-for="snapshot in snapshots.data.value.items"
          :key="snapshot.id"
          type="button"
          class="panel-muted flex min-h-12 items-center justify-between gap-3 p-3 text-left transition hover:border-[var(--brand)]"
          @click="rollbackSnapshot=snapshot"
        >
          <span class="min-w-0">
            <strong class="block truncate">{{ snapshot.app_version }} · {{ snapshot.reason || '更新前快照' }}</strong>
            <span class="muted text-xs">{{ new Date(snapshot.created_at).toLocaleString() }} · schema {{ snapshot.schema_version }}</span>
          </span>
          <RotateCcw :size="16" class="shrink-0 text-[var(--brand)]" />
        </button>
      </div>
    </section>
  </section>

  <ConfirmDialog
    :open="confirmOpen"
    :title="selectedRelease?.switchable ? `切换到稳定版 ${selectedRelease.version}？` : selectedRelease?.prerelease ? `安装测试版 ${selectedRelease.version}？` : `更新到 ${selectedRelease?.version || ''}？`"
    :description="selectedRelease?.prerelease
      ? '测试版可能不稳定。确认后服务端会重新校验该 Release，下载对应平台安装包并在校验通过后自动重启。'
      : selectedRelease?.switchable
        ? '这是一次受兼容清单保护的通道切换。服务端会先创建数据库和配置快照，健康检查失败时自动恢复。'
        : '确认后服务端会重新校验该 Release，下载对应平台安装包并在校验通过后自动重启。'"
    :confirm-label="selectedRelease?.switchable ? '确认切换' : '确认更新'"
    :loading="actions.isBusy('dashboard-update','repo-update-apply')"
    loading-label="正在启动…"
    @update:open="confirmOpen=$event"
    @confirm="applySelectedVersion"
  />
  <ConfirmDialog
    :open="!!rollbackSnapshot"
    title="恢复这个更新快照？"
    :description="rollbackSnapshot ? `将恢复 ${rollbackSnapshot.app_version} 创建的数据库和配置快照。恢复后建议重启应用。` : ''"
    confirm-label="确认恢复"
    :loading="rollbackSnapshot ? actions.isBusy(`rollback-${rollbackSnapshot.id}`) : false"
    loading-label="恢复中…"
    @update:open="($event) => { if (!$event) rollbackSnapshot=null }"
    @confirm="restoreSnapshot"
  />
</template>
