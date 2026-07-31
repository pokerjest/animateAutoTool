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

  it('persists the anime poster background preference', () => {
    const ui = useUIStore()
    expect(ui.backgroundMode).toBe('default')

    ui.setBackgroundMode('anime')

    expect(ui.backgroundMode).toBe('anime')
    expect(localStorage.getItem('animate-background-mode')).toBe('anime')
  })

  it('persists and applies the current mascot skin without changing other preferences', () => {
    const ui = useUIStore()
    ui.setTheme('dark')
    ui.setBackgroundMode('anime')
    ui.setSkin('mascot')

    expect(ui.skin).toBe('mascot')
    expect(localStorage.getItem('animate-ui-skin')).toBe('mascot')
    expect(document.documentElement.dataset.skin).toBe('mascot')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(ui.backgroundMode).toBe('anime')
  })

  it('falls back to the classic skin for unknown stored values', () => {
    localStorage.setItem('animate-ui-skin', 'v1')
    const ui = useUIStore()

    expect(ui.skin).toBe('classic')
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
