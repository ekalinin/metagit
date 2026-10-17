import { useState } from 'react'
import { api, errorText, type ProtectedRef } from '../api'
import { useT } from '../locale'
import { ErrorMessage, Field } from '../ui'

interface Props {
  project: string
  refs: ProtectedRef[]
  reload: () => Promise<void>
}

export function ProtectedRefs({ project, refs, reload }: Props) {
  const text = useT()
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
      <h2>{text.protectedRefs.title}</h2>
      <p className="muted">{text.protectedRefs.hint}</p>
      {refs.length === 0 ? (
        <p className="muted">{text.protectedRefs.empty}</p>
      ) : (
        <ul className="pattern-list" aria-label={text.protectedRefs.listLabel}>
          {refs.map((r) => (
            <li key={r.id}>
              <code>{r.pattern}</code>
              <button
                type="button"
                className="danger small"
                disabled={busy}
                aria-label={text.protectedRefs.deletePattern(r.pattern)}
                onClick={() => void run(() => api.deleteProtected(project, r.id))}
              >
                {text.common.delete}
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
        <Field label={text.protectedRefs.pattern} hint={text.protectedRefs.patternHint}>
          <input
            className="mono"
            value={pattern}
            required
            spellCheck={false}
            placeholder="refs/heads/main"
            onChange={(e) => setPattern(e.target.value)}
          />
        </Field>
        <button type="submit" disabled={busy || pattern.trim() === ''}>
          {text.protectedRefs.add}
        </button>
      </form>
      <ErrorMessage text={error} />
    </section>
  )
}
