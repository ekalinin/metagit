import { useEffect, useState } from 'react'
import { api, errorText, onUnauthorized, type Session } from './api'
import { useLocale, useT } from './locale'
import { LoginPage } from './pages/LoginPage'
import { ProjectPage } from './pages/ProjectPage'
import { ProjectsPage } from './pages/ProjectsPage'
import { SettingsPage } from './pages/SettingsPage'
import { Link, loginPath, matchRoute, navigate, safeNext, useLocation, type Route } from './router'
import { setThemeMode, storedThemeMode, type ThemeMode } from './theme'

export function App() {
  const text = useT()
  const locale = useLocale()
  const location = useLocation()
  const route = matchRoute(location.pathname)
  const [session, setSession] = useState<Session | null>(null)
  const [sessionError, setSessionError] = useState('')
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    let cancelled = false
    api.session().then(
      (s) => {
        if (!cancelled) setSession(s)
      },
      (err: unknown) => {
        if (!cancelled) setSessionError(errorText(err))
      },
    )
    return () => {
      cancelled = true
    }
  }, [attempt])

  useEffect(() => {
    document.documentElement.lang = locale
  }, [locale])

  useEffect(
    () => onUnauthorized(() => setSession((s) => s && { owner_exists: s.owner_exists, authenticated: false })),
    [],
  )

  const needLogin = session !== null && !session.authenticated && route.page !== 'login'
  const leaveLogin = session !== null && session.authenticated && route.page === 'login'
  const wanted = location.pathname + location.search

  useEffect(() => {
    if (needLogin) navigate(loginPath(wanted), { replace: true })
    else if (leaveLogin) navigate(safeNext(location.search), { replace: true })
  }, [needLogin, leaveLogin, wanted, location.search])

  if (session === null) {
    if (!sessionError) return <p className="loading">{text.common.loading}</p>
    return (
      <main className="page">
        <p className="message message-error" role="alert">
          {text.app.connectionFailed(sessionError)}
        </p>
        <button
          type="button"
          onClick={() => {
            setSessionError('')
            setAttempt((n) => n + 1)
          }}
        >
          {text.app.retry}
        </button>
      </main>
    )
  }
  if (needLogin || leaveLogin) return <p className="loading">{text.common.loading}</p>

  if (route.page === 'login') {
    return (
      <LoginPage
        ownerExists={session.owner_exists}
        onLoggedIn={(name) => setSession({ owner_exists: true, authenticated: true, owner_name: name })}
      />
    )
  }

  return (
    <>
      <Header
        route={route}
        ownerName={session.owner_name ?? ''}
        onLoggedOut={() => {
          setSession({ owner_exists: session.owner_exists, authenticated: false })
          navigate('/login')
        }}
      />
      <main className="page">
        <Page route={route} />
      </main>
    </>
  )
}

function Page({ route }: { route: Route }) {
  const text = useT()
  switch (route.page) {
    case 'projects':
      return <ProjectsPage />
    case 'project':
      return <ProjectPage key={route.name} name={route.name} />
    case 'settings':
      return <SettingsPage />
    default:
      return (
        <section>
          <h1>{text.app.notFound}</h1>
          <p>
            <Link to="/">{text.common.toProjects}</Link>
          </p>
        </section>
      )
  }
}

interface HeaderProps {
  route: Route
  ownerName: string
  onLoggedOut: () => void
}

function Header({ route, ownerName, onLoggedOut }: HeaderProps) {
  const text = useT()
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  async function logout() {
    setBusy(true)
    setError('')
    try {
      await api.logout()
      onLoggedOut()
    } catch (err) {
      setError(errorText(err))
      setBusy(false)
    }
  }

  return (
    <header className="topbar">
      <div className="topbar-inner">
        <Link to="/" className="brand">
          metagit
        </Link>
        <nav className="topnav">
          <Link to="/" current={route.page === 'projects' || route.page === 'project'}>
            {text.app.projects}
          </Link>
          <Link to="/settings" current={route.page === 'settings'}>
            {text.app.settings}
          </Link>
        </nav>
        <div className="topbar-user">
          <ThemeSwitch />
          {ownerName && <span className="muted">{ownerName}</span>}
          <button type="button" className="secondary small" disabled={busy} onClick={() => void logout()}>
            {text.app.logout}
          </button>
        </div>
      </div>
      {error && (
        <p className="message message-error topbar-error" role="alert">
          {text.app.logoutFailed(error)}
        </p>
      )}
    </header>
  )
}

const themeModes: ThemeMode[] = ['system', 'light', 'dark']

function ThemeSwitch() {
  const text = useT()
  const [mode, setMode] = useState(storedThemeMode)

  return (
    <fieldset className="theme-switch">
      <legend className="visually-hidden">{text.app.theme}</legend>
      {themeModes.map((m) => (
        <label key={m}>
          <input
            type="radio"
            name="theme"
            value={m}
            checked={mode === m}
            onChange={() => {
              setThemeMode(m)
              setMode(m)
            }}
          />
          <span>{text.app.themeModes[m]}</span>
        </label>
      ))}
    </fieldset>
  )
}
