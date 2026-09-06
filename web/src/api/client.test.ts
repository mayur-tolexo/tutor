import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, createAttempt, getExercise, getTracks, request, signOut } from './client'
import { DEVICE_ID_KEY, resetDeviceIdCache } from '../state/deviceId'

/** Awaits a rejection and returns it typed as ApiError for assertions. */
const rejection = async (p: Promise<unknown>): Promise<ApiError> => {
  try {
    await p
  } catch (e) {
    return e as ApiError
  }
  throw new Error('expected rejection')
}

const jsonResponse = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })

describe('api client', () => {
  const fetchMock = vi.fn<typeof fetch>()

  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem(DEVICE_ID_KEY, 'dev-1234-abcd')
    resetDeviceIdCache()
    fetchMock.mockReset()
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.useRealTimers()
  })

  it('sends the device id header and cookies on every request', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(200, { tracks: [] }))
    await getTracks()
    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/v1/tracks')
    expect(init?.credentials).toBe('include')
    expect((init?.headers as Record<string, string>)['X-Device-Id']).toBe('dev-1234-abcd')
    expect(init?.method).toBe('GET')
  })

  it('passes slash-containing exercise ids through unescaped', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(200, { id: 'cbse-11/largest-of-three' }))
    await getExercise('cbse-11/largest-of-three')
    expect(fetchMock.mock.calls[0][0]).toBe('/v1/exercises/cbse-11/largest-of-three')
  })

  it('maps the error envelope to ApiError with code and retry_after_seconds', async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse(429, { error: { code: 'rate_limited', message: 'slow down', retry_after_seconds: 17 } }),
    )
    const err = await rejection(request('/attempts', { method: 'POST', body: {} }))
    expect(err).toBeInstanceOf(ApiError)
    expect(err.status).toBe(429)
    expect(err.code).toBe('rate_limited')
    expect(err.message).toBe('slow down')
    expect(err.retryAfterSeconds).toBe(17)
  })

  it('falls back to a status-based code when the body is not our envelope', async () => {
    fetchMock.mockResolvedValueOnce(new Response('<html>bad gateway</html>', { status: 502, statusText: 'Bad Gateway' }))
    const err = await rejection(request('/me'))
    expect(err).toBeInstanceOf(ApiError)
    expect(err.code).toBe('infra_error')
    expect(err.status).toBe(502)
  })

  it('maps a thrown fetch to a network ApiError', async () => {
    fetchMock.mockRejectedValueOnce(new TypeError('Failed to fetch'))
    const err = await rejection(request('/me'))
    expect(err).toBeInstanceOf(ApiError)
    expect(err.code).toBe('network')
    expect(err.status).toBe(0)
  })

  it('retries pool_busy with 1s/2s/4s backoff and reports each retry', async () => {
    vi.useFakeTimers()
    const busy = () => jsonResponse(503, { error: { code: 'pool_busy', message: 'busy' } })
    fetchMock
      .mockResolvedValueOnce(busy())
      .mockResolvedValueOnce(busy())
      .mockResolvedValueOnce(busy())
      .mockResolvedValueOnce(jsonResponse(200, { attempt_id: 'a1', outcome: 'passed', passed: true, duration_ms: 3 }))
    const retries: number[] = []
    const p = createAttempt({ exercise_id: 'x', code: '', mode: 'run', idempotency_key: 'k' }, (n) => retries.push(n))

    await vi.advanceTimersByTimeAsync(999)
    expect(fetchMock).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1)
    expect(fetchMock).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(2000)
    expect(fetchMock).toHaveBeenCalledTimes(3)
    await vi.advanceTimersByTimeAsync(4000)
    expect(fetchMock).toHaveBeenCalledTimes(4)

    const attempt = await p
    expect(attempt.attempt_id).toBe('a1')
    expect(retries).toEqual([1, 2, 3])
  })

  it('gives up on pool_busy after three retries', async () => {
    vi.useFakeTimers()
    fetchMock.mockImplementation(async () => jsonResponse(503, { error: { code: 'pool_busy', message: 'busy' } }))
    const p = rejection(createAttempt({ exercise_id: 'x', code: '', mode: 'submit', idempotency_key: 'k' }))
    await vi.advanceTimersByTimeAsync(7000)
    const err = await p
    expect(err).toBeInstanceOf(ApiError)
    expect(err.code).toBe('pool_busy')
    expect(fetchMock).toHaveBeenCalledTimes(4)
  })

  it('does not retry other errors', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(400, { error: { code: 'invalid', message: 'bad code' } }))
    const err = await rejection(createAttempt({ exercise_id: 'x', code: '', mode: 'run', idempotency_key: 'k' }))
    expect(err.code).toBe('invalid')
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('treats 204 as an empty success', async () => {
    fetchMock.mockResolvedValueOnce(new Response(null, { status: 204 }))
    await expect(signOut()).resolves.toBeUndefined()
    expect(fetchMock.mock.calls[0][1]?.method).toBe('POST')
  })
})
