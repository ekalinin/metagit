import { useEffect, useState } from 'react'
import { api, errorText, onUnauthorized, type Session } from './api'
import { LoginPage } from './pages/LoginPage'
import { ProjectPage } from './pages/ProjectPage'
import { ProjectsPage } from './pages/ProjectsPage'
import { SettingsPage } from './pages/SettingsPage'
import { Link, loginPath, matchRoute, navigate, safeNext, useLocation, type Route } from './router'

export function App() {
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
    if (!sessionError) return <p className="loading">Загрузка...</p>
    return (
      <main className="page">
        <p className="message message-error" role="alert">
          Не удалось связаться с сервером: {sessionError}
        </p>
        <button
          type="button"
          onClick={() => {
            setSessionError('')
            setAttempt((n) => n + 1)
          }}
        >
          Повторить
        </button>
      </main>
    )
  }
  if (needLogin || leaveLogin) return <p className="loading">Загрузка...</p>

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
          <h1>Страница не найдена</h1>
          <p>
            <Link to="/">Перейти к списку проектов</Link>
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
            Проекты
          </Link>
          <Link to="/settings" current={route.page === 'settings'}>
            Настройки
          </Link>
        </nav>
        <div className="topbar-user">
          {ownerName && <span className="muted">{ownerName}</span>}
          <button type="button" className="secondary small" disabled={busy} onClick={() => void logout()}>
            Выйти
          </button>
        </div>
      </div>
      {error && (
        <p className="message message-error topbar-error" role="alert">
          Не удалось выйти: {error}
        </p>
      )}
    </header>
  )
}
