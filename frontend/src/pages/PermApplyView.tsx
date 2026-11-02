import { useCallback, useEffect, useState } from 'react'
import { useTipToast } from '../components/TipToast'
import { useI18n } from '../i18n'
import {
  ListConnectionCatalog,
  ListPrivilegeRequests,
  SubmitPrivilegeRequest,
  type ConnectionCatalogItem,
  type PrivilegeRequestView,
} from '../lib/api'

export function PermApplyView() {
  const { t } = useI18n()
  const { showTip, tipNode } = useTipToast()
  const [catalog, setCatalog] = useState<ConnectionCatalogItem[]>([])
  const [mine, setMine] = useState<PrivilegeRequestView[]>([])
  const [connId, setConnId] = useState('')
  const [database, setDatabase] = useState('')
  const [tables, setTables] = useState('')
  const [remark, setRemark] = useState('')
  const [busy, setBusy] = useState(false)

  const reload = useCallback(async () => {
    const [c, m] = await Promise.all([
      ListConnectionCatalog(),
      ListPrivilegeRequests('', 100, true),
    ])
    setCatalog(c || [])
    setMine(m || [])
    if (!connId && c?.length) setConnId(c[0].id)
  }, [connId])

  useEffect(() => {
    void reload().catch((e) => showTip(String(e?.message || e), 'err'))
  }, [reload, showTip])

  async function submit() {
    if (!connId || !database.trim() || !tables.trim()) {
      showTip(t('perm.needFields'), 'err')
      return
    }
    setBusy(true)
    try {
      await SubmitPrivilegeRequest(connId, database.trim(), tables.trim(), remark.trim())
      showTip(t('perm.ok'))
      setTables('')
      setRemark('')
      await reload()
    } catch (e: any) {
      showTip(String(e?.message || e), 'err')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="sub-layout">
      {tipNode}
      <aside className="sub-side" aria-label={t('perm.applyTitle')}>
        <div className="sub-side-head">
          <strong>{t('perm.applyTitle')}</strong>
          <span>{t('perm.applyHint')}</span>
        </div>
        <nav className="sub-side-nav">
          <button type="button" className="sub-side-btn on">
            <svg className="sub-side-ico" viewBox="0 0 24 24" aria-hidden>
              <path
                d="M12 3l8 4v5c0 5-3.5 8.5-8 10-4.5-1.5-8-5-8-10V7l8-4z M9 12h6 M12 9v6"
                fill="none"
                stroke="currentColor"
                strokeWidth="1.7"
                strokeLinecap="round"
                strokeLinejoin="round"
              />
            </svg>
            <span>{t('perm.tab.mine')}</span>
          </button>
        </nav>
      </aside>
      <div className="sub-main">
        <div className="data-table-toolbar">
          <div>
            <h2>{t('perm.applyTitle')}</h2>
            <p>{t('perm.applyHint')}</p>
          </div>
          <button type="button" className="primary" disabled={busy} onClick={() => void submit()}>
            {t('perm.submit')}
          </button>
        </div>

        <div className="data-table-wrap" style={{ marginBottom: 20, padding: '12px 16px' }}>
          <div className="perm-apply-form">
            <label>
              {t('perm.conn')}
              <select value={connId} onChange={(e) => setConnId(e.target.value)}>
                {catalog.length === 0 && <option value="">{t('perm.empty')}</option>}
                {catalog.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.name} · {c.type} · {c.host}:{c.port}
                  </option>
                ))}
              </select>
            </label>
            <label>
              {t('perm.db')}
              <input value={database} onChange={(e) => setDatabase(e.target.value)} />
            </label>
            <label>
              {t('perm.table')}
              <textarea
                value={tables}
                onChange={(e) => setTables(e.target.value)}
                rows={2}
                placeholder={t('perm.tablesHint')}
              />
            </label>
            <label>
              {t('dba.submitTicket.remark')}
              <input value={remark} onChange={(e) => setRemark(e.target.value)} />
            </label>
          </div>
        </div>

        <div className="data-table-toolbar">
          <div>
            <h2>{t('perm.tab.mine')}</h2>
          </div>
        </div>
        <div className="data-table-wrap">
          {mine.length === 0 ? (
            <div className="data-table-empty">{t('perm.empty')}</div>
          ) : (
            <table className="data-table">
              <thead>
                <tr>
                  <th style={{ width: '28%' }}>{t('perm.col.target')}</th>
                  <th style={{ width: '16%' }}>{t('perm.col.status')}</th>
                  <th style={{ width: '20%' }}>{t('perm.col.conn')}</th>
                  <th style={{ width: '36%' }}>{t('perm.col.time')}</th>
                </tr>
              </thead>
              <tbody>
                {mine.map((r) => (
                  <tr key={r.id}>
                    <td>
                      <div className="cell-main mono">
                        {r.databaseName}.{r.tableName}
                      </div>
                      {r.remark && <div className="cell-sub">{r.remark}</div>}
                    </td>
                    <td>
                      <span className="tag">{r.status}</span>
                    </td>
                    <td>
                      <div className="cell-muted mono">{r.connectionId.slice(0, 8)}…</div>
                    </td>
                    <td>
                      <div className="cell-muted">{r.createdAt}</div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </div>
    </div>
  )
}
