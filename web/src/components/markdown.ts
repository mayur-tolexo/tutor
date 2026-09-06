import { marked } from 'marked'
import DOMPurify from 'dompurify'

/** Full markdown for statements, sanitised to safe HTML. */
export function renderMarkdown(md: string): string {
  const html = marked.parse(md, { async: false, gfm: true, breaks: true }) as string
  return DOMPurify.sanitize(html, { USE_PROFILES: { html: true } })
}

const INLINE_TAGS = ['code', 'strong', 'em', 'b', 'i', 'br', 'span', 'del', 'sub', 'sup']

/** Light markdown for hints: inline code and emphasis only; blocks, links and images are stripped. */
export function renderInlineMarkdown(md: string): string {
  const html = marked.parseInline(md, { async: false, gfm: true, breaks: true }) as string
  return DOMPurify.sanitize(html, { ALLOWED_TAGS: INLINE_TAGS, ALLOWED_ATTR: [], KEEP_CONTENT: true })
}
