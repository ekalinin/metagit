import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { App } from './App'
import { LOCALE_KEY } from './locale'
import { fail, goTo, mockFetch, ok } from './test-utils'
import { THEME_KEY } from './theme'

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

describe('theme switch', () => {
  afterEach(() => {
    window.localStorage.clear()
    delete document.documentElement.dataset.theme
  })

  test('a chosen mode is applied and saved, system removes it', async () => {
    goTo('/')
    mockFetch({ 'GET /api/session': loggedIn, 'GET /api/projects': ok([]) })
    const user = userEvent.setup()
    render(<App />)

    const group = await screen.findByRole('group', { name: 'Тема' })
    expect(within(group).getByRole('radio', { name: 'Системная' })).toBeChecked()

    await user.click(within(group).getByRole('radio', { name: 'Темная' }))
    expect(document.documentElement.dataset.theme).toBe('dark')
    expect(window.localStorage.getItem(THEME_KEY)).toBe('dark')

    await user.click(within(group).getByRole('radio', { name: 'Системная' }))
    expect(document.documentElement).not.toHaveAttribute('data-theme')
    expect(window.localStorage.getItem(THEME_KEY)).toBeNull()
  })

  test('the saved mode is checked', async () => {
    window.localStorage.setItem(THEME_KEY, 'light')
    goTo('/')
    mockFetch({ 'GET /api/session': loggedIn, 'GET /api/projects': ok([]) })
    render(<App />)

    expect(await screen.findByRole('radio', { name: 'Светлая' })).toBeChecked()
  })

  test('the login page keeps the chosen mode after logout', async () => {
    goTo('/')
    mockFetch({
      'GET /api/session': loggedIn,
      'GET /api/projects': ok([]),
      'POST /api/logout': ok({ authenticated: false }),
    })
    const user = userEvent.setup()
    render(<App />)

    await user.click(await screen.findByRole('radio', { name: 'Темная' }))
    await user.click(screen.getByRole('button', { name: 'Выйти' }))

    expect(await screen.findByRole('button', { name: 'Войти' })).toBeInTheDocument()
    expect(screen.queryByRole('group', { name: 'Тема' })).not.toBeInTheDocument()
    expect(document.documentElement.dataset.theme).toBe('dark')
  })
})

describe('interface language', () => {
  const settingsRoutes = {
    'GET /api/session': loggedIn,
    'GET /api/tokens': ok([{ id: 1, name: 'laptop', created_at: '2026-10-15T09:00:00Z' }]),
    'GET /api/ssh-keys': ok([]),
  }

  test('a Russian browser gets the Russian interface and lang', async () => {
    goTo('/login')
    mockFetch({ 'GET /api/session': loggedOut })
    render(<App />)

    expect(await screen.findByRole('button', { name: 'Войти' })).toBeInTheDocument()
    expect(document.documentElement).toHaveAttribute('lang', 'ru')
  })

  test('an unsupported browser language gets English', async () => {
    vi.spyOn(window.navigator, 'languages', 'get').mockReturnValue(['de'])
    goTo('/login')
    mockFetch({ 'GET /api/session': loggedOut })
    render(<App />)

    expect(await screen.findByRole('button', { name: 'Log in' })).toBeInTheDocument()
    expect(screen.getByLabelText('Password')).toBeInTheDocument()
    expect(document.documentElement).toHaveAttribute('lang', 'en')
  })

  test('a saved choice wins over a Russian browser', async () => {
    window.localStorage.setItem(LOCALE_KEY, 'en')
    goTo('/')
    mockFetch({ 'GET /api/session': loggedIn, 'GET /api/projects': ok([]) })
    render(<App />)

    expect(await screen.findByRole('heading', { name: 'Projects' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Log out' })).toBeInTheDocument()
  })

  test('choosing English in settings applies at once and is saved, system removes it', async () => {
    goTo('/settings')
    mockFetch(settingsRoutes)
    const user = userEvent.setup()
    render(<App />)

    const created = new Date('2026-10-15T09:00:00Z')
    const group = await screen.findByRole('group', { name: 'Язык' })
    expect(within(group).getByRole('radio', { name: 'Системный' })).toBeChecked()
    expect(await screen.findByText(created.toLocaleString('ru-RU'))).toBeInTheDocument()

    await user.click(within(group).getByRole('radio', { name: 'English' }))
    expect(screen.getByRole('heading', { level: 1, name: 'Settings' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Projects' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Log out' })).toBeInTheDocument()
    expect(screen.getByText(created.toLocaleString('en-US'))).toBeInTheDocument()
    expect(document.documentElement).toHaveAttribute('lang', 'en')
    expect(window.localStorage.getItem(LOCALE_KEY)).toBe('en')

    await user.click(within(screen.getByRole('group', { name: 'Language' })).getByRole('radio', { name: 'System' }))
    expect(screen.getByRole('heading', { level: 1, name: 'Настройки' })).toBeInTheDocument()
    expect(document.documentElement).toHaveAttribute('lang', 'ru')
    expect(window.localStorage.getItem(LOCALE_KEY)).toBeNull()
  })

  test('the login page keeps the chosen language after logout', async () => {
    goTo('/settings')
    mockFetch({ ...settingsRoutes, 'POST /api/logout': ok({ authenticated: false }) })
    const user = userEvent.setup()
    render(<App />)

    await user.click(within(await screen.findByRole('group', { name: 'Язык' })).getByRole('radio', { name: 'English' }))
    await user.click(screen.getByRole('button', { name: 'Log out' }))

    expect(await screen.findByRole('button', { name: 'Log in' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Log in' })).toBeInTheDocument()
  })

  test('a server error text is shown as is', async () => {
    goTo('/login')
    mockFetch({
      'GET /api/session': loggedOut,
      'POST /api/login': fail(401, 'invalid name or password'),
    })
    const user = userEvent.setup()
    render(<App />)

    await user.type(await screen.findByLabelText('Имя'), 'eugene')
    await user.type(screen.getByLabelText('Пароль'), 'wrong')
    await user.click(screen.getByRole('button', { name: 'Войти' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(/^invalid name or password$/)
  })
})
