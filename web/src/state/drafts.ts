import { readJson, readRaw, removeRaw, writeJson, writeRaw } from './storage'

const draftKey = (exerciseId: string) => `tutor.draft.${exerciseId}`
const stdinKey = (exerciseId: string) => `tutor.stdin.${exerciseId}`
const openedKey = (exerciseId: string) => `tutor.opened.${exerciseId}`

/** Returns the saved code draft for an exercise, or null when none exists. */
export function loadDraft(exerciseId: string): string | null {
  return readRaw(draftKey(exerciseId))
}

/** Persists the current code for an exercise. */
export function saveDraft(exerciseId: string, code: string): void {
  writeRaw(draftKey(exerciseId), code)
}

/** Drops the draft so the starter is used on next open. */
export function clearDraft(exerciseId: string): void {
  removeRaw(draftKey(exerciseId))
}

/** Returns the remembered stdin for run mode. */
export function loadStdin(exerciseId: string): string {
  return readRaw(stdinKey(exerciseId)) ?? ''
}

/** Remembers the stdin used for run mode. */
export function saveStdin(exerciseId: string, stdin: string): void {
  writeRaw(stdinKey(exerciseId), stdin)
}

/** True once the exercise has been opened before; drives statement auto-collapse. */
export function wasOpened(exerciseId: string): boolean {
  return readJson<boolean>(openedKey(exerciseId), false)
}

/** Marks the exercise as opened. */
export function markOpened(exerciseId: string): void {
  writeJson(openedKey(exerciseId), true)
}

/**
 * Debounces saves per exercise so every keystroke does not hit storage.
 * Returns a flush to write immediately (used on unmount).
 */
export function createDraftSaver(delayMs = 300) {
  let timer: ReturnType<typeof setTimeout> | null = null
  let pending: { exerciseId: string; code: string } | null = null
  const flush = () => {
    if (timer) clearTimeout(timer)
    timer = null
    if (pending) saveDraft(pending.exerciseId, pending.code)
    pending = null
  }
  const schedule = (exerciseId: string, code: string) => {
    pending = { exerciseId, code }
    if (timer) clearTimeout(timer)
    timer = setTimeout(flush, delayMs)
  }
  return { schedule, flush }
}
