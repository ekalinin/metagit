import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { App } from './App'
import { fail, goTo, mockFetch, ok } from './test-utils'

const loggedOut = ok({ owner_exists: true, authenticated: false })
const loggedIn = ok({ owner_exists: true, authenticated: true, owner_name: 'eugene' })

test('owner_exists false shows the owner create instruction and no form', async () => {
  goTo('/')
  mockFetch({ 'GET /api/session': ok({ owner_exists: false, authenticated: false }) })
  render(<App />)

  expect(await screen.findByText('metagit owner create --name <имя>')).toBeInTheDocument()
  expect(window.location.pathname).toBe('/login')
  expect(screen.queryByRole('textbox')).not.toBeInTheDocument()
  expect(screen.queryByLabelText('Пароль')).not.toBeInTheDocument()
  expect(screen.queryByRole('button')).not.toBeInTheDocument()
})

test('successful login navigates to the projects list', async () => {
  goTo('/login')
  const fetch = mockFetch({
    'GET /api/session': loggedOut,
    'POST /api/login': ok({ authenticated: true, owner_name: 'eugene' }),
    'GET /api/projects': ok([]),
  })
  const user = userEvent.setup()
  render(<App />)

  await user.type(await screen.findByLabelText('Имя'), 'eugene')
  await user.type(screen.getByLabelText('Пароль'), 'secret-password')
  await user.click(screen.getByRole('button', { name: 'Войти' }))

  expect(await screen.findByRole('heading', { name: 'Проекты' })).toBeInTheDocument()
  expect(window.location.pathname).toBe('/')
  expect(screen.getByRole('button', { name: 'Выйти' })).toBeInTheDocument()
  const [login] = fetch.find('POST', '/api/login')
  expect(login.body).toEqual({ name: 'eugene', password: 'secret-password' })
  expect(login.headers['content-type']).toBe('application/json')
})

test('wrong password shows the server error', async () => {
  goTo('/login')
  mockFetch({
    'GET /api/session': loggedOut,
    'POST /api/login': fail(401, 'неверное имя или пароль'),
  })
  const user = userEvent.setup()
  render(<App />)

  await user.type(await screen.findByLabelText('Имя'), 'eugene')
  await user.type(screen.getByLabelText('Пароль'), 'wrong')
  await user.click(screen.getByRole('button', { name: 'Войти' }))

  expect(await screen.findByRole('alert')).toHaveTextContent('неверное имя или пароль')
  expect(window.location.pathname + window.location.search).toBe('/login')
})

test('too many attempts shows the server error', async () => {
  goTo('/login')
  mockFetch({
    'GET /api/session': loggedOut,
    'POST /api/login': fail(429, 'слишком много неудачных попыток входа, попробуйте позже'),
  })
  const user = userEvent.setup()
  render(<App />)

  await user.type(await screen.findByLabelText('Имя'), 'eugene')
  await user.type(screen.getByLabelText('Пароль'), 'wrong')
  await user.click(screen.getByRole('button', { name: 'Войти' }))

  expect(await screen.findByRole('alert')).toHaveTextContent('слишком много неудачных попыток')
})

test('a private page without a session redirects to login and returns after login', async () => {
  goTo('/settings')
  mockFetch({
    'GET /api/session': loggedOut,
    'POST /api/login': ok({ authenticated: true, owner_name: 'eugene' }),
    'GET /api/tokens': ok([]),
    'GET /api/ssh-keys': ok([]),
  })
  const user = userEvent.setup()
  render(<App />)

  await user.type(await screen.findByLabelText('Имя'), 'eugene')
  expect(window.location.pathname).toBe('/login')
  expect(new URLSearchParams(window.location.search).get('next')).toBe('/settings')
  await user.type(screen.getByLabelText('Пароль'), 'secret-password')
  await user.click(screen.getByRole('button', { name: 'Войти' }))

  expect(await screen.findByRole('heading', { name: 'Настройки' })).toBeInTheDocument()
  expect(window.location.pathname).toBe('/settings')
})

test('logout ends the session and opens the login page', async () => {
  goTo('/')
  const fetch = mockFetch({
    'GET /api/session': loggedIn,
    'GET /api/projects': ok([]),
    'POST /api/logout': ok({ authenticated: false }),
  })
  const user = userEvent.setup()
  render(<App />)

  await user.click(await screen.findByRole('button', { name: 'Выйти' }))

  expect(await screen.findByRole('button', { name: 'Войти' })).toBeInTheDocument()
  expect(window.location.pathname).toBe('/login')
  expect(fetch.find('POST', '/api/logout')[0].headers['content-type']).toBe('application/json')
})

test('a 401 on a private page sends the user to login', async () => {
  goTo('/projects/demo')
  mockFetch({
    'GET /api/session': loggedIn,
    'GET /api/projects/demo': fail(401, 'нужно войти'),
    'GET /api/projects/demo/events?limit=100': fail(401, 'нужно войти'),
    'GET /api/projects/demo/archive': fail(401, 'нужно войти'),
  })
  render(<App />)

  expect(await screen.findByRole('button', { name: 'Войти' })).toBeInTheDocument()
  await waitFor(() => expect(window.location.pathname).toBe('/login'))
  expect(new URLSearchParams(window.location.search).get('next')).toBe('/projects/demo')
  expect(screen.queryByRole('button', { name: 'Выйти' })).not.toBeInTheDocument()
})
