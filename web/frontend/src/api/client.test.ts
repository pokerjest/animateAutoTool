import { afterEach, describe, expect, it, vi } from 'vitest'
import { api, ApiError, handlePosterError, normalizePosterURL, posterThumbnailURL, posterURL } from './client'

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

describe('poster URLs', () => {
  it('upgrades legacy poster paths to the v1 endpoint', () => {
    expect(normalizePosterURL('/api/posters/42?source=tmdb')).toBe('/api/v1/posters/42?source=tmdb')
  })

  it('preserves external image URLs and provides a default for empty values', () => {
    expect(normalizePosterURL('https://mikanani.me/images/poster.jpg')).toBe('https://mikanani.me/images/poster.jpg')
    expect(normalizePosterURL()).toBe('/static/img/no_poster.svg')
  })

  it('prefers a metadata ID and falls back to the default after an image error', () => {
    expect(posterURL({ ID: 9, image: '/api/posters/8' })).toBe('/api/v1/posters/9')
    const image = document.createElement('img')
    image.src = '/api/v1/posters/9'
    handlePosterError({ currentTarget: image } as unknown as Event)
    expect(image.getAttribute('src')).toBe('/static/img/no_poster.svg')
  })

  it('adds thumbnail dimensions and a metadata cache version', () => {
    expect(posterThumbnailURL('/api/v1/posters/9?source=tmdb', 320)).toBe('/api/v1/posters/9?source=tmdb&width=320')
    expect(posterURL({ ID: 9, UpdatedAt: '2026-07-23T12:00:00Z' }, { width: 360 })).toBe('/api/v1/posters/9?width=360&v=2026-07-23T12%3A00%3A00Z')
  })
})
