<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import {
  Bot,
  LoaderCircle,
  Maximize2,
  Minimize2,
  MoveDiagonal2,
  RotateCcw,
  Send,
  Settings2,
  Sparkles,
  Trash2,
  X,
} from '@lucide/vue'
import { useAssistantStore } from '../stores/assistant'
import { useSessionStore } from '../stores/session'
import { useUIStore } from '../stores/ui'
import SafeMarkdown from './SafeMarkdown.vue'

const SAFE = 24
const PET_SIZE = 56
const MIN_WIDTH = 340
const MIN_HEIGHT = 420
const DRAG_THRESHOLD = 4

const assistant = useAssistantStore()
const session = useSessionStore()
const ui = useUIStore()
const route = useRoute()
const isMobile = ref(false)
const pet = ref<HTMLButtonElement | null>(null)
const scroll = ref<HTMLElement | null>(null)
const composer = ref<HTMLTextAreaElement | null>(null)
let mediaQuery: MediaQueryList | null = null
let suppressPetClick = false

type PointerOperation = {
  kind: 'pet' | 'panel' | 'resize'
  pointerId: number
  startX: number
  startY: number
  originX: number
  originY: number
  originWidth: number
  originHeight: number
  moved: boolean
}

let pointerOperation: PointerOperation | null = null

const desktopStyle = computed(() => ({
  left: `${assistant.desktopLayout.x}px`,
  top: `${assistant.desktopLayout.y}px`,
  width: `${assistant.desktopLayout.width}px`,
  height: `${assistant.desktopLayout.height}px`,
}))
const petStyle = computed(() => {
  const position = isMobile.value ? assistant.mobilePosition : {
    x: assistant.desktopLayout.bubbleX,
    y: assistant.desktopLayout.bubbleY,
  }
  return { left: `${position.x}px`, top: `${position.y}px` }
})
const mobilePanelStyle = computed(() => ({
  height: assistant.mobileSize === 'full'
    ? 'calc(100dvh - env(safe-area-inset-top, 0px))'
    : 'min(70dvh, 720px)',
}))
const statusLabel = computed(() => {
  if (session.setupPending) return '完成初始化后可用'
  return assistant.status?.configured
    ? `${assistant.status.provider_label} · ${assistant.status.model}`
    : 'AI 未配置'
})

function clamp(value: number, minimum: number, maximum: number) {
  return Math.min(Math.max(value, minimum), Math.max(minimum, maximum))
}

function sameLayout(next: Record<string, unknown>, current: Record<string, unknown>) {
  return Object.entries(next).every(([key, value]) => current[key] === value)
}

function clampDesktopLayout() {
  const viewportWidth = window.innerWidth
  const viewportHeight = window.innerHeight
  const current = assistant.desktopLayout
  const maxWidth = Math.max(240, viewportWidth - SAFE * 2)
  const maxHeight = Math.max(280, viewportHeight - SAFE * 2)
  const width = clamp(current.width, Math.min(MIN_WIDTH, maxWidth), maxWidth)
  const height = clamp(current.height, Math.min(MIN_HEIGHT, maxHeight), maxHeight)
  const maximized = current.maximized
  const x = maximized
    ? SAFE
    : clamp(current.x < 0 ? viewportWidth - width - SAFE : current.x, SAFE, viewportWidth - width - SAFE)
  const y = maximized
    ? SAFE
    : clamp(current.y < 0 ? viewportHeight - height - SAFE : current.y, SAFE, viewportHeight - height - SAFE)
  const bubbleX = clamp(
    current.bubbleX < 0 ? viewportWidth - PET_SIZE - SAFE : current.bubbleX,
    SAFE,
    viewportWidth - PET_SIZE - SAFE,
  )
  const bubbleY = clamp(
    current.bubbleY < 0 ? viewportHeight - PET_SIZE - SAFE : current.bubbleY,
    SAFE,
    viewportHeight - PET_SIZE - SAFE,
  )
  const next = {
    x,
    y,
    width: maximized ? viewportWidth - SAFE * 2 : width,
    height: maximized ? viewportHeight - SAFE * 2 : height,
    bubbleX,
    bubbleY,
  }
  if (!sameLayout(next, current as unknown as Record<string, unknown>)) assistant.updateDesktopLayout(next)
}

