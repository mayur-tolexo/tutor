// localStorage can throw (private mode, quota, disabled); every access is guarded.

/** Reads a raw string, or null when missing or storage is unavailable. */
export function readRaw(key: string): string | null {
  try {
    return localStorage.getItem(key)
  } catch {
    return null
  }
}

/** Writes a raw string; silently drops the write when storage is unavailable. */
export function writeRaw(key: string, value: string): void {
  try {
    localStorage.setItem(key, value)
  } catch {
    // Ignore: drafts and prefs are best-effort.
  }
}

/** Removes a key; no-op when storage is unavailable. */
export function removeRaw(key: string): void {
  try {
    localStorage.removeItem(key)
  } catch {
    // Ignore.
  }
}

/** Reads and JSON-parses a value, returning fallback on any failure. */
export function readJson<T>(key: string, fallback: T): T {
  const raw = readRaw(key)
  if (raw === null) return fallback
  try {
    return JSON.parse(raw) as T
  } catch {
    return fallback
  }
}

/** JSON-serialises and writes a value. */
export function writeJson(key: string, value: unknown): void {
  writeRaw(key, JSON.stringify(value))
}
