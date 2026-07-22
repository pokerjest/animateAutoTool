import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import router from './router'

const response = (data: unknown) => Promise.resolve(new Response(JSON.stringify({ data }), { status: 200, headers: { 'Content-Type': 'application/json' } }))

describe('route guards', () => {
  beforeEach(() => {
	setActivePinia(createPinia())
  })

  it('redirects signed-out users to login', async () => {
	vi.stubGlobal('fetch', vi.fn(() => response({ authenticated: false, setup_pending: false, version: 'test', recovery_local_only: true })))
	await router.replace('/login')
    await router.push('/subscriptions')
    expect(router.currentRoute.value.path).toBe('/login')
    expect(router.currentRoute.value.query.redirect).toBe('/subscriptions')
  })

  it('forces bootstrap setup before protected routes', async () => {
	vi.stubGlobal('fetch', vi.fn(() => response({ authenticated: true, setup_pending: true, username: 'admin', version: 'test', recovery_local_only: true })))
	await router.replace('/login')
    await router.push('/library')
    expect(router.currentRoute.value.path).toBe('/setup')
  })
})
