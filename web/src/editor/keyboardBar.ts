// Pure editing primitives behind the on-screen keyboard bar. No CodeMirror
// imports so they can be unit-tested against plain strings.

/** Normalised selection over the document: from <= to, both in the original doc. */
export interface Sel {
  from: number
  to: number
}

/** A replacement expressed against the original document, like CodeMirror's ChangeSpec. */
export interface Change {
  from: number
  to: number
  insert: string
}

/** Result of a key press: non-overlapping changes in original coordinates, selection in new coordinates. */
export interface KeyEdit {
  changes: Change[]
  selection: Sel
}

export const INDENT = '    '

/** Keys shown on the bar, in display order. */
export const BAR_KEYS = [
  'Tab', '(', ')', '[', ']', '{', '}', ':', '=', '"', "'", ',', '<', '>', '+', '-', '*', '/', '_', '#', '←', '→',
] as const

const PAIRS: Record<string, string> = { '(': ')', '[': ']', '{': '}', '"': '"', "'": "'" }
const CLOSERS = new Set([')', ']', '}'])

const clamp = (n: number, len: number) => Math.max(0, Math.min(len, n))

const normalise = (sel: Sel): Sel => (sel.from <= sel.to ? sel : { from: sel.to, to: sel.from })

const cursor = (pos: number): Sel => ({ from: pos, to: pos })

/**
 * Computes the edit for a bar key at the given selection.
 * Openers wrap a selection or insert a pair with the cursor between; closers and
 * quotes skip over an identical next character; Tab indents; arrows move.
 */
export function insertKey(doc: string, rawSel: Sel, key: string): KeyEdit {
  const sel = normalise(rawSel)
  const { from, to } = sel
  const next = doc[to] ?? ''

  if (key === 'Tab') {
    if (from !== to) return indentSelection(doc, sel)
    return { changes: [{ from, to, insert: INDENT }], selection: cursor(from + INDENT.length) }
  }

  if (key === '←') {
    const pos = from === to ? clamp(from - 1, doc.length) : from
    return { changes: [], selection: cursor(pos) }
  }
  if (key === '→') {
    const pos = from === to ? clamp(to + 1, doc.length) : to
    return { changes: [], selection: cursor(pos) }
  }

  const closer = PAIRS[key]
  if (closer !== undefined) {
    // Wrap a non-empty selection, keeping the inner text selected.
    if (from !== to) {
      return {
        changes: [
          { from, to: from, insert: key },
          { from: to, to, insert: closer },
        ],
        selection: { from: from + 1, to: to + 1 },
      }
    }
    // A quote typed before the same quote steps over it instead of pairing.
    if (key === closer && next === key) {
      return { changes: [], selection: cursor(to + 1) }
    }
    return { changes: [{ from, to, insert: key + closer }], selection: cursor(from + 1) }
  }

  if (CLOSERS.has(key) && from === to && next === key) {
    return { changes: [], selection: cursor(to + 1) }
  }

  return { changes: [{ from, to, insert: key }], selection: cursor(from + key.length) }
}

/**
 * Prepends one indent unit to every line touched by the selection.
 * A selection ending at column 0 does not indent that final line.
 */
export function indentSelection(doc: string, rawSel: Sel): KeyEdit {
  const sel = normalise(rawSel)
  const { from } = sel
  let { to } = sel
  if (to > from && (to === 0 || doc[to - 1] === '\n')) to -= 1

  const starts: number[] = []
  let lineStart = doc.lastIndexOf('\n', from - 1) + 1
  starts.push(lineStart)
  for (let i = lineStart; i < to; i++) {
    if (doc[i] === '\n') starts.push(i + 1)
  }

  const changes = starts.map((s) => ({ from: s, to: s, insert: INDENT }))
  return {
    changes,
    selection: { from: from + INDENT.length, to: sel.to + INDENT.length * starts.length },
  }
}

/** Applies an edit to a plain string; used by tests and never by the editor itself. */
export function applyEdit(doc: string, edit: KeyEdit): string {
  const sorted = [...edit.changes].sort((a, b) => a.from - b.from)
  let out = ''
  let pos = 0
  for (const c of sorted) {
    out += doc.slice(pos, c.from) + c.insert
    pos = c.to
  }
  return out + doc.slice(pos)
}
