import { useCallback, useEffect, useRef, useState } from 'react'
import {
  api,
  ApiError,
  errorText,
  type ArchiveEntry,
  type ProjectDetails,
  type ProjectEvent,
  type Target,
  type TargetInput,
} from '../api'
import { Link } from '../router'
import { authKindLabel, ErrorMessage, formatTime, Notice, StateBadge } from '../ui'
import { ArchiveList, EventLog } from './History'
import { ProtectedRefs } from './ProtectedRefs'
import { TargetForm } from './TargetForm'

interface Props {
  name: string
  // How often the page reloads project data, in milliseconds.
  refreshMs?: number
}

export function ProjectPage({ name, refreshMs = 5000 }: Props) {
  const [details, setDetails] = useState<ProjectDetails | null>(null)
  const [events, setEvents] = useState<ProjectEvent[]>([])
  const [archive, setArchive] = useState<ArchiveEntry[]>([])
  const [error, setError] = useState('')
  const [notFound, setNotFound] = useState(false)
  const seq = useRef(0)

  const load = useCallback(async () => {
    // Only the latest request may update the page, so a slow background
    // refresh cannot bring back data older than the last change.
    const current = ++seq.current
    try {
      const [d, ev, ar] = await Promise.all([api.project(name), api.events(name), api.archive(name)])
      if (current !== seq.current) return
      setDetails(d)
      setEvents(ev)
      setArchive(ar)
      setError('')
    } catch (err) {
      if (current !== seq.current) return
      if (err instanceof ApiError && err.status === 404) setNotFound(true)
      else setError(errorText(err))
    }
  }, [name])

  useEffect(() => {
    if (notFound) return
    void load()
    const timer = window.setInterval(() => void load(), refreshMs)
    return () => window.clearInterval(timer)
  }, [load, refreshMs, notFound])

  if (notFound) {
    return (
      <section>
        <h1>Проект {name} не найден</h1>
        <p>
          <Link to="/">Перейти к списку проектов</Link>
        </p>
      </section>
    )
  }

  return (
    <>
      <p className="breadcrumbs">
        <Link to="/">Проекты</Link> / {name}
      </p>
      <h1>{name}</h1>
      <ErrorMessage text={error} />
      {details === null ? (
        !error && <p className="muted">Загрузка...</p>
      ) : (
        <>
          <section className="card">
            <h2>Адреса для клонирования</h2>
            <dl className="clone">
              <dt>HTTP(S)</dt>
              <dd>
                <code>{details.project.http_url}</code>
              </dd>
              <dt>SSH</dt>
              <dd>
                <code>{details.project.ssh_url}</code>
              </dd>
            </dl>
          </section>
          <Targets project={name} targets={details.targets} reload={load} />
          <ProtectedRefs project={name} refs={details.protected} reload={load} />
          <EventLog events={events} />
          <ArchiveList entries={archive} />
        </>
      )}
    </>
  )
}

type FormState = { mode: 'add' } | { mode: 'edit'; target: Target } | null

interface TargetsProps {
  project: string
  targets: Target[]
  reload: () => Promise<void>
}

