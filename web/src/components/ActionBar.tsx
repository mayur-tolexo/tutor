export type Busy = 'run' | 'submit' | 'hint' | null

interface Props {
  busy: Busy
  online: boolean
  onRun: () => void
  onSubmit: () => void
  onHint: () => void
}

/** Run / Submit / Hint. Disabled while a request is in flight or when offline. */
export function ActionBar({ busy, online, onRun, onSubmit, onHint }: Props) {
  const disabled = busy !== null || !online
  const label = (id: Busy, text: string, icon: string) =>
    busy === id ? (
      <>
        <span className="spinner" aria-hidden /> {text}
      </>
    ) : (
      <>
        <span aria-hidden>{icon}</span> {text}
      </>
    )
  return (
    <div className="abar">
      {!online && <div className="abar-note">You are offline — Run, Submit and Hint need a connection.</div>}
      <div className="abar-row">
        <button type="button" className="btn btn-run" disabled={disabled} onClick={onRun} onMouseDown={(e) => e.preventDefault()}>
          {label('run', 'Run', '▶')}
        </button>
        <button type="button" className="btn btn-submit" disabled={disabled} onClick={onSubmit} onMouseDown={(e) => e.preventDefault()}>
          {label('submit', 'Submit', '✓')}
        </button>
        <button type="button" className="btn btn-hint" disabled={disabled} onClick={onHint} onMouseDown={(e) => e.preventDefault()}>
          {label('hint', 'Hint', '💡')}
        </button>
      </div>
    </div>
  )
}
