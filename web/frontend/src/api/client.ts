export interface ApiEnvelope<T> {
  data: T
  meta?: { page?: number; page_size?: number; total?: number }
  message?: string
}

export class ApiError extends Error {
  constructor(public status: number, message: string, public details?: unknown) {
    super(message)
  }
}

export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  const isForm = init.body instanceof FormData
  if (init.body && !isForm && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json')
  headers.set('Accept', 'application/json')
  const response = await fetch(`/api/v1${path}`, { ...init, headers, credentials: 'same-origin' })
  const contentType = response.headers.get('content-type') || ''
  const payload = contentType.includes('application/json') ? await response.json() : await response.text()
  if (!response.ok) {
    const message = typeof payload === 'string' ? payload : payload?.error?.message || payload?.error || payload?.message || '请求失败'
    throw new ApiError(response.status, message, payload)
  }
  if (typeof payload === 'object' && payload && 'data' in payload) return payload.data as T
  return payload as T
}

export const jsonBody = (value: unknown): RequestInit => ({ body: JSON.stringify(value) })

const noPosterURL = '/static/img/no_poster.svg'

export function normalizePosterURL(image?: string) {
  const value = image?.trim()
  if (!value) return noPosterURL
  if (value.startsWith('/api/posters/')) return `/api/v1/posters/${value.slice('/api/posters/'.length)}`
  return value
}

interface PosterRecord { ID?: number; id?: number; image?: string; Image?: string; UpdatedAt?: string; updated_at?: string }
interface PosterOptions { width?: number; source?: string }

export function posterThumbnailURL(image?: string, width = 360) {
  const normalized = normalizePosterURL(image)
  if (!normalized.startsWith('/api/v1/posters/')) return normalized
  const url = new URL(normalized, window.location.origin)
  if (!url.searchParams.has('width')) url.searchParams.set('width', String(width))
  return `${url.pathname}${url.search}`
}

export function posterURL(item: PosterRecord, options: PosterOptions = {}) {
  const id = item.ID ?? item.id
  const image = item.image ?? item.Image
  if (id) {
    const params = new URLSearchParams()
    if (options.source) params.set('source', options.source)
    if (options.width) params.set('width', String(options.width))
    const updatedAt = item.UpdatedAt ?? item.updated_at
    if (updatedAt) params.set('v', updatedAt)
    const query = params.toString()
    return `/api/v1/posters/${id}${query ? `?${query}` : ''}`
  }
  return normalizePosterURL(image)
}

export function calendarPosterURL(subjectID?: number, image?: string, width = 360) {
  if (!subjectID || !image?.trim()) return normalizePosterURL(image)
  return `/api/v1/calendar/posters/${subjectID}?width=${Math.max(64, Math.min(1280, Math.round(width)))}`
}

export function handlePosterError(event: Event, ...fallbacks: Array<string | undefined>) {
  const image = event.currentTarget
  if (!(image instanceof HTMLImageElement)) return
  const current = image.getAttribute('src') || ''
  const next = [...fallbacks.map(normalizePosterURL), noPosterURL].find(candidate => candidate !== current)
  if (next) image.src = next
}
