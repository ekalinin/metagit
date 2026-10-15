// Helpers for tests: a fetch mock with routes like "GET /api/session".

export interface Call {
  method: string
  url: string
  headers: Record<string, string>
  body: unknown
}

export type Handler = (call: Call) => Response | Promise<Response>

export function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

export function ok(body: unknown, status = 200): Handler {
  return () => json(body, status)
}

export function fail(status: number, error: string): Handler {
  return () => json({ error }, status)
}

export function mockFetch(routes: Record<string, Handler>) {
  const calls: Call[] = []
  const fn = vi.fn(async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
    const method = (init?.method ?? 'GET').toUpperCase()
    const headers: Record<string, string> = {}
    new Headers(init?.headers).forEach((value, key) => {
      headers[key] = value
    })
    const body: unknown = typeof init?.body === 'string' ? JSON.parse(init.body) : undefined
    const call: Call = { method, url, headers, body }
    calls.push(call)
    const handler = routes[`${method} ${url}`]
    if (!handler) return json({ error: `no mock for ${method} ${url}` }, 500)
    return handler(call)
  })
  vi.stubGlobal('fetch', fn)
  return {
    calls,
    find: (method: string, url: string) => calls.filter((c) => c.method === method && c.url === url),
  }
}

export function goTo(path: string): void {
  window.history.replaceState(null, '', path)
}
