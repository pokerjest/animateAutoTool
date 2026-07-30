<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { useQuery, useQueryClient } from '@tanstack/vue-query'
import {
  Archive,
  Cloud,
  Database,
  Download,
  FileCheck2,
  HardDriveUpload,
  KeyRound,
  LockKeyhole,
  RefreshCw,
  ShieldAlert,
  Trash2,
  Upload,
} from '@lucide/vue'
import { api } from '../api/client'
import AppDialog from '../components/AppDialog.vue'
import AsyncButton from '../components/AsyncButton.vue'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import PageHeader from '../components/PageHeader.vue'
import StateBlock from '../components/StateBlock.vue'
import { useAsyncActions } from '../composables/useAsyncActions'
import { useTaskStore } from '../stores/tasks'
import { useUIStore } from '../stores/ui'

interface Stats {
  subscription_count: number
  download_log_count: number
  local_anime_count: number
  user_count: number
  global_config_count: number
  database_size: string
  last_modified: string
}

interface BackupFile {
  key: string
  size: number
  last_modified: string
}

interface Progress {
  task_id: string
  total_bytes: number
  downloaded: number
  status: string
  error?: string
  result?: {
    stats?: Record<string, unknown>
    restore_token?: string
    key?: string
  }
}

interface PasswordPayload {
  password?: string
  password_confirm?: string
  admin_password?: string
}

type PasswordPurpose = 'export' | 'cloud-upload' | 'local-restore' | 'cloud-restore'
type CreationPasswordMode = 'admin' | 'custom'

const ui = useUIStore()
const tasks = useTaskStore()
const qc = useQueryClient()
const actions = useAsyncActions()
const file = ref<File | null>(null)
const analysis = ref<Record<string, unknown> | null>(null)
const restoreToken = ref('')
const deleteKey = ref('')
const restoreOpen = ref(false)
const stageProgress = reactive<Record<string, Progress | undefined>>({})
const uploadProgress = ref<Progress | null>(null)
const passwordOpen = ref(false)
const passwordPurpose = ref<PasswordPurpose>('export')
const pendingCloudKey = ref('')
const creationPasswordMode = ref<CreationPasswordMode>('admin')
const adminPassword = ref('')
const customPassword = ref('')
const customPasswordConfirm = ref('')
const restorePassword = ref('')

const categories = reactive<Record<string, boolean>>({
  configs: true,
  metadata: true,
  subscriptions: true,
  logs: true,
  local: true,
  users: false,
})
const regenerateNFO = ref(true)
const categoryOptions = [
  { key: 'configs', label: '系统配置' },
  { key: 'metadata', label: '元数据' },
  { key: 'subscriptions', label: '订阅' },
  { key: 'logs', label: '下载日志' },
  { key: 'local', label: '本地媒体索引' },
  { key: 'users', label: '管理员账户' },
]

const query = useQuery({
  queryKey: ['backup'],
  queryFn: () => api<{ stats: Stats; r2: { configured: boolean; files: BackupFile[] } }>('/backup'),
})

const selectedCategories = computed(() =>
  Object.entries(categories).filter(([, enabled]) => enabled).map(([key]) => key),
)
const creatingArchive = computed(() =>
  passwordPurpose.value === 'export' || passwordPurpose.value === 'cloud-upload',
)
const passwordDialogTitle = computed(() => {
  const labels: Record<PasswordPurpose, string> = {
    export: '加密导出备份',
    'cloud-upload': '加密并上传到 R2',
    'local-restore': '解密本地备份',
    'cloud-restore': '解密云端备份',
  }
  return labels[passwordPurpose.value]
})
const passwordDialogDescription = computed(() =>
  creatingArchive.value
    ? '备份会压缩为 AES-256 加密 ZIP。默认使用当前管理员登录密码，也可以设置独立密码。'
    : '请输入创建此备份时使用的密码。密码错误时不会进入恢复预览。',
)
const passwordReady = computed(() => {
  if (!creatingArchive.value) return restorePassword.value.length > 0
  if (creationPasswordMode.value === 'admin') return adminPassword.value.length > 0
  return customPassword.value.length >= 8 && customPassword.value === customPasswordConfirm.value
})

