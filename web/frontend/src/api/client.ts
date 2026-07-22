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

export function posterURL(item: { ID?: number; id?: number; image?: string; Image?: string }) {
  const id = item.ID ?? item.id
  const image = item.image ?? item.Image
  if (id) return `/api/v1/posters/${id}`
  return image || '/static/img/no_poster.svg'
}
