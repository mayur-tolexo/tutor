import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import * as api from '../api/client'
import type { Track as TrackT, TrackExercise } from '../api/types'
import { useApp } from '../state/AppContext'
import { readJson, writeJson } from '../state/storage'

const COLLAPSED_KEY = 'tutor.collapsed_units'

type Status = 'not-started' | 'attempted' | 'passed'

/** Chip showing per-exercise progress. */
function StatusChip({ status }: { status: Status }) {
  const text = status === 'passed' ? 'passed' : status === 'attempted' ? 'attempted' : 'not started'
  return <span className={`chip chip-${status}`}>{text}</span>
}

/** Track view: collapsible units listing exercises with progress chips. */
export function Track() {
  const { progress } = useApp()
  const [tracks, setTracks] = useState<TrackT[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [collapsed, setCollapsed] = useState<string[]>(() => readJson<string[]>(COLLAPSED_KEY, []))

  useEffect(() => {
    api
      .getTracks()
      .then((r) => setTracks(r.tracks))
      .catch((e: Error) => setError(e.message))
  }, [])

  const toggle = (unitId: string) => {
    setCollapsed((cur) => {
      const next = cur.includes(unitId) ? cur.filter((u) => u !== unitId) : [...cur, unitId]
      writeJson(COLLAPSED_KEY, next)
      return next
    })
  }

  const statusOf = (ex: TrackExercise): Status => progress.exercises[ex.id]?.status ?? 'not-started'

  if (error) return <main className="page"><div className="err">Could not load the track: {error}</div></main>
  if (!tracks) return <main className="page"><div className="muted">Loading…</div></main>

  return (
    <main className="page">
      {tracks.map((t) => {
        const all = t.units.flatMap((u) => u.exercises)
        const passed = all.filter((e) => statusOf(e) === 'passed').length
        return (
          <section key={t.id} className="track">
            <h1 className="track-title">{t.title}</h1>
            <div className="progressbar" aria-label={`${passed} of ${all.length} passed`}>
              <div className="progressbar-fill" style={{ width: `${all.length ? (passed / all.length) * 100 : 0}%` }} />
            </div>
            <div className="muted small">
              {passed} / {all.length} passed
            </div>
            {t.units.map((u) => {
              const isOpen = !collapsed.includes(u.id)
              const uPassed = u.exercises.filter((e) => statusOf(e) === 'passed').length
              return (
                <div key={u.id} className="unit">
                  <button className="unit-head" onClick={() => toggle(u.id)} aria-expanded={isOpen}>
                    <span className="chev" aria-hidden>
                      {isOpen ? '▾' : '▸'}
                    </span>
                    <span className="unit-title">{u.title}</span>
                    <span className="muted small">
                      {uPassed}/{u.exercises.length}
                    </span>
                  </button>
                  {isOpen && (
                    <ul className="exlist">
                      {u.exercises.map((ex) => (
                        <li key={ex.id}>
                          <Link to={`/ex/${ex.id}`} className="exrow">
                            <span className="exrow-main">
                              <span className="exrow-title">{ex.title}</span>
                              <span className="exrow-meta">
                                <span className="stars" aria-label={`difficulty ${ex.difficulty}`}>
                                  {'★'.repeat(ex.difficulty)}
                                </span>
                                {ex.must_pass && <span className="must">must pass</span>}
                              </span>
                            </span>
                            <StatusChip status={statusOf(ex)} />
                          </Link>
                        </li>
                      ))}
                    </ul>
                  )}
                </div>
              )
            })}
          </section>
        )
      })}
    </main>
  )
}
