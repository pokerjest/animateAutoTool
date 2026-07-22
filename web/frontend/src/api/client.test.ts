import { afterEach, describe, expect, it, vi } from 'vitest'
import { api, ApiError } from './client'

describe('api client', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('unwraps the v1 data envelope', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ data: { ready: true } }), { status: 200, headers: { 'Content-Type': 'application/json' } })))
    await expect(api<{ ready: boolean }>('/health')).resolves.toEqual({ ready: true })
  })

  it('turns structured errors into ApiError', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: { code: 'denied', message: '没有权限' } }), { status: 403, headers: { 'Content-Type': 'application/json' } })))
    await expect(api('/settings')).rejects.toMatchObject({ status: 403, message: '没有权限' } satisfies Partial<ApiError>)
  })
})
