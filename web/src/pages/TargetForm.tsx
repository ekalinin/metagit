import { useId, useState } from 'react'
import { api, errorText, type AuthKind, type Target, type TargetInput } from '../api'
import { authKindLabel, ErrorMessage, Field } from '../ui'

export const KEEP_SECRET_PLACEHOLDER = 'задан, оставьте пустым, чтобы не менять'

interface Props {
  project: string
  // When set, the form edits this target, otherwise it adds a new one.
  target?: Target
  onSaved: (target: Target) => void
  onCancel: () => void
}

const authKinds: AuthKind[] = ['none', 'https', 'ssh']

export function TargetForm({ project, target, onSaved, onCancel }: Props) {
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
      <h3 id={id + '-title'}>{target ? `Изменение target'а ${target.name}` : 'Новый target'}</h3>
      <div className="grid">
        <Field label="Имя">
          <input value={name} required placeholder="github" onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field
          label="URL репозитория"
          hint="https://..., ssh://... или user@host:path. Не указывайте логин, пароль или токен в URL."
        >
          <input
            value={url}
            required
            spellCheck={false}
            placeholder="https://github.com/user/repo.git"
            onChange={(e) => setURL(e.target.value)}
          />
        </Field>
        <Field label="Учетные данные">
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
            <Field label="Логин">
              <input
                value={username}
                autoComplete="off"
                spellCheck={false}
                onChange={(e) => setUsername(e.target.value)}
              />
            </Field>
            <Field label="Токен">
              <input
                type="password"
                value={secret}
                autoComplete="new-password"
                required={!keepSecret}
                placeholder={keepSecret ? KEEP_SECRET_PLACEHOLDER : ''}
                onChange={(e) => setSecret(e.target.value)}
              />
            </Field>
          </>
        )}
      </div>
      {authKind === 'ssh' && (
        <Field label="Приватный SSH-ключ" hint="Ключ без пароля.">
          <textarea
            value={secret}
            rows={6}
            autoComplete="off"
            spellCheck={false}
            required={!keepSecret}
            placeholder={keepSecret ? KEEP_SECRET_PLACEHOLDER : '-----BEGIN OPENSSH PRIVATE KEY-----'}
            onChange={(e) => setSecret(e.target.value)}
          />
        </Field>
      )}
      {authKind !== 'none' && (
        <p className="hint">Учетные данные можно задать или заменить, но не посмотреть.</p>
      )}
      <label className="checkbox">
        <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
        <span>Включен</span>
      </label>
      <ErrorMessage text={error} />
      <div className="buttons">
        <button type="submit" disabled={busy}>
          {target ? 'Сохранить' : 'Добавить target'}
        </button>
        <button type="button" className="secondary" onClick={onCancel}>
          Отмена
        </button>
      </div>
    </form>
  )
}
