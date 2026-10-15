import { useCallback, useEffect, useState } from 'react'
import { api, errorText, type CreatedToken, type SSHKey, type Token } from '../api'
import { fileToBase64, saveFile } from '../files'
import { Link, projectPath } from '../router'
import { ErrorMessage, Field, formatTime, Notice } from '../ui'

export function SettingsPage() {
  return (
    <>
      <h1>Настройки</h1>
      <TokensSection />
      <SSHKeysSection />
      <ExportSection />
      <ImportSection />
    </>
  )
}

function TokensSection() {
  const [tokens, setTokens] = useState<Token[] | null>(null)
  const [name, setName] = useState('')
  // The token value lives only in this state: it is gone after dismissal or navigation.
  const [created, setCreated] = useState<CreatedToken | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const load = useCallback(async () => {
    try {
      setTokens(await api.tokens())
    } catch (err) {
      setError(errorText(err))
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  async function create() {
    setBusy(true)
    setError('')
    setCreated(null)
    try {
      setCreated(await api.createToken(name.trim()))
      setName('')
      await load()
    } catch (err) {
      setError(errorText(err))
    } finally {
      setBusy(false)
    }
  }

  async function revoke(token: Token) {
    if (!window.confirm(`Отозвать токен ${token.name}? Git-запросы с ним перестанут работать.`)) return
    setError('')
    try {
      await api.deleteToken(token.id)
      await load()
    } catch (err) {
      setError(errorText(err))
    }
  }

  return (
    <section className="card">
      <h2>Токены доступа для git</h2>
      <p className="muted">Токены для работы с репозиториями metagit по HTTP(S).</p>
      {created && (
        <div className="secret-once" role="status">
          <p>
            Токен <strong>{created.token.name}</strong> создан. Скопируйте значение сейчас: оно больше не будет
            показано.
          </p>
          <code className="block">{created.value}</code>
          <button type="button" className="secondary small" onClick={() => setCreated(null)}>
            Скрыть
          </button>
        </div>
      )}
      {tokens !== null &&
        (tokens.length === 0 ? (
          <p className="muted">Токенов нет.</p>
        ) : (
          <div className="table-wrap">
            <table aria-label="Токены">
              <thead>
                <tr>
                  <th>Имя</th>
                  <th>Создан</th>
                  <th>Последнее использование</th>
                  <th aria-label="Действия" />
                </tr>
              </thead>
              <tbody>
                {tokens.map((t) => (
                  <tr key={t.id}>
                    <td className="strong">{t.name}</td>
                    <td className="nowrap mono">{formatTime(t.created_at)}</td>
                    <td className="nowrap">
                      {t.last_used_at ? (
                        <span className="mono">{formatTime(t.last_used_at)}</span>
                      ) : (
                        <span className="muted">не использовался</span>
                      )}
                    </td>
                    <td className="actions">
                      <button
                        type="button"
                        className="danger small"
                        aria-label={`Отозвать ${t.name}`}
                        onClick={() => void revoke(t)}
                      >
                        Отозвать
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ))}
      <form
        className="inline-form"
        onSubmit={(e) => {
          e.preventDefault()
          void create()
        }}
      >
        <Field label="Имя токена">
          <input value={name} required placeholder="laptop" onChange={(e) => setName(e.target.value)} />
        </Field>
        <button type="submit" disabled={busy || name.trim() === ''}>
          Создать токен
        </button>
      </form>
      <ErrorMessage text={error} />
    </section>
  )
}

function SSHKeysSection() {
  const [keys, setKeys] = useState<SSHKey[] | null>(null)
  const [name, setName] = useState('')
  const [publicKey, setPublicKey] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const load = useCallback(async () => {
    try {
      setKeys(await api.sshKeys())
    } catch (err) {
      setError(errorText(err))
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  async function add() {
    setBusy(true)
    setError('')
    try {
      await api.addSSHKey(name.trim(), publicKey.trim())
      setName('')
      setPublicKey('')
      await load()
    } catch (err) {
      setError(errorText(err))
    } finally {
      setBusy(false)
    }
  }

  async function remove(key: SSHKey) {
    if (!window.confirm(`Удалить SSH-ключ ${key.name || key.fingerprint}?`)) return
    setError('')
    try {
      await api.deleteSSHKey(key.id)
      await load()
    } catch (err) {
      setError(errorText(err))
    }
  }

  return (
    <section className="card">
      <h2>SSH-ключи</h2>
      <p className="muted">Публичные ключи для работы с репозиториями metagit по SSH.</p>
      {keys !== null &&
        (keys.length === 0 ? (
          <p className="muted">SSH-ключей нет.</p>
        ) : (
          <div className="table-wrap">
            <table aria-label="SSH-ключи">
              <thead>
                <tr>
                  <th>Имя</th>
                  <th>Отпечаток</th>
                  <th>Добавлен</th>
                  <th aria-label="Действия" />
                </tr>
              </thead>
              <tbody>
                {keys.map((k) => (
                  <tr key={k.id}>
                    <td className="strong">{k.name || <span className="muted">без имени</span>}</td>
                    <td>
                      <code>{k.fingerprint}</code>
                    </td>
                    <td className="nowrap mono">{formatTime(k.created_at)}</td>
                    <td className="actions">
                      <button
                        type="button"
                        className="danger small"
                        aria-label={`Удалить ключ ${k.name || k.fingerprint}`}
                        onClick={() => void remove(k)}
                      >
                        Удалить
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ))}
      <form
        onSubmit={(e) => {
          e.preventDefault()
          void add()
        }}
      >
        <Field label="Имя (необязательно)">
          <input value={name} placeholder="laptop" onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label="Публичный ключ">
          <textarea
            className="mono"
            value={publicKey}
            rows={3}
            required
            spellCheck={false}
            placeholder="ssh-ed25519 AAAA... user@host"
            onChange={(e) => setPublicKey(e.target.value)}
          />
        </Field>
        <button type="submit" disabled={busy || publicKey.trim() === ''}>
          Добавить ключ
        </button>
      </form>
      <ErrorMessage text={error} />
    </section>
  )
}

const MIN_PASSPHRASE = 8

function ExportSection() {
  const [passphrase, setPassphrase] = useState('')
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [busy, setBusy] = useState(false)

  async function run() {
    setBusy(true)
    setError('')
    setNotice('')
    try {
      const file = await api.exportSettings(passphrase)
      saveFile(file.blob, file.filename)
      setNotice(`Файл ${file.filename} сохранен.`)
    } catch (err) {
      setError(errorText(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="card">
      <h2>Экспорт настроек</h2>
      <p className="muted">
        Файл содержит проекты, их защищенные refs и target'ы. Git-данные (код и архив) в файл не входят.
      </p>
      <p className="message message-warn">
        В файле есть учетные данные для хостингов. Он шифруется паролем: храните файл и пароль в надежном месте.
      </p>
      <form
        className="inline-form"
        onSubmit={(e) => {
          e.preventDefault()
          void run()
        }}
      >
        <Field label="Пароль для файла" hint={`Не короче ${MIN_PASSPHRASE} символов.`}>
          <input
            type="password"
            value={passphrase}
            minLength={MIN_PASSPHRASE}
            required
            autoComplete="new-password"
            onChange={(e) => setPassphrase(e.target.value)}
          />
        </Field>
        <button type="submit" disabled={busy || passphrase.length < MIN_PASSPHRASE}>
          Скачать файл экспорта
        </button>
      </form>
      <ErrorMessage text={error} />
      <Notice text={notice} />
    </section>
  )
}

function ImportSection() {
  const [file, setFile] = useState<File | null>(null)
  const [passphrase, setPassphrase] = useState('')
  const [created, setCreated] = useState<string[] | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  async function run() {
    if (!file) return
    setBusy(true)
    setError('')
    setCreated(null)
    try {
      const result = await api.importSettings(passphrase, await fileToBase64(file))
      setCreated(result.projects)
      setPassphrase('')
    } catch (err) {
      setError(errorText(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="card">
      <h2>Импорт настроек</h2>
      <p className="muted">
        Импорт выполняется целиком или не выполняется вовсе: при неверном пароле, поврежденном файле или конфликте
        имен проектов metagit ничего не меняет.
      </p>
      <form
        className="inline-form"
        onSubmit={(e) => {
          e.preventDefault()
          void run()
        }}
      >
        <Field label="Файл экспорта">
          <input type="file" onChange={(e) => setFile(e.target.files?.[0] ?? null)} />
        </Field>
        <Field label="Пароль файла">
          <input
            type="password"
            value={passphrase}
            required
            autoComplete="off"
            onChange={(e) => setPassphrase(e.target.value)}
          />
        </Field>
        <button type="submit" disabled={busy || file === null || passphrase === ''}>
          Импортировать
        </button>
      </form>
      <ErrorMessage text={error} />
      {created && (
        <div className="message message-ok" role="status">
          {created.length === 0 ? (
            <p>Импорт завершен, новых проектов нет.</p>
          ) : (
            <>
              <p>Созданы проекты:</p>
              <ul>
                {created.map((name) => (
                  <li key={name}>
                    <Link to={projectPath(name)}>{name}</Link>
                  </li>
                ))}
              </ul>
            </>
          )}
          <p>
            Репозитории проектов пустые. metagit ничего не меняет на хостингах, пока вы не отправите push в проект.
          </p>
        </div>
      )}
    </section>
  )
}
