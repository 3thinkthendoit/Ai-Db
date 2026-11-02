import { useCallback, useEffect, useState } from 'react'
import { useTipToast } from '../components/TipToast'
import { useI18n } from '../i18n'
import {
  ApprovePrivilegeRequest,
  ListGrants,
  ListPrivilegeRequests,
  RejectPrivilegeRequest,
  RevokeGrant,
  type GrantView,
  type PrivilegeRequestView,
} from '../lib/api'

type Tab = 'pending' | 'grants'

export function PermAdminView() {
  const { t } = useI18n()
  const { showTip, tipNode } = useTipToast()
  const [tab, setTab] = useState<Tab>('pending')
  const [pending, setPending] = useState<PrivilegeRequestView[]>([])
  const [grants, setGrants] = useState<GrantView[]>([])
  const [busy, setBusy] = useState(false)

  const reload = useCallback(async () => {
    if (tab === 'pending') {
      setPending(await ListPrivilegeRequests('pending', 200))
      return
    }
    setGrants(await ListGrants(200))
  }, [tab])

  useEffect(() => {
    void reload().catch((e) => showTip(String(e?.message || e), 'err'))
  }, [reload, showTip])

  async function act(kind: 'approve' | 'reject' | 'revoke', id: string) {
    setBusy(true)
    try {
      if (kind === 'approve') await ApprovePrivilegeRequest(id, '')
      else if (kind === 'reject') await RejectPrivilegeRequest(id, '')
      else await RevokeGrant(id)
      showTip(t('perm.ok'))
      await reload()
    } catch (e: any) {
      showTip(String(e?.message || e), 'err')
    } finally {
      setBusy(false)
    }
  }

  const items: { key: Tab; label: string; icon: string }[] = [
    { key: 'pending', label: t('perm.tab.pending'), icon: 'M4 6h16 M4 12h10 M4 18h14' },
    { key: 'grants', label: t('perm.tab.grants'), icon: 'M12 3l8 4v5c0 5-3.5 8.5-8 10-4.5-1.5-8-5-8-10V7l8-4z' },
  ]

  const tabLabel = items.find((i) => i.key === tab)?.label || t('perm.title')

  return (
    <div className="sub-layout">
      {tipNode}
      <aside className="sub-side" aria-label={t('perm.title')}>
        <div className="sub-side-head">
          <strong>{t('perm.title')}</strong>
          <span>{t('perm.hint')}</span>
        </div>
        <nav className="sub-side-nav">
          {items.map((item) => (
            <button
              key={item.key}
              type="button"
              className={`sub-side-btn${tab === item.key ? ' on' : ''}`}
              onClick={() => setTab(item.key)}
            >
              <svg className="sub-side-ico" viewBox="0 0 24 24" aria-hidden>
                <path d={item.icon} fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" />
              </svg>
              <span>{item.label}</span>
            </button>
          ))}
        </nav>
      </aside>
      <div className="sub-main">
        <div className="data-table-toolbar">
          <div>
            <h2>{tabLabel}</h2>
            <p>{t('perm.hint')}</p>
          </div>
        </div>

        {tab === 'pending' && (
          <div className="data-table-wrap">
            {pending.length === 0 ? (
              <div className="data-table-empty">{t('perm.empty')}</div>
            ) : (
              <table className="data-table">
                <thead>
                  <tr>
                    <th style={{ width: '16%' }}>{t('perm.col.applicant')}</th>
                    <th style={{ width: '22%' }}>{t('perm.col.target')}</th>
                    <th style={{ width: '16%' }}>{t('perm.col.conn')}</th>
                    <th style={{ width: '18%' }}>{t('perm.col.time')}</th>
                    <th style={{ width: '28%' }}>{t('perm.col.actions')}</th>
                  </tr>
                </thead>
                <tbody>
                  {pending.map((r) => (
                    <tr key={r.id}>
                      <td>
                        <div className="cell-main">{r.applicantName}</div>
                        {r.remark && <div className="cell-sub">{r.remark}</div>}
                      </td>
                      <td>
                        <div className="cell-muted mono">
                          {r.databaseName}.{r.tableName}
                        </div>
                      </td>
                      <td>
                        <div className="cell-muted mono">{r.connectionId.slice(0, 8)}…</div>
                      </td>
                      <td>
                        <div className="cell-muted">{r.createdAt}</div>
                      </td>
                      <td>
                        <div className="cell-actions">
                          <button type="button" className="sm primary" disabled={busy} onClick={() => void act('approve', r.id)}>
                            {t('perm.approve')}
                          </button>
                          <button type="button" className="sm" disabled={busy} onClick={() => void act('reject', r.id)}>
                            {t('perm.reject')}
                          </button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        )}

        {tab === 'grants' && (
          <div className="data-table-wrap">
            {grants.length === 0 ? (
              <div className="data-table-empty">{t('perm.empty')}</div>
            ) : (
              <table className="data-table">
                <thead>
                  <tr>
                    <th style={{ width: '16%' }}>{t('perm.col.grantee')}</th>
                    <th style={{ width: '22%' }}>{t('perm.col.target')}</th>
                    <th style={{ width: '14%' }}>{t('perm.col.by')}</th>
                    <th style={{ width: '14%' }}>{t('perm.col.conn')}</th>
                    <th style={{ width: '16%' }}>{t('perm.col.time')}</th>
                    <th style={{ width: '18%' }}>{t('perm.col.actions')}</th>
                  </tr>
                </thead>
                <tbody>
                  {grants.map((g) => (
                    <tr key={g.id}>
                      <td>
                        <div className="cell-main">{g.granteeName}</div>
                      </td>
                      <td>
                        <div className="cell-muted mono">
                          {g.databaseName}.{g.tableName}
                        </div>
                      </td>
                      <td>
                        <div className="cell-muted">{g.grantedByName}</div>
                      </td>
                      <td>
                        <div className="cell-muted mono">{g.connectionId.slice(0, 8)}…</div>
                      </td>
                      <td>
                        <div className="cell-muted">{g.createdAt}</div>
                      </td>
                      <td>
                        <div className="cell-actions">
                          <button type="button" className="sm danger" disabled={busy} onClick={() => void act('revoke', g.id)}>
                            {t('perm.revoke')}
                          </button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        )}
      </div>
    </div>
  )
}
