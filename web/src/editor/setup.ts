import { EditorState, type Extension } from '@codemirror/state'
import { EditorView, highlightActiveLine, keymap, lineNumbers } from '@codemirror/view'
import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands'
import { python } from '@codemirror/lang-python'
import { bracketMatching, indentUnit } from '@codemirror/language'
import { insertKey, type Sel } from './keyboardBar'
import { highlightCompartment, highlightFor, prefersDark } from './theme'
import { pinsExtension } from './pinField'

/** Extensions shared by every editor instance; Python indentation on Enter comes from lang-python. */
export function baseExtensions(onChange: (doc: string) => void): Extension[] {
  return [
    lineNumbers(),
    highlightActiveLine(),
    history(),
    bracketMatching(),
    indentUnit.of('    '),
    python(),
    highlightCompartment.of(highlightFor(prefersDark())),
    pinsExtension(),
    keymap.of([indentWithTab, ...defaultKeymap, ...historyKeymap]),
    EditorView.lineWrapping,
    // Keep phone keyboards from auto-correcting or capitalising code.
    EditorView.contentAttributes.of({
      autocapitalize: 'off',
      autocorrect: 'off',
      spellcheck: 'false',
      inputmode: 'text',
      'aria-label': 'Python code',
    }),
    EditorView.updateListener.of((u) => {
      if (u.docChanged) onChange(u.state.doc.toString())
    }),
  ]
}

/** Builds a fresh editor state for the given code. */
export function createState(doc: string, onChange: (doc: string) => void): EditorState {
  return EditorState.create({ doc, extensions: baseExtensions(onChange) })
}

/** Applies a keyboard-bar key at the main selection and keeps the editor focused. */
export function applyBarKey(view: EditorView, key: string): void {
  const main = view.state.selection.main
  const sel: Sel = { from: main.from, to: main.to }
  const edit = insertKey(view.state.doc.toString(), sel, key)
  view.dispatch({
    changes: edit.changes,
    selection: { anchor: edit.selection.from, head: edit.selection.to },
    scrollIntoView: true,
    userEvent: 'input.type',
  })
  view.focus()
}
