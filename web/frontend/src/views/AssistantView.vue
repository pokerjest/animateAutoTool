<script setup lang="ts">
import { nextTick, onMounted, ref } from 'vue'
import { Bot, Send, Settings2, Sparkles, Trash2 } from '@lucide/vue'
import { api } from '../api/client'
import AsyncButton from '../components/AsyncButton.vue'
import PageHeader from '../components/PageHeader.vue'
import { useAsyncActions } from '../composables/useAsyncActions'
import { useUIStore } from '../stores/ui'

interface Message {
  role: 'user' | 'assistant'
  content: string
}

interface AIStatus {
  provider: 'openai' | 'gemini' | 'claude'
  provider_label: string
  configured: boolean
  model: string
}

const ui = useUIStore()
const actions = useAsyncActions()
const messages = ref<Message[]>([{ role: 'assistant', content: '你好，我可以帮助检查订阅、解释媒体库状态，或给出下一步建议。' }])
const input = ref('')
const scroll = ref<HTMLElement | null>(null)
const status = ref<AIStatus | null>(null)

onMounted(async () => {
  try {
    status.value = await api<AIStatus>('/settings/ai')
  } catch {
    status.value = null
  }
})

async function send() {
  const value = input.value.trim()
  if (!value || actions.isBusy('send')) return
  messages.value.push({ role: 'user', content: value })
  input.value = ''
  await nextTick()
  scroll.value?.scrollTo({ top: scroll.value.scrollHeight, behavior: 'smooth' })
  try {
    await actions.run('send', async () => {
      const result = await api<{ message: string }>('/assistant/messages', {
        method: 'POST',
        body: JSON.stringify({ message: value }),
        headers: { 'Content-Type': 'application/json' },
      })
      messages.value.push({ role: 'assistant', content: result.message })
    })
  } catch (error) {
    ui.toast(error instanceof Error ? error.message : 'AI 请求失败', 'error')
  } finally {
    await nextTick()
    scroll.value?.scrollTo({ top: scroll.value.scrollHeight, behavior: 'smooth' })
  }
}

async function clear() {
  try {
    await actions.run('clear', async () => {
      await api('/assistant/messages', { method: 'DELETE' })
      messages.value = [{ role: 'assistant', content: '对话已清空。需要我从哪里开始？' }]
      ui.toast('对话已清空')
    })
  } catch (error) {
    ui.toast(error instanceof Error ? error.message : '清空对话失败', 'error')
  }
}
</script>

<template>
  <div class="page-grid h-[calc(100vh-8.5rem)] min-h-[620px]">
    <PageHeader eyebrow="ASSISTANT" title="AI 助手" description="结合当前系统能力协助诊断和管理；涉及数据变更时仍会明确提示。">
      <div class="flex flex-wrap items-center justify-end gap-2">
        <span v-if="status?.configured" class="badge badge-success max-w-full">
          {{ status.provider_label }} · {{ status.model }}
        </span>
        <AsyncButton class="btn btn-secondary" :loading="actions.isBusy('clear')" loading-label="清空中…" @click="clear">
          <Trash2 :size="16"/>清空对话
        </AsyncButton>
      </div>
    </PageHeader>

    <section v-if="status && !status.configured" class="rounded-2xl border border-[var(--warning)] bg-[var(--warning-soft)] p-4 text-sm leading-6">
      <div class="flex items-start gap-3">
        <Settings2 class="mt-1 shrink-0 text-[var(--warning)]" :size="19"/>
        <div>
          <strong>AI 助手尚未配置完成</strong>
          <p class="muted mt-1">请先选择 OpenAI、Gemini 或 Claude，填写 API Key 和模型，并用 hi 测试连接。</p>
          <RouterLink class="btn btn-secondary mt-3" to="/settings?focus=ai">打开 AI 设置</RouterLink>
        </div>
      </div>
    </section>

    <section class="panel grid min-h-0 grid-rows-[1fr_auto] overflow-hidden">
      <div ref="scroll" class="min-h-0 overflow-y-auto p-4 sm:p-7">
        <div class="mx-auto max-w-3xl space-y-5">
          <article v-for="(message, index) in messages" :key="index" class="flex gap-3" :class="message.role==='user'?'justify-end':''">
            <span v-if="message.role==='assistant'" class="grid h-9 w-9 shrink-0 place-items-center rounded-xl bg-[var(--brand-soft)] text-[var(--brand)]"><Bot :size="18"/></span>
            <div class="max-w-[85%] whitespace-pre-wrap rounded-2xl px-4 py-3 text-sm leading-6" :class="message.role==='user'?'bg-[var(--brand)] text-white':'bg-[var(--surface-muted)]'">
              {{ message.content }}
            </div>
          </article>
          <div v-if="actions.isBusy('send')" class="flex items-center gap-3 muted" role="status">
            <Sparkles class="animate-pulse text-[var(--brand)]" :size="18"/>正在思考当前系统状态…
          </div>
        </div>
      </div>
      <form class="border-t border-[var(--line)] bg-[var(--surface-solid)] p-3 sm:p-5" @submit.prevent="send">
        <div class="mx-auto flex max-w-3xl items-end gap-2 rounded-2xl border border-[var(--line)] bg-[var(--surface-muted)] p-2">
          <textarea v-model="input" class="max-h-36 min-h-12 flex-1 resize-none bg-transparent px-3 py-3 text-sm outline-none" rows="1" placeholder="问问订阅、下载或媒体库…" @keydown.enter.exact.prevent="send"/>
          <AsyncButton type="submit" class="btn btn-primary h-11 min-h-11 px-3" :disabled="!input.trim()||status?.configured===false" :loading="actions.isBusy('send')" loading-label="发送中…" aria-label="发送消息">
            <Send :size="18"/><span class="sr-only">发送消息</span>
          </AsyncButton>
        </div>
        <p class="muted mx-auto mt-2 max-w-3xl text-center text-[.68rem]">AI 可能会出错，执行重要操作前请核实。</p>
      </form>
    </section>
  </div>
</template>
