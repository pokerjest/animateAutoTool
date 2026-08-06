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

async function apiPayload(path: string, init: RequestInit = {}): Promise<unknown> {
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
  return payload
}

export async function apiEnvelope<T>(path: string, init: RequestInit = {}): Promise<ApiEnvelope<T>> {
  const payload = await apiPayload(path, init)
  if (typeof payload === 'object' && payload && 'data' in payload) return payload as ApiEnvelope<T>
  return { data: payload as T }
}

export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  return (await apiEnvelope<T>(path, init)).data
}

export const jsonBody = (value: unknown): RequestInit => ({ body: JSON.stringify(value) })

const noPosterURL = '/static/img/no_poster.svg'
const posterAttemptsKey = 'posterAttempts'

export function normalizePosterURL(image?: string) {
  const value = image?.trim()
  if (!value) return noPosterURL
  if (value.startsWith('/api/posters/')) return `/api/v1/posters/${value.slice('/api/posters/'.length)}`

  return value
}

const mikanPosterHosts = new Set(['mikanani.me', 'www.mikanani.me', 'mikanime.tv', 'mikanani.kas.pub'])

function mikanPosterSource(image?: string) {
  const value = image?.trim()
  if (!value) return ''
  try {
    const parsed = new URL(value)
    const trustedAuthority = !parsed.username && !parsed.password && (!parsed.port || parsed.port === '443')
    if (parsed.protocol === 'https:' && trustedAuthority && mikanPosterHosts.has(parsed.hostname.toLowerCase())) {
      return parsed.toString()
    }
  } catch {
    return ''
  }
  return ''
}

export function mikanPosterProxyURL(image?: string, width = 360) {
  const source = mikanPosterSource(image)
  if (!source) return ''
  const params = new URLSearchParams({
    url: source,
    width: String(Math.max(64, Math.min(1280, Math.round(width)))),
  })
  return `/api/v1/subscriptions/mikan/poster?${params}`
}
export function mikanDiscoveryPosterURL(image?: string, width = 360) {
  return mikanPosterProxyURL(image, width) || normalizePosterURL(image)
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
  return calendarPosterProxyURL(subjectID, width) || normalizePosterURL(image)
}

export function calendarPosterProxyURL(subjectID?: number, width = 360) {
  if (!subjectID) return ''
  return `/api/v1/calendar/posters/${subjectID}?width=${Math.max(64, Math.min(1280, Math.round(width)))}`
}

export function subscriptionPosterURL(id?: number, source: 'mikan' | 'local' = 'mikan', width = 360) {
  if (!id) return ''
  return `/api/v1/subscriptions/${id}/poster?source=${source}&width=${Math.max(64, Math.min(1280, Math.round(width)))}`
}

function posterAttempts(image: HTMLImageElement) {
  const raw = image.dataset[posterAttemptsKey] || ''
  return new Set(raw ? raw.split('\n') : [])
}

function rememberPosterAttempt(image: HTMLImageElement, candidate: string) {
  if (candidate === noPosterURL) return
  const attempts = posterAttempts(image)
  attempts.add(candidate)
  image.dataset[posterAttemptsKey] = [...attempts].join('\n')
}

function hasAttemptedMikanProxy(attempted: Set<string>, source: string) {
  return [...attempted].some(candidate => {
    if (!candidate.startsWith('/api/v1/subscriptions/mikan/poster?')) return false
    const parsed = new URL(candidate, window.location.origin)
    return parsed.searchParams.get('url') === source
  })
}

export function handlePosterError(event: Event, ...fallbacks: Array<string | undefined>) {
  const image = event.currentTarget
  if (!(image instanceof HTMLImageElement)) return false
  const current = image.getAttribute('src') || ''
  if (current) rememberPosterAttempt(image, current)
  const attempted = posterAttempts(image)

  const mikanSource = mikanPosterSource(current)
  const mikanProxy = mikanPosterProxyURL(current)
  if (mikanProxy && !hasAttemptedMikanProxy(attempted, mikanSource)) {
    rememberPosterAttempt(image, mikanProxy)
    image.src = mikanProxy
    return true
  }

  if (current.startsWith('/api/v1/subscriptions/mikan/poster?')) {
    const next = fallbacks
      .map(normalizePosterURL)
      .find(candidate => candidate !== current && !attempted.has(candidate))
    if (next) rememberPosterAttempt(image, next)
    image.src = next || noPosterURL
    return true
  }

  const next = [...fallbacks.map(normalizePosterURL), noPosterURL]
    .find(candidate => candidate !== current && !attempted.has(candidate))
  if (!next) return false
  rememberPosterAttempt(image, next)
  image.src = next
  return true
}
