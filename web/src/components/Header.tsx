import { Link } from 'react-router-dom'
import { useApp } from '../state/AppContext'
import { useToast } from './Toast'
import { GoogleSignIn } from './GoogleSignIn'

/** Top bar: app name, offline pill, and sign-in or the signed-in identity. */
export function Header() {
  const { me, online, googleClientId, signIn, signOut } = useApp()
  const toast = useToast()

  const onCredential = (token: string) => {
    signIn(token).catch((e: Error) => toast.show(e.message || 'Sign-in failed', 'error'))
  }

  return (
    <header className="header">
      <Link to="/" className="brand" aria-label="tutor home">
        <span className="mark" aria-hidden />
        tutor
      </Link>
      {!online && <span className="pill pill-offline">offline</span>}
      <div className="spacer" />
      {me.signed_in ? (
        <div className="who">
          <span className="avatar" aria-hidden>
            {(me.display_name ?? '?').slice(0, 1).toUpperCase()}
          </span>
          <span className="who-name">{me.display_name ?? 'Signed in'}</span>
          <button className="btn btn-ghost" onClick={() => void signOut()}>
            Sign out
          </button>
        </div>
      ) : googleClientId ? (
        <GoogleSignIn clientId={googleClientId} onCredential={onCredential} />
      ) : null}
    </header>
  )
}
