import { defineStore } from 'pinia'
import { api } from '../api/client'

export interface AssistantMessage {
  role: 'user' | 'assistant'
  content: string
}

export interface AssistantStatus {
  provider: 'openai' | 'gemini' | 'claude'
  provider_label: string
  configured: boolean
  model: string
}

export interface AssistantDesktopLayout {
  x: number
  y: number
  width: number
  height: number
  bubbleX: number
  bubbleY: number
  maximized: boolean
  restore?: Pick<AssistantDesktopLayout, 'x' | 'y' | 'width' | 'height'>
}

export interface AssistantMobilePosition {
  x: number
  y: number
}

export type AssistantMobileSize = 'half' | 'full'

const layoutKey = 'animate.assistant.layout.v1'
const openKey = 'animate.assistant.open'
const mobilePositionKey = 'animate.assistant.mobile.position'
const mobileSizeKey = 'animate.assistant.mobile.size'

const defaultDesktopLayout = (): AssistantDesktopLayout => ({
  x: -1,
  y: -1,
  width: 420,
  height: 600,
  bubbleX: -1,
  bubbleY: -1,
  maximized: false,
})

function readJSON<T>(key: string, fallback: T): T {
  try {
    const value = localStorage.getItem(key)
    return value ? { ...fallback, ...JSON.parse(value) } : fallback
  } catch {
    return fallback
  }
}

function persist(key: string, value: unknown) {
  try {
    localStorage.setItem(key, typeof value === 'string' ? value : JSON.stringify(value))
  } catch {
    // Layout persistence is best effort and must never break the assistant.
  }
}

export const useAssistantStore = defineStore('assistant', {
  state: () => ({
    messages: [] as AssistantMessage[],
    input: '',
    status: null as AssistantStatus | null,
    hydrated: false,
    hydrating: false,
    sending: false,
    clearing: false,
    open: localStorage.getItem(openKey) === 'true',
    unread: false,
    error: '',
    desktopLayout: readJSON(layoutKey, defaultDesktopLayout()),
    mobilePosition: readJSON<AssistantMobilePosition>(mobilePositionKey, { x: -1, y: -1 }),
    mobileSize: (localStorage.getItem(mobileSizeKey) === 'full' ? 'full' : 'half') as AssistantMobileSize,
  }),
  getters: {
    configured: state => state.status?.configured === true,
  },
  actions: {
    async hydrate() {
      if (this.hydrated || this.hydrating) return
      this.hydrating = true
      const [statusResult, messagesResult] = await Promise.allSettled([
        api<AssistantStatus>('/settings/ai'),
        api<{ messages: AssistantMessage[] }>('/assistant/messages'),
      ])
      if (statusResult.status === 'fulfilled') this.status = statusResult.value
      if (messagesResult.status === 'fulfilled') this.messages = messagesResult.value.messages || []
      this.hydrated = true
      this.hydrating = false
    },
    async refreshStatus() {
      this.status = await api<AssistantStatus>('/settings/ai')
      return this.status
    },
    async send(value?: string) {
      const content = (value ?? this.input).trim()
      if (!content || this.sending) return
      this.messages.push({ role: 'user', content })
      this.input = ''
      this.error = ''
      this.sending = true
      try {
        const result = await api<{ message: string }>('/assistant/messages', {
          method: 'POST',
          body: JSON.stringify({ message: content }),
          headers: { 'Content-Type': 'application/json' },
        })
        this.messages.push({ role: 'assistant', content: result.message })
        if (!this.open) this.unread = true
      } catch (error) {
        this.error = error instanceof Error ? error.message : 'AI 请求失败'
        throw error
      } finally {
        this.sending = false
      }
    },
    async clear() {
      if (this.clearing) return
      this.clearing = true
      try {
        await api('/assistant/messages', { method: 'DELETE' })
        this.messages = []
        this.input = ''
        this.error = ''
        this.unread = false
      } finally {
        this.clearing = false
      }
    },
    expand() {
      this.open = true
      this.unread = false
      persist(openKey, 'true')
    },
    collapse() {
      this.open = false
      persist(openKey, 'false')
    },
    updateDesktopLayout(layout: Partial<AssistantDesktopLayout>) {
      this.desktopLayout = { ...this.desktopLayout, ...layout }
      persist(layoutKey, this.desktopLayout)
    },
    resetDesktopLayout() {
      this.desktopLayout = defaultDesktopLayout()
      persist(layoutKey, this.desktopLayout)
    },
    updateMobilePosition(position: AssistantMobilePosition) {
      this.mobilePosition = position
      persist(mobilePositionKey, position)
    },
    setMobileSize(size: AssistantMobileSize) {
      this.mobileSize = size
      persist(mobileSizeKey, size)
    },
  },
})
