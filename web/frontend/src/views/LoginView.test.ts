import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import LoginView from './LoginView.vue'
import { useSessionStore } from '../stores/session'
import type { SessionState } from '../api/types'

const baseState: SessionState = {
  authenticated: false,
  setup_pending: true,
  local_setup_available: true,
  version: 'test',
  recovery_local_only: true,
}

async function renderLogin(state: SessionState) {
  const pinia = createPinia()
  setActivePinia(pinia)
  const session = useSessionStore()
  session.state = state
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/login', component: LoginView },
      { path: '/setup', component: { template: '<div>setup</div>' } },
      { path: '/recover', component: { template: '<div>recover</div>' } },
      { path: '/', component: { template: '<div>home</div>' } },
    ],
  })
  await router.push('/login')
  await router.isReady()
  const wrapper = mount(LoginView, { global: { plugins: [pinia, router] } })
  return { wrapper, router, session }
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('LoginView first-run setup', () => {
  it('offers password-free initialization only for a direct local first run', async () => {
    const { wrapper } = await renderLogin(baseState)
    expect(wrapper.text()).toContain('创建你的管理员账户')
    expect(wrapper.text()).toContain('开始初始化')
    expect(wrapper.find('input[type="password"]').exists()).toBe(false)
  })

  it('explains the local-only boundary when local bootstrap is unavailable', async () => {
    const { wrapper } = await renderLogin({ ...baseState, local_setup_available: false })
    expect(wrapper.text()).toContain('首次初始化尚未完成')
    expect(wrapper.text()).toContain('初始化完成前不允许远程访问')
    expect(wrapper.find('input[type="password"]').exists()).toBe(true)
  })

  it('creates the local bootstrap session before opening setup', async () => {
    const { wrapper, router, session } = await renderLogin(baseState)
    const begin = vi.spyOn(session, 'beginLocalSetup').mockResolvedValue({ ...baseState, authenticated: true })
    const button = wrapper.findAll('button').find(item => item.text().includes('开始初始化'))
    expect(button).toBeDefined()
    await button!.trigger('click')
    await flushPromises()
    expect(begin).toHaveBeenCalledOnce()
    expect(router.currentRoute.value.path).toBe('/setup')
  })
})
