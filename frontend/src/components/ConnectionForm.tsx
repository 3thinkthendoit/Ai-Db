import { useEffect, useState } from 'react'
import { Connection, ConnType, emptyConnection } from '../types'
import { useI18n } from '../i18n'
import { BodyPortal } from '../lib/portal'

interface Props {
  initial: Connection | null
  onSave: (c: Connection) => Promise<void>
  onTest: (c: Connection) => Promise<void>
  onClose: () => void
}

export function ConnectionForm({ initial, onSave, onTest, onClose }: Props) {
  const { t } = useI18n()
  const [form, setForm] = useState<Connection>(initial ?? emptyConnection())
  const [passwordTouched, setPasswordTouched] = useState(false)
  const [busy, setBusy] = useState(false)
  const [msg, setMsg] = useState('')

  useEffect(() => {
    const base = initial ?? emptyConnection()
    setForm({ ...base, password: '' })
    setPasswordTouched(false)
    setMsg('')
  }, [initial])

  function set<K extends keyof Connection>(key: K, value: Connection[K]) {
    setForm((f) => {
      const next = { ...f, [key]: value }
      if (key === 'type') {
        const typ = value as ConnType
        const ports: Record<ConnType, number> = { mysql: 3306, postgres: 5432, redis: 6379 }
        next.port = ports[typ]
        if (typ === 'redis') next.user = ''
      }
      return next
    })
  }

  function payload(): Connection {
    return {
      ...form,
      // Empty password means "keep existing" on update when not touched / left blank.
      password: passwordTouched ? form.password : '',
    }
  }

  async function run(fn: () => Promise<void>, ok: string) {
    setBusy(true)
    setMsg('')
    try {
      await fn()
      setMsg(ok)
    } catch (e: any) {
      setMsg(String(e?.message || e))
    } finally {
      setBusy(false)
    }
  }

  const passwordPlaceholder = form.id && form.passwordSet && !passwordTouched
    ? t('conn.form.passwordKeep')
    : undefined

  return (
    <BodyPortal>
    <div className="modal-backdrop">
      <div className="modal" role="dialog" aria-modal="true">
        <div className="modal-head">
          <h2>{form.id ? t('conn.form.edit') : t('conn.form.new')}</h2>
          <button
            type="button"
            className="modal-close"
            disabled={busy}
            title={t('modal.close')}
            aria-label={t('modal.close')}
            onClick={onClose}
          >
            ×
          </button>
        </div>
        <label>{t('conn.form.name')}<input value={form.name} onChange={(e) => set('name', e.target.value)} /></label>
        <label>{t('conn.form.type')}
          <select value={form.type} onChange={(e) => set('type', e.target.value as ConnType)}>
            <option value="mysql">MySQL</option>
            <option value="postgres">PostgreSQL</option>
            <option value="redis">Redis</option>
          </select>
        </label>
        <div className="row2">
          <label>{t('conn.form.host')}<input value={form.host} onChange={(e) => set('host', e.target.value)} /></label>
          <label>{t('conn.form.port')}<input type="number" value={form.port} onChange={(e) => set('port', Number(e.target.value))} /></label>
        </div>
        <div className="row2">
          <label>{t('conn.form.user')}<input value={form.user} onChange={(e) => set('user', e.target.value)} /></label>
          <label>{t('conn.form.password')}
            <input
              type="password"
              value={form.password}
              placeholder={passwordPlaceholder}
              onChange={(e) => {
                setPasswordTouched(true)
                set('password', e.target.value)
              }}
            />
          </label>
        </div>
        <label>{form.type === 'redis' ? t('conn.form.dbIndex') : t('conn.form.database')}
          <input value={form.database} onChange={(e) => set('database', e.target.value)} />
        </label>
        {msg && <p className="form-msg">{msg}</p>}
        <div className="modal-actions">
          <button type="button" disabled={busy} onClick={onClose}>{t('conn.form.cancel')}</button>
          <button type="button" disabled={busy} onClick={() => run(() => onTest(payload()), t('conn.form.testOk'))}>{t('conn.form.test')}</button>
          <button type="button" className="primary" disabled={busy || !form.name} onClick={() => run(async () => { await onSave(payload()); onClose() }, t('conn.form.saved'))}>{t('conn.form.save')}</button>
        </div>
      </div>
    </div>
    </BodyPortal>
  )
}
