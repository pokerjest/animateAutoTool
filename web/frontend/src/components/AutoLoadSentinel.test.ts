import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import AutoLoadSentinel from './AutoLoadSentinel.vue'

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('AutoLoadSentinel', () => {
  it('waits for a slow request and retries when the sentinel remains visible', async () => {
    let onIntersect: IntersectionObserverCallback | undefined
    vi.stubGlobal('IntersectionObserver', class {
      constructor(callback: IntersectionObserverCallback) {
        onIntersect = callback
      }
      observe() {}
      disconnect() {}
      unobserve() {}
      takeRecords() { return [] }
      root = null
      rootMargin = '320px 0px'
      thresholds = [0]
    })

    const wrapper = mount(AutoLoadSentinel, { props: { remaining: 10 } })
    onIntersect!([{ isIntersecting: true } as IntersectionObserverEntry], {} as IntersectionObserver)
    await wrapper.vm.$nextTick()
    expect(wrapper.emitted('load')).toHaveLength(1)

    await wrapper.setProps({ loading: true })
    onIntersect!([{ isIntersecting: true } as IntersectionObserverEntry], {} as IntersectionObserver)
    expect(wrapper.emitted('load')).toHaveLength(1)

    await wrapper.setProps({ loading: false })
    await wrapper.vm.$nextTick()
    expect(wrapper.emitted('load')).toHaveLength(2)
  })
})
