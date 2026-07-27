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
    localStorage.clear()
    let proxyTestBody: Record<string, string> | undefined
    let settingsSaveBody: { values: Record<string, string> } | undefined
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
    const alistPanel = wrapper.get('[data-testid="media-app-alist"]')
    expect(jellyfinPanel.text()).toContain('Jellyfin')
    expect(jellyfinPanel.text()).toContain('AnimateTool 连接地址')
    expect(jellyfinPanel.text()).not.toContain('AList')
    expect(alistPanel.text()).toContain('AList')
    expect(alistPanel.text()).toContain('服务地址')
    expect(alistPanel.text()).not.toContain('Jellyfin')
    expect(wrapper.text()).toContain('浏览器直连地址（可选）')
    expect(wrapper.find('input[placeholder*="example-tailnet.ts.net"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('AnimateTool NetBird 地址（可选）')
    expect(wrapper.find('input[placeholder*="100.90.80.70:8306"]').exists()).toBe(true)
    expect(wrapper.find('input[placeholder*="127.0.0.1:8096"]').exists()).toBe(true)
    const playbackHelp = wrapper.get('[data-testid="jellyfin-playback-help"]')
    expect(playbackHelp.text()).toContain('Jellyfin 直连')
    expect(playbackHelp.text()).toContain('NetBird 代理')
    expect(playbackHelp.text()).toContain('AnimateTool 代理')
    expect(playbackHelp.text()).toContain('HTTPS')
    expect(jellyfinPanel.text()).toContain('不向浏览器暴露 Jellyfin API Key')
    const sourceSettings = wrapper.get('[data-testid="playback-source-settings"]')
    expect(sourceSettings.text()).toContain('本设备播放线路')
    expect(sourceSettings.findAll('button')).toHaveLength(3)
    await sourceSettings.findAll('button')[2].trigger('click')
    expect(localStorage.getItem('player.preferredSource')).toBe('netbird')
    expect(wrapper.get('[data-testid="jellyfin-library-selection"]').text()).toContain('媒体模式显示的 Jellyfin 媒体库')
    expect(wrapper.text()).toContain('添加其他媒体提供商')

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

    wrapper.unmount()
    queryClient.clear()
  })
})
