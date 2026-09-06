import { HighlightStyle, defaultHighlightStyle, syntaxHighlighting } from '@codemirror/language'
import { Compartment, type Extension } from '@codemirror/state'
import { tags as t } from '@lezer/highlight'

/** Token colours tuned for a dark background; the light scheme uses CodeMirror's default. */
const darkHighlight = HighlightStyle.define([
  { tag: t.keyword, color: '#E8A356' },
  { tag: [t.controlKeyword, t.operatorKeyword], color: '#E8A356' },
  { tag: [t.definition(t.variableName), t.function(t.variableName)], color: '#D9C6A5' },
  { tag: [t.function(t.propertyName), t.propertyName], color: '#9FC4B8' },
  { tag: [t.string, t.special(t.string)], color: '#A8C686' },
  { tag: [t.number, t.bool, t.null, t.atom], color: '#8FB8D8' },
  { tag: t.comment, color: '#8A8272', fontStyle: 'italic' },
  { tag: t.operator, color: '#C9B99A' },
  { tag: [t.className, t.typeName], color: '#E3B77E' },
  { tag: t.invalid, color: '#E06B5A' },
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
