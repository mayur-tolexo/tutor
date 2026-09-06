import { HighlightStyle, defaultHighlightStyle, syntaxHighlighting } from '@codemirror/language'
import { Compartment, type Extension } from '@codemirror/state'
import { tags as t } from '@lezer/highlight'

/** Token colours tuned for a dark background; the light scheme uses CodeMirror's default. */
const darkHighlight = HighlightStyle.define([
  { tag: t.keyword, color: '#ff7b72' },
  { tag: [t.controlKeyword, t.operatorKeyword], color: '#ff7b72' },
  { tag: [t.definition(t.variableName), t.function(t.variableName)], color: '#d2a8ff' },
  { tag: [t.function(t.propertyName), t.propertyName], color: '#79c0ff' },
  { tag: [t.string, t.special(t.string)], color: '#a5d6ff' },
  { tag: [t.number, t.bool, t.null, t.atom], color: '#79c0ff' },
  { tag: t.comment, color: '#8b949e', fontStyle: 'italic' },
  { tag: t.operator, color: '#ff7b72' },
  { tag: [t.className, t.typeName], color: '#ffa657' },
  { tag: t.invalid, color: '#f85149' },
])

const query = '(prefers-color-scheme: dark)'

/** Highlight extension matching the current colour scheme. */
export function highlightFor(dark: boolean): Extension {
  return syntaxHighlighting(dark ? darkHighlight : defaultHighlightStyle, { fallback: true })
}

/** Compartment so the highlight style can be swapped when the scheme changes. */
export const highlightCompartment = new Compartment()

/** True when the OS prefers a dark scheme. */
export function prefersDark(): boolean {
  return typeof window !== 'undefined' && !!window.matchMedia && window.matchMedia(query).matches
}

/** Calls `onChange` when the colour scheme flips; returns an unsubscribe. */
export function watchScheme(onChange: (dark: boolean) => void): () => void {
  if (typeof window === 'undefined' || !window.matchMedia) return () => {}
  const mq = window.matchMedia(query)
  const handler = (e: MediaQueryListEvent) => onChange(e.matches)
  mq.addEventListener('change', handler)
  return () => mq.removeEventListener('change', handler)
}
