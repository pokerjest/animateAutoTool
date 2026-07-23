import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { createPinia } from 'pinia'
import SettingsView from './SettingsView.vue'

function response(data: unknown) {
  return Promise.resolve(new Response(JSON.stringify({ data }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
}

afterEach(() => {
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('SettingsView proxy settings', () => {
  it('shows per-service switches and tests the current unsaved proxy address', async () => {
    let proxyTestBody: Record<string, string> | undefined
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      if (path.endsWith('/api/v1/settings') && (!init?.method || init.method === 'GET')) {
        return response({ values: { proxy_url: '', proxy_bangumi_enabled: 'false', proxy_mikan_enabled: 'false' }, configured: {}, stats: {} })
      }
      if (path.includes('/api/v1/audit-logs')) return response({ items: [] })
      if (path.endsWith('/api/v1/settings/maintenance')) return response({ deployment: { items: [] }, updater: {} })
      if (path.endsWith('/api/v1/settings/proxy/test')) {
        proxyTestBody = JSON.parse(String(init?.body)) as Record<string, string>
        return response({ connected: true, detail: '代理连接成功', protocol: 'http' })
      }
      throw new Error(`unexpected request: ${path}`)
    })
    vi.stubGlobal('fetch', fetchMock)

    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
    const wrapper = mount(SettingsView, {
      attachTo: document.body,
      global: {
        plugins: [createPinia(), [VueQueryPlugin, { queryClient }]],
        stubs: { RouterLink: { template: '<a><slot /></a>' } },
      },
    })

    await vi.waitFor(() => expect(wrapper.text()).toContain('网络代理'))
    const networkTab = wrapper.findAll('button').find(button => button.text().includes('网络代理'))
    expect(networkTab).toBeDefined()
    await networkTab!.trigger('click')

    expect(wrapper.text()).toContain('Mikan 使用代理')
    expect(wrapper.text()).toContain('AI 服务使用代理')
    expect(wrapper.text()).toContain('应用更新使用代理')

    const proxyInput = wrapper.get('input[placeholder*="http://127.0.0.1:7890"]')
    await proxyInput.setValue('127.0.0.1:7890')
    const testButton = wrapper.findAll('button').find(button => button.text().includes('测试当前代理地址'))
    expect(testButton).toBeDefined()
    await testButton!.trigger('click')
    await flushPromises()

    expect(proxyTestBody).toEqual({ proxy_url: '127.0.0.1:7890' })
    expect(wrapper.text()).toContain('代理连接成功')

    const mediaTab = wrapper.findAll('button').find(button => button.text().includes('媒体服务'))
    expect(mediaTab).toBeDefined()
    await mediaTab!.trigger('click')
    expect(wrapper.text()).toContain('Jellyfin 服务端连接地址')
    expect(wrapper.text()).toContain('浏览器直连地址（Tailscale）')
    expect(wrapper.find('input[placeholder*="example-tailnet.ts.net"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('留空时全部走 AnimateTool 代理')

    wrapper.unmount()
    queryClient.clear()
  })
})
