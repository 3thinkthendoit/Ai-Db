import { useState } from 'react'
import { Connection } from '../types'
import { useI18n } from '../i18n'
import { TestConnection } from '../lib/api'
import { ConnTypeIcon, connTypeLabel } from '../components/ConnTypeIcon'

interface Props {
  connections: Connection[]
  connectedIds: Set<string>
  activeId: string
  onSelect: (id: string) => void
  onEdit: (c: Connection) => void
  onDelete: (id: string) => void
  onAdd: () => void
}

type TestResult = { ok: boolean; message: string }

function addrLine(c: Connection) {
  const auth = c.user ? `${c.user}@` : ''
  const base = `${auth}${c.host}:${c.port}`
  return c.database ? `${base}/${c.database}` : base
}

export function ConnectionsView(props: Props) {
  const { t } = useI18n()
  const [testing, setTesting] = useState('')
  const [testResult, setTestResult] = useState<Record<string, TestResult>>({})

  async function runTest(c: Connection) {
    setTesting(c.id)
    setTestResult((prev) => {
      const next = { ...prev }
      delete next[c.id]
      return next
    })
    try {
      await TestConnection(c as any)
      setTestResult((prev) => ({ ...prev, [c.id]: { ok: true, message: t('conn.test.ok') } }))
    } catch (e: any) {
      setTestResult((prev) => ({
        ...prev,
        [c.id]: { ok: false, message: `${t('conn.test.fail')}${String(e?.message || e)}` },
      }))
    } finally {
      setTesting('')
    }
  }

  return (
    <>
      <div className="data-table-toolbar">
        <div>
          <h2>{t('conn.page.title')}</h2>
          <p>
            {t('conn.page.hint.a')}
            <b>{t('conn.page.hint.no')}</b>
            {t('conn.page.hint.b')}
          </p>
        </div>
        <button type="button" className="primary" onClick={props.onAdd}>
          {t('conn.add')}
        </button>
      </div>

      <div className="data-table-wrap">
        {props.connections.length === 0 ? (
          <div className="data-table-empty">{t('conn.empty')}</div>
        ) : (
          <table className="data-table">
            <thead>
              <tr>
                <th style={{ width: '22%' }}>{t('conn.col.name')}</th>
                <th style={{ width: '12%' }}>{t('conn.col.type')}</th>
                <th style={{ width: '28%' }}>{t('conn.col.addr')}</th>
                <th style={{ width: '14%' }}>{t('conn.col.status')}</th>
                <th style={{ width: '24%' }}>{t('conn.col.actions')}</th>
              </tr>
            </thead>
            <tbody>
              {props.connections.map((c) => {
                const live = props.connectedIds.has(c.id)
                const result = testResult[c.id]
                const selected = c.id === props.activeId
                return (
                  <tr
                    key={c.id}
                    className={selected ? 'is-selected' : ''}
                    onClick={() => props.onSelect(c.id)}
                    style={{ cursor: 'pointer' }}
                  >
                    <td>
                      <div className="cell-main" style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                        <ConnTypeIcon type={c.type} size={16} />
                        <span>{c.name}</span>
                      </div>
                      {result && (
                        <div className={`cell-sub${result.ok ? '' : ''}`} style={{ color: result.ok ? 'var(--ok)' : 'var(--danger)' }}>
                          {result.message}
                        </div>
                      )}
                    </td>
                    <td>
                      <span className={`tag ${c.type}`}>{connTypeLabel(c.type)}</span>
                    </td>
                    <td>
                      <div className="cell-muted mono">{addrLine(c)}</div>
                      {!c.passwordSet && c.type !== 'redis' && (
                        <div className="cell-sub">
                          <span className="tag danger">{t('conn.noSecret')}</span>
                        </div>
                      )}
                    </td>
                    <td>
                      {live ? (
                        <span className="tag ok">{t('conn.liveTag')}</span>
                      ) : (
                        <span className="pill" style={{ marginLeft: 0 }}>{t('conn.status.offline')}</span>
                      )}
                    </td>
                    <td>
                      <div className="cell-actions" onClick={(e) => e.stopPropagation()}>
                        <button
                          type="button"
                          className="sm"
                          disabled={testing === c.id}
                          onClick={() => void runTest(c)}
                        >
                          {testing === c.id ? t('conn.test.testing') : t('conn.test.run')}
                        </button>
                        <button type="button" className="sm" onClick={() => props.onEdit(c)}>
                          {t('conn.edit')}
                        </button>
                        <button type="button" className="sm danger" onClick={() => props.onDelete(c.id)}>
                          {t('conn.delete')}
                        </button>
                      </div>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        )}
      </div>
    </>
  )
}
