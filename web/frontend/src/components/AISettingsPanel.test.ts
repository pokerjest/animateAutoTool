import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import AISettingsPanel from './AISettingsPanel.vue'

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

describe('AISettingsPanel', () => {
  it('keeps three provider configs, selects one, and tests with a real hi request', async () => {
    const requests: Array<{ path: string; body: Record<string, string> }> = []
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      const body = JSON.parse(String(init?.body || '{}')) as Record<string, string>
      requests.push({ path, body })
      if (path.endsWith('/api/v1/settings/ai/models')) {
        return response({ provider: 'gemini', provider_label: 'Google Gemini', models: ['gemini-test', 'gemini-pro'] })
      }
      if (path.endsWith('/api/v1/settings/ai/test')) {
        return response({
          provider: 'gemini',
          provider_label: 'Google Gemini',
          model: body.model,
          connected: true,
          reply: 'Hi!',
          detail: '已真实发送 hi 并收到模型回复',
          latency_ms: 123,
          checked_at: new Date().toISOString(),
        })
      }
      throw new Error(`unexpected request: ${path}`)
    }))

    const form: Record<string, string> = {
      ai_provider: 'openai',
      ai_openai_base_url: 'https://api.openai.com/v1',
      ai_openai_api_key: '',
      ai_openai_model: 'gpt-test',
      ai_gemini_base_url: 'https://generativelanguage.googleapis.com',
      ai_gemini_api_key: '',
      ai_gemini_model: '',
      ai_gemini_api_format: 'native',
      ai_claude_base_url: 'https://api.anthropic.com',
      ai_claude_api_key: '',
      ai_claude_model: '',
      ai_claude_api_format: 'native',
    }
    const wrapper = mount(AISettingsPanel, {
      props: { form, configured: { ai_openai_api_key: true } },
      global: { plugins: [createPinia()] },
    })

    expect(wrapper.text()).toContain('OpenAI / GPT')
    expect(wrapper.text()).toContain('Google Gemini')
    expect(wrapper.text()).toContain('Anthropic Claude')
    expect(wrapper.text()).toContain('读取模型和 hi 测试会直接使用当前表单，无需先保存')
    expect(wrapper.text()).toContain('Google OAuth Client ID / Client Secret 用于登录和用户授权')

    const gemini = wrapper.get('[data-testid="ai-provider-gemini"]')
    await gemini.findAll('button').find(button => button.text().includes('设为当前'))!.trigger('click')
    expect(form.ai_provider).toBe('gemini')

    await gemini.get('[data-testid="ai-format-gemini"]').setValue('openai')
    expect(form.ai_gemini_api_format).toBe('openai')
    expect(form.ai_gemini_base_url).toBe('https://generativelanguage.googleapis.com/v1beta/openai')

    const textInputs = gemini.findAll('input[type="text"]')
    await textInputs[0].setValue('https://gemini.example.test')
    await gemini.get('input[type="password"]').setValue('gemini-secret')

    await gemini.findAll('button').find(button => button.text().includes('读取模型列表'))!.trigger('click')
    await flushPromises()
    expect(gemini.text()).toContain('gemini-pro')
    expect(requests.at(-1)?.body).toEqual({
      provider: 'gemini',
      format: 'openai',
      base_url: 'https://gemini.example.test',
      api_key: 'gemini-secret',
      model: '',
    })

    await gemini.findAll('button').find(button => button.text().includes('gemini-test'))!.trigger('click')
    expect(form.ai_gemini_model).toBe('gemini-test')

    await gemini.findAll('button').find(button => button.text().includes('用 hi 测试连接'))!.trigger('click')
    await flushPromises()

    expect(requests.at(-1)?.body).toEqual({
      provider: 'gemini',
      format: 'openai',
      base_url: 'https://gemini.example.test',
      api_key: 'gemini-secret',
      model: 'gemini-test',
    })
    expect(wrapper.get('[data-testid="ai-test-result-gemini"]').text()).toContain('模型回复：Hi!')
    expect(wrapper.get('[data-testid="ai-test-result-gemini"]').text()).toContain('123 ms')
  })

  it('keeps a custom gateway URL when changing Claude API format', async () => {
    const form: Record<string, string> = {
      ai_provider: 'claude',
      ai_openai_base_url: 'https://api.openai.com/v1',
      ai_openai_api_key: '',
      ai_openai_model: '',
      ai_gemini_api_format: 'native',
      ai_gemini_base_url: 'https://generativelanguage.googleapis.com',
      ai_gemini_api_key: '',
      ai_gemini_model: '',
      ai_claude_api_format: 'native',
      ai_claude_base_url: 'https://claude-gateway.example.test/api',
      ai_claude_api_key: '',
      ai_claude_model: '',
    }
    const wrapper = mount(AISettingsPanel, {
      props: { form, configured: {} },
      global: { plugins: [createPinia()] },
    })

    await wrapper.get('[data-testid="ai-format-claude"]').setValue('openai')

    expect(form.ai_claude_api_format).toBe('openai')
    expect(form.ai_claude_base_url).toBe('https://claude-gateway.example.test/api')
  })

  it('keeps fallback Gemini models selectable when the live list is rate limited', async () => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      const body = JSON.parse(String(init?.body || '{}')) as Record<string, string>
      if (path.endsWith('/api/v1/settings/ai/models')) {
        return response({
          provider: 'gemini',
          provider_label: 'Google Gemini',
          format: 'native',
          models: ['gemini-3.6-flash', 'gemini-2.5-flash'],
          source: 'fallback',
          upstream_status: 429,
          retry_after_seconds: 20,
          warning: 'Google Gemini 当前项目额度已达到限制；请等待至少 20 秒后重试。',
        })
      }
      if (path.endsWith('/api/v1/settings/ai/test')) {
        return response({
          provider: 'gemini',
          provider_label: 'Google Gemini',
          format: 'native',
          model: body.model,
          connected: body.model === 'gemini-2.5-flash',
          detail: body.model === 'gemini-2.5-flash' ? '已真实发送 hi 并收到模型回复' : '当前模型配额受限',
          upstream_status: body.model === 'gemini-2.5-flash' ? 0 : 429,
          latency_ms: 80,
          checked_at: new Date().toISOString(),
        })
      }
      throw new Error(`unexpected request: ${path}`)
    }))

    const form: Record<string, string> = {
      ai_provider: 'gemini',
      ai_openai_base_url: 'https://api.openai.com/v1',
      ai_openai_api_key: '',
      ai_openai_model: '',
      ai_gemini_base_url: 'https://generativelanguage.googleapis.com',
      ai_gemini_api_key: '',
      ai_gemini_model: 'gemini-3.6-flash',
      ai_gemini_api_format: 'native',
      ai_claude_base_url: 'https://api.anthropic.com',
      ai_claude_api_key: '',
      ai_claude_model: '',
      ai_claude_api_format: 'native',
    }
    const wrapper = mount(AISettingsPanel, {
      props: { form, configured: { ai_gemini_api_key: true } },
      global: { plugins: [createPinia()] },
    })
    const gemini = wrapper.get('[data-testid="ai-provider-gemini"]')

    await gemini.findAll('button').find(button => button.text().includes('读取模型列表'))!.trigger('click')
    await flushPromises()

    expect(gemini.text()).toContain('实时模型列表读取失败，切换功能仍然可用')
    expect(gemini.text()).toContain('内置备用列表')
    await gemini.findAll('button').find(button => button.text().includes('gemini-2.5-flash'))!.trigger('click')
    expect(form.ai_gemini_model).toBe('gemini-2.5-flash')

    await gemini.findAll('button').find(button => button.text().includes('用 hi 测试连接'))!.trigger('click')
    await flushPromises()
    expect(gemini.text()).toContain('本次可用')
    expect(gemini.text()).toContain('Gemini Models API 不返回各模型剩余 RPM、TPM 或每日额度')
  })
})