function clearPasswordInputs() {
  adminPassword.value = ''
  customPassword.value = ''
  customPasswordConfirm.value = ''
  restorePassword.value = ''
}

function closePasswordDialog() {
  passwordOpen.value = false
  pendingCloudKey.value = ''
  clearPasswordInputs()
}

function requestPassword(purpose: PasswordPurpose, key = '') {
  clearPasswordInputs()
  passwordPurpose.value = purpose
  pendingCloudKey.value = key
  creationPasswordMode.value = 'admin'
  passwordOpen.value = true
}

function creationPayload(): PasswordPayload {
  if (creationPasswordMode.value === 'admin') {
    return { admin_password: adminPassword.value }
  }
  return {
    password: customPassword.value,
    password_confirm: customPasswordConfirm.value,
  }
}

async function confirmPassword() {
  if (!passwordReady.value) return
  const purpose = passwordPurpose.value
  const key = pendingCloudKey.value
  const payload = creatingArchive.value ? creationPayload() : { password: restorePassword.value }
  closePasswordDialog()

  if (purpose === 'export') await exportBackup(payload)
  if (purpose === 'cloud-upload') await uploadCloud(payload)
  if (purpose === 'local-restore') await uploadAnalyze(payload)
  if (purpose === 'cloud-restore') await stage(key, payload)
}

function requestAnalyze() {
  if (!file.value) return
  if (file.value.name.toLowerCase().endsWith('.zip')) {
    requestPassword('local-restore')
    return
  }
  void uploadAnalyze({})
}

function requestCloudRestore(item: BackupFile) {
  if (item.key.toLowerCase().endsWith('.zip')) {
    requestPassword('cloud-restore', item.key)
    return
  }
  void stage(item.key, {})
}

async function responseError(response: Response) {
  const contentType = response.headers.get('content-type') || ''
  if (contentType.includes('application/json')) {
    const payload = await response.json().catch(() => null)
    return payload?.error?.message || payload?.error || payload?.message || '请求失败'
  }
  return (await response.text().catch(() => '')) || '请求失败'
}

function attachmentFilename(response: Response) {
  const disposition = response.headers.get('content-disposition') || ''
  const encoded = disposition.match(/filename\*=UTF-8''([^;]+)/i)?.[1]
  if (encoded) return decodeURIComponent(encoded)
  const plain = disposition.match(/filename="?([^";]+)"?/i)?.[1]
  return plain || `animateData_full_${new Date().toISOString().replace(/\D/g, '').slice(0, 14)}.zip`
}

async function exportBackup(payload: PasswordPayload) {
  try {
    await actions.run('export', async () => {
      const response = await fetch('/api/v1/backup/export', {
        method: 'POST',
        credentials: 'same-origin',
        headers: {
          Accept: 'application/zip, application/json',
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ mode: 'full', ...payload }),
      })
      if (!response.ok) throw new Error(await responseError(response))
      const blob = await response.blob()
      const url = URL.createObjectURL(blob)
      const anchor = document.createElement('a')
      anchor.href = url
      anchor.download = attachmentFilename(response)
      document.body.appendChild(anchor)
      anchor.click()
      anchor.remove()
      URL.revokeObjectURL(url)
      ui.toast('加密 ZIP 备份已导出')
    })
  } catch (error) {
    ui.toast(error instanceof Error ? error.message : '导出失败', 'error')
  }
}

