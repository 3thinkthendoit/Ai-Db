import { useEffect, useState } from 'react'
import { ObjectTree } from '../components/ObjectTree'
import { ResultTable } from '../components/ResultTable'
import { ObjectNode, QueryResult } from '../types'
import { ExecuteQuery, IsMutatingQuery, WritesAllowed } from '../../wailsjs/go/main/App'
import { useI18n } from '../i18n'

interface Props {
  connectionId: string
  connected: boolean
  connType: string
  objects: ObjectNode[]
  onRefreshObjects: () => void
}

export function QueryView({ connectionId, connected, connType, objects, onRefreshObjects }: Props) {
  const { t } = useI18n()
  const [sql, setSql] = useState(connType === 'redis' ? 'PING' : 'SELECT 1')
  const [result, setResult] = useState<QueryResult | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    setSql(connType === 'redis' ? 'PING' : 'SELECT 1')
    setResult(null)
    setError('')
  }, [connectionId, connType])

  async function run() {
    if (!connected || !connectionId) {
      setError(t('query.needConnect'))
      return
    }
    setBusy(true)
    setError('')
    try {
      const mutating = await IsMutatingQuery(connectionId, sql)
      if (mutating) {
        const allowed = await WritesAllowed()
        if (!allowed) {
          setError(t('query.writeBlocked'))
          setBusy(false)
          return
        }
        if (!window.confirm(t('query.writeConfirm'))) {
          setBusy(false)
          return
        }
      }
      const res = await ExecuteQuery(connectionId, sql)
      setResult(res as unknown as QueryResult)
    } catch (e: any) {
      setError(String(e?.message || e))
      setResult(null)
    } finally {
      setBusy(false)
    }
  }

  function onPick(node: ObjectNode, parent?: ObjectNode) {
    if (connType === 'redis') {
      if (node.kind === 'key') setSql(`GET ${node.name}`)
      return
    }
    if (node.kind === 'table') {
      const schema = parent?.name
      if (schema && connType === 'postgres') {
        setSql(`SELECT * FROM "${schema}"."${node.name}" LIMIT 100`)
      } else if (schema && connType === 'mysql') {
        setSql(`SELECT * FROM \`${schema}\`.\`${node.name}\` LIMIT 100`)
      } else {
        setSql(`SELECT * FROM ${node.name} LIMIT 100`)
      }
    }
  }

  return (
    <div className="query-view">
      <aside className="objects-pane">
        <div className="pane-title">
          <span>{t('query.objects')}</span>
          <button type="button" className="sm" disabled={!connected} onClick={onRefreshObjects}>{t('query.refresh')}</button>
        </div>
        <ObjectTree nodes={objects} onPick={onPick} />
      </aside>
      <section className="editor-pane">
        <textarea
          className="sql-editor"
          value={sql}
          onChange={(e) => setSql(e.target.value)}
          spellCheck={false}
          placeholder={connType === 'redis' ? t('query.placeholder.redis') : t('query.placeholder.sql')}
        />
        <div className="editor-toolbar">
          <button type="button" className="primary" disabled={busy || !connected} onClick={run}>
            {busy ? t('query.running') : t('query.run')}
          </button>
          {error && <span className="error-text">{error}</span>}
        </div>
        <ResultTable result={result} />
      </section>
    </div>
  )
}
