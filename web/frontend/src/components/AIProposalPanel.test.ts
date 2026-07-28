import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { createPinia } from 'pinia'
import AIProposalPanel from './AIProposalPanel.vue'

function response(data: unknown) {
  return Promise.resolve(new Response(JSON.stringify({ data }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  }))
}

afterEach(() => {
  vi.unstubAllGlobals()
  localStorage.clear()
})

describe('AIProposalPanel', () => {
  it('requires ready proposal confirmation before applying and tracks the returned task', async () => {
    const requests: string[] = []
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      requests.push(`${init?.method || 'GET'} ${path}`)
      if (path.endsWith('/api/v1/ai/proposals/proposal-1')) {
        return response({
          id: 'proposal-1', type: 'health_diagnosis', status: 'ready', target_type: 'health', target_id: 'current',
          summary: '发现一个可修复的问题', confidence: .9, evidence: ['健康报告'], warnings: [],
          actionable: true, payload: { action: 'repair_download_logs' }, provider: 'gemini', model: 'gemini-test',
          created_at: '2026-07-28T00:00:00Z', updated_at: '2026-07-28T00:00:00Z',
        })
      }
      if (path.endsWith('/confirm')) return response({ confirmation_token: 'one-time-token', expires_in_seconds: 300 })
      if (path.endsWith('/apply')) return response({ task_id: 'repair-task-1', status: 'running' })
      throw new Error(`unexpected request: ${path}`)
    }))

    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
    const wrapper = mount(AIProposalPanel, {
      props: { proposalId: 'proposal-1' },
      global: { plugins: [createPinia(), [VueQueryPlugin, { queryClient }]] },
    })

    await vi.waitFor(() => expect(wrapper.text()).toContain('待确认'))
    await wrapper.get('button').trigger('click')
    await flushPromises()

    expect(requests).toContain('POST /api/v1/ai/proposals/proposal-1/confirm')
    expect(requests).toContain('POST /api/v1/ai/proposals/proposal-1/apply')
    expect(wrapper.text()).toContain('发现一个可修复的问题')
  })

  it('does not expose an execute button for a non-actionable ready proposal', async () => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      const path = String(input)
      if (path.endsWith('/api/v1/ai/proposals/proposal-2')) {
        return response({
          id: 'proposal-2', type: 'health_diagnosis', status: 'ready', target_type: 'health', target_id: 'current',
          summary: '建议人工检查', confidence: .4, evidence: [], warnings: ['没有安全的自动修复动作'],
          actionable: false, payload: {}, provider: 'openai', model: 'gpt-test',
          created_at: '2026-07-28T00:00:00Z', updated_at: '2026-07-28T00:00:00Z',
        })
      }
      throw new Error(`unexpected request: ${path}`)
    }))
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
    const wrapper = mount(AIProposalPanel, {
      props: { proposalId: 'proposal-2' },
      global: { plugins: [createPinia(), [VueQueryPlugin, { queryClient }]] },
    })
    await vi.waitFor(() => expect(wrapper.text()).toContain('建议人工检查'))
    expect(wrapper.text()).not.toContain('确认并执行')
  })
})
