import { Suspense, lazy } from 'react'
import { BrowserRouter, Route, Routes } from 'react-router-dom'
import { Header } from './components/Header'
import { ToastProvider } from './components/Toast'
import { AppProvider } from './state/AppContext'
import { Track } from './pages/Track'

// The exercise page pulls in CodeMirror, so it loads on demand.
const Exercise = lazy(() => import('./pages/Exercise').then((m) => ({ default: m.Exercise })))
const loading = <main className="page"><div className="muted">Loading…</div></main>

/** Router and providers; the exercise route is a splat because ids contain slashes. */
export function App() {
  return (
    <BrowserRouter>
      <ToastProvider>
        <AppProvider>
          <div className="app">
            <Header />
            <Routes>
              <Route path="/" element={<Track />} />
              <Route
                path="/ex/*"
                element={
                  <Suspense fallback={loading}>
                    <Exercise />
                  </Suspense>
                }
              />
              <Route path="*" element={<main className="page"><div className="muted">Page not found.</div></main>} />
            </Routes>
          </div>
        </AppProvider>
      </ToastProvider>
    </BrowserRouter>
  )
}
