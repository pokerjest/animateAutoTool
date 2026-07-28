<script setup lang="ts">
import { computed, reactive } from 'vue'
import { Bot, CheckCircle2, KeyRound, RefreshCw, Send, Sparkles } from '@lucide/vue'
import { api } from '../api/client'
import { useAsyncActions } from '../composables/useAsyncActions'
import { useUIStore } from '../stores/ui'
import AsyncButton from './AsyncButton.vue'

type AIProviderID = 'openai' | 'gemini' | 'claude'
type AIProviderFormat = 'native' | 'openai'

interface AIFormatDefinition {
  value: AIProviderFormat
  label: string
  description: string
  defaultBaseURL: string
  protocolHint: string
}

interface AIProviderDefinition {
  id: AIProviderID
  label: string
  eyebrow: string
  description: string
  baseURLKey: string
  apiKeyKey: string
  modelKey: string
  formatKey?: string
  formats: AIFormatDefinition[]
  credentialLabel: string
  credentialHint: string
}

interface AIConnectionResult {
  provider: AIProviderID
  provider_label: string
  format: AIProviderFormat
  model: string
  connected: boolean
  reply?: string
  detail: string
  latency_ms: number
  checked_at: string
}

const props = defineProps<{
  form: Record<string, string>
  configured: Record<string, boolean>
}>()

const providers: AIProviderDefinition[] = [
  {
    id: 'openai',
    label: 'OpenAI / GPT',
    eyebrow: 'OPENAI',
    description: '支持 OpenAI 官方接口以及兼容 Chat Completions 协议的自定义 API。',
    baseURLKey: 'ai_openai_base_url',
    apiKeyKey: 'ai_openai_api_key',
    modelKey: 'ai_openai_model',
    credentialLabel: 'OpenAI API Key',
    credentialHint: '模型调用使用单个 API Key，不使用 OAuth Client ID / Client Secret。',
    formats: [{
      value: 'openai',
      label: 'OpenAI Chat Completions',
      description: '固定使用 OpenAI-compatible Chat Completions，适配 OpenAI 官方接口和兼容网关。',
      defaultBaseURL: 'https://api.openai.com/v1',
      protocolHint: '请求路径：/chat/completions · Authorization: Bearer API Key',
    }],
  },
  {
    id: 'gemini',
    label: 'Google Gemini',
    eyebrow: 'GOOGLE AI',
    description: '支持 Gemini 原生 API，也可连接采用 OpenAI Chat Completions 格式的 Gemini 网关。',
    baseURLKey: 'ai_gemini_base_url',
    apiKeyKey: 'ai_gemini_api_key',
    modelKey: 'ai_gemini_model',
    formatKey: 'ai_gemini_api_format',
    credentialLabel: 'Gemini API Key',
    credentialHint: 'Google OAuth Client ID / Client Secret 用于登录和用户授权；这里的模型调用使用 Gemini API Key。',
    formats: [
      {
        value: 'native',
        label: 'Gemini 原生 API',
        description: '使用 generateContent、x-goog-api-key 和 Gemini 原生工具调用格式。',
        defaultBaseURL: 'https://generativelanguage.googleapis.com',
        protocolHint: '请求路径：/v1beta/models/{model}:generateContent · x-goog-api-key',
      },
      {
        value: 'openai',
        label: 'OpenAI 兼容 API',
        description: '用于 Google 官方兼容入口或只支持 OpenAI Chat Completions 的自定义网关。',
        defaultBaseURL: 'https://generativelanguage.googleapis.com/v1beta/openai',
        protocolHint: '请求路径：/chat/completions · Authorization: Bearer Gemini API Key',
      },
    ],
  },
  {
    id: 'claude',
    label: 'Anthropic Claude',
    eyebrow: 'ANTHROPIC',
    description: '支持 Claude 原生 Messages API，也可连接采用 OpenAI Chat Completions 格式的兼容网关。',
    baseURLKey: 'ai_claude_base_url',
    apiKeyKey: 'ai_claude_api_key',
    modelKey: 'ai_claude_model',
    formatKey: 'ai_claude_api_format',
    credentialLabel: 'Anthropic API Key',
    credentialHint: '模型调用使用单个 Anthropic API Key，不使用 OAuth Client ID / Client Secret。',
    formats: [
      {
        value: 'native',
        label: 'Claude Messages API（推荐）',
        description: '保留 Claude 原生工具调用和消息能力，适合长期使用。',
        defaultBaseURL: 'https://api.anthropic.com',
        protocolHint: '请求路径：/v1/messages · x-api-key · anthropic-version',
      },
      {
        value: 'openai',
        label: 'OpenAI 兼容 API',
        description: '适合迁移或测试 OpenAI SDK；部分 Claude 原生能力可能无法完整映射。',
        defaultBaseURL: 'https://api.anthropic.com/v1',
        protocolHint: '请求路径：/chat/completions · Authorization: Bearer Anthropic API Key',
      },
    ],
  },
]

