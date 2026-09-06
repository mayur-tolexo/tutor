import type { Attempt, Hint } from '../api/types'

/** A block of feedback anchored to a 1-based line in the editor. */
export type PinSpec =
  | { kind: 'error'; line: number; type: string; message: string }
  | { kind: 'hint'; line: number; level: 1 | 2 | 3; text: string; hintId: string }

/** Clamps a 1-based line into the document, or returns null when it is not a usable number. */
export function clampLine(line: number | undefined, lineCount: number): number | null {
  if (line === undefined || !Number.isFinite(line) || lineCount < 1) return null
  return Math.min(Math.max(1, Math.round(line)), lineCount)
}

/**
 * Maps the latest attempt and its hints to line-anchored pins.
 * Errors come first, then hints in arrival order; entries without a line are dropped.
 */
export function buildPins(attempt: Attempt | null, hints: Hint[], lineCount: number): PinSpec[] {
  const out: PinSpec[] = []
  const errLine = clampLine(attempt?.error?.line, lineCount)
  if (attempt?.error && errLine !== null) {
    out.push({ kind: 'error', line: errLine, type: attempt.error.type, message: attempt.error.message })
  }
  for (const h of hints) {
    const line = clampLine(h.line, lineCount)
    if (line !== null) out.push({ kind: 'hint', line, level: h.level, text: h.text, hintId: h.hint_id })
  }
  return out
}
