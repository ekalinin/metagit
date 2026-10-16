import { useState } from 'react'
import { api, errorText } from '../api'
import { useT } from '../locale'
import { ErrorMessage, Field } from '../ui'

interface Props {
  ownerExists: boolean
  onLoggedIn: (ownerName: string) => void
}

export function LoginPage({ ownerExists, onLoggedIn }: Props) {
  const text = useT()
  const [name, setName] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  if (!ownerExists) {
    return (
      <main className="login">
        <div className="card login-card">
          <p className="brand">metagit</p>
          <h1>{text.login.ownerMissing}</h1>
          <p>{text.login.ownerCreate}</p>
          <pre>
            <code>{text.login.ownerCommand}</code>
          </pre>
          <p>{text.login.ownerThen}</p>
        </div>
      </main>
    )
  }

  async function submit() {
    setBusy(true)
    setError('')
    try {
      const result = await api.login(name, password)
      onLoggedIn(result.owner_name)
    } catch (err) {
      setError(errorText(err))
      setBusy(false)
    }
  }

  return (
    <main className="login">
      <div className="card login-card">
        <p className="brand">metagit</p>
        <h1>{text.login.title}</h1>
        <form
          onSubmit={(e) => {
            e.preventDefault()
            void submit()
          }}
        >
          <Field label={text.common.name}>
            <input
              name="name"
              autoComplete="username"
              required
              autoFocus
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </Field>
          <Field label={text.login.password}>
            <input
              name="password"
              type="password"
              autoComplete="current-password"
              required
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          </Field>
          <ErrorMessage text={error} />
          <button type="submit" disabled={busy}>
            {text.login.submit}
          </button>
        </form>
      </div>
    </main>
  )
}
