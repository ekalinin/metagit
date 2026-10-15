import { useState } from 'react'
import { api, errorText, type ProtectedRef } from '../api'
import { ErrorMessage, Field } from '../ui'

interface Props {
  project: string
  refs: ProtectedRef[]
  reload: () => Promise<void>
}

export function ProtectedRefs({ project, refs, reload }: Props) {
  const [pattern, setPattern] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  async function run(action: () => Promise<unknown>): Promise<boolean> {
    setBusy(true)
    setError('')
    try {
      await action()
      await reload()
      return true
    } catch (err) {
      setError(errorText(err))
      return false
    } finally {
      setBusy(false)
    }
  }

  async function add() {
    if (await run(() => api.addProtected(project, pattern.trim()))) setPattern('')
  }

  return (
    <section className="card">
      <h2>Защищенные refs</h2>
      <p className="muted">
        Для refs, подходящих под шаблон, metagit отклоняет force-push, удаление и перенос тегов.
      </p>
      {refs.length === 0 ? (
        <p className="muted">Защищенных refs нет.</p>
      ) : (
        <ul className="pattern-list" aria-label="Шаблоны защищенных refs">
          {refs.map((r) => (
            <li key={r.id}>
              <code>{r.pattern}</code>
              <button
                type="button"
                className="danger small"
                disabled={busy}
                aria-label={`Удалить шаблон ${r.pattern}`}
                onClick={() => void run(() => api.deleteProtected(project, r.id))}
              >
                Удалить
              </button>
            </li>
          ))}
        </ul>
      )}
      <form
        className="inline-form"
        onSubmit={(e) => {
          e.preventDefault()
          void add()
        }}
      >
        <Field label="Шаблон" hint="Например: refs/heads/main или refs/tags/*">
          <input
            value={pattern}
            required
            spellCheck={false}
            placeholder="refs/heads/main"
            onChange={(e) => setPattern(e.target.value)}
          />
        </Field>
        <button type="submit" disabled={busy || pattern.trim() === ''}>
          Добавить шаблон
        </button>
      </form>
      <ErrorMessage text={error} />
    </section>
  )
}
