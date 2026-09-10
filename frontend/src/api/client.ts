import type { AlbumDetail, FolderDetail, RootListing } from './types'

/** Non-2xx response. `code` is the backend's snake_case error code. */
export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly body: unknown

  constructor(status: number, code: string, body: unknown) {
    super(`${code} (HTTP ${status})`)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.body = body
  }
}

export function isApiError(err: unknown, status?: number, code?: string): err is ApiError {
  return (
    err instanceof ApiError &&
    (status === undefined || err.status === status) &&
    (code === undefined || err.code === code)
  )
}

// Absolute URLs so the same code runs under jsdom, whose fetch rejects
// relative paths. In the browser this is a no-op.
function absolute(path: string): string {
  return new URL(path, window.location.origin).toString()
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const res = await fetch(absolute(path), {
    credentials: 'include',
    ...init,
    headers: { Accept: 'application/json', ...(init.headers ?? {}) },
  })
  const text = await res.text()
  let body: unknown = null
  if (text) {
    try {
      body = JSON.parse(text)
    } catch {
      body = text
    }
  }
  if (!res.ok) {
    const code =
      typeof body === 'object' && body !== null && 'error' in body
        ? String((body as { error: unknown }).error)
        : `http_${res.status}`
    throw new ApiError(res.status, code, body)
  }
  return body as T
}

export const api = {
  listRoot: (): Promise<RootListing> => request<RootListing>('/api/albums'),
  getFolder: (slug: string): Promise<FolderDetail> =>
    request<FolderDetail>(`/api/folders/${encodeURIComponent(slug)}`),
  getAlbum: (slug: string): Promise<AlbumDetail> =>
    request<AlbumDetail>(`/api/albums/${encodeURIComponent(slug)}`),
  unlock: (slug: string, password: string): Promise<void> =>
    request<void>(`/api/albums/${encodeURIComponent(slug)}/unlock`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ password }),
    }),
}
