import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useParams } from 'react-router-dom'
import type { EditorView } from '@codemirror/view'
import * as api from '../api/client'
import { ApiError } from '../api/client'
import type { Attempt, AttemptMode, Exercise as ExerciseT, Hint } from '../api/types'
import { CodeEditor } from '../editor/CodeEditor'
import { applyBarKey } from '../editor/setup'
import { useKeyboardOffset } from '../editor/useKeyboardOffset'
import { ActionBar, type Busy } from '../components/ActionBar'
import { KeyboardBar } from '../components/KeyboardBar'
import { OutputDrawer, type ResultView } from '../components/OutputDrawer'
import { StatementSheet } from '../components/StatementSheet'
import { useToast } from '../components/Toast'
import { useApp } from '../state/AppContext'
import { uuidv4 } from '../state/deviceId'
import { clearDraft, createDraftSaver, loadDraft, loadStdin, markOpened, saveStdin, wasOpened } from '../state/drafts'

/** Exercise view: statement, editor, symbol bar, actions and the output drawer. */
export function Exercise() {
  const params = useParams()
  const id = params['*'] ?? ''
  const { online, lang, setLang, refreshProgress } = useApp()
  const toast = useToast()

  const [exercise, setExercise] = useState<ExerciseT | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [code, setCode] = useState('')
  const [stdin, setStdin] = useState('')
  const [sheetOpen, setSheetOpen] = useState(true)
  const [busy, setBusy] = useState<Busy>(null)
  const [result, setResult] = useState<ResultView | null>(null)
  const [hints, setHints] = useState<Hint[]>([])
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [note, setNote] = useState<string | null>(null)

  const viewRef = useRef<EditorView | null>(null)
  const saver = useMemo(() => createDraftSaver(300), [])
  const kbOffset = useKeyboardOffset()

  // Load the exercise, restore the draft and stdin, and collapse the statement on repeat visits.
  useEffect(() => {
    let cancelled = false
    setExercise(null)
    setLoadError(null)
    setResult(null)
    setHints([])
    setDrawerOpen(false)
    api
      .getExercise(id)
      .then((ex) => {
        if (cancelled) return
        setExercise(ex)
        setCode(loadDraft(id) ?? ex.starter)
        // First Run uses the first example input, so input() never hits EOF out of the box.
        setStdin(loadStdin(id) || ex.visible_cases[0]?.stdin || '')
        setSheetOpen(!wasOpened(id))
        markOpened(id)
      })
      .catch((e: Error) => !cancelled && setLoadError(e.message))
    return () => {
      cancelled = true
      saver.flush()
    }
  }, [id, saver])

  const onCodeChange = useCallback(
    (doc: string) => {
      setCode(doc)
      saver.schedule(id, doc)
    },
    [id, saver],
  )

  const onStdinChange = (v: string) => {
    setStdin(v)
    saveStdin(id, v)
  }

  const reset = () => {
    if (!exercise) return
    if (!window.confirm('Replace your code with the starter?')) return
    clearDraft(id)
    setCode(exercise.starter)
  }

  const showError = (e: unknown) => {
    if (e instanceof ApiError) {
      if (e.code === 'rate_limited') {
        toast.show(`Thoda ruk jao — too many runs, try again in ${e.retryAfterSeconds ?? 30}s`, 'error')
        return
      }
      if (e.code === 'pool_busy') {
        toast.show('Servers are busy right now — try again in a minute', 'error')
        return
      }
    }
    toast.show(e instanceof Error ? e.message : 'Something went wrong', 'error')
  }

  /** Sends the current code; returns the attempt or null on error (already toasted). */
  const runAttempt = async (mode: AttemptMode): Promise<Attempt | null> => {
    saver.flush()
    setNote(null)
    try {
      const attempt = await api.createAttempt(
        { exercise_id: id, code, mode, stdin: mode === 'run' ? stdin : undefined, idempotency_key: uuidv4() },
        (n) => setNote(`Servers busy, retrying… (${n}/3)`),
      )
      setNote(null)
      setResult({ mode, attempt })
      setDrawerOpen(true)
      if (mode === 'submit') {
        if (attempt.passed) setHints([])
        void refreshProgress()
      }
      return attempt
    } catch (e) {
      setNote(null)
      showError(e)
      return null
    }
  }

  const doRun = async () => {
    setBusy('run')
    await runAttempt('run')
    setBusy(null)
  }

  const doSubmit = async () => {
    setBusy('submit')
    await runAttempt('submit')
    setBusy(null)
  }

  // Hints hang off an attempt, so a first Hint press submits silently before asking.
  const doHint = async () => {
    setBusy('hint')
    try {
      let attemptId = result?.attempt.attempt_id
      if (!attemptId) attemptId = (await runAttempt('submit'))?.attempt_id
      if (!attemptId) return
      const hint = await api.requestHint({ attempt_id: attemptId })
      setHints((hs) => [...hs, hint])
      setDrawerOpen(true)
    } catch (e) {
      showError(e)
    } finally {
      setBusy(null)
    }
  }

  const toggleLang = () => {
    void setLang(lang === 'hinglish' ? 'en' : 'hinglish')
    toast.show(lang === 'hinglish' ? 'Next hints will be in English' : 'Agle hints Hinglish mein aayenge')
  }

  const onBarKey = (key: string) => {
    const view = viewRef.current
    if (view) applyBarKey(view, key)
  }

  if (loadError) return <main className="page"><div className="err">Could not load this exercise: {loadError}</div></main>
  if (!exercise) return <main className="page"><div className="muted">Loading…</div></main>

  const dockStyle = { bottom: kbOffset }

  return (
    <main className="exercise">
      <StatementSheet exercise={exercise} open={sheetOpen} onToggle={() => setSheetOpen((o) => !o)} onReset={reset} />
      <CodeEditor value={code} onChange={onCodeChange} viewRef={viewRef} />

      <div className="drawer-anchor" style={dockStyle}>
        <OutputDrawer
          open={drawerOpen}
          onClose={() => setDrawerOpen(false)}
          result={result}
          stdin={stdin}
          onStdinChange={onStdinChange}
          hints={hints}
          lang={lang}
          onToggleLang={toggleLang}
        />
      </div>

      <div className="dock" style={dockStyle}>
        {!drawerOpen && (result || hints.length > 0) && (
          <button type="button" className="drawer-reopen" onClick={() => setDrawerOpen(true)}>
            ▴ {result?.mode === 'submit' ? (result.attempt.passed ? 'Passed' : 'Result') : 'Output'}
          </button>
        )}
        {note && <div className="note dock-note">{note}</div>}
        <ActionBar busy={busy} online={online} onRun={() => void doRun()} onSubmit={() => void doSubmit()} onHint={() => void doHint()} />
        <KeyboardBar onKey={onBarKey} />
      </div>
    </main>
  )
}