function clampMobilePosition() {
  const minimumY = 84
  const maximumY = window.innerHeight - PET_SIZE - 84
  const current = assistant.mobilePosition
  const next = {
    x: clamp(current.x < 0 ? window.innerWidth - PET_SIZE - 14 : current.x, 12, window.innerWidth - PET_SIZE - 12),
    y: clamp(current.y < 0 ? maximumY : current.y, minimumY, maximumY),
  }
  if (next.x !== current.x || next.y !== current.y) assistant.updateMobilePosition(next)
}

function syncViewport() {
  isMobile.value = Boolean(mediaQuery?.matches)
  if (isMobile.value) clampMobilePosition()
  else clampDesktopLayout()
}

function isFullPlayerPath(path: string) {
  return path === '/player' || path === '/media/local-player' || path.startsWith('/media/play/')
}

async function scrollToLatest(behavior: ScrollBehavior = 'smooth') {
  await nextTick()
  scroll.value?.scrollTo?.({ top: scroll.value.scrollHeight, behavior })
}

async function expand() {
  if (isMobile.value) clampMobilePosition()
  else clampDesktopLayout()
  assistant.expand()
  if (!session.setupPending && assistant.hydrated) void assistant.refreshStatus().catch(() => undefined)
  await nextTick()
  composer.value?.focus()
  await scrollToLatest('auto')
}

async function collapseAndFocus() {
  assistant.collapse()
  await nextTick()
  pet.value?.focus()
}

async function submit() {
  if (!assistant.input.trim() || assistant.sending || !assistant.configured || session.setupPending) return
  try {
    const pending = assistant.send()
    await scrollToLatest()
    await pending
  } catch (error) {
    ui.toast(error instanceof Error ? error.message : 'AI 请求失败', 'error')
  } finally {
    await scrollToLatest()
  }
}

async function clearConversation() {
  try {
    await assistant.clear()
    ui.toast('对话已清空')
  } catch (error) {
    ui.toast(error instanceof Error ? error.message : '清空对话失败', 'error')
  }
}

function startPointer(event: PointerEvent, kind: PointerOperation['kind']) {
  if (event.button !== 0) return
  if (kind === 'panel' && (event.target as Element).closest('button, a, textarea')) return
  const current = assistant.desktopLayout
  const petPosition = isMobile.value ? assistant.mobilePosition : { x: current.bubbleX, y: current.bubbleY }
  pointerOperation = {
    kind,
    pointerId: event.pointerId,
    startX: event.clientX,
    startY: event.clientY,
    originX: kind === 'pet' ? petPosition.x : current.x,
    originY: kind === 'pet' ? petPosition.y : current.y,
    originWidth: current.width,
    originHeight: current.height,
    moved: false,
  }
  ;(event.currentTarget as HTMLElement).setPointerCapture(event.pointerId)
}