async function uploadAnalyze(payload: PasswordPayload) {
  if (!file.value) return
  try {
    await actions.run('analyze', async () => {
      const data = new FormData()
      data.append('backup_file', file.value!)
      Object.entries(payload).forEach(([key, value]) => {
        if (value) data.append(key, value)
      })
      const result = await api<{ stats: Record<string, unknown>; restore_token: string }>('/backup/analyze', {
        method: 'POST',
        body: data,
      })
      analysis.value = result.stats
      restoreToken.value = result.restore_token
      restoreOpen.value = true
      ui.toast('备份文件解密并校验通过')
    })
  } catch (error) {
    ui.toast(error instanceof Error ? error.message : '分析失败', 'error')
  }
}

async function restore() {
  if (!selectedCategories.value.length) {
    ui.toast('请至少选择一类数据', 'error')
    return
  }
  try {
    await actions.run('restore', async () => {
      await api('/backup/restore', {
        method: 'POST',
        body: JSON.stringify({
          restore_token: restoreToken.value,
          categories: selectedCategories.value,
          regenerate_nfo: regenerateNFO.value,
        }),
        headers: { 'Content-Type': 'application/json' },
      })
      ui.toast('恢复已完成')
      analysis.value = null
      restoreOpen.value = false
      file.value = null
      await qc.invalidateQueries()
    })
  } catch (error) {
    ui.toast(error instanceof Error ? error.message : '恢复失败', 'error')
  }
}

async function pollProgress(taskID: string, onProgress: (progress: Progress) => void) {
  for (let index = 0; index < 180; index += 1) {
    await new Promise(resolve => setTimeout(resolve, 1000))
    const progress = await api<Progress>(`/backup/r2/progress/${taskID}`)
    onProgress(progress)
    if (progress.status === 'completed') return progress
    if (progress.status === 'error') throw new Error(progress.error || '云备份任务失败')
  }
  throw new Error('云备份任务等待超时，请稍后刷新查看')
}

async function refreshBackupQuery() {
  await query.refetch()
}

function progressLabel(status?: string) {
  const labels: Record<string, string> = {
    pending: '准备任务',
    preparing: '正在创建完整备份',
    compressing: '正在压缩并加密',
    uploading: '正在上传加密 ZIP',
    downloading: '正在下载云备份',
    decrypting: '正在解密并校验',
    analyzing: '正在分析备份内容',
    completed: '已完成',
    error: '任务失败',
  }
  return labels[status || 'pending'] || '正在处理'
}

async function uploadCloud(payload: PasswordPayload) {
  uploadProgress.value = null
  let taskID = ''
  try {
    await actions.run('cloud-upload', async () => {
      const task = await api<{ task_id: string }>('/backup/r2/upload', {
        method: 'POST',
        body: JSON.stringify(payload),
        headers: { 'Content-Type': 'application/json' },
      })
      taskID = task.task_id
      tasks.upsert({
        id: taskID,
        kind: 'backup',
        title: 'R2 云备份',
        detail: '正在创建完整备份',
        tone: 'running',
      })
      await pollProgress(taskID, progress => {
        uploadProgress.value = progress
        tasks.upsert({
          id: taskID,
          kind: 'backup',
          title: 'R2 云备份',
          detail: progressLabel(progress.status),
          current: progress.downloaded,
          total: progress.total_bytes,
          tone: 'running',
        })
      })
      tasks.upsert({
        id: taskID,
        kind: 'backup',
        title: 'R2 云备份',
        detail: '加密云备份上传完成',
        tone: 'success',
      })
      ui.toast('加密云备份上传完成')
      await refreshBackupQuery()
    })
  } catch (error) {
    if (taskID) {
      tasks.upsert({
        id: taskID,
        kind: 'backup',
        title: 'R2 云备份',
        detail: error instanceof Error ? error.message : '上传失败',
        tone: 'error',
      })
    }
    ui.toast(error instanceof Error ? error.message : '上传失败', 'error')
  }
}

