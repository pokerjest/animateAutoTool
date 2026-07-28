import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import AssistantWidget from './AssistantWidget.vue'
import { useAssistantStore } from '../stores/assistant'
import { useSessionStore } from '../stores/session'

const Page = { template: '<div>页面</div>' }

function response(data: unknown) {
  return Promise.resolve(new Response(JSON.stringify({ data }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  }))
}

function matchMedia(matches: boolean) {
  return vi.fn().mockImplementation((query: string) => ({
    matches,
    media: query,
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }))
}

async function mountWidget(mobile = false, setupPending = false) {
  vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
    if (String(input).endsWith('/settings/ai')) {
      return response({ provider: 'openai', provider_label: 'OpenAI', configured: true, model: 'gpt-test' })
    }
    return response({ messages: [] })
  }))
  Object.defineProperty(window, 'matchMedia', { writable: true, value: matchMedia(mobile) })
  Object.defineProperty(HTMLElement.prototype, 'scrollTo', { configurable: true, value: vi.fn() })
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: Page },
      { path: '/player', component: Page },
    ],
  })
  const pinia = createPinia()
  if (setupPending) {
    useSessionStore(pinia).state = {
      authenticated: true,
      setup_pending: true,
    } as never
  }
  await router.push('/')
  await router.isReady()
  const wrapper = mount(AssistantWidget, { global: { plugins: [pinia, router] } })
  await flushPromises()
  return { wrapper, router, store: useAssistantStore(pinia) }
}

beforeEach(() => {
  localStorage.clear()
})

afterEach(() => {
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('AssistantWidget', () => {
  it('opens from the desktop pet and automatically collapses on a full player route', async () => {
    const mounted = await mountWidget()
    await mounted.wrapper.get('button[aria-label="打开 AI 助手"]').trigger('click')
    await flushPromises()

    expect(mounted.wrapper.get('[role="dialog"]').attributes('aria-label')).toBe('AI 助手')
    expect(mounted.store.open).toBe(true)

    await mounted.router.push('/player')
    await flushPromises()

    expect(mounted.store.open).toBe(false)
    expect(mounted.wrapper.find('[role="dialog"]').exists()).toBe(false)
    expect(mounted.wrapper.get('button[aria-label="打开 AI 助手"]').attributes('type')).toBe('button')
  })

  it('uses a half-height mobile bottom sheet and can expand it to full screen', async () => {
    const mounted = await mountWidget(true)
    await mounted.wrapper.get('button[aria-label="打开 AI 助手"]').trigger('click')
    await flushPromises()

    const dialog = mounted.wrapper.get('[role="dialog"]')
    expect(dialog.attributes('aria-modal')).toBe('true')
    expect(dialog.attributes('style')).toContain('70dvh')
    expect(dialog.classes()).toContain('grid-cols-[minmax(0,1fr)]')
    expect(dialog.get('form').classes()).toEqual(expect.arrayContaining(['w-full', 'min-w-0', 'max-w-full', 'overflow-hidden']))

    await mounted.wrapper.get('button[aria-label="切换到全屏"]').trigger('click')
    expect(mounted.store.mobileSize).toBe('full')
    expect(mounted.wrapper.get('[role="dialog"]').attributes('style')).toContain('100dvh')
  })

  it('stays visible during initialization without calling protected AI endpoints', async () => {
    const mounted = await mountWidget(false, true)
    expect(fetch).not.toHaveBeenCalled()

    await mounted.wrapper.get('button[aria-label="打开 AI 助手"]').trigger('click')
    await flushPromises()

    expect(mounted.wrapper.text()).toContain('AI 助手正在等待初始化完成')
    expect(mounted.wrapper.get('button[aria-label="发送消息"]').attributes()).toHaveProperty('disabled')
    expect(fetch).not.toHaveBeenCalled()
  })
})
