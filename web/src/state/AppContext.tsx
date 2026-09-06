import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import * as api from '../api/client'
import type { Lang, Me, Progress } from '../api/types'
import { loadLang, saveLang } from './prefs'
import { getDeviceId } from './deviceId'

interface AppState {
  me: Me
  progress: Progress
  googleClientId: string
  online: boolean
  lang: Lang
  /** Persists the language on the server and mirrors it locally. */
  setLang: (lang: Lang) => Promise<void>
  refreshMe: () => Promise<void>
  refreshProgress: () => Promise<void>
  signIn: (idToken: string) => Promise<void>
  signOut: () => Promise<void>
}

const anonymous = (): Me => ({ signed_in: false, lang_pref: loadLang(), device_id: getDeviceId() })

const AppContext = createContext<AppState | null>(null)

/** Loads config, identity and progress once and exposes them app-wide. */
export function AppProvider({ children }: { children: ReactNode }) {
  const [me, setMe] = useState<Me>(anonymous)
  const [progress, setProgress] = useState<Progress>({ exercises: {} })
  const [googleClientId, setGoogleClientId] = useState('')
  const [online, setOnline] = useState(typeof navigator === 'undefined' ? true : navigator.onLine)

  const refreshMe = useCallback(async () => {
    try {
      const m = await api.getMe()
      setMe(m)
      saveLang(m.lang_pref)
    } catch {
      // Stay anonymous with the local language mirror.
    }
  }, [])

  const refreshProgress = useCallback(async () => {
    try {
      setProgress(await api.getProgress())
    } catch {
      // Keep whatever we last saw.
    }
  }, [])

  useEffect(() => {
    api.getConfig().then((c) => setGoogleClientId(c.google_client_id ?? '')).catch(() => {})
    void refreshMe()
    void refreshProgress()
  }, [refreshMe, refreshProgress])

  useEffect(() => {
    const up = () => setOnline(true)
    const down = () => setOnline(false)
    window.addEventListener('online', up)
    window.addEventListener('offline', down)
    return () => {
      window.removeEventListener('online', up)
      window.removeEventListener('offline', down)
    }
  }, [])

  const setLang = useCallback(async (lang: Lang) => {
    saveLang(lang)
    setMe((m) => ({ ...m, lang_pref: lang }))
    try {
      setMe(await api.updateMe(lang))
    } catch {
      // Local mirror already applied; server catches up on the next successful PUT.
    }
  }, [])

  const signIn = useCallback(
    async (idToken: string) => {
      const m = await api.signInWithGoogle(idToken)
      setMe(m)
      saveLang(m.lang_pref)
      await refreshProgress()
    },
    [refreshProgress],
  )

  const signOut = useCallback(async () => {
    try {
      await api.signOut()
    } finally {
      setMe(anonymous())
      await refreshMe()
      await refreshProgress()
    }
  }, [refreshMe, refreshProgress])

  const value = useMemo<AppState>(
    () => ({
      me,
      progress,
      googleClientId,
      online,
      lang: me.lang_pref,
      setLang,
      refreshMe,
      refreshProgress,
      signIn,
      signOut,
    }),
    [me, progress, googleClientId, online, setLang, refreshMe, refreshProgress, signIn, signOut],
  )

  return <AppContext.Provider value={value}>{children}</AppContext.Provider>
}

/** Access app-wide state; must be used under AppProvider. */
export function useApp(): AppState {
  const ctx = useContext(AppContext)
  if (!ctx) throw new Error('useApp outside AppProvider')
  return ctx
}
