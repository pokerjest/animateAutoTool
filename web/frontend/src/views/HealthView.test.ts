import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import HealthView from './HealthView.vue'

function response(data: unknown) {
  return Promise.resolve(new Response(JSON.stringify({ data }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  }))
}

afterEach(() => vi.unstubAllGlobals())

describe('health diagnostics export', () => {
  it('separates issue-only developer diagnostics from ordinary logs', async () => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      const path = String(input)
      if (path.endsWith('/api/v1/health')) return response({
        generated_at: '2026-07-27T12:00:00Z', configs: {}, subscription_total: 1, subscription_active: 1,
        download_completed: 3, download_downloading: 0, download_failed: 1, local_anime_count: 1,
        local_episode_count: 3, open_library_issues: 1, stale_subscriptions_72h: 0,
        health_tone: 'rose', summary: '存在需要处理的问题', recommendations: [],
      })
      if (path.endsWith('/api/v1/runtime')) return response({
        uptime_seconds: 3600, go: { goroutines: 12, gomaxprocs: 4, num_cpu: 4 },
        memory: { heap_alloc_bytes: 1048576, sys_bytes: 2097152 }, gc: { num_gc: 3 },
      })
      throw new Error(`unexpected request: ${path}`)
    }))
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
    const wrapper = mount(HealthView, { global: { plugins: [[VueQueryPlugin, { queryClient }]] } })

    await vi.waitFor(() => expect(wrapper.text()).toContain('存在需要处理的问题'))
    const links = wrapper.findAll('a')
    expect(links.find(link => link.text().includes('导出健康诊断'))?.attributes('href')).toBe('/api/v1/diagnostics/health/export')
    expect(links.find(link => link.text().includes('导出普通日志'))?.attributes('href')).toBe('/api/v1/diagnostics/logs/export')
    expect(wrapper.text()).toContain('不包含配置密钥和普通运行流水')
  })
})
