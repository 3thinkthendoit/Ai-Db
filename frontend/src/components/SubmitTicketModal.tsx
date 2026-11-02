import { useState } from 'react'
import { BodyPortal } from '../lib/portal'
import { useI18n } from '../i18n'

interface Props {
  sql: string
  busy?: boolean
  error?: string
  onClose: () => void
  onSubmit: (title: string, content: string) => void | Promise<void>
}

export function SubmitTicketModal({ sql, busy = false, error = '', onClose, onSubmit }: Props) {
  const { t } = useI18n()
  const [title, setTitle] = useState('')
  const [content, setContent] = useState('')
  const [err, setErr] = useState('')

  async function handleSubmit() {
    const nextTitle = title.trim()
    const nextContent = content.trim()
    if (!nextTitle || !nextContent) {
      setErr(t('dba.submitTicket.needFields'))
      return
    }
    setErr('')
    await onSubmit(nextTitle, nextContent)
  }

  const shownErr = err || error

  return (
    <BodyPortal>
      <div
        className="modal-backdrop"
        onMouseDown={(e) => {
          if (e.target === e.currentTarget && !busy) onClose()
        }}
      >
        <div className="modal ticket-modal" role="dialog" aria-modal="true" onClick={(e) => e.stopPropagation()}>
          <div className="modal-head">
            <div className="ticket-modal-head-text">
              <h3>{t('dba.submitTicket')}</h3>
              <p className="ticket-modal-sub">{t('dba.submitTicket.hint')}</p>
            </div>
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

          <label>
            {t('dba.submitTicket.title')}
            <input
              value={title}
              maxLength={128}
              autoFocus
              disabled={busy}
              placeholder={t('dba.submitTicket.title.ph')}
              onChange={(e) => setTitle(e.target.value)}
            />
          </label>

          <label>
            {t('dba.submitTicket.content')}
            <textarea
              value={content}
              rows={3}
              maxLength={512}
              disabled={busy}
              placeholder={t('dba.submitTicket.content.ph')}
              onChange={(e) => setContent(e.target.value)}
            />
          </label>

          <div className="ticket-sql-block">
            <div className="ticket-sql-label">{t('dba.submitTicket.sql')}</div>
            <pre className="ticket-sql-pre mono">{sql}</pre>
          </div>

          {shownErr && <p className="error-text">{shownErr}</p>}

          <div className="modal-actions">
            <button type="button" disabled={busy} onClick={onClose}>
              {t('conn.form.cancel')}
            </button>
            <button type="button" className="primary" disabled={busy} onClick={() => void handleSubmit()}>
              {busy ? t('admin.saving') : t('dba.submitTicket.submit')}
            </button>
          </div>
        </div>
      </div>
    </BodyPortal>
  )
}
