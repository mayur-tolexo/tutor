import { describe, expect, it } from 'vitest'
import { renderInlineMarkdown, renderMarkdown } from './markdown'

describe('markdown rendering', () => {
  it('renders inline code and bold in hints', () => {
    expect(renderInlineMarkdown('Use `max()` and **check** a')).toBe('Use <code>max()</code> and <strong>check</strong> a')
  })

  it('strips block elements, links, images and scripts from hints but keeps text', () => {
    const out = renderInlineMarkdown('# big [x](http://e) ![i](http://e/i.png) <script>alert(1)</script> <a href="x">y</a>')
    expect(out).not.toMatch(/<(h1|a|img|script)/)
    expect(out).toContain('# big')
    expect(out).toContain('y')
    expect(out).not.toContain('alert(1)')
  })

  it('keeps line breaks in hints', () => {
    expect(renderInlineMarkdown('a\nb')).toBe('a<br>b')
  })

  it('renders statement markdown as sanitised block HTML', () => {
    const out = renderMarkdown('## Task\n\n- one\n- two\n\n<img src=x onerror="alert(1)">')
    expect(out).toContain('<h2>Task</h2>')
    expect(out).toContain('<li>one</li>')
    expect(out).not.toContain('onerror')
  })
})
