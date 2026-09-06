import { describe, expect, it } from 'vitest'
import { stripMarkdown } from './speak'

describe('stripMarkdown', () => {
  it('removes inline code ticks and emphasis', () => {
    expect(stripMarkdown('Use `max()` and **check** the *first* value')).toBe('Use max() and check the first value')
    expect(stripMarkdown('__bold__ and _soft_')).toBe('bold and soft')
  })

  it('keeps underscores inside identifiers', () => {
    expect(stripMarkdown('call `my_func` or my_var')).toBe('call my_func or my_var')
  })

  it('drops code fences but keeps their content', () => {
    expect(stripMarkdown('Try:\n```python\nprint(a)\n```')).toBe('Try:\nprint(a)')
  })

  it('unwraps links and images, strips headings and bullets', () => {
    expect(stripMarkdown('# Idea\n- see [docs](http://x)\n- ![alt](http://i)')).toBe('Idea\nsee docs\nalt')
  })

  it('collapses whitespace and leaves plain text untouched', () => {
    expect(stripMarkdown('Pehle a aur b ko   compare karo.')).toBe('Pehle a aur b ko compare karo.')
    expect(stripMarkdown('')).toBe('')
  })
})
