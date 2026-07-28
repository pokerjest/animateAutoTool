import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useAssistantStore } from './assistant'

function response(data: unknown) {
  return Promise.resolve(new Response(JSON.stringify({ data }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  }))
}

describe('assistant store', () => {
  beforeEach(() => {
    localStorage.clear()
    setActivePinia(createPinia())
  })

  it('hydrates displayable history without persisting messages in browser storage', async () => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      if (String(input).endsWith('/settings/ai')) {
        return response({ provider: 'gemini', provider_label: 'Google Gemini', configured: true, model: 'gemini-test' })
      }
      return response({ messages: [{ role: 'assistant', content: '恢复后的回复' }] })
    }))
    const store = useAssistantStore()

    await store.hydrate()

    expect(store.configured).toBe(true)
    expect(store.messages).toEqual([{ role: 'assistant', content: '恢复后的回复' }])
    const storage = Array.from({ length: localStorage.length }, (_, index) => localStorage.getItem(localStorage.key(index) || '')).join('')
    expect(storage).not.toContain('恢复后的回复')
  })

  it('keeps an in-flight reply across collapsed state and raises an unread marker', async () => {
    let resolveReply!: (value: Response) => void
    vi.stubGlobal('fetch', vi.fn(() => new Promise<Response>(resolve => { resolveReply = resolve })))
    const store = useAssistantStore()
    store.input = '检查健康状态'

    const pending = store.send()
    store.collapse()
    resolveReply(new Response(JSON.stringify({ data: { message: '系统正常' } }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    }))
    await pending

    expect(store.messages.at(-1)).toEqual({ role: 'assistant', content: '系统正常' })
    expect(store.unread).toBe(true)
    store.expand()
    expect(store.unread).toBe(false)
  })

  it('persists layout but never mixes it with conversation content', () => {
    const store = useAssistantStore()
    store.messages.push({ role: 'user', content: 'private media path' })
    store.updateDesktopLayout({ x: 120, y: 80, width: 500 })
    store.updateMobilePosition({ x: 20, y: 160 })
    store.setMobileSize('full')

    expect(localStorage.getItem('animate.assistant.layout.v1')).toContain('"width":500')
    expect(localStorage.getItem('animate.assistant.mobile.position')).toBe('{"x":20,"y":160}')
    expect(localStorage.getItem('animate.assistant.mobile.size')).toBe('full')
    const storage = Array.from({ length: localStorage.length }, (_, index) => localStorage.getItem(localStorage.key(index) || '')).join('')
    expect(storage).not.toContain('private media path')
  })
})