async function deleteCloud() {
  const key = deleteKey.value
  try {
    await actions.run(`delete-${key}`, async () => {
      await api('/backup/r2/delete', {
        method: 'POST',
        body: JSON.stringify({ key }),
        headers: { 'Content-Type': 'application/json' },
      })
      deleteKey.value = ''
      await refreshBackupQuery()
      ui.toast('云备份已删除')
    })
  } catch (error) {
    ui.toast(error instanceof Error ? error.message : '删除失败', 'error')
  }
}

async function stage(key: string, payload: PasswordPayload) {
  if (!key) return
  stageProgress[key] = undefined
  let taskID = ''
  try {
    await actions.run(`stage-${key}`, async () => {
      const task = await api<{ task_id: string }>('/backup/r2/stage', {
        method: 'POST',
        body: JSON.stringify({ key, ...payload }),
        headers: { 'Content-Type': 'application/json' },
      })
      taskID = task.task_id
      tasks.upsert({
        id: taskID,
        kind: 'backup',
        title: '云备份恢复',
        detail: '正在下载云备份',
        tone: 'running',
      })
      const progress = await pollProgress(taskID, value => {
        stageProgress[key] = value
        tasks.upsert({
          id: taskID,
          kind: 'backup',
          title: '云备份恢复',
          detail: progressLabel(value.status),
          current: value.downloaded,
          total: value.total_bytes,
          tone: 'running',
        })
      })
      analysis.value = progress.result?.stats || {}
      restoreToken.value = progress.result?.restore_token || ''
      restoreOpen.value = true
      tasks.upsert({
        id: taskID,
        kind: 'backup',
        title: '云备份恢复',
        detail: '云备份解密并校验完成',
        tone: 'success',
      })
      ui.toast('云备份解密并校验完成')
    })
  } catch (error) {
    if (taskID) {
      tasks.upsert({
        id: taskID,
        kind: 'backup',
        title: '云备份恢复',
        detail: error instanceof Error ? error.message : '暂存失败',
        tone: 'error',
      })
    }
    ui.toast(error instanceof Error ? error.message : '暂存失败', 'error')
  }
}

const percent = (progress?: Progress) =>
  progress?.total_bytes ? Math.min(100, Math.round(progress.downloaded / progress.total_bytes * 100)) : 0
const formatSize = (size: number) =>
  size > 1024 * 1024 ? `${(size / 1024 / 1024).toFixed(1)} MB` : `${Math.ceil(size / 1024)} KB`
</script>

