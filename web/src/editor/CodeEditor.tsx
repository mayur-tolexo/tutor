import { useEffect, useRef, type MutableRefObject } from 'react'
import { EditorView } from '@codemirror/view'
import { createState } from './setup'
import { highlightCompartment, highlightFor, watchScheme } from './theme'

interface Props {
  /** Initial or externally reset code; only applied when it differs from the editor's doc. */
  value: string
  onChange: (doc: string) => void
  /** Receives the live view so the keyboard bar can dispatch into it. */
  viewRef: MutableRefObject<EditorView | null>
}

/** CodeMirror-backed Python editor that fills its container. */
export function CodeEditor({ value, onChange, viewRef }: Props) {
  const hostRef = useRef<HTMLDivElement>(null)
  const onChangeRef = useRef(onChange)
  onChangeRef.current = onChange

  useEffect(() => {
    if (!hostRef.current) return
    const view = new EditorView({
      state: createState(value, (doc) => onChangeRef.current(doc)),
      parent: hostRef.current,
    })
    viewRef.current = view
    const unwatch = watchScheme((dark) => view.dispatch({ effects: highlightCompartment.reconfigure(highlightFor(dark)) }))
    return () => {
      unwatch()
      view.destroy()
      viewRef.current = null
    }
    // The view owns the document after mount; `value` changes are handled below.
  }, [])

  // External resets (starter, restored draft) replace the doc without remounting.
  useEffect(() => {
    const view = viewRef.current
    if (!view) return
    const current = view.state.doc.toString()
    if (current !== value) {
      view.dispatch({ changes: { from: 0, to: current.length, insert: value } })
    }
  }, [value, viewRef])

  return <div className="editor" ref={hostRef} />
}
