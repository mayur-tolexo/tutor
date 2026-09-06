import { describe, expect, it } from 'vitest'
import { applyEdit, indentSelection, insertKey, type Sel } from './keyboardBar'

const at = (pos: number): Sel => ({ from: pos, to: pos })

/** Renders the resulting doc with | marking the cursor (or [..] a selection). */
function show(doc: string, sel: Sel, key: string): string {
  const edit = insertKey(doc, sel, key)
  const out = applyEdit(doc, edit)
  const { from, to } = edit.selection
  return from === to ? out.slice(0, from) + '|' + out.slice(from) : out.slice(0, from) + '[' + out.slice(from, to) + ']' + out.slice(to)
}

describe('insertKey auto-pair', () => {
  it('inserts a pair with the cursor between', () => {
    expect(show('print', at(5), '(')).toBe('print(|)')
    expect(show('x = ', at(4), '[')).toBe('x = [|]')
    expect(show('d = ', at(4), '{')).toBe('d = {|}')
    expect(show('s = ', at(4), '"')).toBe('s = "|"')
    expect(show('s = ', at(4), "'")).toBe("s = '|'")
  })

  it('pairs brackets even when the next char is a closer', () => {
    expect(show('f()', at(2), '(')).toBe('f((|))')
  })

  it('wraps a selection and keeps it selected', () => {
    expect(show('print x', { from: 6, to: 7 }, '(')).toBe('print ([x])')
    expect(show('ab', { from: 2, to: 0 }, '"')).toBe('"[ab]"')
  })
})

describe('insertKey skip-over-closer', () => {
  it('steps over an identical closer instead of inserting', () => {
    expect(show('f()', at(2), ')')).toBe('f()|')
    expect(show('a[]', at(2), ']')).toBe('a[]|')
    expect(show('{}', at(1), '}')).toBe('{}|')
  })

  it('steps over a matching quote', () => {
    expect(show('""', at(1), '"')).toBe('""|')
    expect(show("''", at(1), "'")).toBe("''|")
  })

  it('inserts the closer when the next char differs', () => {
    expect(show('f(x', at(3), ')')).toBe('f(x)|')
    expect(show('a', at(1), ')')).toBe('a)|')
  })

  it('does not skip over a closer when there is a selection', () => {
    expect(show('x)', { from: 0, to: 1 }, ')')).toBe(')|)')
  })
})

describe('insertKey plain keys and arrows', () => {
  it('inserts literal keys and replaces a selection', () => {
    expect(show('a  b', at(2), '+')).toBe('a +| b')
    expect(show('abc', { from: 1, to: 2 }, ':')).toBe('a:|c')
    expect(show('', at(0), '#')).toBe('#|')
  })

  it('moves the cursor with arrows and clamps at the edges', () => {
    expect(show('ab', at(1), '←')).toBe('|ab')
    expect(show('ab', at(0), '←')).toBe('|ab')
    expect(show('ab', at(1), '→')).toBe('ab|')
    expect(show('ab', at(2), '→')).toBe('ab|')
  })

  it('collapses a selection toward the arrow direction', () => {
    expect(show('abcd', { from: 1, to: 3 }, '←')).toBe('a|bcd')
    expect(show('abcd', { from: 1, to: 3 }, '→')).toBe('abc|d')
  })
})

describe('Tab', () => {
  it('inserts four spaces at a collapsed cursor', () => {
    expect(show('x', at(0), 'Tab')).toBe('    |x')
    expect(show('if a:\n', at(6), 'Tab')).toBe('if a:\n    |')
  })

  it('indents a single partially selected line', () => {
    expect(show('abc', { from: 1, to: 2 }, 'Tab')).toBe('    a[b]c')
  })

  it('indents every line touched by a multi-line selection', () => {
    const doc = 'a\nb\nc\nd'
    const edit = insertKey(doc, { from: 1, to: 5 }, 'Tab')
    expect(applyEdit(doc, edit)).toBe('    a\n    b\n    c\nd')
    expect(edit.selection).toEqual({ from: 5, to: 17 })
  })

  it('does not indent a trailing line when the selection ends at its start', () => {
    const doc = 'a\nb\nc'
    expect(applyEdit(doc, indentSelection(doc, { from: 0, to: 4 }))).toBe('    a\n    b\nc')
  })

  it('indents the whole document when everything is selected', () => {
    const doc = 'a\nb'
    expect(applyEdit(doc, indentSelection(doc, { from: 0, to: 3 }))).toBe('    a\n    b')
  })
})
