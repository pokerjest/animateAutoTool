import { afterEach, describe, expect, it, vi } from 'vitest'
import { api, ApiError, calendarPosterProxyURL, calendarPosterURL, handlePosterError, mikanPosterProxyURL, normalizePosterURL, posterThumbnailURL, posterURL } from './client'

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

  it('keeps direct Mikan images and provides a same-origin fallback URL', () => {
    expect(normalizePosterURL('https://mikanani.me/images/poster.jpg')).toBe('https://mikanani.me/images/poster.jpg')
    expect(mikanPosterProxyURL('https://mikanani.me/images/poster.jpg')).toBe('/api/v1/subscriptions/mikan/poster?url=https%3A%2F%2Fmikanani.me%2Fimages%2Fposter.jpg&width=360')
    expect(normalizePosterURL('https://example.com/poster.jpg')).toBe('https://example.com/poster.jpg')
    expect(normalizePosterURL()).toBe('/static/img/no_poster.svg')
  })

  it('falls back from direct Mikan to the host proxy and then the placeholder', () => {
    const image = document.createElement('img')
    image.setAttribute('src', 'https://mikanani.me/images/poster.jpg')
    handlePosterError({ currentTarget: image } as unknown as Event)
    expect(image.getAttribute('src')).toBe('/api/v1/subscriptions/mikan/poster?url=https%3A%2F%2Fmikanani.me%2Fimages%2Fposter.jpg&width=360')
    handlePosterError({ currentTarget: image } as unknown as Event)
    expect(image.getAttribute('src')).toBe('/static/img/no_poster.svg')
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

  it('keeps direct calendar images and provides a same-origin fallback', () => {
    expect(calendarPosterURL(99, 'https://lain.bgm.tv/pic/cover/l/test.jpg', 360)).toBe('https://lain.bgm.tv/pic/cover/l/test.jpg')
    expect(calendarPosterProxyURL(99, 360)).toBe('/api/v1/calendar/posters/99?width=360')
    expect(calendarPosterProxyURL(0, 360)).toBe('')
  })
})