function movePointer(event: PointerEvent) {
  const operation = pointerOperation
  if (!operation || operation.pointerId !== event.pointerId) return
  const deltaX = event.clientX - operation.startX
  const deltaY = event.clientY - operation.startY
  if (Math.hypot(deltaX, deltaY) >= DRAG_THRESHOLD) operation.moved = true
  if (operation.kind === 'pet') {
    if (isMobile.value) {
      assistant.updateMobilePosition({
        x: clamp(operation.originX + deltaX, 12, window.innerWidth - PET_SIZE - 12),
        y: clamp(operation.originY + deltaY, 84, window.innerHeight - PET_SIZE - 84),
      })
    } else {
      assistant.updateDesktopLayout({
        bubbleX: clamp(operation.originX + deltaX, SAFE, window.innerWidth - PET_SIZE - SAFE),
        bubbleY: clamp(operation.originY + deltaY, SAFE, window.innerHeight - PET_SIZE - SAFE),
      })
    }
    return
  }
  if (operation.kind === 'panel') {
    assistant.updateDesktopLayout({
      x: clamp(operation.originX + deltaX, SAFE, window.innerWidth - assistant.desktopLayout.width - SAFE),
      y: clamp(operation.originY + deltaY, SAFE, window.innerHeight - assistant.desktopLayout.height - SAFE),
    })
    return
  }
  assistant.updateDesktopLayout({
    width: clamp(
      operation.originWidth + deltaX,
      Math.min(MIN_WIDTH, window.innerWidth - SAFE * 2),
      window.innerWidth - operation.originX - SAFE,
    ),
    height: clamp(
      operation.originHeight + deltaY,
      Math.min(MIN_HEIGHT, window.innerHeight - SAFE * 2),
      window.innerHeight - operation.originY - SAFE,
    ),
  })
}

function endPointer(event: PointerEvent) {
  if (!pointerOperation || pointerOperation.pointerId !== event.pointerId) return
  if (pointerOperation.kind === 'pet') suppressPetClick = pointerOperation.moved
  pointerOperation = null
  const target = event.currentTarget as HTMLElement
  if (target.hasPointerCapture(event.pointerId)) target.releasePointerCapture(event.pointerId)
}

function petClick() {
  if (suppressPetClick) {
    suppressPetClick = false
    return
  }
  void expand()
}

function toggleMaximize() {
  const current = assistant.desktopLayout
  if (current.maximized) {
    const restore = current.restore || { x: -1, y: -1, width: 420, height: 600 }
    assistant.updateDesktopLayout({ ...restore, maximized: false, restore: undefined })
    clampDesktopLayout()
    return
  }
  assistant.updateDesktopLayout({
    restore: { x: current.x, y: current.y, width: current.width, height: current.height },
    x: SAFE,
    y: SAFE,
    width: window.innerWidth - SAFE * 2,
    height: window.innerHeight - SAFE * 2,
    maximized: true,
  })
}

function resetDesktop() {
  assistant.resetDesktopLayout()
  clampDesktopLayout()
}

function handleEscape(event: KeyboardEvent) {
  if (event.key === 'Escape' && assistant.open) void collapseAndFocus()
}

watch(() => route.path, path => {
  if (isFullPlayerPath(path) && assistant.open) assistant.collapse()
}, { immediate: true })
watch(() => session.setupPending, pending => {
  if (!pending) void assistant.hydrate()
})
watch(() => [assistant.messages.length, assistant.sending, assistant.open], () => {
  if (assistant.open) void scrollToLatest()
})

onMounted(() => {
  mediaQuery = window.matchMedia('(max-width: 767px)')
  mediaQuery.addEventListener('change', syncViewport)
  window.addEventListener('resize', syncViewport)
  window.addEventListener('keydown', handleEscape)
  syncViewport()
  if (!session.setupPending) {
    void assistant.hydrate().then(() => {
      if (assistant.open) void scrollToLatest('auto')
    })
  }
})

onBeforeUnmount(() => {
  mediaQuery?.removeEventListener('change', syncViewport)
  window.removeEventListener('resize', syncViewport)
  window.removeEventListener('keydown', handleEscape)
})
</script>

