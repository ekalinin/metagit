import { useState } from 'react'
import { api, errorText } from '../api'
import { ErrorMessage, Field } from '../ui'

interface Props {
  ownerExists: boolean
  onLoggedIn: (ownerName: string) => void
}

export function LoginPage({ ownerExists, onLoggedIn }: Props) {
  const [name, setName] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  if (!ownerExists) {
    return (
      <main className="login">
        <div className="card login-card">
          <p className="brand">metagit</p>
          <h1>Владелец не создан</h1>
          <p>
            Учетная запись владельца создается на сервере, а не в браузере. Выполните на сервере команду:
          </p>
          <pre>
            <code>metagit owner create --name &lt;имя&gt;</code>
          </pre>
          <p>После этого обновите страницу и войдите.</p>
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
        <h1>Вход</h1>
        <form
          onSubmit={(e) => {
            e.preventDefault()
            void submit()
          }}
        >
          <Field label="Имя">
            <input
              name="name"
              autoComplete="username"
              required
              autoFocus
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </Field>
          <Field label="Пароль">
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
            Войти
          </button>
        </form>
      </div>
    </main>
  )
}
