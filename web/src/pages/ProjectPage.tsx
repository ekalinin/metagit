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
import { useT } from '../locale'
import { Link } from '../router'
import { authKindLabel, ErrorMessage, formatTime, Lamp, Notice, StateBadge, stateTone } from '../ui'
import { ArchiveList, EventLog } from './History'
import { ProtectedRefs } from './ProtectedRefs'
import { TargetForm } from './TargetForm'

interface Props {
  name: string
  // How often the page reloads project data, in milliseconds.
  refreshMs?: number
}

export function ProjectPage({ name, refreshMs = 5000 }: Props) {
  const text = useT()
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
        <h1>{text.project.notFound(name)}</h1>
        <p>
          <Link to="/">{text.common.toProjects}</Link>
        </p>
      </section>
    )
  }

  return (
    <>
      <p className="breadcrumbs">
        <Link to="/">{text.app.projects}</Link> / {name}
      </p>
      <h1 className="project-name">{name}</h1>
      <ErrorMessage text={error} />
      {details === null ? (
        !error && <p className="muted">{text.common.loading}</p>
      ) : (
        <>
          {/* The clone addresses are the source of the delivery lines drawn below. */}
          <section className="card source">
            <h2>{text.projects.cloneAddresses}</h2>
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
  const text = useT()
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
      target.enabled ? text.project.targetDisabled(target.name) : text.project.targetEnabled(target.name),
    )
  }

  function remove(target: Target) {
    if (!window.confirm(text.project.confirmDelete(target.name))) return
    if (form?.mode === 'edit' && form.target.id === target.id) setForm(null)
    void act(target, () => api.deleteTarget(project, target.id), text.project.targetDeleted(target.name))
  }

  return (
    <section className="targets">
      {/* The trunk runs down the left gutter and branches into every row; without targets it ends in a buffer stop. */}
      <div className={targets.length === 0 ? 'track-head dead-end' : 'track-head'}>
        <div className="section-head">
          <h2>{text.projects.targets}</h2>
          {form?.mode !== 'add' && (
            <button type="button" className="small" onClick={() => setForm({ mode: 'add' })}>
              {text.project.addTarget}
            </button>
          )}
        </div>
        <p className="muted">{text.project.targetsHint}</p>
        <ErrorMessage text={error} />
        <Notice text={notice} />
        {targets.length === 0 && <p className="muted">{text.project.noTargets}</p>}
      </div>
      {targets.length > 0 && (
        <div className="table-wrap">
          <table className="targets-table">
            <thead>
              <tr>
                <th className="track" aria-hidden="true" />
                <th>{text.common.name}</th>
                <th>{text.project.state}</th>
                <th>{text.project.lastSuccess}</th>
                <th>URL</th>
                <th>{text.project.credentials}</th>
                <th>{text.project.enabled}</th>
                <th aria-label={text.common.actions} />
              </tr>
            </thead>
            <tbody>
              {targets.map((t) => (
                <tr key={t.id} className={t.enabled ? undefined : 'disabled-row'}>
                  <td className={`track track-${t.enabled ? t.state : 'off'}`} aria-hidden="true">
                    {t.enabled && t.state === 'error' && <span className="track-break" />}
                    <Lamp tone={t.enabled ? stateTone(t.state) : 'off'} />
                  </td>
                  <td className="strong">{t.name}</td>
                  <td>
                    <StateBadge state={t.state} />
                    {t.last_error && <div className="target-error">{t.last_error}</div>}
                    {t.state === 'error' && t.next_attempt_at && (
                      <div className="muted small">{text.project.nextAttempt(formatTime(t.next_attempt_at))}</div>
                    )}
                  </td>
                  <td className="nowrap">
                    {t.last_success_at ? (
                      <span className="mono">{formatTime(t.last_success_at)}</span>
                    ) : (
                      <span className="muted">{text.project.never}</span>
                    )}
                  </td>
                  <td>
                    <code className="target-url">{t.url}</code>
                  </td>
                  <td>
                    {authKindLabel(t.auth_kind)}
                    {t.auth_kind === 'https' && t.username && (
                      <div className="muted small">{text.project.username(t.username)}</div>
                    )}
                  </td>
                  <td>{t.enabled ? text.project.yes : text.project.no}</td>
                  <td className="actions">
                    <button
                      type="button"
                      className="small"
                      disabled={busyId === t.id || !t.enabled}
                      title={t.enabled ? undefined : text.project.targetOff}
                      onClick={() =>
                        void act(t, () => api.syncTarget(project, t.id), text.project.syncStarted(t.name))
                      }
                    >
                      {text.project.syncNow}
                    </button>
                    <button
                      type="button"
                      className="secondary small"
                      aria-label={text.project.editNamed(t.name)}
                      onClick={() => setForm({ mode: 'edit', target: t })}
                    >
                      {text.project.edit}
                    </button>
                    <button
                      type="button"
                      className="secondary small"
                      disabled={busyId === t.id}
                      aria-label={`${t.enabled ? text.project.disable : text.project.enable} ${t.name}`}
                      onClick={() => toggle(t)}
                    >
                      {t.enabled ? text.project.disable : text.project.enable}
                    </button>
                    <button
                      type="button"
                      className="danger small"
                      disabled={busyId === t.id}
                      aria-label={text.common.deleteNamed(t.name)}
                      onClick={() => remove(t)}
                    >
                      {text.common.delete}
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
            setNotice(
              form.mode === 'edit' ? text.project.targetSaved(saved.name) : text.project.targetAdded(saved.name),
            )
            void reload()
          }}
        />
      )}
    </section>
  )
}