<template>
  <button
    v-if="!assistant.open"
    ref="pet"
    type="button"
    class="assistant-pet fixed z-[60] grid h-14 w-14 touch-none place-items-center rounded-[1.35rem] border border-white/65 bg-gradient-to-br from-[var(--brand)] to-[var(--brand-strong)] text-white shadow-2xl"
    :class="{ 'assistant-pet-thinking': assistant.sending }"
    :style="petStyle"
    aria-label="打开 AI 助手"
    @pointerdown="startPointer($event, 'pet')"
    @pointermove="movePointer"
    @pointerup="endPointer"
    @pointercancel="endPointer"
    @click="petClick"
  >
    <Bot :size="25" />
    <Sparkles class="assistant-pet-sparkle absolute -right-1 -top-1 rounded-full bg-[var(--surface-solid)] p-1 text-[var(--brand)] shadow" :size="19" />
    <span v-if="assistant.unread" class="absolute -left-1 -top-1 h-3.5 w-3.5 rounded-full border-2 border-[var(--surface-solid)] bg-[var(--warning)]" aria-label="有未读回复"></span>
  </button>

  <section
    v-else-if="!isMobile"
    class="glass fixed z-[60] grid min-h-0 grid-cols-[minmax(0,1fr)] grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden rounded-[1.5rem] shadow-2xl"
    :style="desktopStyle"
    role="dialog"
    aria-label="AI 助手"
    aria-modal="false"
  >
    <header
      class="flex touch-none select-none items-center gap-3 border-b border-[var(--line)] bg-[var(--surface-solid)] px-3 py-2.5"
      :class="assistant.desktopLayout.maximized ? '' : 'cursor-move'"
      @pointerdown="!assistant.desktopLayout.maximized && startPointer($event, 'panel')"
      @pointermove="movePointer"
      @pointerup="endPointer"
      @pointercancel="endPointer"
    >
      <span class="relative grid h-10 w-10 shrink-0 place-items-center rounded-xl bg-[var(--brand-soft)] text-[var(--brand)]">
        <Bot :size="20" />
        <span v-if="assistant.sending" class="absolute inset-0 animate-ping rounded-xl border border-[var(--brand)]"></span>
      </span>
      <div class="min-w-0 flex-1">
        <strong class="block truncate text-sm">AnimateTool 助手</strong>
        <span class="muted block truncate text-[.68rem]">{{ statusLabel }}</span>
      </div>
      <div class="flex items-center gap-1">
        <button class="btn btn-quiet h-9 min-h-9 w-9 p-0" type="button" title="恢复默认尺寸" aria-label="恢复默认尺寸" @click="resetDesktop"><RotateCcw :size="16" /></button>
        <button class="btn btn-quiet h-9 min-h-9 w-9 p-0" type="button" :title="assistant.desktopLayout.maximized ? '还原窗口' : '最大化'" :aria-label="assistant.desktopLayout.maximized ? '还原窗口' : '最大化'" @click="toggleMaximize"><Minimize2 v-if="assistant.desktopLayout.maximized" :size="16" /><Maximize2 v-else :size="16" /></button>
        <button class="btn btn-quiet h-9 min-h-9 w-9 p-0" type="button" title="清空对话" aria-label="清空对话" :disabled="assistant.clearing" @click="clearConversation"><LoaderCircle v-if="assistant.clearing" class="animate-spin" :size="16" /><Trash2 v-else :size="16" /></button>
        <button class="btn btn-quiet h-9 min-h-9 w-9 p-0" type="button" title="收起" aria-label="收起 AI 助手" @click="collapseAndFocus"><X :size="17" /></button>
      </div>
    </header>

    <div class="flex min-h-0 flex-col">
      <div v-if="session.setupPending" class="shrink-0 border-b border-[var(--line)] bg-[var(--brand-soft)] p-3 text-xs leading-5">
        <div class="flex items-start gap-2">
          <Sparkles class="mt-0.5 shrink-0 text-[var(--brand)]" :size="17" />
          <div><strong>AI 助手正在等待初始化完成</strong><p class="muted mt-0.5">完成当前初始化流程后，助手会自动连接并恢复对话。</p></div>
        </div>
      </div>
      <div v-else-if="assistant.status && !assistant.configured" class="shrink-0 border-b border-[var(--line)] bg-[var(--warning-soft)] p-3 text-xs leading-5">
        <div class="flex items-start gap-2">
          <Settings2 class="mt-0.5 shrink-0 text-[var(--warning)]" :size="17" />
          <div>
            <strong>AI 助手尚未配置</strong>
            <p class="muted mt-0.5">先选择服务商、填写 API Key 和模型，再用 hi 测试连接。</p>
            <RouterLink class="mt-2 inline-flex font-bold text-[var(--brand-strong)]" to="/settings?focus=ai">打开 AI 设置</RouterLink>
          </div>
        </div>
      </div>

      <div ref="scroll" class="min-h-0 flex-1 overflow-y-auto overscroll-contain p-4">
        <div class="space-y-4">
          <article v-if="assistant.messages.length === 0 && !assistant.hydrating" class="flex gap-2.5">
            <span class="grid h-8 w-8 shrink-0 place-items-center rounded-xl bg-[var(--brand-soft)] text-[var(--brand)]"><Bot :size="17" /></span>
            <div class="max-w-[85%] rounded-2xl bg-[var(--surface-muted)] px-3.5 py-2.5 text-sm leading-6">你好，我可以帮助检查订阅、分析健康问题，或者解释媒体库状态。</div>
          </article>
          <article v-for="(message, index) in assistant.messages" :key="`${index}-${message.role}`" class="flex gap-2.5" :class="message.role === 'user' ? 'justify-end' : ''">
            <span v-if="message.role === 'assistant'" class="grid h-8 w-8 shrink-0 place-items-center rounded-xl bg-[var(--brand-soft)] text-[var(--brand)]"><Bot :size="17" /></span>
            <div class="max-w-[85%] overflow-hidden rounded-2xl px-3.5 py-2.5 text-sm leading-6" :class="message.role === 'user' ? 'whitespace-pre-wrap bg-[var(--brand)] text-white' : 'bg-[var(--surface-muted)]'">
              <SafeMarkdown v-if="message.role === 'assistant'" :content="message.content" />
              <template v-else>{{ message.content }}</template>
            </div>
          </article>
          <div v-if="assistant.hydrating || assistant.sending" class="flex items-center gap-2 text-xs font-bold muted" role="status" aria-live="polite">
            <Sparkles class="animate-pulse text-[var(--brand)]" :size="17" />{{ assistant.hydrating ? '正在恢复对话…' : '正在读取系统上下文并思考…' }}
          </div>
          <div v-if="assistant.error" class="rounded-xl bg-[var(--danger-soft)] p-3 text-xs leading-5 text-[var(--danger)]" role="alert">
            <strong class="block">AI 请求未完成</strong>
            <span class="mt-1 block font-semibold">{{ assistant.error }}</span>
          </div>
        </div>
      </div>
    </div>

    <form class="w-full min-w-0 max-w-full overflow-hidden border-t border-[var(--line)] bg-[var(--surface-solid)] p-3" @submit.prevent="submit">
      <div class="flex min-w-0 items-end gap-2 rounded-2xl border border-[var(--line)] bg-[var(--surface-muted)] p-1.5">
        <textarea ref="composer" v-model="assistant.input" rows="1" class="max-h-32 min-h-11 min-w-0 flex-1 resize-none bg-transparent px-2.5 py-2.5 text-sm outline-none" placeholder="问问订阅、下载或媒体库…" @keydown.enter.exact.prevent="submit"></textarea>
        <button class="btn btn-primary h-11 min-h-11 w-11 shrink-0 p-0" type="submit" :disabled="!assistant.input.trim() || assistant.sending || !assistant.configured || session.setupPending" aria-label="发送消息"><LoaderCircle v-if="assistant.sending" class="animate-spin" :size="18" /><Send v-else :size="18" /></button>
      </div>
      <p class="muted mt-2 text-center text-[.64rem]">AI 可能会出错；修改操作仍需进入业务页面预览并确认。</p>
    </form>

    <button
      v-if="!assistant.desktopLayout.maximized"
      type="button"
      class="absolute bottom-1 right-1 grid h-8 w-8 touch-none place-items-center rounded-lg text-[var(--ink-muted)]"
      aria-label="调整助手窗口大小"
      @pointerdown="startPointer($event, 'resize')"
      @pointermove="movePointer"
      @pointerup="endPointer"
      @pointercancel="endPointer"
    >
      <MoveDiagonal2 :size="16" />
    </button>
  </section>

  <div v-else class="fixed inset-0 z-[60]">
    <button type="button" class="absolute inset-0 bg-black/45 backdrop-blur-[2px]" aria-label="收起 AI 助手" @click="collapseAndFocus"></button>
    <section
      class="glass absolute inset-x-0 bottom-0 grid min-h-0 grid-cols-[minmax(0,1fr)] grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden rounded-t-[1.7rem] shadow-2xl"
      :style="mobilePanelStyle"
      role="dialog"
      aria-modal="true"
      aria-label="AI 助手"
    >
      <header class="flex items-center gap-3 border-b border-[var(--line)] bg-[var(--surface-solid)] px-4 py-3">
        <span class="relative grid h-10 w-10 shrink-0 place-items-center rounded-xl bg-[var(--brand-soft)] text-[var(--brand)]"><Bot :size="20" /><span v-if="assistant.sending" class="absolute inset-0 animate-ping rounded-xl border border-[var(--brand)]"></span></span>
        <div class="min-w-0 flex-1"><strong class="block truncate text-sm">AnimateTool 助手</strong><span class="muted block truncate text-[.68rem]">{{ statusLabel }}</span></div>
        <button class="btn btn-quiet h-10 min-h-10 w-10 p-0" type="button" :aria-label="assistant.mobileSize === 'full' ? '切换到半屏' : '切换到全屏'" @click="assistant.setMobileSize(assistant.mobileSize === 'full' ? 'half' : 'full')"><Minimize2 v-if="assistant.mobileSize === 'full'" :size="18" /><Maximize2 v-else :size="18" /></button>
        <button class="btn btn-quiet h-10 min-h-10 w-10 p-0" type="button" aria-label="清空对话" :disabled="assistant.clearing" @click="clearConversation"><LoaderCircle v-if="assistant.clearing" class="animate-spin" :size="17" /><Trash2 v-else :size="17" /></button>
        <button class="btn btn-quiet h-10 min-h-10 w-10 p-0" type="button" aria-label="收起 AI 助手" @click="collapseAndFocus"><X :size="18" /></button>
      </header>

      <div class="flex min-h-0 flex-col">
        <div v-if="session.setupPending" class="shrink-0 border-b border-[var(--line)] bg-[var(--brand-soft)] p-3 text-xs leading-5">
          <div class="flex items-start gap-2"><Sparkles class="mt-0.5 shrink-0 text-[var(--brand)]" :size="17" /><div><strong>AI 助手正在等待初始化完成</strong><p class="muted mt-0.5">完成初始化后，助手会自动连接并恢复对话。</p></div></div>
        </div>
        <div v-else-if="assistant.status && !assistant.configured" class="shrink-0 border-b border-[var(--line)] bg-[var(--warning-soft)] p-3 text-xs leading-5">
          <div class="flex items-start gap-2"><Settings2 class="mt-0.5 shrink-0 text-[var(--warning)]" :size="17" /><div><strong>AI 助手尚未配置</strong><p class="muted mt-0.5">请先完成服务商、API Key 和模型配置。</p><RouterLink class="mt-1 inline-flex font-bold text-[var(--brand-strong)]" to="/settings?focus=ai">打开 AI 设置</RouterLink></div></div>
        </div>

        <div ref="scroll" class="min-h-0 flex-1 overflow-y-auto overscroll-contain p-4 pb-[max(1rem,env(safe-area-inset-bottom))]">
          <div class="space-y-4">
            <article v-if="assistant.messages.length === 0 && !assistant.hydrating" class="flex gap-2.5"><span class="grid h-8 w-8 shrink-0 place-items-center rounded-xl bg-[var(--brand-soft)] text-[var(--brand)]"><Bot :size="17" /></span><div class="max-w-[85%] rounded-2xl bg-[var(--surface-muted)] px-3.5 py-2.5 text-sm leading-6">你好，我可以帮助检查订阅、分析健康问题，或者解释媒体库状态。</div></article>
            <article v-for="(message, index) in assistant.messages" :key="`mobile-${index}-${message.role}`" class="flex gap-2.5" :class="message.role === 'user' ? 'justify-end' : ''">
              <span v-if="message.role === 'assistant'" class="grid h-8 w-8 shrink-0 place-items-center rounded-xl bg-[var(--brand-soft)] text-[var(--brand)]"><Bot :size="17" /></span>
              <div class="max-w-[85%] overflow-hidden rounded-2xl px-3.5 py-2.5 text-sm leading-6" :class="message.role === 'user' ? 'whitespace-pre-wrap bg-[var(--brand)] text-white' : 'bg-[var(--surface-muted)]'"><SafeMarkdown v-if="message.role === 'assistant'" :content="message.content" /><template v-else>{{ message.content }}</template></div>
            </article>
            <div v-if="assistant.hydrating || assistant.sending" class="flex items-center gap-2 text-xs font-bold muted" role="status"><Sparkles class="animate-pulse text-[var(--brand)]" :size="17" />{{ assistant.hydrating ? '正在恢复对话…' : '正在读取系统上下文并思考…' }}</div>
            <div v-if="assistant.error" class="rounded-xl bg-[var(--danger-soft)] p-3 text-xs leading-5 text-[var(--danger)]" role="alert">
              <strong class="block">AI 请求未完成</strong>
              <span class="mt-1 block font-semibold">{{ assistant.error }}</span>
            </div>
          </div>
        </div>
      </div>

      <form class="w-full min-w-0 max-w-full overflow-hidden border-t border-[var(--line)] bg-[var(--surface-solid)] p-3 pb-[max(.75rem,env(safe-area-inset-bottom))]" @submit.prevent="submit">
        <div class="flex min-w-0 items-end gap-2 rounded-2xl border border-[var(--line)] bg-[var(--surface-muted)] p-1.5">
          <textarea ref="composer" v-model="assistant.input" rows="1" class="max-h-28 min-h-11 min-w-0 flex-1 resize-none bg-transparent px-2.5 py-2.5 text-sm outline-none" placeholder="问问 AnimateTool…" @keydown.enter.exact.prevent="submit"></textarea>
          <button class="btn btn-primary h-11 min-h-11 w-11 shrink-0 p-0" type="submit" :disabled="!assistant.input.trim() || assistant.sending || !assistant.configured || session.setupPending" aria-label="发送消息"><LoaderCircle v-if="assistant.sending" class="animate-spin" :size="18" /><Send v-else :size="18" /></button>
        </div>
      </form>
    </section>
  </div>
