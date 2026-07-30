import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import LocalOrganizeDialog from './LocalOrganizeDialog.vue'
import { useTaskStore } from '../../stores/tasks'

function response(data: unknown, status = 200) {
  return Promise.resolve(new Response(JSON.stringify({ data }), { status, headers: { 'Content-Type': 'application/json' } }))
}

afterEach(() => vi.unstubAllGlobals())

describe('LocalOrganizeDialog', () => {
  it('previews typed changes, allows exclusions, and starts one tracked task', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      if (path.endsWith('/api/v1/local-anime/organize/preview')) return response({
        plan_id: 'plan-1', expires_at: '2026-07-27T12:15:00Z', series_template: '{title}', episode_template: '{title} - S{season}E{episode}{ext}',
        selected_count: 2, change_count: 2, unchanged_count: 0, conflict_count: 0, skipped_count: 0,
        items: [
          { anime_id: 1, title: '番剧 A', source_path: '/old/a', target_path: '/new/a', metadata_matched: true, warnings: [], changes: [{ kind: 'video', original: '/old/a/1.mkv', target: '/new/a/Season 01/a v2.mkv', status: 'ready', managed_by_qb: true, parse_source: 'filename:dash', parse_confidence: 0.98, version: 'v2' }] },
          { anime_id: 2, title: '番剧 B', source_path: '/old/b', target_path: '/new/b', metadata_matched: false, warnings: ['未匹配规范元数据'], changes: [{ kind: 'video', original: '/old/b/1.mkv', target: '/new/b/Season 01/b.mkv', status: 'ready', managed_by_qb: false }] },
        ],
      })
      if (path.endsWith('/api/v1/local-anime/organize') && init?.method === 'POST') return response({ task_id: 'local-organize-plan-1', status: 'running' }, 202)
      throw new Error(`unexpected request: ${path}`)
    })
    vi.stubGlobal('fetch', fetchMock)
    const pinia = createPinia()
    setActivePinia(pinia)
    const wrapper = mount(LocalOrganizeDialog, {
      props: { open: false, selection: { mode: 'ids', anime_ids: [1, 2] } },
      global: {
        plugins: [pinia],
        stubs: { AppDialog: { props: ['open'], template: '<section v-if="open"><slot /></section>' } },
      },
    })
    await wrapper.setProps({ open: true })
    await vi.waitFor(() => expect(wrapper.text()).toContain('番剧 B'))
    expect(wrapper.text()).toContain('qB 安全移动')
    expect(wrapper.text()).toContain('未匹配规范元数据')
    expect(wrapper.text()).toContain('版本 v2')

    const checkboxes = wrapper.findAll('input[type="checkbox"]')
    await checkboxes[1].setValue(false)
    await wrapper.findAll('button').find(button => button.text().includes('整理 1 项'))!.trigger('click')
    await flushPromises()

    const applyCall = fetchMock.mock.calls.find(([input]) => String(input).endsWith('/api/v1/local-anime/organize'))
    expect(JSON.parse(String((applyCall?.[1] as RequestInit).body))).toEqual({ plan_id: 'plan-1', include_anime_ids: [1] })
    expect(useTaskStore().isRunning('local-organize-plan-1')).toBe(true)
    expect(wrapper.emitted('applied')).toHaveLength(1)
  })
})
