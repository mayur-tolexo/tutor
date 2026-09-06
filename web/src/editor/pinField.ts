import { RangeSet, StateEffect, StateField, type Extension, type Text } from '@codemirror/state'
import { Decoration, EditorView, GutterMarker, WidgetType, gutterLineClass, type DecorationSet } from '@codemirror/view'
import type { Lang } from '../api/types'
import { renderInlineMarkdown } from '../components/markdown'
import { isSpeechAvailable, speak } from '../speech/speak'
import type { PinSpec } from './pins'

/** Replaces every pin; an empty list clears them. */
export const setPins = StateEffect.define<{ pins: PinSpec[]; lang: Lang }>()

interface PinState {
  decos: DecorationSet
  /** 1-based lines that carry a pin, in the current document. */
  lines: number[]
}

const empty: PinState = { decos: Decoration.none, lines: [] }

/** Outer widget element: padding only, so CodeMirror measures the full height (margins would be missed). */
function wrap(box: HTMLElement): HTMLElement {
  const el = document.createElement('div')
  el.className = 'cm-pin'
  el.append(box)
  return el
}

class ErrorWidget extends WidgetType {
  constructor(private readonly type: string, private readonly message: string) {
    super()
  }
  eq(other: ErrorWidget) {
    return other.type === this.type && other.message === this.message
  }
  toDOM() {
    const box = document.createElement('div')
    box.className = 'cm-pin-box cm-pin-error'
    const t = document.createElement('strong')
    t.textContent = this.type
    box.append(t, document.createTextNode(`: ${this.message}`))
    return wrap(box)
  }
  ignoreEvent() {
    return true
  }
}

class HintWidget extends WidgetType {
  constructor(private readonly level: number, private readonly text: string, private readonly lang: Lang, private readonly id: string) {
    super()
  }
  eq(other: HintWidget) {
    return other.id === this.id && other.lang === this.lang
  }
  toDOM() {
    const box = document.createElement('div')
    box.className = 'cm-pin-box cm-pin-hint'
    const head = document.createElement('div')
    head.className = 'cm-pin-head'
    const badge = document.createElement('span')
    badge.className = `badge badge-l${this.level}`
    badge.textContent = `Hint ${this.level}/3`
    head.append(badge)
    if (isSpeechAvailable()) {
      const btn = document.createElement('button')
      btn.type = 'button'
      btn.className = 'btn btn-ghost btn-xs speak-btn'
      btn.textContent = this.lang === 'en' ? '🔊 Listen' : '🔊 Suno'
      // Keep the editor focused so the phone keyboard and symbol bar stay put.
      btn.addEventListener('mousedown', (e) => e.preventDefault())
      btn.addEventListener('pointerdown', (e) => e.preventDefault())
      btn.addEventListener('click', () => speak(this.text, this.lang))
      head.append(btn)
    }
    const body = document.createElement('div')
    body.className = 'cm-pin-text md'
    body.innerHTML = renderInlineMarkdown(this.text)
    box.append(head, body)
    return wrap(box)
  }
  ignoreEvent() {
    return true
  }
}

const errorLine = Decoration.line({ class: 'cm-line-error' })

class DotMarker extends GutterMarker {
  elementClass = 'cm-gutter-error'
}
const dot = new DotMarker()

/** Builds decorations for the given pins against the current document. */
function build(doc: Text, pins: PinSpec[], lang: Lang): PinState {
  const ranges = []
  const lines = new Set<number>()
  for (const p of pins) {
    const line = doc.line(Math.min(p.line, doc.lines))
    lines.add(line.number)
    if (p.kind === 'error') {
      ranges.push(errorLine.range(line.from))
      ranges.push(Decoration.widget({ widget: new ErrorWidget(p.type, p.message), block: true, side: 1 }).range(line.to))
    } else {
      ranges.push(Decoration.widget({ widget: new HintWidget(p.level, p.text, lang, p.hintId), block: true, side: 2 }).range(line.to))
    }
  }
  return { decos: Decoration.set(ranges, true), lines: [...lines].sort((a, b) => a - b) }
}

/** Line-anchored feedback; cleared as soon as an edit touches a pinned line. */
export const pinField = StateField.define<PinState>({
  create: () => empty,
  update(value, tr) {
    for (const e of tr.effects) {
      if (e.is(setPins)) return build(tr.newDoc, e.value.pins, e.value.lang)
    }
    if (!tr.docChanged || value.lines.length === 0) return value
    let touched = false
    tr.changes.iterChangedRanges((fromA, toA) => {
      for (const n of value.lines) {
        const l = tr.startState.doc.line(n)
        if (fromA <= l.to && toA >= l.from) touched = true
      }
    })
    if (touched) return empty
    const decos = value.decos.map(tr.changes)
    const lines = new Set<number>()
    decos.between(0, tr.newDoc.length, (from) => {
      lines.add(tr.newDoc.lineAt(from).number)
    })
    return { decos, lines: [...lines].sort((a, b) => a - b) }
  },
  provide: (f) => [
    EditorView.decorations.from(f, (v) => v.decos),
    gutterLineClass.compute([f], (state) => {
      const marks = state
        .field(f)
        .lines.map((n) => dot.range(state.doc.line(n).from))
      return RangeSet.of(marks, true)
    }),
  ],
})

/** Extension bundle for pinned feedback. */
export function pinsExtension(): Extension {
  return [pinField]
}

/** Dispatches a new pin set into the view. */
export function applyPins(view: EditorView, pins: PinSpec[], lang: Lang): void {
  view.dispatch({ effects: setPins.of({ pins, lang }) })
}
