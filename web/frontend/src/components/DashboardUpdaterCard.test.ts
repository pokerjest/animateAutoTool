import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { createPinia, setActivePinia } from 'pinia'
import DashboardUpdaterCard from './DashboardUpdaterCard.vue'

function response(data: unknown, status = 200) {
  return Promise.resolve(new Response(JSON.stringify({ data }), { status, headers: { 'Content-Type': 'application/json' } }))
}

function mountCard() {
  const pinia = createPinia()
  setActivePinia(pinia)
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
  return mount(DashboardUpdaterCard, {
    global: {
      plugins: [pinia, [VueQueryPlugin, { queryClient }]],
      stubs: {
        ConfirmDialog: {
          props: ['open'],
          emits: ['confirm', 'update:open'],
          template: '<div v-if="open"><button data-testid="confirm-update" @click="$emit(\'confirm\')">确认</button></div>',
        },
      },
    },
  })
}

afterEach(() => {
  vi.unstubAllGlobals()
  localStorage.clear()
})

describe('DashboardUpdaterCard', () => {
  it('switches between stable and beta release catalogs', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const path = String(input)
      if (path.includes('channel=stable')) return response({
        channel: 'stable',
        current_version: 'v0.9.7',
        latest_version: 'v0.9.8',
        items: [{ version: 'v0.9.8', prerelease: false, release_url: 'https://example.test/v0.9.8', asset_available: true, newer_than_current: true }],
      })
      if (path.includes('channel=beta')) return response({
        channel: 'beta',
        current_version: 'v0.9.7',
        latest_version: 'v0.9.9-beta.1',
        items: [{ version: 'v0.9.9-beta.1', prerelease: true, release_url: 'https://example.test/v0.9.9-beta.1', asset_available: true, newer_than_current: true }],
      })
      throw new Error(`unexpected request: ${path}`)
    })
    vi.stubGlobal('fetch', fetchMock)

    const wrapper = mountCard()
    await vi.waitFor(() => expect(wrapper.text()).toContain('v0.9.8 · 稳定版'))

    await wrapper.findAll('button').find(button => button.text().includes('测试版'))!.trigger('click')
    await vi.waitFor(() => expect(wrapper.text()).toContain('v0.9.9-beta.1 · 测试版'))
    expect(localStorage.getItem('animate.updater.channel')).toBe('beta')
  })

  it('submits only the selected release version after confirmation', async () => {
    let applyBody = ''
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      if (path.includes('/api/v1/settings/updater/releases')) return response({
        channel: 'stable',
        current_version: 'v0.9.7',
        latest_version: 'v0.9.8',
        items: [{ version: 'v0.9.8', prerelease: false, release_url: 'https://example.test/v0.9.8', asset_available: true, newer_than_current: true }],
      })
      if (path.endsWith('/api/v1/settings/updater/apply') && init?.method === 'POST') {
        applyBody = String(init.body)
        return response({ task_id: 'repo-update-apply', status: 'running' }, 202)
      }
      throw new Error(`unexpected request: ${path}`)
    }))

    const wrapper = mountCard()
    await vi.waitFor(() => expect(wrapper.text()).toContain('v0.9.8 · 稳定版'))
    await wrapper.findAll('button').find(button => button.text().includes('更新到所选版本'))!.trigger('click')
    await wrapper.get('[data-testid="confirm-update"]').trigger('click')
    await flushPromises()

    expect(JSON.parse(applyBody)).toEqual({ version: 'v0.9.8' })
  })
})
