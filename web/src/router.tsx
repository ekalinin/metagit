import { useSyncExternalStore, type MouseEvent, type ReactNode } from 'react'

// A tiny router on top of the History API.

const NAVIGATE_EVENT = 'metagit:navigate'

export function navigate(to: string, options: { replace?: boolean } = {}): void {
  if (options.replace) {
    window.history.replaceState(null, '', to)
  } else {
    window.history.pushState(null, '', to)
  }
  window.dispatchEvent(new Event(NAVIGATE_EVENT))
}

function subscribe(onChange: () => void): () => void {
  window.addEventListener('popstate', onChange)
  window.addEventListener(NAVIGATE_EVENT, onChange)
  return () => {
    window.removeEventListener('popstate', onChange)
    window.removeEventListener(NAVIGATE_EVENT, onChange)
  }
}

function currentPath(): string {
  return window.location.pathname + window.location.search
}

export interface Location {
  pathname: string
  search: string
}

export function useLocation(): Location {
  const path = useSyncExternalStore(subscribe, currentPath)
  const i = path.indexOf('?')
  return i < 0 ? { pathname: path, search: '' } : { pathname: path.slice(0, i), search: path.slice(i) }
}

export type Route =
  | { page: 'login' }
  | { page: 'projects' }
  | { page: 'project'; name: string }
  | { page: 'settings' }
  | { page: 'not-found' }

export function matchRoute(pathname: string): Route {
  if (pathname === '/login') return { page: 'login' }
  if (pathname === '/') return { page: 'projects' }
  if (pathname === '/settings') return { page: 'settings' }
  const m = /^\/projects\/([^/]+)\/?$/.exec(pathname)
  if (m) {
    try {
      return { page: 'project', name: decodeURIComponent(m[1]) }
    } catch {
      // Malformed percent-encoding.
    }
  }
  return { page: 'not-found' }
}

export function projectPath(name: string): string {
  return '/projects/' + encodeURIComponent(name)
}

// loginPath returns the login page address that remembers the wanted page.
export function loginPath(wanted: string): string {
  if (wanted === '/' || wanted === '' || wanted.startsWith('/login')) return '/login'
  return '/login?next=' + encodeURIComponent(wanted)
}

// safeNext returns the page to open after login. Only local paths are allowed.
export function safeNext(search: string): string {
  const next = new URLSearchParams(search).get('next')
  if (!next || !next.startsWith('/') || next.startsWith('//') || next.startsWith('/\\') || next.startsWith('/login')) {
    return '/'
  }
  return next
}

interface LinkProps {
  to: string
  children: ReactNode
  className?: string
  current?: boolean
}

export function Link({ to, children, className, current }: LinkProps) {
  function onClick(e: MouseEvent<HTMLAnchorElement>) {
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return
    e.preventDefault()
    navigate(to)
  }
  return (
    <a href={to} className={className} aria-current={current ? 'page' : undefined} onClick={onClick}>
      {children}
    </a>
  )
}
