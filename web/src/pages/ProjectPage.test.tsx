import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ArchiveEntry, ProjectDetails, ProjectEvent, ProtectedRef, Target } from '../api'
import { json, mockFetch, ok, type Handler } from '../test-utils'
import { ProjectPage } from './ProjectPage'

const github: Target = {
  id: 1,
  name: 'github',
  url: 'https://github.com/ekalinin/demo.git',
  auth_kind: 'https',
  username: 'ekalinin',
  has_secret: true,
  enabled: true,
  state: 'synced',
  last_error: '',
  last_success_at: '2026-10-15T09:00:00Z',
}

const bitbucket: Target = {
  id: 2,
  name: 'bitbucket',
  url: 'git@bitbucket.org:ekalinin/demo.git',
  auth_kind: 'ssh',
  username: '',
  has_secret: true,
  enabled: true,
  state: 'error',
  last_error: 'authentication failed',
  next_attempt_at: '2026-10-15T10:00:00Z',
}

const events: ProjectEvent[] = [
  {
    id: 3,
    kind: 'diverged',
    target_name: 'github',
    ref: 'refs/heads/main',
    archive_ref: 'refs/metagit/archive/github/main/1',
    message: 'main на github разошелся',
    created_at: '2026-10-15T09:30:00Z',
  },
  {
    id: 2,
    kind: 'archive_push',
    target_name: '',
    ref: 'refs/heads/feature',
    archive_ref: 'refs/metagit/archive/push/feature/1',
    message: '',
    created_at: '2026-10-15T09:20:00Z',
  },
  {
    id: 1,
    kind: 'delivery_error',
    target_name: 'bitbucket',
    ref: '',
    archive_ref: '',
    message: 'authentication failed',
    created_at: '2026-10-15T09:10:00Z',
  },
]

const archive: ArchiveEntry[] = [
  {
    id: 1,
    source: 'push',
    target_name: '',
    orig_ref: 'refs/heads/feature',
    archive_ref: 'refs/metagit/archive/push/feature/1',
    object: '0123456789abcdef0123456789abcdef01234567',
    created_at: '2026-10-15T09:20:00Z',
    fetch_command: 'git fetch https://git.example.com/demo.git refs/metagit/archive/push/feature/1',
  },
  {
    id: 2,
    source: 'target',
    target_name: 'github',
    orig_ref: 'refs/heads/main',
    archive_ref: 'refs/metagit/archive/github/main/1',
    object: 'fedcba9876543210fedcba9876543210fedcba98',
    created_at: '2026-10-15T09:30:00Z',
    fetch_command: 'git fetch https://git.example.com/demo.git refs/metagit/archive/github/main/1',
  },
]

function routes(extra: Record<string, Handler> = {}, protectedRefs: ProtectedRef[] = []) {
  const details = (): ProjectDetails => ({
    project: {
      name: 'demo',
      created_at: '2026-10-01T10:00:00Z',
      http_url: 'https://git.example.com/demo.git',
      ssh_url: 'ssh://git@git.example.com:2222/demo.git',
    },
    targets: [github, bitbucket],
    protected: [...protectedRefs],
  })
  return mockFetch({
    'GET /api/projects/demo': () => json(details()),
    'GET /api/projects/demo/events?limit=100': ok(events),
    'GET /api/projects/demo/archive': ok(archive),
    ...extra,
  })
}

test('shows clone addresses and targets with state, last error and times', async () => {
  routes()
  render(<ProjectPage name="demo" />)

  expect(await screen.findByText('https://git.example.com/demo.git')).toBeInTheDocument()
  expect(screen.getByText('ssh://git@git.example.com:2222/demo.git')).toBeInTheDocument()

  const targets = screen.getByRole('heading', { name: "Target'ы" }).closest('section')!
  const rows = within(targets).getAllByRole('row')
  const githubRow = rows.find((r) => within(r).queryByText('github'))!
  expect(within(githubRow).getByText('синхронизирован')).toBeInTheDocument()
  expect(within(githubRow).getByText(new Date(github.last_success_at!).toLocaleString('ru-RU'))).toBeInTheDocument()
  const bitbucketRow = rows.find((r) => within(r).queryByText('bitbucket'))!
  expect(within(bitbucketRow).getByText('ошибка')).toBeInTheDocument()
  expect(within(bitbucketRow).getByText('authentication failed')).toBeInTheDocument()
  expect(
    within(bitbucketRow).getByText(/следующая попытка/),
  ).toHaveTextContent(new Date(bitbucket.next_attempt_at!).toLocaleString('ru-RU'))
})

test('"Доставить сейчас" starts a delivery', async () => {
  const fetch = routes({ 'POST /api/projects/demo/targets/1/sync': ok({ started: true }, 202) })
  const user = userEvent.setup()
  render(<ProjectPage name="demo" />)

  const row = (await screen.findAllByRole('row')).find((r) => within(r).queryByText('github'))!
  await user.click(within(row).getByRole('button', { name: 'Доставить сейчас' }))

  expect(await screen.findByText('Доставка на github запущена.')).toBeInTheDocument()
  const [call] = fetch.find('POST', '/api/projects/demo/targets/1/sync')
  expect(call.headers['content-type']).toBe('application/json')
})

