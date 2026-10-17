import { useCallback, useEffect, useState } from 'react'
import { api, errorText, type CreatedToken, type SSHKey, type Token } from '../api'
import { fileToBase64, saveFile } from '../files'
import { setLocaleMode, storedLocaleMode, useT, type LocaleMode } from '../locale'
import { Link, projectPath } from '../router'
import { ErrorMessage, Field, formatTime, Notice } from '../ui'

export function SettingsPage() {
  const text = useT()
  return (
    <>
      <h1>{text.settings.title}</h1>
      <LanguageSection />
      <TokensSection />
      <SSHKeysSection />
      <ExportSection />
      <ImportSection />
    </>
  )
}

const localeModes: LocaleMode[] = ['system', 'ru', 'en']

function LanguageSection() {
  const text = useT()
  const [mode, setMode] = useState(storedLocaleMode)

  return (
    <section className="card">
      <h2>{text.settings.language}</h2>
      <fieldset className="locale-switch">
        <legend className="visually-hidden">{text.settings.language}</legend>
        {localeModes.map((m) => (
          <label key={m}>
            <input
              type="radio"
              name="locale"
              value={m}
              checked={mode === m}
              onChange={() => {
                setLocaleMode(m)
                setMode(m)
              }}
            />
            {/* Language names are written in their own language. */}
            <span lang={m === 'system' ? undefined : m}>{text.settings.languageModes[m]}</span>
          </label>
        ))}
      </fieldset>
    </section>
  )
}

function TokensSection() {
  const text = useT()
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
    if (!window.confirm(text.settings.confirmRevoke(token.name))) return
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
      <h2>{text.settings.tokens}</h2>
      <p className="muted">{text.settings.tokensHint}</p>
      {created && (
        <div className="secret-once" role="status">
          <p>{text.settings.tokenCreated(created.token.name)}</p>
          <code className="block">{created.value}</code>
          <button type="button" className="secondary small" onClick={() => setCreated(null)}>
            {text.settings.hide}
          </button>
        </div>
      )}
      {tokens !== null &&
        (tokens.length === 0 ? (
          <p className="muted">{text.settings.noTokens}</p>
        ) : (
          <div className="table-wrap">
            <table aria-label={text.settings.tokensTable}>
              <thead>
                <tr>
                  <th>{text.common.name}</th>
                  <th>{text.settings.created}</th>
                  <th>{text.settings.lastUsed}</th>
                  <th aria-label={text.common.actions} />
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
                        <span className="muted">{text.settings.neverUsed}</span>
                      )}
                    </td>
                    <td className="actions">
                      <button
                        type="button"
                        className="danger small"
                        aria-label={text.settings.revokeNamed(t.name)}
                        onClick={() => void revoke(t)}
                      >
                        {text.settings.revoke}
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
        <Field label={text.settings.tokenName}>
          <input value={name} required placeholder="laptop" onChange={(e) => setName(e.target.value)} />
        </Field>
        <button type="submit" disabled={busy || name.trim() === ''}>
          {text.settings.createToken}
        </button>
      </form>
      <ErrorMessage text={error} />
    </section>
  )
}

function SSHKeysSection() {
  const text = useT()
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
    if (!window.confirm(text.settings.confirmDeleteKey(key.name || key.fingerprint))) return
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
      <h2>{text.settings.sshKeys}</h2>
      <p className="muted">{text.settings.sshKeysHint}</p>
      {keys !== null &&
        (keys.length === 0 ? (
          <p className="muted">{text.settings.noKeys}</p>
        ) : (
          <div className="table-wrap">
            <table aria-label={text.settings.sshKeys}>
              <thead>
                <tr>
                  <th>{text.common.name}</th>
                  <th>{text.settings.fingerprint}</th>
                  <th>{text.settings.added}</th>
                  <th aria-label={text.common.actions} />
                </tr>
              </thead>
              <tbody>
                {keys.map((k) => (
                  <tr key={k.id}>
                    <td className="strong">{k.name || <span className="muted">{text.settings.unnamed}</span>}</td>
                    <td>
                      <code>{k.fingerprint}</code>
                    </td>
                    <td className="nowrap mono">{formatTime(k.created_at)}</td>
                    <td className="actions">
                      <button
                        type="button"
                        className="danger small"
                        aria-label={text.settings.deleteKey(k.name || k.fingerprint)}
                        onClick={() => void remove(k)}
                      >
                        {text.common.delete}
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
        <Field label={text.settings.keyName}>
          <input value={name} placeholder="laptop" onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label={text.settings.publicKey}>
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
          {text.settings.addKey}
        </button>
      </form>
      <ErrorMessage text={error} />
    </section>
  )
}

const MIN_PASSPHRASE = 8

function ExportSection() {
  const text = useT()
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
      setNotice(text.settings.fileSaved(file.filename))
    } catch (err) {
      setError(errorText(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="card">
      <h2>{text.settings.export}</h2>
      <p className="muted">{text.settings.exportHint}</p>
      <p className="message message-warn">{text.settings.exportWarning}</p>
      <form
        className="inline-form"
        onSubmit={(e) => {
          e.preventDefault()
          void run()
        }}
      >
        <Field label={text.settings.exportPassword} hint={text.settings.passwordHint(MIN_PASSPHRASE)}>
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
          {text.settings.download}
        </button>
      </form>
      <ErrorMessage text={error} />
      <Notice text={notice} />
    </section>
  )
}

function ImportSection() {
  const text = useT()
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
      <h2>{text.settings.import}</h2>
      <p className="muted">{text.settings.importHint}</p>
      <form
        className="inline-form"
        onSubmit={(e) => {
          e.preventDefault()
          void run()
        }}
      >
        <Field label={text.settings.importFile}>
          <input type="file" onChange={(e) => setFile(e.target.files?.[0] ?? null)} />
        </Field>
        <Field label={text.settings.importPassword}>
          <input
            type="password"
            value={passphrase}
            required
            autoComplete="off"
            onChange={(e) => setPassphrase(e.target.value)}
          />
        </Field>
        <button type="submit" disabled={busy || file === null || passphrase === ''}>
          {text.settings.importSubmit}
        </button>
      </form>
      <ErrorMessage text={error} />
      {created && (
        <div className="message message-ok" role="status">
          {created.length === 0 ? (
            <p>{text.settings.importNone}</p>
          ) : (
            <>
              <p>{text.settings.importCreated}</p>
              <ul>
                {created.map((name) => (
                  <li key={name}>
                    <Link to={projectPath(name)}>{name}</Link>
                  </li>
                ))}
              </ul>
            </>
          )}
          <p>{text.settings.importEmptyRepos}</p>
        </div>
      )}
    </section>
  )
}
