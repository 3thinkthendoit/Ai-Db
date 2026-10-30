import { useEffect, useState } from 'react'
import { Connection } from '../types'
import { useI18n } from '../i18n'

interface Props {
  connections: Connection[]
  defaultConnectionId: string
  onClose: () => void
  onCreate: (name: string, connectionId: string) => void
}

export function NewTaskModal({ connections, defaultConnectionId, onClose, onCreate }: Props) {
  const { t } = useI18n()
  const [name, setName] = useState('')
  const [connectionId, setConnectionId] = useState(defaultConnectionId || connections[0]?.id || '')

  useEffect(() => {
    setConnectionId(defaultConnectionId || connections[0]?.id || '')
  }, [defaultConnectionId, connections])

  const canCreate = !!connectionId

  return (
    <div className="overlay">
      <div className="modal" role="dialog" aria-modal="true">
        <div className="modal-head">
          <h3>{t('task.newTitle')}</h3>
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
        <label>
          {t('ss.namePh')}
          <input
            autoFocus
            value={name}
            placeholder={t('ss.unnamedTask')}
            onChange={(e) => setName(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && canCreate) onCreate(name, connectionId)
            }}
          />
        </label>
        <label>
          {t('task.connection')}
          <select value={connectionId} onChange={(e) => setConnectionId(e.target.value)}>
            {connections.length === 0 && <option value="">{t('task.noConnection')}</option>}
            {connections.map((c) => (
              <option key={c.id} value={c.id}>{c.name} ({c.type})</option>
            ))}
          </select>
        </label>
        {connections.length === 0 && <p className="form-msg">{t('task.needConnection')}</p>}
        <div className="modal-actions">
          <button type="button" className="sm" onClick={onClose}>{t('ss.cancel')}</button>
          <button
            type="button"
            className="sm primary"
            disabled={!canCreate}
            onClick={() => onCreate(name, connectionId)}
          >
            {t('task.create')}
          </button>
        </div>
      </div>
    </div>
  )
}
