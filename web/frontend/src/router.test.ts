import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import router from './router'
import { useWorkspaceStore } from './stores/workspace'

const response = (data: unknown) => Promise.resolve(new Response(JSON.stringify({ data }), { status: 200, headers: { 'Content-Type': 'application/json' } }))

describe('route guards', () => {
  beforeEach(() => {
    localStorage.clear()
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

  it('switches workspace automatically for direct media and management URLs', async () => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      const path = String(input)
      if (path.includes('/api/v1/media/providers')) {
        return response({ providers: [{ id: 'jellyfin', configured: true, connected: true }] })
      }
      return response({ authenticated: true, setup_pending: false, username: 'admin', version: 'test', recovery_local_only: true })
    }))
    await router.replace('/login')

    await router.push('/media/item/jellyfin/episode-guid')
    const workspace = useWorkspaceStore()
    expect(workspace.mode).toBe('media')
    expect(workspace.lastMediaRoute).toBe('/media/item/jellyfin/episode-guid')

    await router.push('/subscriptions')
    expect(workspace.mode).toBe('manage')
    expect(workspace.lastManageRoute).toBe('/subscriptions')
  })
})
