import type { Attempt, AttemptMode, CaseResult, Hint, Lang } from '../api/types'
import { HintCard } from './HintCard'

export const TIMEOUT_MESSAGE = "Program didn't finish in time — infinite loop?"

export interface ResultView {
  mode: AttemptMode
  attempt: Attempt
}

interface Props {
  open: boolean
  onClose: () => void
  result: ResultView | null
  stdin: string
  onStdinChange: (v: string) => void
  hints: Hint[]
  lang: Lang
  onToggleLang: () => void
}

/** Slide-up panel showing run output, submit verdicts and stacked hints. */
export function OutputDrawer({ open, onClose, result, stdin, onStdinChange, hints, lang, onToggleLang }: Props) {
  const title = result?.mode === 'run' ? 'Output' : result?.mode === 'submit' ? 'Result' : 'Hints'
  return (
    <div className={`drawer ${open ? 'open' : ''}`} aria-hidden={!open}>
      <div className="drawer-head">
        <span className="drawer-title">{title}</span>
        {result && <span className="drawer-ms">{result.attempt.duration_ms} ms</span>}
        <span className="spacer" />
        <button type="button" className="btn btn-ghost btn-xs" onClick={onClose} aria-label="Close output">
          ▾
        </button>
      </div>
      <div className="drawer-body">
        {result?.mode === 'run' && <RunPane attempt={result.attempt} stdin={stdin} onStdinChange={onStdinChange} />}
        {result?.mode === 'submit' && <SubmitPane attempt={result.attempt} />}
        {hints.map((h) => (
          <HintCard key={h.hint_id} hint={h} lang={lang} onToggleLang={onToggleLang} />
        ))}
      </div>
    </div>
  )
}

function RunPane({ attempt, stdin, onStdinChange }: { attempt: Attempt; stdin: string; onStdinChange: (v: string) => void }) {
  const run = attempt.run
  return (
    <>
      <label className="field">
        <span className="field-label">Input (stdin) — one value per line, then Run again</span>
        <textarea
          className="stdin"
          rows={2}
          value={stdin}
          onChange={(e) => onStdinChange(e.target.value)}
          autoCapitalize="off"
          autoCorrect="off"
          spellCheck={false}
          placeholder="e.g. 5"
        />
      </label>
      {attempt.error && <ErrorBlock error={attempt.error} />}
      {run?.timed_out && <div className="err">{TIMEOUT_MESSAGE}</div>}
      {run && (
        <>
          <div className="pane-label">Output</div>
          <pre className="stdout">{run.stdout || <span className="muted">(no output)</span>}</pre>
          {run.stderr && <pre className="stderr">{run.stderr}</pre>}
          {run.exit_code !== 0 && !run.timed_out && <div className="muted small">exit code {run.exit_code}</div>}
        </>
      )}
      {!run && attempt.outcome === 'infra_error' && <div className="err">Could not run your program. Try again.</div>}
    </>
  )
}

function SubmitPane({ attempt }: { attempt: Attempt }) {
  const cases = attempt.cases ?? []
  const firstFail = cases.find((c) => !c.passed && !c.hidden && (c.expected !== undefined || c.actual !== undefined))
  if (attempt.passed) {
    return (
      <div className="passed">
        <div className="passed-big">Passed!</div>
        <div className="muted">
          {cases.length} / {cases.length} cases — shabash!
        </div>
      </div>
    )
  }
  return (
    <>
      {attempt.error && <ErrorBlock error={attempt.error} />}
      {attempt.outcome === 'timeout' && <div className="err">{TIMEOUT_MESSAGE}</div>}
      <ul className="cases">
        {cases.map((c, i) => (
          <li key={c.id} className={c.passed ? 'ok' : 'bad'}>
            <span className="case-mark" aria-hidden>
              {c.passed ? '✓' : '✗'}
            </span>
            <span>Case {i + 1}</span>
            {c.hidden && <span className="muted small">hidden</span>}
            {c.timed_out && <span className="err small">timed out</span>}
          </li>
        ))}
      </ul>
      {firstFail && <FailDetail c={firstFail} />}
    </>
  )
}

function FailDetail({ c }: { c: CaseResult }) {
  return (
    <div className="fail">
      <div className="fail-col">
        <div className="pane-label">Input</div>
        <pre>{c.stdin ?? ''}</pre>
      </div>
      <div className="fail-col">
        <div className="pane-label">Expected</div>
        <pre>{c.expected ?? ''}</pre>
      </div>
      <div className="fail-col">
        <div className="pane-label">Your output</div>
        <pre className={c.stderr ? 'stderr' : ''}>{c.actual || c.stderr || <span className="muted">(nothing)</span>}</pre>
      </div>
    </div>
  )
}

function ErrorBlock({ error }: { error: NonNullable<Attempt['error']> }) {
  return (
    <div className="err">
      <strong>{error.type}</strong>
      {error.line !== undefined && <> at line {error.line}</>}: {error.message}
    </div>
  )
}