function Targets({ project, targets, reload }: TargetsProps) {
  const [form, setForm] = useState<FormState>(null)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [busyId, setBusyId] = useState<number | null>(null)

  async function act(target: Target, action: () => Promise<unknown>, done: string) {
    setBusyId(target.id)
    setError('')
    setNotice('')
    try {
      await action()
      setNotice(done)
      await reload()
    } catch (err) {
      setError(errorText(err))
    } finally {
      setBusyId(null)
    }
  }

  function toggle(target: Target) {
    // No secret in the input: the stored one is kept.
    const input: TargetInput = {
      name: target.name,
      url: target.url,
      auth_kind: target.auth_kind,
      username: target.username,
      enabled: !target.enabled,
    }
    void act(
      target,
      () => api.updateTarget(project, target.id, input),
      target.enabled ? `Target ${target.name} выключен.` : `Target ${target.name} включен.`,
    )
  }

  function remove(target: Target) {
    if (!window.confirm(`Удалить target ${target.name}? Репозиторий на хостинге не изменится.`)) return
    if (form?.mode === 'edit' && form.target.id === target.id) setForm(null)
    void act(target, () => api.deleteTarget(project, target.id), `Target ${target.name} удален.`)
  }

  return (
    <section className="card">
      <div className="section-head">
        <h2>Target'ы</h2>
        {form?.mode !== 'add' && (
          <button type="button" className="small" onClick={() => setForm({ mode: 'add' })}>
            Добавить target
          </button>
        )}
      </div>
      <p className="muted">
        Хостинги, на которые metagit доставляет ветки и теги проекта. Удаление target'а не трогает репозиторий на
        хостинге.
      </p>
      <ErrorMessage text={error} />
      <Notice text={notice} />
      {targets.length === 0 ? (
        <p className="muted">Target'ов пока нет.</p>
      ) : (
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Имя</th>
                <th>URL</th>
                <th>Учетные данные</th>
                <th>Включен</th>
                <th>Состояние</th>
                <th>Последняя успешная доставка</th>
                <th aria-label="Действия" />
              </tr>
            </thead>
            <tbody>
              {targets.map((t) => (
                <tr key={t.id} className={t.enabled ? undefined : 'disabled-row'}>
                  <td className="strong">{t.name}</td>
                  <td>
                    <code className="target-url">{t.url}</code>
                  </td>
                  <td>
                    {authKindLabel(t.auth_kind)}
                    {t.auth_kind === 'https' && t.username && <div className="muted small">логин {t.username}</div>}
                  </td>
                  <td>{t.enabled ? 'да' : 'нет'}</td>
                  <td>
                    <StateBadge state={t.state} />
                    {t.last_error && <div className="target-error">{t.last_error}</div>}
                    {t.state === 'error' && t.next_attempt_at && (
                      <div className="muted small">следующая попытка: {formatTime(t.next_attempt_at)}</div>
                    )}
                  </td>
                  <td>{t.last_success_at ? formatTime(t.last_success_at) : <span className="muted">не было</span>}</td>
                  <td className="actions">
                    <button
                      type="button"
                      className="small"
                      disabled={busyId === t.id || !t.enabled}
                      title={t.enabled ? undefined : 'Target выключен'}
                      onClick={() =>
                        void act(t, () => api.syncTarget(project, t.id), `Доставка на ${t.name} запущена.`)
                      }
                    >
                      Доставить сейчас
                    </button>
                    <button
                      type="button"
                      className="secondary small"
                      aria-label={`Изменить ${t.name}`}
                      onClick={() => setForm({ mode: 'edit', target: t })}
                    >
                      Изменить
                    </button>
                    <button
                      type="button"
                      className="secondary small"
                      disabled={busyId === t.id}
                      aria-label={`${t.enabled ? 'Выключить' : 'Включить'} ${t.name}`}
                      onClick={() => toggle(t)}
                    >
                      {t.enabled ? 'Выключить' : 'Включить'}
                    </button>
                    <button
                      type="button"
                      className="danger small"
                      disabled={busyId === t.id}
                      aria-label={`Удалить ${t.name}`}
                      onClick={() => remove(t)}
                    >
                      Удалить
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {form && (
        <TargetForm
          key={form.mode === 'edit' ? `edit-${form.target.id}` : 'add'}
          project={project}
          target={form.mode === 'edit' ? form.target : undefined}
          onCancel={() => setForm(null)}
          onSaved={(saved) => {
            setForm(null)
            setError('')
            setNotice(form.mode === 'edit' ? `Target ${saved.name} сохранен.` : `Target ${saved.name} добавлен.`)
            void reload()
          }}
        />
      )}
    </section>
  )
}