<template>
  <div class="page-grid">
    <PageHeader
      eyebrow="DATA SAFETY"
      title="备份与恢复"
      description="所有新备份都会先压缩并使用 AES-256 加密，再导出到本地或上传到 R2。"
    >
      <AsyncButton
        class="btn btn-primary"
        :loading="actions.isBusy('export')"
        loading-label="正在压缩…"
        @click="requestPassword('export')"
      >
        <Download :size="18" />
        导出加密 ZIP
      </AsyncButton>
    </PageHeader>

    <StateBlock v-if="query.isLoading.value" state="loading" />
    <StateBlock
      v-else-if="query.isError.value"
      state="error"
      title="备份状态加载失败"
      :retrying="query.isFetching.value"
      @retry="query.refetch()"
    />

    <template v-else-if="query.data.value">
      <section class="grid gap-4 sm:grid-cols-2 xl:grid-cols-5">
        <article
          v-for="item in [
            { label: '订阅', value: query.data.value.stats.subscription_count },
            { label: '下载与运行日志', value: query.data.value.stats.download_log_count },
            { label: '本地番剧', value: query.data.value.stats.local_anime_count },
            { label: '系统配置', value: query.data.value.stats.global_config_count },
            { label: '数据库大小', value: query.data.value.stats.database_size },
          ]"
          :key="item.label"
          class="panel p-4"
        >
          <Database class="text-[var(--brand)]" :size="20" />
          <p class="muted mt-4 text-xs font-bold">{{ item.label }}</p>
          <strong class="mt-1 block text-xl font-black">{{ item.value }}</strong>
        </article>
      </section>

      <section class="grid gap-5 xl:grid-cols-2">
        <article class="panel p-5 sm:p-6">
          <div class="flex items-center gap-3">
            <span class="grid h-11 w-11 place-items-center rounded-xl bg-[var(--brand-soft)] text-[var(--brand)]">
              <HardDriveUpload :size="22" />
            </span>
            <div>
              <p class="eyebrow">LOCAL RESTORE</p>
              <h3 class="text-xl font-black">从文件恢复</h3>
            </div>
          </div>
          <p class="muted mt-4 text-sm leading-6">
            选择 AnimateTool 加密 ZIP。旧版未加密的 .db 和 .sqlite 仍可导入，系统会先分析内容，不会立即覆盖数据。
          </p>
          <label class="mt-5 grid min-h-32 cursor-pointer place-items-center rounded-2xl border-2 border-dashed border-[var(--line)] p-5 text-center">
            <Upload class="muted mb-2" :size="24" />
            <strong>{{ file?.name || '选择 .zip / .db / .sqlite 备份' }}</strong>
            <input
              class="sr-only"
              type="file"
              accept=".zip,.db,.sqlite"
              @change="file = ($event.target as HTMLInputElement).files?.[0] || null"
            />
          </label>
          <AsyncButton
            class="btn btn-secondary mt-4 w-full"
            :disabled="!file"
            :loading="actions.isBusy('analyze')"
            loading-label="正在解密校验…"
            @click="requestAnalyze"
          >
            <FileCheck2 :size="18" />
            分析备份内容
          </AsyncButton>
        </article>

        <article class="panel min-w-0 p-5 sm:p-6">
          <div class="flex items-center gap-3">
            <span class="grid h-11 w-11 place-items-center rounded-xl bg-[var(--sky-soft)] text-[var(--sky)]">
              <Cloud :size="22" />
            </span>
            <div>
              <p class="eyebrow">CLOUDFLARE R2</p>
              <h3 class="text-xl font-black">云端加密存档</h3>
            </div>
          </div>

          <div v-if="query.data.value.r2.configured" class="mt-5 min-w-0">
            <AsyncButton
              class="btn btn-primary w-full"
              :loading="actions.isBusy('cloud-upload')"
              loading-label="正在备份…"
              @click="requestPassword('cloud-upload')"
            >
              <Cloud :size="18" />
              压缩加密后上传
            </AsyncButton>

            <div v-if="actions.isBusy('cloud-upload')" class="panel-muted mt-4 p-4">
              <div class="flex items-center justify-between gap-3 text-sm">
                <strong>{{ progressLabel(uploadProgress?.status) }}</strong>
                <span v-if="uploadProgress?.total_bytes" class="muted">
                  {{ formatSize(uploadProgress.downloaded) }} / {{ formatSize(uploadProgress.total_bytes) }}
                </span>
              </div>
              <div class="mt-3 h-2 overflow-hidden rounded-full bg-[var(--line)]">
                <div
                  v-if="uploadProgress?.total_bytes"
                  class="h-full bg-[var(--sky)] transition-all"
                  :style="{ width: `${percent(uploadProgress || undefined)}%` }"
                />
                <div v-else class="h-full w-1/3 animate-pulse rounded-full bg-[var(--sky)]" />
              </div>
            </div>

            <div
              v-if="query.data.value.r2.files.length"
              class="mt-4 max-h-[30rem] divide-y divide-[var(--line)] overflow-y-auto pr-1"
            >
              <div
                v-for="item in query.data.value.r2.files"
                :key="item.key"
                class="grid min-w-0 grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-3 py-3"
              >
                <Archive :size="20" class="text-[var(--sky)]" />
                <div class="min-w-0">
                  <p class="truncate text-sm font-bold">{{ item.key }}</p>
                  <p class="muted text-xs">{{ item.last_modified }} · {{ formatSize(item.size) }}</p>
                </div>
                <div class="flex gap-2">
                  <AsyncButton
                    class="btn btn-quiet h-11 min-h-11 px-3 text-xs"
                    :loading="actions.isBusy(`stage-${item.key}`)"
                    loading-label="恢复中…"
                    @click="requestCloudRestore(item)"
                  >
                    <RefreshCw :size="16" />
                    恢复
                  </AsyncButton>
                  <button
                    class="btn btn-danger h-11 min-h-11 w-11 p-0"
                    :disabled="actions.isBusy(`stage-${item.key}`)"
                    aria-label="删除云备份"
                    @click="deleteKey = item.key"
                  >
                    <Trash2 :size="17" />
                  </button>
                </div>
                <div v-if="actions.isBusy(`stage-${item.key}`)" class="col-span-3">
                  <div class="h-2 overflow-hidden rounded-full bg-[var(--line)]">
                    <div
                      class="h-full bg-[var(--sky)] transition-all"
                      :style="{ width: `${percent(stageProgress[item.key])}%` }"
                    />
                  </div>
                  <p class="muted mt-1 text-xs">
                    {{ progressLabel(stageProgress[item.key]?.status) }}
                    <template v-if="stageProgress[item.key]?.total_bytes">
                      · {{ percent(stageProgress[item.key]) }}%
                    </template>
                  </p>
                </div>
              </div>
            </div>
            <p v-else class="muted mt-8 text-center text-sm">R2 中还没有备份</p>
          </div>

          <div v-else class="panel-muted mt-5 p-5 text-center">
            <Cloud class="muted mx-auto mb-3" :size="24" />
            <h4 class="font-extrabold">尚未配置 R2</h4>
            <p class="muted mt-1 text-sm">在设置页添加 Endpoint、Bucket 和访问凭据。</p>
            <RouterLink class="btn btn-secondary mt-4" to="/settings">打开设置</RouterLink>
          </div>
        </article>
      </section>
    </template>

    <div class="panel-muted mt-5 flex items-start gap-3 p-4 text-sm leading-6">
      <ShieldAlert class="mt-0.5 shrink-0 text-[var(--warning)]" :size="19" />
      <p class="muted">
        <strong class="text-[var(--foreground)]">备份安全提示：</strong>
        新备份使用 AES-256 加密，忘记密码后无法恢复。完整备份可能包含已保存凭据；选择性备份仍会跳过密码、Token 和 API Key。
      </p>
    </div>

    <AppDialog
      :open="passwordOpen"
      :title="passwordDialogTitle"
      :description="passwordDialogDescription"
      @update:open="value => { if (!value) closePasswordDialog() }"
    >
      <form class="space-y-5" @submit.prevent="confirmPassword">
        <template v-if="creatingArchive">
          <div class="grid gap-3 sm:grid-cols-2">
            <button
              type="button"
              class="panel-muted min-h-24 p-4 text-left"
              :class="creationPasswordMode === 'admin' ? 'ring-2 ring-[var(--brand)]' : ''"
              @click="creationPasswordMode = 'admin'"
            >
              <KeyRound class="mb-3 text-[var(--brand)]" :size="21" />
              <strong class="block">管理员登录密码</strong>
              <span class="muted mt-1 block text-xs">默认方式，需要验证当前密码</span>
            </button>
            <button
              type="button"
              class="panel-muted min-h-24 p-4 text-left"
              :class="creationPasswordMode === 'custom' ? 'ring-2 ring-[var(--brand)]' : ''"
              @click="creationPasswordMode = 'custom'"
            >
              <LockKeyhole class="mb-3 text-[var(--brand)]" :size="21" />
              <strong class="block">独立备份密码</strong>
              <span class="muted mt-1 block text-xs">至少 8 个字符，可与登录密码不同</span>
            </button>
          </div>

          <label v-if="creationPasswordMode === 'admin'" class="label">
            当前管理员登录密码
            <input
              v-model="adminPassword"
              class="field"
              type="password"
              autocomplete="current-password"
              required
              autofocus
            />
          </label>
          <div v-else class="grid gap-4 sm:grid-cols-2">
            <label class="label">
              自定义备份密码
              <input
                v-model="customPassword"
                class="field"
                type="password"
                autocomplete="new-password"
                minlength="8"
                required
              />
            </label>
            <label class="label">
              再次输入
              <input
                v-model="customPasswordConfirm"
                class="field"
                type="password"
                autocomplete="new-password"
                minlength="8"
                required
              />
            </label>
            <p
              v-if="customPasswordConfirm && customPassword !== customPasswordConfirm"
              class="text-sm font-bold text-[var(--danger)] sm:col-span-2"
              role="alert"
            >
              两次输入的密码不一致
            </p>
          </div>
        </template>

        <label v-else class="label">
          备份压缩包密码
          <input
            v-model="restorePassword"
            class="field"
            type="password"
            autocomplete="off"
            required
            autofocus
          />
          <span class="muted text-xs font-normal">请输入导出或上传该备份时使用的密码。</span>
        </label>

        <div class="panel-muted flex items-start gap-3 p-4 text-sm leading-6">
          <LockKeyhole class="mt-0.5 shrink-0 text-[var(--success)]" :size="19" />
          <p class="muted">密码仅用于本次压缩或解密，不会保存到数据库、浏览器存储、任务记录或日志。</p>
        </div>

        <AsyncButton
          type="submit"
          class="btn btn-primary w-full"
          :disabled="!passwordReady"
          :loading="actions.isBusy('export') || actions.isBusy('cloud-upload') || actions.isBusy('analyze')"
          loading-label="正在处理…"
        >
          <LockKeyhole :size="18" />
          {{ creatingArchive ? '确认并创建加密备份' : '解密并分析备份' }}
        </AsyncButton>
      </form>
    </AppDialog>

    <AppDialog
      :open="restoreOpen"
      title="确认恢复范围"
      description="恢复会覆盖所选类别的当前数据。账户与配置等敏感类别默认不全部勾选。"
      @update:open="restoreOpen = $event"
    >
      <div v-if="analysis" class="panel-muted mb-5 grid grid-cols-2 gap-3 p-4 text-sm">
        <div v-for="(value, key) in analysis" :key="key">
          <p class="muted text-xs">{{ key }}</p>
          <strong>{{ value }}</strong>
        </div>
      </div>
      <div class="grid gap-2 sm:grid-cols-2">
        <label
          v-for="item in categoryOptions"
          :key="item.key"
          class="panel-muted flex min-h-12 items-center gap-3 p-3 font-bold"
        >
          <input v-model="categories[item.key]" type="checkbox" class="h-5 w-5 accent-[var(--brand)]" />
          {{ item.label }}
        </label>
      </div>
      <label class="mt-4 flex min-h-12 items-center gap-3 font-bold">
        <input v-model="regenerateNFO" type="checkbox" class="h-5 w-5 accent-[var(--brand)]" />
        恢复后重新生成 NFO
      </label>
      <AsyncButton
        class="btn btn-danger mt-6 w-full"
        :disabled="!restoreToken || !selectedCategories.length"
        :loading="actions.isBusy('restore')"
        loading-label="正在恢复…"
        @click="restore"
      >
        <ShieldAlert :size="18" />
        确认恢复所选数据
      </AsyncButton>
    </AppDialog>

    <ConfirmDialog
      :open="Boolean(deleteKey)"
      danger
      :loading="Boolean(deleteKey && actions.isBusy(`delete-${deleteKey}`))"
      loading-label="删除中…"
      title="删除云端备份？"
      :description="`将永久删除 ${deleteKey}，此操作无法撤销。`"
      confirm-label="永久删除"
      @update:open="value => { if (!value) deleteKey = '' }"
      @confirm="deleteCloud"
    />
  </div>
</template>
