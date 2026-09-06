import { createContext, useCallback, useContext, useMemo, useRef, useState, type ReactNode } from 'react'

interface ToastItem {
  id: number
  text: string
  kind: 'info' | 'error'
}

interface ToastApi {
  /** Shows a transient message; errors stay a little longer. */
  show: (text: string, kind?: 'info' | 'error') => void
}

const ToastContext = createContext<ToastApi>({ show: () => {} })

/** Provides `useToast` and renders the stacked toasts at the top of the screen. */
export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([])
  const seq = useRef(0)

  const show = useCallback((text: string, kind: 'info' | 'error' = 'info') => {
    const id = ++seq.current
    setItems((xs) => [...xs, { id, text, kind }])
    setTimeout(() => setItems((xs) => xs.filter((x) => x.id !== id)), kind === 'error' ? 6000 : 3500)
  }, [])

  const api = useMemo(() => ({ show }), [show])

  return (
    <ToastContext.Provider value={api}>
      {children}
      <div className="toasts" role="status" aria-live="polite">
        {items.map((t) => (
          <div key={t.id} className={`toast toast-${t.kind}`}>
            {t.text}
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  )
}

/** Access the toast API from any component. */
export function useToast(): ToastApi {
  return useContext(ToastContext)
}
