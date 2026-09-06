import { useEffect, useRef } from 'react'

const GSI_SRC = 'https://accounts.google.com/gsi/client'

interface GsiCredential {
  credential: string
}

interface GsiApi {
  accounts: {
    id: {
      initialize: (cfg: { client_id: string; callback: (r: GsiCredential) => void }) => void
      renderButton: (el: HTMLElement, opts: Record<string, string | number>) => void
      disableAutoSelect: () => void
    }
  }
}

declare global {
  interface Window {
    google?: GsiApi
  }
}

let loading: Promise<void> | null = null

/** Loads the GSI script once and resolves when `window.google` is ready. */
function loadGsi(): Promise<void> {
  if (window.google) return Promise.resolve()
  if (loading) return loading
  loading = new Promise((resolve, reject) => {
    const s = document.createElement('script')
    s.src = GSI_SRC
    s.async = true
    s.defer = true
    s.onload = () => resolve()
    s.onerror = () => {
      loading = null
      reject(new Error('gsi load failed'))
    }
    document.head.appendChild(s)
  })
  return loading
}

interface Props {
  clientId: string
  onCredential: (idToken: string) => void
}

/** Renders Google's own sign-in button and forwards the ID token. */
export function GoogleSignIn({ clientId, onCredential }: Props) {
  const host = useRef<HTMLDivElement>(null)
  const cb = useRef(onCredential)
  cb.current = onCredential

  useEffect(() => {
    let cancelled = false
    loadGsi()
      .then(() => {
        if (cancelled || !host.current || !window.google) return
        window.google.accounts.id.initialize({ client_id: clientId, callback: (r) => cb.current(r.credential) })
        window.google.accounts.id.renderButton(host.current, {
          type: 'standard',
          size: 'medium',
          theme: 'outline',
          text: 'signin',
          shape: 'pill',
        })
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [clientId])

  return <div ref={host} className="gsi-host" />
}
