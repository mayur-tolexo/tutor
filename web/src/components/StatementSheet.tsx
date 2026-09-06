import { useMemo } from 'react'
import type { Exercise } from '../api/types'
import { renderMarkdown } from './markdown'

interface Props {
  exercise: Exercise
  open: boolean
  onToggle: () => void
  /** Replaces the draft with the starter code. */
  onReset: () => void
}

/** Collapsible problem statement with visible test cases as example blocks. */
export function StatementSheet({ exercise, open, onToggle, onReset }: Props) {
  const html = useMemo(() => renderMarkdown(exercise.statement_md), [exercise.statement_md])

  return (
    <section className={`sheet ${open ? 'open' : ''}`}>
      <div className="sheet-head">
        <button className="sheet-toggle" onClick={onToggle} aria-expanded={open}>
          <span className="chev" aria-hidden>
            {open ? '▾' : '▸'}
          </span>
          <span className="sheet-title">{exercise.title}</span>
          <span className="sheet-meta">{'★'.repeat(exercise.difficulty)}</span>
        </button>
        <button type="button" className="btn btn-ghost btn-xs" onClick={onReset} title="Reset to starter">
          Reset
        </button>
      </div>
      {open && (
        <div className="sheet-body">
          <div className="md" dangerouslySetInnerHTML={{ __html: html }} />
          {exercise.visible_cases.length > 0 && (
            <div className="examples">
              {exercise.visible_cases.map((c, i) => (
                <div className="example" key={c.id}>
                  <div className="example-col">
                    <div className="example-label">Example input {exercise.visible_cases.length > 1 ? i + 1 : ''}</div>
                    <pre>{c.stdin || '(no input)'}</pre>
                  </div>
                  <div className="example-col">
                    <div className="example-label">Example output</div>
                    <pre>{c.stdout}</pre>
                  </div>
                </div>
              ))}
            </div>
          )}
          {exercise.concepts.length > 0 && (
            <div className="concepts">
              {exercise.concepts.map((c) => (
                <span className="tag" key={c}>
                  {c}
                </span>
              ))}
            </div>
          )}
        </div>
      )}
    </section>
  )
}