test('disabling a target keeps its secret', async () => {
  const fetch = routes({ 'PUT /api/projects/demo/targets/1': ok({ ...github, enabled: false }) })
  const user = userEvent.setup()
  render(<ProjectPage name="demo" />)

  await user.click(await screen.findByRole('button', { name: 'Выключить github' }))

  await waitFor(() => expect(fetch.find('PUT', '/api/projects/demo/targets/1')).toHaveLength(1))
  const body = fetch.find('PUT', '/api/projects/demo/targets/1')[0].body
  expect(body).toEqual({
    name: 'github',
    url: 'https://github.com/ekalinin/demo.git',
    auth_kind: 'https',
    username: 'ekalinin',
    enabled: false,
  })
})

test('deleting a target asks for confirmation', async () => {
  const fetch = routes({ 'DELETE /api/projects/demo/targets/2': ok({ deleted: true }) })
  const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true)
  const user = userEvent.setup()
  render(<ProjectPage name="demo" />)

  await user.click(await screen.findByRole('button', { name: 'Удалить bitbucket' }))

  expect(confirm).toHaveBeenCalledWith(expect.stringContaining('bitbucket'))
  await waitFor(() => expect(fetch.find('DELETE', '/api/projects/demo/targets/2')).toHaveLength(1))
})

test('adding a protected ref pattern sends it and shows it in the list', async () => {
  const protectedRefs: ProtectedRef[] = []
  const fetch = routes(
    {
      'POST /api/projects/demo/protected': (call) => {
        const { pattern } = call.body as { pattern: string }
        protectedRefs.push({ id: 10, pattern })
        return json({ id: 10, pattern }, 201)
      },
    },
    protectedRefs,
  )
  const user = userEvent.setup()
  render(<ProjectPage name="demo" />)

  const section = (await screen.findByRole('heading', { name: 'Защищенные refs' })).closest('section')!
  expect(within(section).getByText(/отклоняет force-push, удаление и перенос тегов/)).toBeInTheDocument()
  expect(within(section).getByText('Защищенных refs нет.')).toBeInTheDocument()

  await user.type(within(section).getByLabelText('Шаблон'), 'refs/heads/main')
  await user.click(within(section).getByRole('button', { name: 'Добавить шаблон' }))

  const list = await within(section).findByRole('list', { name: 'Шаблоны защищенных refs' })
  expect(within(list).getByText('refs/heads/main')).toBeInTheDocument()
  expect(fetch.find('POST', '/api/projects/demo/protected')[0].body).toEqual({ pattern: 'refs/heads/main' })
  expect(within(section).getByLabelText('Шаблон')).toHaveValue('')
})

test('event log and archive show readable labels and the git fetch command', async () => {
  routes()
  render(<ProjectPage name="demo" />)

  const log = (await screen.findByRole('heading', { name: 'Журнал событий' })).closest('section')!
  const kinds = within(log)
    .getAllByRole('row')
    .slice(1)
    .map((r) => within(r).getAllByRole('cell')[1].textContent)
  expect(kinds).toEqual(['расхождение', 'архив при push', 'ошибка доставки'])
  expect(within(log).getByText('refs/metagit/archive/github/main/1')).toBeInTheDocument()

  const arch = screen.getByRole('heading', { name: 'Архив' }).closest('section')!
  expect(within(arch).getByText('0123456789ab')).toBeInTheDocument()
  expect(
    within(arch).getByText('git fetch https://git.example.com/demo.git refs/metagit/archive/push/feature/1'),
  ).toBeInTheDocument()
  const sources = within(arch)
    .getAllByRole('row')
    .slice(1)
    .map((r) => within(r).getAllByRole('cell')[1].textContent)
  expect(sources).toEqual(['push', 'github'])
})

test('background refresh does not lose form input', async () => {
  const fetch = routes()
  const user = userEvent.setup()
  render(<ProjectPage name="demo" refreshMs={50} />)

  await user.click(await screen.findByRole('button', { name: 'Изменить github' }))
  const token = screen.getByLabelText('Токен')
  await user.type(token, 'typed')
  const pattern = screen.getByLabelText('Шаблон')
  await user.type(pattern, 'refs/tags/*')

  const before = fetch.find('GET', '/api/projects/demo').length
  await waitFor(() => expect(fetch.find('GET', '/api/projects/demo').length).toBeGreaterThan(before + 1))
  expect(screen.getByLabelText('Токен')).toHaveValue('typed')
  expect(screen.getByLabelText('Шаблон')).toHaveValue('refs/tags/*')
})

test('an unknown project shows "not found"', async () => {
  mockFetch({
    'GET /api/projects/nope': () => json({ error: 'не найдено' }, 404),
    'GET /api/projects/nope/events?limit=100': () => json({ error: 'не найдено' }, 404),
    'GET /api/projects/nope/archive': () => json({ error: 'не найдено' }, 404),
  })
  render(<ProjectPage name="nope" />)

  expect(await screen.findByRole('heading', { name: 'Проект nope не найден' })).toBeInTheDocument()
})
