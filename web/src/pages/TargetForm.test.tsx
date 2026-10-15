import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { Target } from '../api'
import { json, mockFetch } from '../test-utils'
import { TargetForm } from './TargetForm'

const github: Target = {
  id: 5,
  name: 'github',
  url: 'https://github.com/ekalinin/demo.git',
  auth_kind: 'https',
  username: 'ekalinin',
  has_secret: true,
  enabled: true,
  state: 'synced',
  last_error: '',
}

function setup() {
  const fetch = mockFetch({
    'PUT /api/projects/demo/targets/5': (call) => json({ ...github, ...(call.body as object) }),
    'POST /api/projects/demo/targets': (call) => json({ ...github, id: 9, ...(call.body as object) }, 201),
  })
  const onSaved = vi.fn()
  return { fetch, onSaved }
}

test('on edit the stored secret is never shown and an empty field keeps it', async () => {
  const { fetch, onSaved } = setup()
  const user = userEvent.setup()
  render(<TargetForm project="demo" target={github} onSaved={onSaved} onCancel={() => {}} />)

  const token = screen.getByLabelText('Токен')
  expect(token).toHaveValue('')
  expect(token).toHaveAttribute('placeholder', expect.stringContaining('задан'))
  expect(screen.getByLabelText('Логин')).toHaveValue('ekalinin')

  await user.click(screen.getByRole('button', { name: 'Сохранить' }))

  await waitFor(() => expect(onSaved).toHaveBeenCalled())
  const body = fetch.find('PUT', '/api/projects/demo/targets/5')[0].body as Record<string, unknown>
  expect(body.secret ?? '').toBe('')
  expect(body).toMatchObject({
    name: 'github',
    url: 'https://github.com/ekalinin/demo.git',
    auth_kind: 'https',
    username: 'ekalinin',
    enabled: true,
  })
})

test('on edit a typed token is sent', async () => {
  const { fetch, onSaved } = setup()
  const user = userEvent.setup()
  render(<TargetForm project="demo" target={github} onSaved={onSaved} onCancel={() => {}} />)

  await user.type(screen.getByLabelText('Токен'), 'ghp_new_token')
  await user.click(screen.getByLabelText('Включен'))
  await user.click(screen.getByRole('button', { name: 'Сохранить' }))

  await waitFor(() => expect(onSaved).toHaveBeenCalled())
  expect(fetch.find('PUT', '/api/projects/demo/targets/5')[0].body).toMatchObject({
    secret: 'ghp_new_token',
    enabled: false,
  })
})

test('changing the auth kind requires a new secret', async () => {
  setup()
  const user = userEvent.setup()
  render(<TargetForm project="demo" target={github} onSaved={vi.fn()} onCancel={() => {}} />)

  await user.selectOptions(screen.getByLabelText('Учетные данные'), 'ssh')

  const key = screen.getByLabelText('Приватный SSH-ключ')
  expect(key).toHaveValue('')
  expect(key).not.toHaveAttribute('placeholder', expect.stringContaining('задан'))
  expect(key).toBeRequired()
})

test('adds a target with an SSH key', async () => {
  const { fetch, onSaved } = setup()
  const user = userEvent.setup()
  render(<TargetForm project="demo" onSaved={onSaved} onCancel={() => {}} />)

  expect(screen.getByText(/Не указывайте логин, пароль или токен в URL/)).toBeInTheDocument()
  await user.type(screen.getByLabelText('Имя'), 'gitlab')
  await user.type(screen.getByLabelText('URL репозитория'), 'git@gitlab.com:ekalinin/demo.git')
  await user.selectOptions(screen.getByLabelText('Учетные данные'), 'ssh')
  await user.type(screen.getByLabelText('Приватный SSH-ключ'), 'PRIVATE KEY')
  await user.click(screen.getByRole('button', { name: 'Добавить target' }))

  await waitFor(() => expect(onSaved).toHaveBeenCalled())
  expect(fetch.find('POST', '/api/projects/demo/targets')[0].body).toEqual({
    name: 'gitlab',
    url: 'git@gitlab.com:ekalinin/demo.git',
    auth_kind: 'ssh',
    username: '',
    secret: 'PRIVATE KEY',
    enabled: true,
  })
})

test('shows the server error', async () => {
  mockFetch({
    'POST /api/projects/demo/targets': () => json({ error: 'URL не должен содержать учетные данные' }, 400),
  })
  const user = userEvent.setup()
  render(<TargetForm project="demo" onSaved={vi.fn()} onCancel={() => {}} />)

  await user.type(screen.getByLabelText('Имя'), 'x')
  await user.type(screen.getByLabelText('URL репозитория'), 'https://user:pass@example.com/x.git')
  await user.click(screen.getByRole('button', { name: 'Добавить target' }))

  expect(await screen.findByRole('alert')).toHaveTextContent('URL не должен содержать учетные данные')
})