const actions = useAsyncActions()
const ui = useUIStore()
const modelOptions = reactive<Record<AIProviderID, string[]>>({ openai: [], gemini: [], claude: [] })
const testResults = reactive<Record<AIProviderID, AIConnectionResult | null>>({ openai: null, gemini: null, claude: null })

const activeProvider = computed<AIProviderID>({
  get: () => (['openai', 'gemini', 'claude'].includes(props.form.ai_provider) ? props.form.ai_provider : 'openai') as AIProviderID,
  set: value => { props.form.ai_provider = value },
})

function providerFormat(provider: AIProviderDefinition): AIFormatDefinition {
  const saved = provider.formatKey ? props.form[provider.formatKey] : provider.formats[0].value
  return provider.formats.find(item => item.value === saved) || provider.formats[0]
}

function setProviderFormat(provider: AIProviderDefinition, format: AIProviderFormat) {
  if (!provider.formatKey) return
  const currentURL = (props.form[provider.baseURLKey] || '').trim().replace(/\/+$/, '')
  const usesOfficialDefault = !currentURL || provider.formats.some(item => item.defaultBaseURL === currentURL)
  props.form[provider.formatKey] = format
  if (usesOfficialDefault) {
    props.form[provider.baseURLKey] = provider.formats.find(item => item.value === format)?.defaultBaseURL || ''
  }
  modelOptions[provider.id] = []
  testResults[provider.id] = null
}

function requestBody(provider: AIProviderDefinition) {
  return {
    provider: provider.id,
    format: providerFormat(provider).value,
    base_url: props.form[provider.baseURLKey] || '',
    api_key: props.form[provider.apiKeyKey] || '',
    model: props.form[provider.modelKey] || '',
  }
}

async function loadModels(provider: AIProviderDefinition) {
  try {
    await actions.run(`ai-models-${provider.id}`, async () => {
      const result = await api<{ models: string[] }>('/settings/ai/models', {
        method: 'POST',
        body: JSON.stringify(requestBody(provider)),
        headers: { 'Content-Type': 'application/json' },
      })
      modelOptions[provider.id] = result.models || []
      ui.toast(result.models?.length ? `${provider.label} 返回 ${result.models.length} 个模型` : `${provider.label} 没有返回可用模型`, 'info')
    })
  } catch (error) {
    ui.toast(error instanceof Error ? error.message : '模型列表读取失败', 'error')
  }
}

async function testConnection(provider: AIProviderDefinition) {
  testResults[provider.id] = null
  try {
    await actions.run(`ai-test-${provider.id}`, async () => {
      testResults[provider.id] = await api<AIConnectionResult>('/settings/ai/test', {
        method: 'POST',
        body: JSON.stringify(requestBody(provider)),
        headers: { 'Content-Type': 'application/json' },
      })
    })
  } catch (error) {
    testResults[provider.id] = {
      provider: provider.id,
      provider_label: provider.label,
      format: providerFormat(provider).value,
      model: props.form[provider.modelKey] || '',
      connected: false,
      detail: error instanceof Error ? error.message : '连接测试失败',
      latency_ms: 0,
      checked_at: new Date().toISOString(),
    }
  }
}
</script>

