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

  it('shows the actionable provider error instead of replacing it with a generic failure', async () => {
    const message = '调用大模型失败：Google Gemini 当前项目额度已达到限制（HTTP 429）；请等待至少 20 秒后重试。'
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({
      error: { code: 'ai_rate_limited', message },
    }), {
      status: 429,
      headers: { 'Content-Type': 'application/json', 'Retry-After': '20' },
    })))
    const store = useAssistantStore()

    await expect(store.send('帮我分析问题')).rejects.toMatchObject({ status: 429, message })

    expect(store.error).toBe(message)
    expect(store.messages.at(-1)).toEqual({ role: 'user', content: '帮我分析问题' })
    expect(store.sending).toBe(false)
  })
})
