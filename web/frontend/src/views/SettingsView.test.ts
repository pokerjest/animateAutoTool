import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
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
    localStorage.clear()
    let proxyTestBody: Record<string, string> | undefined
    let settingsSaveBody: { values: Record<string, string> } | undefined
    let jellyfinTestURL = ''
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      if (path.endsWith('/api/v1/settings') && init?.method === 'PUT') {
        settingsSaveBody = JSON.parse(String(init.body)) as { values: Record<string, string> }
        return response(null)
      }
      if (path.endsWith('/api/v1/settings') && (!init?.method || init.method === 'GET')) {
        return response({ values: { proxy_url: '', proxy_bangumi_enabled: 'false', proxy_mikan_enabled: 'false', auth_ip_allowlist_enabled: 'false', auth_ip_allowlist: '' }, configured: {}, stats: {}, request_ip: '100.64.1.20' })
      }
      if (path.includes('/api/v1/audit-logs')) return response({ items: [] })
      if (path.endsWith('/api/v1/settings/maintenance')) return response({ deployment: { items: [] }, updater: {} })
      if (path.includes('/api/v1/settings/updater/releases')) return response({
        channel: path.includes('channel=beta') ? 'beta' : 'stable',
        current_version: 'v0.9.8',
        latest_version: path.includes('channel=beta') ? 'v0.9.9-beta.1' : 'v0.9.9',
        items: [{
          version: path.includes('channel=beta') ? 'v0.9.9-beta.1' : 'v0.9.9',
          prerelease: path.includes('channel=beta'),
          release_url: 'https://example.test/release',
          asset_available: true,
          newer_than_current: true,
        }],
      })
      if (path.endsWith('/api/v1/settings/proxy/test')) {
        proxyTestBody = JSON.parse(String(init?.body)) as Record<string, string>
        return response({ connected: true, detail: '代理连接成功', protocol: 'http' })
      }
      if (path.includes('/api/v1/settings/connections/jellyfin')) {
        jellyfinTestURL = path
        return response({ connected: true, detail: '', source: 'proxy', source_label: 'AnimateTool 代理', checked_at: new Date().toISOString() })
      }
      throw new Error(`unexpected request: ${path}`)
    })
    vi.stubGlobal('fetch', fetchMock)

    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/settings', component: SettingsView }] })
    await router.push('/settings')
    await router.isReady()
    const wrapper = mount(SettingsView, {
      attachTo: document.body,
      global: {
        plugins: [createPinia(), router, [VueQueryPlugin, { queryClient }]],
        stubs: { RouterLink: { template: '<a><slot /></a>' } },
      },
    })

    await vi.waitFor(() => expect(wrapper.text()).toContain('网络代理'))
    const downloaderTab = wrapper.findAll('button').find(button => button.text().includes('下载器'))
    expect(downloaderTab).toBeDefined()
    await downloaderTab!.trigger('click')
    expect(wrapper.text()).toContain('下载完成后自动整理')
    expect(wrapper.text()).toContain('系列文件夹模板')
    expect(wrapper.text()).toContain('剧集文件模板')
    expect(wrapper.get('[data-testid="auto-rename-help"]').text()).toContain('系列名/Season 01/系列名 - S01E01.mkv')

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
    const jellyfinPanel = wrapper.get('[data-testid="media-app-jellyfin"]')
    expect(jellyfinPanel.text()).toContain('Jellyfin')
    expect(jellyfinPanel.text()).toContain('AnimateTool 连接地址')
    expect(jellyfinPanel.text()).not.toContain('AList')
    expect(wrapper.find('[data-testid="media-app-alist"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('AList')
    expect(wrapper.text()).toContain('浏览器直连地址（可选）')
    expect(wrapper.find('input[placeholder*="example-tailnet.ts.net"]').exists()).toBe(true)
    expect(wrapper.find('input[placeholder*="127.0.0.1:8096"]').exists()).toBe(true)
    const playbackHelp = wrapper.get('[data-testid="jellyfin-playback-help"]')
    expect(playbackHelp.text()).toContain('Jellyfin 直连')
    expect(playbackHelp.text()).toContain('AnimateTool 代理')
    expect(wrapper.text()).not.toContain('NetBird')
    expect(wrapper.find('[data-testid="playback-source-settings"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="jellyfin-library-selection"]').text()).toContain('媒体模式显示的 Jellyfin 媒体库')
    expect(wrapper.text()).toContain('添加其他媒体提供商')
    const jellyfinTest = wrapper.findAll('button').find(button => button.text().includes('测试当前线路'))
    expect(jellyfinTest).toBeDefined()
    await jellyfinTest!.trigger('click')
    await flushPromises()
    expect(jellyfinTestURL).toContain('/settings/connections/jellyfin?source=proxy')

    const appearanceTab = wrapper.findAll('button').find(button => button.text() === '外观')
    expect(appearanceTab).toBeDefined()
    await appearanceTab!.trigger('click')
    expect(wrapper.text()).toContain('主题模式')
    expect(wrapper.text()).not.toContain('修改管理员密码')
    const backgroundMode = wrapper.get('[data-testid="background-mode"]')
    await backgroundMode.setValue('anime')
    expect(localStorage.getItem('animate-background-mode')).toBe('anime')
    expect(wrapper.text()).toContain('手机、平板和电脑加载 640、960、1280px')

    const securityTab = wrapper.findAll('button').find(button => button.text() === '安全')
    expect(securityTab).toBeDefined()
    await securityTab!.trigger('click')
    expect(wrapper.text()).toContain('修改管理员密码')
    expect(wrapper.text()).toContain('最近安全审计')
    expect(wrapper.text()).toContain('IP 白名单免密访问')
    expect(wrapper.text()).not.toContain('主题模式')
    const allowlistPanel = wrapper.get('[data-testid="auth-ip-allowlist"]')
    expect(allowlistPanel.text()).toContain('100.64.1.20')
    await allowlistPanel.findAll('button').find(button => button.text().includes('填入当前 IP'))!.trigger('click')
    expect((allowlistPanel.get('textarea').element as HTMLTextAreaElement).value).toBe('100.64.1.20')
    await allowlistPanel.get('input[type="checkbox"]').setValue(true)
    await allowlistPanel.get('textarea').setValue('192.168.1.20\n100.64.0.0/10')
    await allowlistPanel.findAll('button').find(button => button.text().includes('保存免密设置'))!.trigger('click')
    await flushPromises()
    expect(settingsSaveBody?.values.auth_ip_allowlist_enabled).toBe('true')
    expect(settingsSaveBody?.values.auth_ip_allowlist).toBe('192.168.1.20\n100.64.0.0/10')

    const maintenanceTab = wrapper.findAll('button').find(button => button.text() === '应用维护')
    expect(maintenanceTab).toBeDefined()
    await maintenanceTab!.trigger('click')
    await vi.waitFor(() => expect(wrapper.text()).toContain('v0.9.9 · 稳定版'))
    expect(wrapper.text()).toContain('选择要更新的版本')
    expect(wrapper.text()).toContain('稳定版')
    expect(wrapper.text()).toContain('测试版')
    expect(wrapper.text()).not.toContain('立即检查')
    await wrapper.findAll('button').find(button => button.text().includes('测试版'))!.trigger('click')
    await vi.waitFor(() => expect(wrapper.text()).toContain('v0.9.9-beta.1 · 测试版'))

    wrapper.unmount()
    queryClient.clear()
  })
})
