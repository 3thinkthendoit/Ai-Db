import { useEffect, useState } from 'react'
import {
  CreateDatabase,
  CreateDatabaseOptions,
  WritesAllowed,
  type CreateDatabaseResult,
  type CreateDatabaseSpec,
} from '../lib/api'
import { useI18n } from '../i18n'
import { BodyPortal } from '../lib/portal'

interface Props {
  connectionId: string
  connType: string
  onClose: () => void
  /** Called after a successful creation so the caller refreshes the tree. */
  onCreated: () => void
}

/** CSPRNG password for the auto-generated account credential. The alphabet skips
 * look-alikes (0/O, 1/l/I) and shell-hostile characters so copy-paste is safe. */
function randomPassword(len = 16): string {
  const alphabet = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789-_+=.'
  const buf = new Uint32Array(len)
  crypto.getRandomValues(buf)
  return Array.from(buf, (n) => alphabet[n % alphabet.length]).join('')
}

/** Create-database dialog: database identity (charset/collation or encoding)
 * plus an optional login account granted on the new database. */
export function CreateDatabaseModal({ connectionId, connType, onClose, onCreated }: Props) {
  const { t } = useI18n()
  const [opts, setOpts] = useState<CreateDatabaseOptions>({ charsets: [], encodings: [] })
  const [name, setName] = useState('')
  const [charset, setCharset] = useState('')
  const [collate, setCollate] = useState('')
  const [encoding, setEncoding] = useState('')
  const [account, setAccount] = useState('')
  // Pre-fill a fresh random password every time the dialog opens (remount).
  const [password, setPassword] = useState(() => randomPassword())
  const [host, setHost] = useState('%')
  const [busy, setBusy] = useState(false)
  const [msg, setMsg] = useState('')
  // Probe the global write gate up front: creation is DDL, so with "Allow
  // writes" off the submit would only fail after the form is filled.
  const [writesOff, setWritesOff] = useState(false)

  useEffect(() => {
    let cancelled = false
    WritesAllowed()
      .then((ok) => {
        if (!cancelled) setWritesOff(!ok)
      })
      .catch(() => {
        // Probe failure must not block the dialog; the server re-checks on submit.
      })
    CreateDatabaseOptions(connectionId)
      .then((o) => {
        if (!cancelled) setOpts(o)
      })
      .catch((e: unknown) => {
        if (!cancelled) setMsg(String((e as Error)?.message || e))
      })
    return () => {
      cancelled = true
    }
  }, [connectionId])

  const chosen = opts.charsets.find((c) => c.name === charset)
  const collations = chosen?.collations ?? []

  async function submit() {
    if (busy) return
    setMsg('')
    if (!name.trim()) {
      setMsg(t('query.createDb.errName'))
      return
    }
    if (account.trim() && !password) {
      setMsg(t('query.createDb.errPassword'))
      return
    }
    const spec: CreateDatabaseSpec = { name: name.trim() }
    if (connType === 'mysql') {
      if (charset) spec.charset = charset
      if (collate) spec.collate = collate
      if (account.trim()) {
        spec.account = account.trim()
        spec.password = password
        spec.host = host.trim() || '%'
      }
    } else {
      if (encoding) spec.encoding = encoding
      if (collate.trim()) spec.collate = collate.trim()
      if (account.trim()) {
        spec.account = account.trim()
        spec.password = password
      }
    }
    setBusy(true)
    try {
      const res: CreateDatabaseResult = await CreateDatabase(connectionId, spec)
      if (res.ok) {
        onCreated()
        onClose()
        return
      }
      setMsg(res.error || t('query.createDb.errFailed'))
    } catch (e: unknown) {
      setMsg(String((e as Error)?.message || e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <BodyPortal>
      <div className="modal-backdrop" onMouseDown={(e) => { if (e.target === e.currentTarget) onClose() }}>
        <div className="modal" role="dialog" aria-modal="true">
          <div className="modal-head">
            <h3>{t('query.createDb.title')}</h3>
            <button type="button" className="modal-close" onClick={onClose} aria-label={t('modal.close')}>×</button>
          </div>
          <label>
            {t('query.createDb.name')}
            <input value={name} onChange={(e) => setName(e.target.value)} autoFocus spellCheck={false} />
          </label>
          {connType === 'mysql' ? (
            <div className="row2">
              <label>
                {t('query.createDb.charset')}
                <select value={charset} onChange={(e) => { setCharset(e.target.value); setCollate('') }}>
                  <option value="">{t('query.createDb.serverDefault')}</option>
                  {opts.charsets.map((c) => (
                    <option key={c.name} value={c.name}>{c.name}</option>
                  ))}
                </select>
              </label>
              <label>
                {t('query.createDb.collate')}
                <select value={collate} onChange={(e) => setCollate(e.target.value)} disabled={!charset}>
                  <option value="">
                    {charset ? (chosen?.defaultCollation || t('query.createDb.default')) : t('query.createDb.serverDefault')}
                  </option>
                  {collations.map((c) => (
                    <option key={c} value={c}>{c}</option>
                  ))}
                </select>
              </label>
            </div>
          ) : (
            <div className="row2">
              <label>
                {t('query.createDb.encoding')}
                <select value={encoding} onChange={(e) => setEncoding(e.target.value)}>
                  <option value="">{t('query.createDb.serverDefault')}</option>
                  {opts.encodings.map((c) => (
                    <option key={c} value={c}>{c}</option>
                  ))}
                </select>
              </label>
              <label>
                {t('query.createDb.collate')}
                <input value={collate} onChange={(e) => setCollate(e.target.value)} placeholder="C / en_US.utf8" spellCheck={false} />
              </label>
            </div>
          )}
          <div className="row2">
            <label>
              {t('query.createDb.account')}
              <input value={account} onChange={(e) => setAccount(e.target.value)} spellCheck={false} />
            </label>
            <label>
              {t('query.createDb.password')}
              <input type="text" value={password} onChange={(e) => setPassword(e.target.value)} spellCheck={false} autoComplete="off" />
            </label>
          </div>
          {connType === 'mysql' && (
            <label>
              {t('query.createDb.host')}
              <select value={host} onChange={(e) => setHost(e.target.value)}>
                <option value="%">{t('query.createDb.hostAll')}</option>
                <option value="localhost">{t('query.createDb.hostLocal')}</option>
              </select>
            </label>
          )}
          <p className="muted">{t('query.createDb.grantHint')}</p>
          {writesOff && <p className="error-text">{t('query.createDb.writesOff')}</p>}
          {msg && <p className="error-text">{msg}</p>}
          <div className="modal-actions">
            <button type="button" disabled={busy} onClick={onClose}>{t('conn.form.cancel')}</button>
            <button type="button" disabled={busy || writesOff} onClick={() => void submit()}>{t('query.createDb.submit')}</button>
          </div>
        </div>
      </div>
    </BodyPortal>
  )
}
