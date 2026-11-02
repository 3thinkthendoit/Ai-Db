import { useState } from 'react'
import { BodyPortal } from '../lib/portal'
import { useI18n } from '../i18n'
import type { MutationView } from '../lib/api'

export type TicketDetailAction = 'plan' | 'execute' | 'reject' | 'rollback'

interface Props {
  ticket: MutationView
  isReviewer: boolean
  busy?: boolean
  error?: string
  nodeLabel: string
  statusLabel: string
  formatTime: (iso: string) => string
  onClose: () => void
  onAction: (kind: TicketDetailAction, note: string) => void | Promise<void>
}

export function TicketDetailModal({
  ticket,
  isReviewer,
  busy = false,
  error = '',
  nodeLabel,
  statusLabel,
  formatTime,
  onClose,
  onAction,
}: Props) {
  const { t } = useI18n()
  const [note, setNote] = useState('')
  const [localErr, setLocalErr] = useState('')

  const pending = ticket.status === 'pending'
  const approved = ticket.status === 'approved'
  const canRollback = ticket.status === 'executed' && ticket.rollbackable
  const showReviewActions = isReviewer && (pending || approved || canRollback)

  async function run(kind: TicketDetailAction) {
    setLocalErr('')
    if (kind === 'reject' && !note.trim()) {
      setLocalErr(t('dba.reject.needReason'))
      return
    }
    await onAction(kind, note.trim())
  }

  const shownErr = localErr || error

  return (
    <BodyPortal>
      <div
        className="modal-backdrop"
        onMouseDown={(e) => {
          if (e.target === e.currentTarget && !busy) onClose()
        }}
      >
        <div className="modal ticket-detail-modal" role="dialog" aria-modal="true" onClick={(e) => e.stopPropagation()}>
          <div className="modal-head">
            <div className="ticket-modal-head-text">
              <h3>{t('dba.detail.title')}</h3>
              <p className="ticket-modal-sub">{t('dba.detail.hint')}</p>
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

          <div className="ticket-detail-grid">
            <div className="ticket-detail-item">
              <span className="ticket-detail-label">{t('dba.col.title')}</span>
              <div className="ticket-detail-value">{ticket.title || '—'}</div>
            </div>
            <div className="ticket-detail-item">
              <span className="ticket-detail-label">{t('dba.col.owner')}</span>
              <div className="ticket-detail-value">{ticket.ownerName || '—'}</div>
            </div>
            <div className="ticket-detail-item">
              <span className="ticket-detail-label">{t('dba.col.status')}</span>
              <div className="ticket-detail-value">
                <span className="tag">{statusLabel}</span>
                {ticket.rollbackable ? ` · ${t('dba.rollbackable')}` : ''}
              </div>
            </div>
            <div className="ticket-detail-item">
              <span className="ticket-detail-label">{t('dba.col.type')}</span>
              <div className="ticket-detail-value">
                <span className="tag">{ticket.stmtClass || '—'}</span>
              </div>
            </div>
            <div className="ticket-detail-item ticket-detail-span2">
              <span className="ticket-detail-label">{t('dba.col.node')}</span>
              <div className="ticket-detail-value">{nodeLabel}</div>
            </div>
            <div className="ticket-detail-item ticket-detail-span2">
              <span className="ticket-detail-label">{t('dba.col.time')}</span>
              <div className="ticket-detail-value">{formatTime(ticket.createdAt)}</div>
            </div>
            <div className="ticket-detail-item ticket-detail-span2">
              <span className="ticket-detail-label">{t('dba.col.content')}</span>
              <div className="ticket-detail-value ticket-detail-prewrap">{ticket.remark || '—'}</div>
            </div>
            {ticket.reviewNote && (
              <div className="ticket-detail-item ticket-detail-span2">
                <span className="ticket-detail-label">{t('dba.col.reviewNote')}</span>
                <div className="ticket-detail-value ticket-detail-prewrap">{ticket.reviewNote}</div>
              </div>
            )}
            <div className="ticket-detail-item ticket-detail-span2">
              <span className="ticket-detail-label">{t('dba.col.sql')}</span>
              <pre className="ticket-sql-pre mono">{ticket.sqlText || '—'}</pre>
            </div>
          </div>

          {showReviewActions && (pending || approved) && (
            <label>
              {pending ? t('dba.reject.reason') : t('dba.detail.note')}
              <textarea
                value={note}
                rows={2}
                maxLength={512}
                disabled={busy}
                placeholder={pending ? t('dba.reject.reason.ph') : t('dba.detail.note.ph')}
                onChange={(e) => setNote(e.target.value)}
              />
            </label>
          )}

          {shownErr && <p className="error-text">{shownErr}</p>}

          <div className="modal-actions ticket-detail-actions">
            <button type="button" disabled={busy} onClick={onClose}>
              {t('conn.form.cancel')}
            </button>
            {isReviewer && pending && (
              <>
                <button type="button" className="sm danger" disabled={busy} onClick={() => void run('reject')}>
                  {t('dba.reject')}
                </button>
                <button type="button" disabled={busy} onClick={() => void run('plan')}>
                  {t('dba.planExecute')}
                </button>
                <button type="button" className="primary" disabled={busy} onClick={() => void run('execute')}>
                  {t('dba.executeNow')}
                </button>
              </>
            )}
            {isReviewer && approved && (
              <button type="button" className="primary" disabled={busy} onClick={() => void run('execute')}>
                {t('dba.execute')}
              </button>
            )}
            {isReviewer && canRollback && (
              <button type="button" disabled={busy} onClick={() => void run('rollback')}>
                {t('dba.rollback')}
              </button>
            )}
          </div>
        </div>
      </div>
    </BodyPortal>
  )
}
