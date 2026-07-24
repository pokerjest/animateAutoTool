import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import AppBackground from './AppBackground.vue'
import { useUIStore } from '../stores/ui'

const loadedSources: string[] = []

class LoadedImage {
  decoding = ''
  onload: ((event: Event) => void) | null = null
  onerror: ((event: Event) => void) | null = null
  private value = ''

  set src(value: string) {
    this.value = value
    loadedSources.push(value)
    queueMicrotask(() => this.onload?.(new Event('load')))
  }

  get src() { return this.value }
}

function response(data: unknown) {
  return Promise.resolve(new Response(JSON.stringify({ data }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
}

describe('AppBackground', () => {
  beforeEach(() => {
    localStorage.clear()
    loadedSources.length = 0
    Object.defineProperty(window, 'innerWidth', { configurable: true, writable: true, value: 390 })
    vi.stubGlobal('Image', LoadedImage)
  })

  afterEach(() => vi.unstubAllGlobals())

  it('does not request a poster while the default background is selected', async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    const pinia = createPinia()
    setActivePinia(pinia)

    const wrapper = mount(AppBackground, { global: { plugins: [pinia] } })
    await flushPromises()

    expect(fetchMock).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="anime-background"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('loads one random poster and switches between mobile, tablet, and desktop sizes', async () => {
    const fetchMock = vi.fn(() => response({ url: '/api/v1/posters/42?source=bangumi' }))
    vi.stubGlobal('fetch', fetchMock)
    const pinia = createPinia()
    setActivePinia(pinia)
    useUIStore().setBackgroundMode('anime')

    const wrapper = mount(AppBackground, { global: { plugins: [pinia] } })
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(loadedSources.at(-1)).toBe('/api/v1/posters/42?source=bangumi&width=640')
    expect(wrapper.get('[data-testid="anime-background"]').attributes('style')).toContain('width=640')

    window.innerWidth = 900
    window.dispatchEvent(new Event('resize'))
    await flushPromises()
    expect(loadedSources.at(-1)).toBe('/api/v1/posters/42?source=bangumi&width=960')

    window.innerWidth = 1440
    window.dispatchEvent(new Event('resize'))
    await flushPromises()
    expect(loadedSources.at(-1)).toBe('/api/v1/posters/42?source=bangumi&width=1280')
    expect(fetchMock).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('falls back to the default gradient when the poster cannot load', async () => {
    class FailedImage extends LoadedImage {
      override set src(value: string) {
        loadedSources.push(value)
        queueMicrotask(() => this.onerror?.(new Event('error')))
      }
    }
    vi.stubGlobal('Image', FailedImage)
    vi.stubGlobal('fetch', vi.fn(() => response({ url: '/api/v1/posters/42?source=bangumi' })))
    const pinia = createPinia()
    setActivePinia(pinia)
    useUIStore().setBackgroundMode('anime')

    const wrapper = mount(AppBackground, { global: { plugins: [pinia] } })
    await flushPromises()

    expect(wrapper.find('[data-testid="anime-background"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
