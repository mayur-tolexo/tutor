import { describe, expect, it } from 'vitest'
import type { Attempt, Hint } from '../api/types'
import { buildPins, clampLine } from './pins'

const attempt = (error?: Attempt['error']): Attempt => ({
  attempt_id: 'a1', outcome: error ? 'runtime_error' : 'passed', passed: !error, duration_ms: 1, error,
})
const hint = (id: string, level: 1 | 2 | 3, line?: number): Hint => ({
  hint_id: id, level, source: 'model', lang: 'hinglish', text: `hint ${id}`, line,
})

describe('clampLine', () => {
  it('clamps into 1..lineCount and rejects missing values', () => {
    expect(clampLine(3, 10)).toBe(3)
    expect(clampLine(0, 10)).toBe(1)
    expect(clampLine(-4, 10)).toBe(1)
    expect(clampLine(99, 10)).toBe(10)
    expect(clampLine(2.6, 10)).toBe(3)
    expect(clampLine(undefined, 10)).toBeNull()
    expect(clampLine(NaN, 10)).toBeNull()
    expect(clampLine(1, 0)).toBeNull()
  })
})

describe('buildPins', () => {
  it('returns nothing without an attempt or lines', () => {
    expect(buildPins(null, [], 5)).toEqual([])
    expect(buildPins(attempt(), [hint('h1', 1)], 5)).toEqual([])
    expect(buildPins(attempt({ type: 'NameError', message: 'x' }), [], 5)).toEqual([])
  })

  it('pins the error line first, then hints in order', () => {
    const pins = buildPins(attempt({ type: 'ZeroDivisionError', message: 'division by zero', line: 4 }), [hint('h1', 1, 2), hint('h2', 2)], 10)
    expect(pins).toEqual([
      { kind: 'error', line: 4, type: 'ZeroDivisionError', message: 'division by zero' },
      { kind: 'hint', line: 2, level: 1, text: 'hint h1', hintId: 'h1' },
    ])
  })

  it('clamps lines past the end of the document', () => {
    const pins = buildPins(attempt({ type: 'SyntaxError', message: 'unexpected EOF', line: 12 }), [hint('h1', 3, 40)], 6)
    expect(pins.map((p) => p.line)).toEqual([6, 6])
  })
})
