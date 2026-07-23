import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { createPinia, setActivePinia } from 'pinia'
import DashboardView from './DashboardView.vue'
import LibraryView from './LibraryView.vue'
import LocalAnimeView from './LocalAnimeView.vue'
import SettingsView from './SettingsView.vue'
import SubscriptionsView from './SubscriptionsView.vue'
import { useTaskStore } from '../stores/tasks'

function response(data: unknown, status = 200) {
  return Promise.resolve(new Response(JSON.stringify({ data }), { status, headers: { 'Content-Type': 'application/json' } }))
}

function mountView(component: Parameters<typeof mount>[0]) {
  const pinia = createPinia()
  setActivePinia(pinia)
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
  const wrapper = mount(component, {
    global: {
      plugins: [pinia, [VueQueryPlugin, { queryClient }]],
      stubs: { RouterLink: { template: '<a><slot /></a>' } },
    },
  })
  return { wrapper, tasks: useTaskStore() }
}

function buttonByText(wrapper: VueWrapper, text: string) {
  const button = wrapper.findAll('button').find(item => item.text().includes(text))
  if (!button) throw new Error(`button not found: ${text}`)
  return button
}

afterEach(() => vi.unstubAllGlobals())

describe('background task buttons', () => {
  it('keeps dashboard sync loading until the registered task completes', async () => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      if (path.endsWith('/api/v1/dashboard')) return response({ active_subscriptions: 0, downloads: 0, library_items: 0, local_series: 0, open_issues: 0, services: {}, tasks: [], recent_downloads: [] })
      if (path.endsWith('/api/v1/tasks/sync') && init?.method === 'POST') return response({ task_id: 'manual-sync', status: 'running' }, 202)
      throw new Error(`unexpected request: ${path}`)
    }))
    const { wrapper, tasks } = mountView(DashboardView)
    await vi.waitFor(() => expect(wrapper.text()).toContain('立即同步'))

    await buttonByText(wrapper, '立即同步').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('同步中…')

    tasks.upsert({ id: 'manual-sync', kind: 'sync', title: '立即同步', detail: '同步完成', tone: 'success', updatedAt: new Date().toISOString() })
    await flushPromises()
    expect(wrapper.text()).toContain('立即同步')
    expect(wrapper.text()).not.toContain('同步中…')
  })

  it('tracks local scans independently from the request lifetime', async () => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      if (path.endsWith('/api/v1/local-anime')) return response({ directories: [], items: [], scan_status: {}, diagnostics: [] })
      if (path.endsWith('/api/v1/local-anime/scan') && init?.method === 'POST') return response({ task_id: 'local-scan', status: 'running' }, 202)
      throw new Error(`unexpected request: ${path}`)
    }))
    const { wrapper, tasks } = mountView(LocalAnimeView)
    await vi.waitFor(() => expect(wrapper.text()).toContain('重新扫描'))

    await buttonByText(wrapper, '重新扫描').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('扫描中…')

    tasks.upsert({ id: 'local-scan', kind: 'scan', title: '本地扫描', detail: '扫描完成', tone: 'success', updatedAt: new Date().toISOString() })
    await flushPromises()
    expect(wrapper.text()).not.toContain('扫描中…')
  })

  it('keeps metadata refresh active until its task update arrives', async () => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      if (path.endsWith('/api/v1/library')) return response({ items: [] })
      if (path.endsWith('/api/v1/library/refresh') && init?.method === 'POST') return response({ task_id: 'metadata-refresh', status: 'running' }, 202)
      throw new Error(`unexpected request: ${path}`)
    }))
    const { wrapper, tasks } = mountView(LibraryView)
    await vi.waitFor(() => expect(wrapper.text()).toContain('刷新全部元数据'))

    await buttonByText(wrapper, '刷新全部元数据').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('刷新中…')

    tasks.upsert({ id: 'metadata-refresh', kind: 'metadata', title: '刷新全部元数据', detail: '刷新完成', tone: 'success', updatedAt: new Date().toISOString() })
    await flushPromises()
    expect(wrapper.text()).not.toContain('刷新中…')
  })

  it('isolates subscription check state to the clicked row', async () => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      if (path.endsWith('/api/v1/subscriptions') && !init?.method) return response({
        items: [
          { ID: 1, title: '番剧一', rss_url: 'https://example.test/1', image: '', subtitle_group: '字幕组 A', season: '', filter_rule: '', exclude_rule: '', expected_episodes: 12, downloaded_count: 1, is_active: true, last_run_status: '', last_run_summary: '', last_error_display: '' },
          { ID: 2, title: '番剧二', rss_url: 'https://example.test/2', image: '', subtitle_group: '字幕组 B', season: '', filter_rule: '', exclude_rule: '', expected_episodes: 12, downloaded_count: 2, is_active: true, last_run_status: '', last_run_summary: '', last_error_display: '' },
        ],
        trend: { checked_count: 0, success_count: 0, warning_count: 0, error_count: 0, active_issue_count: 0 },
        scheduler: {},
      })
      if (path.endsWith('/api/v1/subscriptions/1/run') && init?.method === 'POST') return response({ task_id: 'subscription-1', status: 'running' }, 202)
      throw new Error(`unexpected request: ${path}`)
    }))
    const { wrapper, tasks } = mountView(SubscriptionsView)
    await vi.waitFor(() => expect(wrapper.text()).toContain('番剧二'))
    const checks = wrapper.findAll('button').filter(button => button.text().trim() === '检查')
    expect(checks).toHaveLength(2)

    await checks[0].trigger('click')
    await flushPromises()
    const rowButtons = wrapper.findAll('button').filter(button => ['检查', '检查中…'].includes(button.text().trim()))
    expect(rowButtons[0].text()).toContain('检查中…')
    expect(rowButtons[1].text().trim()).toBe('检查')

    tasks.upsert({ id: 'subscription-1', kind: 'subscription', title: '订阅检查', detail: '检查完成', tone: 'success', updatedAt: new Date().toISOString() })
    await flushPromises()
    expect(wrapper.text()).not.toContain('检查中…')
  })

  it('shows settings save progress and sends one write request', async () => {
    let finishSave!: () => void
    let savedBody = ''
    const pendingSave = new Promise<Response>(resolve => { finishSave = () => resolve(new Response(JSON.stringify({ data: null }), { status: 200, headers: { 'Content-Type': 'application/json' } })) })
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      if (path.endsWith('/api/v1/settings') && init?.method === 'PUT') { savedBody = String(init.body); return pendingSave }
      if (path.endsWith('/api/v1/settings')) return response({ values: { qb_mode: 'managed', qb_url: '' }, configured: {}, stats: {} })
      if (path.includes('/api/v1/audit-logs')) return response({ items: [] })
      if (path.endsWith('/api/v1/settings/maintenance')) return response({})
      throw new Error(`unexpected request: ${path}`)
    })
    vi.stubGlobal('fetch', fetchMock)
    const { wrapper } = mountView(SettingsView)
    await vi.waitFor(() => expect((wrapper.get('select').element as HTMLSelectElement).value).toBe('managed'))

    await buttonByText(wrapper, '保存更改').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('正在保存…')
    expect(savedBody).toContain('"qb_mode":"managed"')
    expect(fetchMock.mock.calls.filter(([, init]) => (init as RequestInit | undefined)?.method === 'PUT')).toHaveLength(1)

    finishSave()
    await flushPromises()
    expect(wrapper.text()).not.toContain('正在保存…')
  })
})