<template>
  <div class="grid gap-5">
    <section class="rounded-2xl border border-[var(--line)] bg-[var(--brand-soft)] p-4 sm:p-5">
      <div class="grid gap-3 sm:grid-cols-[auto_minmax(0,1fr)] sm:items-start">
        <span class="grid h-10 w-10 shrink-0 place-items-center rounded-xl bg-[var(--surface-solid)] text-[var(--brand)]"><Sparkles :size="19"/></span>
        <div class="min-w-0">
          <h4 class="font-black">选择当前生效的 AI 服务</h4>
          <p class="muted mt-1 text-sm leading-6">读取模型和 hi 测试会直接使用当前表单，无需先保存；空白 Key 会沿用已保存凭据。只有 AI 助手使用当前配置前才需要保存。</p>
        </div>
      </div>
    </section>

    <section
      v-for="provider in providers"
      :key="provider.id"
      class="overflow-hidden rounded-2xl border p-4 transition sm:p-5"
      :class="activeProvider===provider.id?'border-[var(--brand)] bg-[var(--brand-soft)] shadow-sm':'border-[var(--line)] bg-[var(--surface-muted)]'"
      :data-testid="`ai-provider-${provider.id}`"
    >
      <header class="grid gap-4 border-b border-[var(--line)] pb-5 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center">
        <div class="flex min-w-0 items-start gap-3">
          <span class="grid h-11 w-11 shrink-0 place-items-center rounded-xl bg-[var(--surface-solid)] text-[var(--brand)]"><Bot :size="20"/></span>
          <div class="min-w-0">
            <p class="eyebrow">{{ provider.eyebrow }}</p>
            <h4 class="text-lg font-black">{{ provider.label }}</h4>
            <p class="muted mt-1 max-w-2xl text-sm leading-5">{{ provider.description }}</p>
          </div>
        </div>
        <button
          type="button"
          class="btn min-h-10 w-full shrink-0 sm:w-auto sm:min-w-32"
          :class="activeProvider===provider.id?'btn-primary':'btn-secondary'"
          @click="activeProvider=provider.id"
        >
          <CheckCircle2 :size="16"/>
          {{ activeProvider===provider.id ? '当前生效' : '设为当前' }}
        </button>
      </header>

      <div class="mt-5 grid gap-3 md:grid-cols-2">
        <label class="label rounded-xl border border-[var(--line)] bg-[var(--surface-solid)] p-3 sm:p-4">
          API 格式
          <select
            v-if="provider.formats.length > 1"
            class="field"
            :value="providerFormat(provider).value"
            :data-testid="`ai-format-${provider.id}`"
            @change="setProviderFormat(provider, ($event.target as HTMLSelectElement).value as AIProviderFormat)"
          >
            <option v-for="format in provider.formats" :key="format.value" :value="format.value">{{ format.label }}</option>
          </select>
          <span v-else class="field flex items-center">{{ providerFormat(provider).label }}</span>
          <span class="text-xs font-normal leading-5 muted">{{ providerFormat(provider).description }}</span>
        </label>
        <div class="label rounded-xl border border-[var(--line)] bg-[var(--surface-solid)] p-3 sm:p-4">
          凭据类型
          <div class="field flex items-center font-bold">{{ provider.credentialLabel }}</div>
          <span class="text-xs font-normal leading-5 muted">{{ provider.credentialHint }}</span>
        </div>
      </div>

      <div class="mt-4 rounded-xl border border-[var(--line)] bg-[var(--surface-solid)] p-3 sm:p-4">
        <label class="label">
          自定义 API Base URL
          <input v-model="form[provider.baseURLKey]" class="field" type="text" :placeholder="providerFormat(provider).defaultBaseURL" autocomplete="off"/>
          <span class="text-xs font-normal leading-5 muted">{{ providerFormat(provider).protocolHint }}。只填写 API 根地址，不要把具体聊天路径重复写进去。</span>
        </label>
        <div class="mt-4 grid gap-4 md:grid-cols-2">
          <label class="label content-start">
            API Key
            <input v-model="form[provider.apiKeyKey]" class="field" type="password" autocomplete="new-password" data-1p-ignore="true" placeholder="留空表示继续使用已保存的 Key"/>
            <span v-if="configured[provider.apiKeyKey]" class="flex items-center gap-1 text-xs font-normal text-[var(--success)]"><KeyRound :size="12"/>凭据已安全保存</span>
          </label>
          <label class="label content-start">
            模型 ID
            <input v-model="form[provider.modelKey]" class="field" type="text" placeholder="读取模型列表后选择，或手动填写供应商模型 ID" autocomplete="off"/>
          </label>
        </div>
      </div>

      <section class="mt-4 rounded-xl border border-[var(--line)] bg-[var(--surface-solid)] p-3 sm:p-4">
        <div class="grid gap-3 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center">
          <div class="min-w-0">
            <strong class="block">模型与连接测试</strong>
            <p class="muted mt-1 text-xs leading-5">读取和测试都使用上方尚未保存的输入值。</p>
          </div>
          <div class="grid gap-2 sm:flex sm:flex-wrap sm:justify-end">
            <AsyncButton class="btn btn-secondary w-full sm:w-auto" :loading="actions.isBusy(`ai-models-${provider.id}`)" loading-label="读取中…" @click="loadModels(provider)">
              <RefreshCw :size="16"/>读取模型列表
            </AsyncButton>
            <AsyncButton class="btn btn-primary w-full sm:w-auto" :loading="actions.isBusy(`ai-test-${provider.id}`)" loading-label="正在发送 hi…" @click="testConnection(provider)">
              <Send :size="16"/>用 hi 测试连接
            </AsyncButton>
          </div>
        </div>

        <div v-if="modelOptions[provider.id].length" class="mt-4 border-t border-[var(--line)] pt-4">
          <div class="mb-3 flex flex-wrap items-center justify-between gap-2">
            <span class="text-xs font-bold muted">可用模型</span>
            <span class="badge">{{ modelOptions[provider.id].length }} 个</span>
          </div>
          <div class="grid max-h-48 gap-2 overflow-y-auto pr-1 sm:grid-cols-2">
            <button
              v-for="modelName in modelOptions[provider.id]"
              :key="modelName"
              type="button"
              class="flex min-h-11 min-w-0 items-center gap-2 rounded-xl border px-3 py-2 text-left text-xs font-bold transition"
              :class="form[provider.modelKey]===modelName?'border-[var(--brand)] bg-[var(--brand-soft)] text-[var(--brand-strong)]':'border-[var(--line)] bg-[var(--surface-muted)] text-[var(--ink-muted)] hover:border-[var(--brand)]'"
              @click="form[provider.modelKey]=modelName"
            >
              <span class="min-w-0 flex-1 break-all">{{ modelName }}</span>
              <CheckCircle2 v-if="form[provider.modelKey]===modelName" class="shrink-0" :size="15"/>
            </button>
          </div>
        </div>

        <div
          v-if="testResults[provider.id]"
          class="mt-4 border-t border-[var(--line)] pt-4 text-sm"
          :data-testid="`ai-test-result-${provider.id}`"
        >
          <div class="rounded-xl border border-[var(--line)] bg-[var(--surface-muted)] p-3 sm:p-4">
            <div class="flex flex-wrap items-center justify-between gap-2">
              <strong :class="testResults[provider.id]?.connected?'text-[var(--success)]':'text-[var(--danger)]'">
                {{ testResults[provider.id]?.connected ? '连接成功' : '连接失败' }}
              </strong>
              <span class="badge">{{ testResults[provider.id]?.latency_ms }} ms</span>
            </div>
            <p class="muted mt-1 leading-5">{{ testResults[provider.id]?.detail }}</p>
            <p v-if="testResults[provider.id]?.reply" class="mt-3 break-words rounded-lg bg-[var(--surface-solid)] px-3 py-2">
              <span class="muted">模型回复：</span>{{ testResults[provider.id]?.reply }}
            </p>
          </div>
        </div>
      </section>
    </section>
  </div>
</template>
