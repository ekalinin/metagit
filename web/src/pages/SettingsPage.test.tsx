import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { SSHKey, Token } from '../api'
import { json, mockFetch, ok, type Handler } from '../test-utils'
import { SettingsPage } from './SettingsPage'

function section(heading: string): HTMLElement {
  return screen.getByRole('heading', { name: heading }).closest('section')!
}

function setup(extra: Record<string, Handler> = {}) {
  return mockFetch({ 'GET /api/tokens': ok([]), 'GET /api/ssh-keys': ok([]), ...extra })
}

test('a created token value is shown once and the list shows only name and dates', async () => {
  const tokens: Token[] = []
  const value = 'mgt_0123456789abcdef'
  const fetch = setup({
    'GET /api/tokens': () => json(tokens),
    'POST /api/tokens': (call) => {
      const token = { id: 1, name: (call.body as { name: string }).name, created_at: '2026-10-15T09:00:00Z' }
      tokens.push(token)
      return json({ token, value }, 201)
    },
  })
  const user = userEvent.setup()
  render(<SettingsPage />)

  await screen.findByText('Токенов нет.')
  const s = section('Токены доступа для git')
  await user.type(within(s).getByLabelText('Имя токена'), 'laptop')
  await user.click(within(s).getByRole('button', { name: 'Создать токен' }))

  expect(await within(s).findByText(value)).toBeInTheDocument()
  expect(within(s).getByText(/больше не будет показано/)).toBeInTheDocument()
  expect(fetch.find('POST', '/api/tokens')[0].body).toEqual({ name: 'laptop' })

  const table = await within(s).findByRole('table', { name: 'Токены' })
  expect(within(table).getByText('laptop')).toBeInTheDocument()
  expect(within(table).getByText(new Date('2026-10-15T09:00:00Z').toLocaleString('ru-RU'))).toBeInTheDocument()
  expect(within(table).getByText('не использовался')).toBeInTheDocument()
  expect(table).not.toHaveTextContent(value)

  await user.click(within(s).getByRole('button', { name: 'Скрыть' }))
  expect(screen.queryByText(value)).not.toBeInTheDocument()
})

test('revoking a token sends DELETE', async () => {
  const tokens: Token[] = [{ id: 4, name: 'old', created_at: '2026-10-01T09:00:00Z', last_used_at: '2026-10-10T09:00:00Z' }]
  const fetch = setup({
    'GET /api/tokens': () => json(tokens),
    'DELETE /api/tokens/4': () => {
      tokens.pop()
      return json({ deleted: true })
    },
  })
  vi.spyOn(window, 'confirm').mockReturnValue(true)
  const user = userEvent.setup()
  render(<SettingsPage />)

  await user.click(await screen.findByRole('button', { name: 'Отозвать old' }))

  expect(await screen.findByText('Токенов нет.')).toBeInTheDocument()
  expect(fetch.find('DELETE', '/api/tokens/4')[0].headers['content-type']).toBe('application/json')
})

test('adds an SSH key', async () => {
  const keys: SSHKey[] = []
  const fetch = setup({
    'GET /api/ssh-keys': () => json(keys),
    'POST /api/ssh-keys': (call) => {
      const { name } = call.body as { name: string }
      const key = { id: 1, name, fingerprint: 'SHA256:abc', created_at: '2026-10-15T09:00:00Z' }
      keys.push(key)
      return json(key, 201)
    },
  })
  const user = userEvent.setup()
  render(<SettingsPage />)

  await screen.findByText('SSH-ключей нет.')
  const s = section('SSH-ключи')
  await user.type(within(s).getByLabelText('Имя (необязательно)'), 'laptop')
  await user.type(within(s).getByLabelText('Публичный ключ'), 'ssh-ed25519 AAAAC3 me@host')
  await user.click(within(s).getByRole('button', { name: 'Добавить ключ' }))

  expect(await within(s).findByText('SHA256:abc')).toBeInTheDocument()
  expect(fetch.find('POST', '/api/ssh-keys')[0].body).toEqual({
    name: 'laptop',
    public_key: 'ssh-ed25519 AAAAC3 me@host',
  })
})

test('export sends the passphrase and downloads the file', async () => {
  const fetch = setup({
    'POST /api/export': () =>
      new Response(new Uint8Array([1, 2, 3]), {
        status: 200,
        headers: {
          'Content-Type': 'application/octet-stream',
          'Content-Disposition': 'attachment; filename="metagit-settings-20261015-090000.age"',
        },
      }),
  })
  const createObjectURL = vi.fn(() => 'blob:export')
  Object.defineProperty(URL, 'createObjectURL', { value: createObjectURL, configurable: true })
  Object.defineProperty(URL, 'revokeObjectURL', { value: vi.fn(), configurable: true })
  const downloads: string[] = []
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
    downloads.push(`${this.download} ${this.href}`)
  })
  const user = userEvent.setup()
  render(<SettingsPage />)

  const s = section('Экспорт настроек')
  expect(within(s).getByText(/учетные данные для хостингов/)).toBeInTheDocument()
  const button = within(s).getByRole('button', { name: 'Скачать файл экспорта' })
  await user.type(within(s).getByLabelText('Пароль для файла'), 'short')
  expect(button).toBeDisabled()
  await user.type(within(s).getByLabelText('Пароль для файла'), '-enough')
  expect(button).toBeEnabled()
  await user.click(button)

  await waitFor(() => expect(downloads).toEqual(['metagit-settings-20261015-090000.age blob:export']))
  expect(fetch.find('POST', '/api/export')[0].body).toEqual({ passphrase: 'short-enough' })
  expect(createObjectURL).toHaveBeenCalledTimes(1)
})

test('import sends the file as base64 and shows created projects', async () => {
  const fetch = setup({ 'POST /api/import': ok({ projects: ['alpha', 'beta'] }) })
  const user = userEvent.setup()
  render(<SettingsPage />)

  const s = section('Импорт настроек')
  const file = new File(['hello metagit'], 'backup.age', { type: 'application/octet-stream' })
  await user.upload(within(s).getByLabelText('Файл экспорта'), file)
  await user.type(within(s).getByLabelText('Пароль файла'), 'passphrase')
  await user.click(within(s).getByRole('button', { name: 'Импортировать' }))

  const status = await within(s).findByRole('status')
  expect(within(status).getByRole('link', { name: 'alpha' })).toHaveAttribute('href', '/projects/alpha')
  expect(within(status).getByRole('link', { name: 'beta' })).toHaveAttribute('href', '/projects/beta')
  expect(status).toHaveTextContent('Репозитории проектов пустые')
  expect(status).toHaveTextContent('ничего не меняет на хостингах, пока вы не отправите push')
  expect(fetch.find('POST', '/api/import')[0].body).toEqual({
    passphrase: 'passphrase',
    data: btoa('hello metagit'),
  })
})

test('import error shows the server message', async () => {
  setup({ 'POST /api/import': () => json({ error: 'неверный пароль или поврежденный файл' }, 400) })
  const user = userEvent.setup()
  render(<SettingsPage />)

  const s = section('Импорт настроек')
  await user.upload(within(s).getByLabelText('Файл экспорта'), new File(['x'], 'backup.age'))
  await user.type(within(s).getByLabelText('Пароль файла'), 'wrong')
  await user.click(within(s).getByRole('button', { name: 'Импортировать' }))

  expect(await within(s).findByRole('alert')).toHaveTextContent('неверный пароль или поврежденный файл')
  expect(within(s).queryByRole('status')).not.toBeInTheDocument()
})