</template>

<style scoped>
.assistant-pet {
  animation: assistant-float 3.2s ease-in-out infinite;
}
.assistant-pet:hover {
  transform: translateY(-2px) rotate(-2deg);
}
.assistant-pet-thinking {
  animation: assistant-thinking 1.1s ease-in-out infinite;
}
.assistant-pet-sparkle {
  animation: assistant-sparkle 2.4s ease-in-out infinite;
}
@keyframes assistant-float {
  0%, 100% { transform: translateY(0) rotate(0); }
  50% { transform: translateY(-5px) rotate(2deg); }
}
@keyframes assistant-thinking {
  0%, 100% { box-shadow: 0 16px 38px color-mix(in srgb, var(--brand) 25%, transparent); transform: scale(1); }
  50% { box-shadow: 0 18px 52px color-mix(in srgb, var(--brand) 48%, transparent); transform: scale(1.06); }
}
@keyframes assistant-sparkle {
  0%, 100% { transform: scale(.9) rotate(0); opacity: .78; }
  50% { transform: scale(1.08) rotate(18deg); opacity: 1; }
}
@media (prefers-reduced-motion: reduce) {
  .assistant-pet,
  .assistant-pet-thinking,
  .assistant-pet-sparkle {
    animation: none;
  }
}
</style>
