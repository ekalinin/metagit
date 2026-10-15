import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ProjectSummary, Target } from '../api'
import { json, mockFetch, ok } from '../test-utils'
import { ProjectsPage } from './ProjectsPage'

function target(id: number, state: Target['state']): Target {
  return {
    id,
    name: `t${id}`,
    url: `https://example.com/t${id}.git`,
    auth_kind: 'none',
    username: '',
    has_secret: false,
    enabled: true,
    state,
    last_error: '',
  }
}

const demo: ProjectSummary = {
  name: 'demo',
  created_at: '2026-10-01T10:00:00Z',
  http_url: 'https://git.example.com/demo.git',
  ssh_url: 'ssh://git@git.example.com:2222/demo.git',
  targets: [target(1, 'synced'), target(2, 'synced'), target(3, 'error')],
}

test('lists projects with clone URLs and a summary of target states', async () => {
  mockFetch({ 'GET /api/projects': ok([demo]) })
  render(<ProjectsPage />)

  expect(await screen.findByRole('link', { name: 'demo' })).toHaveAttribute('href', '/projects/demo')
  expect(screen.getByText('https://git.example.com/demo.git')).toBeInTheDocument()
  expect(screen.getByText('ssh://git@git.example.com:2222/demo.git')).toBeInTheDocument()
  expect(screen.getByText('синхронизирован: 2')).toBeInTheDocument()
  expect(screen.getByText('ошибка: 1')).toBeInTheDocument()
})

test('every target has a lamp named after it, a disabled one is out', async () => {
  const off = { ...target(2, 'synced'), enabled: false }
  mockFetch({ 'GET /api/projects': ok([{ ...demo, targets: [target(1, 'synced'), off, target(3, 'error')] }]) })
  const { container } = render(<ProjectsPage />)

  await screen.findByRole('link', { name: 'demo' })
  const lamps = [...container.querySelectorAll('.lamp-strip .lamp')]
  expect(lamps.map((l) => l.getAttribute('title'))).toEqual([
    't1: синхронизирован',
    't2: синхронизирован, выключен',
    't3: ошибка',
  ])
  expect(lamps.map((l) => l.className)).toEqual(['lamp lamp-ok', 'lamp lamp-off', 'lamp lamp-err'])
  expect(container.querySelector('.lamp-strip')).toHaveAttribute('aria-hidden', 'true')
  expect(screen.getByText('выключено: 1')).toBeInTheDocument()
})

test('deletion is enabled only after the exact project name is typed', async () => {
  let projects = [demo]
  const fetch = mockFetch({
    'GET /api/projects': () => json(projects),
    'POST /api/projects/demo/delete': () => {
      projects = []
      return json({ deleted: true })
    },
  })
  const user = userEvent.setup()
  render(<ProjectsPage />)

  await user.click(await screen.findByRole('button', { name: 'Удалить demo' }))
  expect(screen.getByText(/Репозитории на хостингах \(target'ах\) не затрагиваются/)).toBeInTheDocument()
  const confirm = screen.getByLabelText('Введите имя проекта для подтверждения')
  const remove = screen.getByRole('button', { name: 'Удалить проект' })
  expect(remove).toBeDisabled()

  await user.type(confirm, 'dem')
  expect(remove).toBeDisabled()
  await user.type(confirm, 'oo')
  expect(remove).toBeDisabled()
  await user.type(confirm, '{Backspace}')
  expect(remove).toBeEnabled()

  await user.click(remove)

  expect(await screen.findByText(/Проектов пока нет/)).toBeInTheDocument()
  const calls = fetch.find('POST', '/api/projects/demo/delete')
  expect(calls).toHaveLength(1)
  expect(calls[0].body).toEqual({ confirm: 'demo' })
})

test('creates a project', async () => {
  const fetch = mockFetch({
    'GET /api/projects': ok([]),
    'POST /api/projects': ok({ ...demo, targets: undefined }, 201),
  })
  const user = userEvent.setup()
  render(<ProjectsPage />)

  const form = (await screen.findByRole('heading', { name: 'Новый проект' })).closest('section')!
  await user.type(within(form).getByLabelText('Имя проекта'), 'demo')
  await user.click(within(form).getByRole('button', { name: 'Создать проект' }))

  expect(fetch.find('POST', '/api/projects')[0].body).toEqual({ name: 'demo' })
  // The list is reloaded after creation.
  await waitFor(() => expect(fetch.find('GET', '/api/projects')).toHaveLength(2))
  expect(within(form).getByLabelText('Имя проекта')).toHaveValue('')
})
