import { messages } from './locale'
import { loginPath, navigate } from './router'

export type AuthKind = 'none' | 'https' | 'ssh'
export type TargetState = 'synced' | 'pending' | 'error'
export type EventKind = 'delivery_error' | 'diverged' | 'archive_push'

export interface Session {
  owner_exists: boolean
  authenticated: boolean
  owner_name?: string
}

export interface LoginResult {
  authenticated: boolean
  owner_name: string
}

export interface Target {
  id: number
  name: string
  url: string
  auth_kind: AuthKind
  username: string
  has_secret: boolean
  enabled: boolean
  state: TargetState
  last_error: string
  last_success_at?: string
  next_attempt_at?: string
}

// TargetInput is sent on create and update. An omitted or empty secret keeps
// the stored one.
export interface TargetInput {
  name: string
  url: string
  auth_kind: AuthKind
  username: string
  secret?: string
  enabled: boolean
}

export interface Project {
  name: string
  created_at: string
  http_url: string
  ssh_url: string
}

export interface ProjectSummary extends Project {
  targets: Target[]
}

export interface ProtectedRef {
  id: number
  pattern: string
}

export interface ProjectDetails {
  project: Project
  targets: Target[]
  protected: ProtectedRef[]
}

export interface ProjectEvent {
  id: number
  kind: EventKind
  target_name: string
  ref: string
  archive_ref: string
  message: string
  created_at: string
}

export interface ArchiveEntry {
  id: number
  source: 'push' | 'target'
  target_name: string
  orig_ref: string
  archive_ref: string
  object: string
  created_at: string
  fetch_command: string
}

export interface Token {
  id: number
  name: string
  created_at: string
  last_used_at?: string
}

export interface CreatedToken {
  token: Token
  value: string
}

export interface SSHKey {
  id: number
  name: string
  fingerprint: string
  created_at: string
}

export interface ImportResult {
  projects: string[]
}

export interface DownloadedFile {
  blob: Blob
  filename: string
}

export class ApiError extends Error {
  readonly status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

export function errorText(err: unknown): string {
  if (err instanceof Error && err.message) return err.message
  return messages().api.unknownError
}

type Method = 'GET' | 'POST' | 'PUT' | 'DELETE'

interface RequestOptions {
  body?: unknown
  // Public endpoints answer 401 for wrong credentials, not for a missing session.
  public?: boolean
}

const unauthorizedListeners = new Set<() => void>()

// onUnauthorized subscribes to "the session is missing or expired".
export function onUnauthorized(listener: () => void): () => void {
  unauthorizedListeners.add(listener)
  return () => {
    unauthorizedListeners.delete(listener)
  }
}

function redirectToLogin(): void {
  const { pathname, search } = window.location
  if (pathname !== '/login') {
    navigate(loginPath(pathname + search), { replace: true })
  }
  unauthorizedListeners.forEach((listener) => listener())
}

async function errorMessage(resp: Response): Promise<string> {
  try {
    const data: unknown = await resp.json()
    if (data !== null && typeof data === 'object' && 'error' in data && typeof data.error === 'string' && data.error) {
      return data.error
    }
  } catch {
    // Not a JSON body.
  }
  return messages().api.serverError(resp.status)
}

async function send(method: Method, path: string, options: RequestOptions = {}): Promise<Response> {
  const init: RequestInit = { method, credentials: 'same-origin' }
  if (method !== 'GET') {
    // The server rejects changing requests without this header.
    init.headers = { 'Content-Type': 'application/json' }
    if (options.body !== undefined) init.body = JSON.stringify(options.body)
  }
  let resp: Response
  try {
    resp = await fetch(path, init)
  } catch {
    throw new ApiError(0, messages().api.unreachable)
  }
  if (resp.ok) return resp
  const message = await errorMessage(resp)
  if (resp.status === 401 && !options.public) redirectToLogin()
  throw new ApiError(resp.status, message)
}

async function request<T>(method: Method, path: string, options: RequestOptions = {}): Promise<T> {
  const resp = await send(method, path, options)
  const text = await resp.text()
  return (text ? JSON.parse(text) : null) as T
}

function filenameFrom(disposition: string | null): string {
  const m = disposition ? /filename\*?=(?:UTF-8'')?"?([^";]+)"?/i.exec(disposition) : null
  if (!m) return 'metagit-settings.age'
  try {
    return decodeURIComponent(m[1])
  } catch {
    return m[1]
  }
}

function projectURL(name: string): string {
  return '/api/projects/' + encodeURIComponent(name)
}

export const api = {
  session: () => request<Session>('GET', '/api/session', { public: true }),
  login: (name: string, password: string) =>
    request<LoginResult>('POST', '/api/login', { body: { name, password }, public: true }),
  logout: () => request<unknown>('POST', '/api/logout', { public: true }),

  projects: () => request<ProjectSummary[]>('GET', '/api/projects'),
  createProject: (name: string) => request<Project>('POST', '/api/projects', { body: { name } }),
  project: (name: string) => request<ProjectDetails>('GET', projectURL(name)),
  deleteProject: (name: string, confirm: string) =>
    request<unknown>('POST', projectURL(name) + '/delete', { body: { confirm } }),

  createTarget: (project: string, input: TargetInput) =>
    request<Target>('POST', projectURL(project) + '/targets', { body: input }),
  updateTarget: (project: string, id: number, input: TargetInput) =>
    request<Target>('PUT', `${projectURL(project)}/targets/${id}`, { body: input }),
  deleteTarget: (project: string, id: number) => request<unknown>('DELETE', `${projectURL(project)}/targets/${id}`),
  syncTarget: (project: string, id: number) => request<unknown>('POST', `${projectURL(project)}/targets/${id}/sync`),

  protectedRefs: (project: string) => request<ProtectedRef[]>('GET', projectURL(project) + '/protected'),
  addProtected: (project: string, pattern: string) =>
    request<ProtectedRef>('POST', projectURL(project) + '/protected', { body: { pattern } }),
  deleteProtected: (project: string, id: number) =>
    request<unknown>('DELETE', `${projectURL(project)}/protected/${id}`),

  events: (project: string, limit = 100) =>
    request<ProjectEvent[]>('GET', `${projectURL(project)}/events?limit=${limit}`),
  archive: (project: string) => request<ArchiveEntry[]>('GET', projectURL(project) + '/archive'),

  tokens: () => request<Token[]>('GET', '/api/tokens'),
  createToken: (name: string) => request<CreatedToken>('POST', '/api/tokens', { body: { name } }),
  deleteToken: (id: number) => request<unknown>('DELETE', `/api/tokens/${id}`),

  sshKeys: () => request<SSHKey[]>('GET', '/api/ssh-keys'),
  addSSHKey: (name: string, publicKey: string) =>
    request<SSHKey>('POST', '/api/ssh-keys', { body: { name, public_key: publicKey } }),
  deleteSSHKey: (id: number) => request<unknown>('DELETE', `/api/ssh-keys/${id}`),

  exportSettings: async (passphrase: string): Promise<DownloadedFile> => {
    const resp = await send('POST', '/api/export', { body: { passphrase } })
    return { blob: await resp.blob(), filename: filenameFrom(resp.headers.get('Content-Disposition')) }
  },
  importSettings: (passphrase: string, data: string) =>
    request<ImportResult>('POST', '/api/import', { body: { passphrase, data } }),
}
