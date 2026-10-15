import { Fragment, useCallback, useEffect, useState } from 'react'
import { api, errorText, type ProjectSummary, type Target, type TargetState } from '../api'
import { Link, projectPath } from '../router'
import { ErrorMessage, Field, formatTime, Lamp, stateLabel, stateTone } from '../ui'

export function ProjectsPage() {
  const [projects, setProjects] = useState<ProjectSummary[] | null>(null)
  const [error, setError] = useState('')
  const [deleting, setDeleting] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      setProjects(await api.projects())
      setError('')
    } catch (err) {
      setError(errorText(err))
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  return (
    <>
      <h1>Проекты</h1>
      <ErrorMessage text={error} />
      {projects === null ? (
        !error && <p className="muted">Загрузка...</p>
      ) : projects.length === 0 ? (
        <p className="muted">Проектов пока нет. Создайте первый проект ниже.</p>
      ) : (
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Проект</th>
                <th>Адреса для клонирования</th>
                <th>Target'ы</th>
                <th aria-label="Действия" />
              </tr>
            </thead>
            <tbody>
              {projects.map((p) => (
                <Fragment key={p.name}>
                  <tr>
                    <td>
                      <Link to={projectPath(p.name)} className="strong">
                        {p.name}
                      </Link>
                      <div className="muted small">создан {formatTime(p.created_at)}</div>
                    </td>
                    <td>
                      <div className="clone-line">
                        <span className="muted small">HTTP(S)</span> <code>{p.http_url}</code>
                      </div>
                      <div className="clone-line">
                        <span className="muted small">SSH</span> <code>{p.ssh_url}</code>
                      </div>
                    </td>
                    <td>
                      <TargetsSummary targets={p.targets} />
                    </td>
                    <td className="actions">
                      <button
                        type="button"
                        className="danger small"
                        aria-label={`Удалить ${p.name}`}
                        onClick={() => setDeleting(deleting === p.name ? null : p.name)}
                      >
                        Удалить
                      </button>
                    </td>
                  </tr>
                  {deleting === p.name && (
                    <tr>
                      <td colSpan={4}>
                        <DeleteProject
                          name={p.name}
                          onCancel={() => setDeleting(null)}
                          onDeleted={() => {
                            setDeleting(null)
                            void load()
                          }}
                        />
                      </td>
                    </tr>
                  )}
                </Fragment>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <CreateProject onCreated={() => void load()} />
    </>
  )
}

const summaryOrder: TargetState[] = ['error', 'pending', 'synced']

function TargetsSummary({ targets }: { targets: Target[] }) {
  if (targets.length === 0) return <span className="muted">нет target'ов</span>
  const disabled = targets.filter((t) => !t.enabled).length
  return (
    <div className="summary">
      <span className="lamp-strip" aria-hidden="true">
        {targets.map((t) => (
          <Lamp
            key={t.id}
            tone={t.enabled ? stateTone(t.state) : 'off'}
            title={`${t.name}: ${stateLabel(t.state)}${t.enabled ? '' : ', выключен'}`}
          />
        ))}
      </span>
      {summaryOrder.map((state) => {
        const count = targets.filter((t) => t.state === state).length
        if (count === 0) return null
        return (
          <span key={state} className={`count count-${state}`}>
            {stateLabel(state)}: {count}
          </span>
        )
      })}
      {disabled > 0 && <span className="count">выключено: {disabled}</span>}
    </div>
  )
}

function CreateProject({ onCreated }: { onCreated: () => void }) {
  const [name, setName] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit() {
    setBusy(true)
    setError('')
    try {
      await api.createProject(name.trim())
      setName('')
      onCreated()
    } catch (err) {
      setError(errorText(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="card">
      <h2>Новый проект</h2>
      <form
        className="inline-form"
        onSubmit={(e) => {
          e.preventDefault()
          void submit()
        }}
      >
        <Field label="Имя проекта" hint="Строчные латинские буквы, цифры, точка, подчеркивание и дефис.">
          <input value={name} required onChange={(e) => setName(e.target.value)} placeholder="my-project" />
        </Field>
        <button type="submit" disabled={busy || name.trim() === ''}>
          Создать проект
        </button>
      </form>
      <ErrorMessage text={error} />
    </section>
  )
}

interface DeleteProjectProps {
  name: string
  onCancel: () => void
  onDeleted: () => void
}

function DeleteProject({ name, onCancel, onDeleted }: DeleteProjectProps) {
  const [confirm, setConfirm] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit() {
    setBusy(true)
    setError('')
    try {
      await api.deleteProject(name, confirm)
      onDeleted()
    } catch (err) {
      setError(errorText(err))
      setBusy(false)
    }
  }

  return (
    <form
      className="danger-zone"
      onSubmit={(e) => {
        e.preventDefault()
        if (confirm === name) void submit()
      }}
    >
      <p>
        Будут удалены репозиторий проекта <strong>{name}</strong> и его архив в metagit. Репозитории на хостингах
        (target'ах) не затрагиваются.
      </p>
      <Field label="Введите имя проекта для подтверждения">
        <input value={confirm} autoComplete="off" autoFocus onChange={(e) => setConfirm(e.target.value)} />
      </Field>
      <div className="buttons">
        <button type="submit" className="danger" disabled={busy || confirm !== name}>
          Удалить проект
        </button>
        <button type="button" className="secondary" onClick={onCancel}>
          Отмена
        </button>
      </div>
      <ErrorMessage text={error} />
    </form>
  )
}
