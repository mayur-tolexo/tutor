import type {
  ApiErrorBody,
  Attempt,
  AttemptRequest,
  Config,
  Exercise,
  Hint,
  HintRequest,
  Lang,
  Me,
  Progress,
  TracksResponse,
} from './types'
import { getDeviceId } from '../state/deviceId'

/** Error thrown for any non-2xx response or network failure, carrying the wire code. */
export class ApiError extends Error {
  readonly code: string
  readonly status: number
  readonly retryAfterSeconds?: number

  constructor(status: number, code: string, message: string, retryAfterSeconds?: number) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.retryAfterSeconds = retryAfterSeconds
  }
}

/** Options for a single request; pool_busy retries are opt-in per call. */
export interface RequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'DELETE'
  body?: unknown
  signal?: AbortSignal
  /** Called before each pool_busy retry with the attempt number (1-based). */
  onRetry?: (attempt: number) => void
  /** Maximum pool_busy retries; 0 disables. */
  maxRetries?: number
}

export const POOL_BUSY_BACKOFF_MS = [1000, 2000, 4000]

const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms))

/** Parses an error body into ApiError, falling back to the HTTP status when the body is not ours. */
async function toApiError(res: Response): Promise<ApiError> {
  let body: ApiErrorBody | undefined
  try {
    body = (await res.json()) as ApiErrorBody
  } catch {
    // Non-JSON body (proxy error page etc.); fall through to status-based mapping.
  }
  if (body?.error?.code) {
    return new ApiError(res.status, body.error.code, body.error.message, body.error.retry_after_seconds)
  }
  const fallback: Record<number, string> = {
    400: 'invalid',
    401: 'unauthorized',
    404: 'not_found',
    429: 'rate_limited',
    502: 'infra_error',
    503: 'pool_busy',
  }
  return new ApiError(res.status, fallback[res.status] ?? 'infra_error', res.statusText || `HTTP ${res.status}`)
}

/**
 * Typed fetch against /v1. Always sends the device id header and cookies.
 * Retries only on pool_busy (503) with fixed backoff when maxRetries > 0.
 */
export async function request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const maxRetries = opts.maxRetries ?? 0
  for (let attempt = 0; ; attempt++) {
    let res: Response
    try {
      res = await fetch(`/v1${path}`, {
        method: opts.method ?? 'GET',
        credentials: 'include',
        signal: opts.signal,
        headers: {
          'X-Device-Id': getDeviceId(),
          ...(opts.body !== undefined ? { 'Content-Type': 'application/json' } : {}),
        },
        body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
      })
    } catch (e) {
      if (e instanceof DOMException && e.name === 'AbortError') throw e
      throw new ApiError(0, 'network', 'Network error — check your connection')
    }

    if (res.ok) {
      if (res.status === 204) return undefined as T
      return (await res.json()) as T
    }

    const err = await toApiError(res)
    if (err.code === 'pool_busy' && attempt < maxRetries) {
      opts.onRetry?.(attempt + 1)
      await sleep(POOL_BUSY_BACKOFF_MS[Math.min(attempt, POOL_BUSY_BACKOFF_MS.length - 1)])
      continue
    }
    throw err
  }
}

/** GET /v1/config */
export const getConfig = () => request<Config>('/config')

/** GET /v1/tracks */
export const getTracks = () => request<TracksResponse>('/tracks')

/** GET /v1/exercises/{id}; the id may contain slashes and is sent as-is. */
export const getExercise = (id: string) => request<Exercise>(`/exercises/${id}`)

/** POST /v1/attempts with pool_busy auto-retry (1s, 2s, 4s). */
export const createAttempt = (body: AttemptRequest, onRetry?: (n: number) => void, signal?: AbortSignal) =>
  request<Attempt>('/attempts', { method: 'POST', body, maxRetries: 3, onRetry, signal })

/** POST /v1/hints */
export const requestHint = (body: HintRequest) => request<Hint>('/hints', { method: 'POST', body, maxRetries: 3 })

/** GET /v1/progress */
export const getProgress = () => request<Progress>('/progress')

/** GET /v1/me */
export const getMe = () => request<Me>('/me')

/** PUT /v1/me */
export const updateMe = (lang_pref: Lang) => request<Me>('/me', { method: 'PUT', body: { lang_pref } })

/** POST /v1/auth/google */
export const signInWithGoogle = (id_token: string) =>
  request<Me>('/auth/google', { method: 'POST', body: { id_token } })

/** POST /v1/auth/logout */
export const signOut = () => request<void>('/auth/logout', { method: 'POST' })
