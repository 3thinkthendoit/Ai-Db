import { useState } from 'react'
import { Connection } from '../types'
import { useI18n } from '../i18n'
import { TestConnection } from '../../wailsjs/go/main/App'

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

function typeLabel(t: string) {
  if (t === 'mysql') return 'MySQL'
  if (t === 'postgres') return 'PostgreSQL'
  if (t === 'redis') return 'Redis'
  return t
}

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
    <div className="panel conn-panel">
      <header className="panel-head panel-head-row">
        <div>
          <h2>{t('conn.page.title')}</h2>
          <p>
            {t('conn.page.hint.a')}
            <b>{t('conn.page.hint.no')}</b>
            {t('conn.page.hint.b')}
          </p>
        </div>
        <button type="button" className="primary" onClick={props.onAdd}>{t('conn.add')}</button>
      </header>

      <div className="conn-list-cards">
        {props.connections.length === 0 && (
          <div className="card muted">{t('conn.empty')}</div>
        )}

        {props.connections.map((c) => {
          const live = props.connectedIds.has(c.id)
          const result = testResult[c.id]
          return (
            <div
              key={c.id}
              className={`card conn-card selectable${c.id === props.activeId ? ' on' : ''}`}
              onClick={() => props.onSelect(c.id)}
            >
              <div className="conn-card-main">
                <div className="conn-card-name">
                  {c.name}
                  <span className={`tag ${c.type}`}>{typeLabel(c.type)}</span>
                  {live && <span className="tag ok">{t('conn.liveTag')}</span>}
                  {!c.passwordSet && c.type !== 'redis' && (
                    <span className="tag danger">{t('conn.noSecret')}</span>
                  )}
                </div>
                <div className="conn-card-addr mono">{addrLine(c)}</div>
                {result && (
                  <div className={`conn-card-test${result.ok ? ' ok' : ' bad'}`}>
                    {result.message}
                  </div>
                )}
              </div>
              <div className="conn-card-actions" onClick={(e) => e.stopPropagation()}>
                <button
                  type="button"
                  className="sm"
                  disabled={testing === c.id}
                  onClick={() => void runTest(c)}
                >
                  {testing === c.id ? t('conn.test.testing') : t('conn.test.run')}
                </button>
                <button type="button" className="sm" onClick={() => props.onEdit(c)}>{t('conn.edit')}</button>
                <button type="button" className="sm danger" onClick={() => props.onDelete(c.id)}>{t('conn.delete')}</button>
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}
