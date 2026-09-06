import { readRaw, writeRaw } from './storage'

export const DEVICE_ID_KEY = 'tutor.device_id'

let cached: string | null = null

/** RFC 4122 v4 uuid; falls back to getRandomValues when randomUUID is missing (older WebViews). */
export function uuidv4(): string {
  const c: Crypto = crypto
  if (typeof c.randomUUID === 'function') return c.randomUUID()
  const b = new Uint8Array(16)
  c.getRandomValues(b)
  b[6] = (b[6] & 0x0f) | 0x40
  b[8] = (b[8] & 0x3f) | 0x80
  const h = Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('')
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`
}

/** Returns the stable per-browser device id, generating and persisting it on first use. */
export function getDeviceId(): string {
  if (cached) return cached
  const stored = readRaw(DEVICE_ID_KEY)
  if (stored && stored.length >= 8) {
    cached = stored
    return stored
  }
  const fresh = uuidv4()
  writeRaw(DEVICE_ID_KEY, fresh)
  cached = fresh
  return fresh
}

/** Test hook: forget the in-memory copy so the next call re-reads storage. */
export function resetDeviceIdCache(): void {
  cached = null
}
