import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useUIStore } from './ui'

describe('ui store', () => {
  beforeEach(() => { localStorage.clear(); setActivePinia(createPinia()) })

  it('persists and applies the explicit dark theme', () => {
    const ui = useUIStore()
    ui.setTheme('dark')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(localStorage.getItem('animate-theme')).toBe('dark')
  })

  it('queues a globally visible toast', () => {
    vi.useFakeTimers()
    const ui = useUIStore()
    ui.toast('保存成功')
    expect(ui.toasts[0]?.message).toBe('保存成功')
    vi.runAllTimers()
    expect(ui.toasts).toHaveLength(0)
    vi.useRealTimers()
  })
})
