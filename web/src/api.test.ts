import { api, ApiError } from './api'
import { fail, goTo, mockFetch, ok } from './test-utils'

test('a 401 response redirects to /login and remembers the wanted page', async () => {
  goTo('/projects/demo')
  mockFetch({ 'GET /api/projects': fail(401, 'нужно войти') })

  const err: unknown = await api.projects().catch((e: unknown) => e)

  expect(err).toBeInstanceOf(ApiError)
  expect((err as ApiError).status).toBe(401)
  expect(window.location.pathname).toBe('/login')
  expect(new URLSearchParams(window.location.search).get('next')).toBe('/projects/demo')
})

test('a 401 from login does not redirect', async () => {
  goTo('/login')
  mockFetch({ 'POST /api/login': fail(401, 'неверное имя или пароль') })

  await expect(api.login('eugene', 'wrong')).rejects.toThrow('неверное имя или пароль')
  expect(window.location.pathname + window.location.search).toBe('/login')
})

test('changing requests send Content-Type application/json, including DELETE', async () => {
  const fetch = mockFetch({
    'DELETE /api/tokens/7': ok({ deleted: true }),
    'POST /api/projects/demo/targets/3/sync': ok({ started: true }, 202),
    'GET /api/tokens': ok([]),
  })

  await api.deleteToken(7)
  await api.syncTarget('demo', 3)
  await api.tokens()

  expect(fetch.calls.map((c) => [c.method, c.headers['content-type']])).toEqual([
    ['DELETE', 'application/json'],
    ['POST', 'application/json'],
    ['GET', undefined],
  ])
})

test('the server error message is used for errors', async () => {
  mockFetch({ 'POST /api/projects': fail(409, 'проект "demo" уже существует') })

  await expect(api.createProject('demo')).rejects.toThrow('проект "demo" уже существует')
})
