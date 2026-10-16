import { useId, useState } from 'react'
import { api, errorText, type AuthKind, type Target, type TargetInput } from '../api'
import { useT } from '../locale'
import { authKindLabel, ErrorMessage, Field } from '../ui'

interface Props {
  project: string
  // When set, the form edits this target, otherwise it adds a new one.
  target?: Target
  onSaved: (target: Target) => void
  onCancel: () => void
}

const authKinds: AuthKind[] = ['none', 'https', 'ssh']

export function TargetForm({ project, target, onSaved, onCancel }: Props) {
  const text = useT()
  // The form keeps its own copy of the values, so background refreshes of the
  // target list do not overwrite what the user is typing.
  const [name, setName] = useState(target?.name ?? '')
  const [url, setURL] = useState(target?.url ?? '')
  const [authKind, setAuthKind] = useState<AuthKind>(target?.auth_kind ?? 'none')
  const [username, setUsername] = useState(target?.username ?? '')
  const [secret, setSecret] = useState('')
  const [enabled, setEnabled] = useState(target?.enabled ?? true)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const id = useId()

  // The stored secret is kept only while the auth kind stays the same.
  const keepSecret = target !== undefined && target.has_secret && target.auth_kind === authKind

  async function submit() {
    setBusy(true)
    setError('')
    const input: TargetInput = {
      name: name.trim(),
      url: url.trim(),
      auth_kind: authKind,
      username: authKind === 'https' ? username.trim() : '',
      enabled,
    }
    // The secret is write-only: it is sent only when the user typed a new one.
    if (authKind !== 'none' && secret !== '') input.secret = secret
    try {
      const saved = target ? await api.updateTarget(project, target.id, input) : await api.createTarget(project, input)
      setSecret('')
      onSaved(saved)
    } catch (err) {
      setError(errorText(err))
      setBusy(false)
    }
  }

  return (
    <form
      className="card target-form"
      aria-labelledby={id + '-title'}
      onSubmit={(e) => {
        e.preventDefault()
        void submit()
      }}
    >
      <h3 id={id + '-title'}>{target ? text.targetForm.editTitle(target.name) : text.targetForm.newTitle}</h3>
      <div className="grid">
        <Field label={text.common.name}>
          <input value={name} required placeholder="github" onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label={text.targetForm.url} hint={text.targetForm.urlHint}>
          <input
            className="mono"
            value={url}
            required
            spellCheck={false}
            placeholder="https://github.com/user/repo.git"
            onChange={(e) => setURL(e.target.value)}
          />
        </Field>
        <Field label={text.project.credentials}>
          <select
            value={authKind}
            onChange={(e) => {
              setAuthKind(e.target.value as AuthKind)
              setSecret('')
            }}
          >
            {authKinds.map((kind) => (
              <option key={kind} value={kind}>
                {authKindLabel(kind)}
              </option>
            ))}
          </select>
        </Field>
        {authKind === 'https' && (
          <>
            <Field label={text.targetForm.username}>
              <input
                value={username}
                autoComplete="off"
                spellCheck={false}
                onChange={(e) => setUsername(e.target.value)}
              />
            </Field>
            <Field label={text.targetForm.token}>
              <input
                type="password"
                value={secret}
                autoComplete="new-password"
                required={!keepSecret}
                placeholder={keepSecret ? text.targetForm.keepSecret : ''}
                onChange={(e) => setSecret(e.target.value)}
              />
            </Field>
          </>
        )}
      </div>
      {authKind === 'ssh' && (
        <Field label={text.targetForm.sshKey} hint={text.targetForm.sshKeyHint}>
          <textarea
            className="mono"
            value={secret}
            rows={6}
            autoComplete="off"
            spellCheck={false}
            required={!keepSecret}
            placeholder={keepSecret ? text.targetForm.keepSecret : '-----BEGIN OPENSSH PRIVATE KEY-----'}
            onChange={(e) => setSecret(e.target.value)}
          />
        </Field>
      )}
      {authKind !== 'none' && <p className="hint">{text.targetForm.writeOnly}</p>}
      <label className="checkbox">
        <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
        <span>{text.project.enabled}</span>
      </label>
      <ErrorMessage text={error} />
      <div className="buttons">
        <button type="submit" disabled={busy}>
          {target ? text.targetForm.save : text.project.addTarget}
        </button>
        <button type="button" className="secondary" onClick={onCancel}>
          {text.common.cancel}
        </button>
      </div>
    </form>
  )
}
