import { useEffect, useState } from 'react'
import { Connection } from '../types'
import { useI18n } from '../i18n'
import { BodyPortal } from '../lib/portal'

interface Props {
  connections: Connection[]
  /** 空字符串 = 不预选，须用户点选后才能确认。 */
  defaultConnectionId: string
  hint: string
  confirmLabel: string
  onClose: () => void
  onPick: (connectionId: string) => void
  onManage: () => void
}

function typeLabel(t: string) {
  if (t === 'mysql') return 'MySQL'
  if (t === 'postgres') return 'PostgreSQL'
  if (t === 'redis') return 'Redis'
  if (t === 'mongo') return 'MongoDB'
  return t
}

function addrLine(c: Connection) {
  const auth = c.user ? `${c.user}@` : ''
  return `${auth}${c.host}:${c.port}`
}

export function PickConnectionModal({
  connections,
  defaultConnectionId,
  hint,
  confirmLabel,
  onClose,
  onPick,
  onManage,
}: Props) {
  const { t } = useI18n()
  // 不回落到 connections[0]：未选择时保持空。
  const [connectionId, setConnectionId] = useState(defaultConnectionId || '')

  useEffect(() => {
    setConnectionId(defaultConnectionId || '')
  }, [defaultConnectionId, connections])

  const canOpen = !!connectionId

  return (
    <BodyPortal>
      <div className="overlay" onMouseDown={(e) => { if (e.target === e.currentTarget) onClose() }}>
        <div
          className="modal pick-conn-modal"
          role="dialog"
          aria-modal="true"
          aria-labelledby="pick-conn-title"
        >
          <div className="modal-head">
            <div className="pick-conn-head-text">
              <h3 id="pick-conn-title">{t('work.pickConn.title')}</h3>
              <p className="pick-conn-sub">{hint}</p>
            </div>
            <button
              type="button"
              className="modal-close"
              title={t('modal.close')}
              aria-label={t('modal.close')}
              onClick={onClose}
            >
              ×
            </button>
          </div>

          {connections.length === 0 ? (
            <div className="pick-conn-empty">
              <p>{t('task.needConnection')}</p>
            </div>
          ) : (
            <div className="pick-conn-list" role="listbox" aria-label={t('task.connection')}>
              {connections.map((c) => {
                const on = c.id === connectionId
                return (
                  <button
                    key={c.id}
                    type="button"
                    role="option"
                    aria-selected={on}
                    className={`pick-conn-item${on ? ' on' : ''}`}
                    onClick={() => setConnectionId(c.id)}
                    onDoubleClick={() => onPick(c.id)}
                  >
                    <span className="pick-conn-item-main">
                      <span className="pick-conn-item-name">
                        {c.name}
                        <span className={`tag ${c.type}`}>{typeLabel(c.type)}</span>
                      </span>
                      <span className="pick-conn-item-addr mono">{addrLine(c)}</span>
                    </span>
                    <span className={`pick-conn-check${on ? ' on' : ''}`} aria-hidden>
                      {on ? '✓' : ''}
                    </span>
                  </button>
                )
              })}
            </div>
          )}

          <div className="modal-actions pick-conn-actions">
            <button type="button" className="sm ghost-link" onClick={onManage}>
              {t('work.db.manage')}
            </button>
            <div className="pick-conn-actions-right">
              <button type="button" className="sm" onClick={onClose}>{t('ss.cancel')}</button>
              {connections.length > 0 && (
                <button
                  type="button"
                  className="sm primary"
                  disabled={!canOpen}
                  onClick={() => onPick(connectionId)}
                >
                  {confirmLabel}
                </button>
              )}
            </div>
          </div>
        </div>
      </div>
    </BodyPortal>
  )
}
