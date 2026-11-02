import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTipToast } from '../components/TipToast'
import { TicketDetailModal, type TicketDetailAction } from '../components/TicketDetailModal'
import { useI18n } from '../i18n'
import {
  ApproveMutation,
  ExecuteMutation,
  ListMutations,
  ListQueryAudit,
  RejectMutation,
  RollbackMutation,
  type MutationView,
  type QueryAuditView,
} from '../lib/api'

type Tab = 'pending' | 'approved' | 'executed' | 'mine' | 'audit'

interface Props {
  /** Session role — non-reviewers only see own tickets / own audit. */
  viewerRole?: string
}

function clipText(text: string, n = 48) {
  const s = (text || '').replace(/\s+/g, ' ').trim()
  if (!s) return '—'
  const chars = Array.from(s)
  if (chars.length <= n) return s
  return `${chars.slice(0, n).join('')}…`
}

function formatTime(iso: string) {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

function nodeLabel(m: MutationView) {
  const name = (m.connectionName || '').trim() || m.connectionId.slice(0, 8)
  const host = (m.connectionHost || '').trim()
  const port = Number(m.connectionPort) > 0 ? Number(m.connectionPort) : 0
  const addr = host ? (port ? `${host}:${port}` : host) : ''
  const schema = (m.activeSchema || '').trim()
  const type = (m.connType || '').trim()
  const parts = [name]
  if (addr) parts.push(addr)
  if (schema) parts.push(schema)
  const head = parts.join(' / ')
  return type ? `${head} · ${type}` : head
}

function statusKey(status: string) {
  const s = (status || '').trim().toLowerCase()
  if (!s) return ''
  return `dba.status.${s}`
}

export function DbaReviewView({ viewerRole = 'user' }: Props) {
  const { t } = useI18n()
  const { showTip, tipNode } = useTipToast()
  const isReviewer = viewerRole === 'dba' || viewerRole === 'admin'
  const [tab, setTab] = useState<Tab>(() => (isReviewer ? 'pending' : 'mine'))
  const [mutations, setMutations] = useState<MutationView[]>([])
  const [audit, setAudit] = useState<QueryAuditView[]>([])
  const [busy, setBusy] = useState(false)
  const [detail, setDetail] = useState<MutationView | null>(null)
  const [detailErr, setDetailErr] = useState('')

  const items = useMemo(() => {
    const all: { key: Tab; label: string; icon: string }[] = []
    if (isReviewer) {
      all.push(
        { key: 'pending', label: t('dba.tab.pending'), icon: 'M4 6h16 M4 12h10 M4 18h14' },
        { key: 'approved', label: t('dba.tab.approved'), icon: 'M5 12l4 4L19 6' },
        { key: 'executed', label: t('dba.tab.executed'), icon: 'M12 3v18 M5 12h14' },
      )
    }
    all.push(
      { key: 'mine', label: t('dba.tab.mine'), icon: 'M12 12a4 4 0 1 0 0-8 4 4 0 0 0 0 8z M4 20c1.5-3.5 4.5-5 8-5s6.5 1.5 8 5' },
      { key: 'audit', label: t('dba.tab.audit'), icon: 'M9 5h11 M9 12h11 M9 19h11 M4 5h.01 M4 12h.01 M4 19h.01' },
    )
    return all
  }, [isReviewer, t])

  useEffect(() => {
    if (!isReviewer && (tab === 'pending' || tab === 'approved' || tab === 'executed')) {
      setTab('mine')
    }
  }, [isReviewer, tab])

  const reload = useCallback(async () => {
    if (tab === 'audit') {
      setAudit(await ListQueryAudit(200))
      return
    }
    if (tab === 'pending') {
      setMutations(await ListMutations('pending', 200))
      return
    }
    if (tab === 'approved') {
      const [approved, executing] = await Promise.all([
        ListMutations('approved', 200),
        ListMutations('executing', 50),
      ])
      setMutations([...approved, ...executing])
      return
    }
    if (tab === 'mine') {
      setMutations(await ListMutations('', 200, true))
      return
    }
    setMutations(await ListMutations('executed', 200))
  }, [tab])

  useEffect(() => {
    void reload().catch((e) => showTip(String(e?.message || e), 'err'))
  }, [reload, showTip])

  function statusLabel(status: string) {
    const key = statusKey(status)
    if (!key) return status || '—'
    const label = t(key)
    return label === key ? status : label
  }

  async function handleDetailAction(kind: TicketDetailAction, note: string) {
    if (!detail) return
    const id = detail.id
    setBusy(true)
    setDetailErr('')
    try {
      if (kind === 'plan') {
        await ApproveMutation(id, note)
      } else if (kind === 'execute') {
        if (detail.status === 'pending') {
          await ApproveMutation(id, note)
        }
        await ExecuteMutation(id, note)
      } else if (kind === 'reject') {
        await RejectMutation(id, note)
      } else {
        await RollbackMutation(id, note)
      }
      setDetail(null)
      showTip(t('dba.ok'))
      await reload()
    } catch (e: any) {
      setDetailErr(String(e?.message || e))
    } finally {
      setBusy(false)
    }
  }

  const tabLabel = items.find((i) => i.key === tab)?.label || t('dba.title')

  return (
    <div className="sub-layout">
      {tipNode}
      <aside className="sub-side" aria-label={t('dba.title')}>
        <div className="sub-side-head">
          <strong>{t('dba.title')}</strong>
          <span>{t('dba.hint')}</span>
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
            <p>{t('dba.hint')}</p>
          </div>
        </div>

        {tab !== 'audit' && (
          <div className="data-table-wrap">
            {mutations.length === 0 ? (
              <div className="data-table-empty">{t('dba.empty')}</div>
            ) : (
              <table className="data-table">
                <thead>
                  <tr>
                    <th style={{ width: '11%' }}>{t('dba.col.title')}</th>
                    <th style={{ width: '12%' }}>{t('dba.col.content')}</th>
                    <th style={{ width: '9%' }}>{t('dba.col.owner')}</th>
                    <th style={{ width: '16%' }}>{t('dba.col.node')}</th>
                    <th style={{ width: '7%' }}>{t('dba.col.type')}</th>
                    <th style={{ width: '8%' }}>{t('dba.col.status')}</th>
                    <th style={{ width: '16%' }}>{t('dba.col.sql')}</th>
                    <th style={{ width: '12%' }}>{t('dba.col.time')}</th>
                    <th style={{ width: '9%' }}>{t('dba.col.actions')}</th>
                  </tr>
                </thead>
                <tbody>
                  {mutations.map((m) => {
                    const title = m.title || '—'
                    const content = m.remark || '—'
                    const sql = m.sqlText || ''
                    const node = nodeLabel(m)
                    return (
                      <tr key={m.id}>
                        <td>
                          <div className="cell-main cell-ellipsis" title={title}>
                            {clipText(title, 24)}
                          </div>
                        </td>
                        <td>
                          <div className="cell-muted cell-ellipsis" title={content}>
                            {clipText(content, 28)}
                          </div>
                        </td>
                        <td>
                          <div className="cell-muted cell-ellipsis" title={m.ownerName}>
                            {clipText(m.ownerName || '—', 16)}
                          </div>
                        </td>
                        <td>
                          <div className="cell-muted cell-ellipsis" title={node}>
                            {clipText(node, 40)}
                          </div>
                        </td>
                        <td>
                          <span className="tag">{m.stmtClass || '—'}</span>
                        </td>
                        <td>
                          <span
                            className={`tag${
                              m.status === 'executed'
                                ? ' ok'
                                : m.status === 'rejected' || m.status === 'failed'
                                  ? ' danger'
                                  : ''
                            }`}
                          >
                            {statusLabel(m.status)}
                          </span>
                        </td>
                        <td>
                          <div className="cell-muted mono cell-ellipsis" title={sql}>
                            {clipText(sql, 40)}
                          </div>
                        </td>
                        <td>
                          <div className="cell-muted cell-ellipsis" title={m.createdAt}>
                            {formatTime(m.createdAt)}
                          </div>
                        </td>
                        <td>
                          <div className="cell-actions">
                            <button
                              type="button"
                              className="sm"
                              disabled={busy}
                              onClick={() => {
                                setDetailErr('')
                                setDetail(m)
                              }}
                            >
                              {t('dba.view')}
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
        )}

        {tab === 'audit' && (
          <div className="data-table-wrap">
            {audit.length === 0 ? (
              <div className="data-table-empty">{t('dba.empty')}</div>
            ) : (
              <table className="data-table">
                <thead>
                  <tr>
                    <th style={{ width: '14%' }}>{t('dba.col.actor')}</th>
                    <th style={{ width: '12%' }}>{t('dba.col.outcome')}</th>
                    <th style={{ width: '36%' }}>{t('dba.col.sql')}</th>
                    <th style={{ width: '12%' }}>{t('dba.col.duration')}</th>
                    <th style={{ width: '26%' }}>{t('dba.col.time')}</th>
                  </tr>
                </thead>
                <tbody>
                  {audit.map((a) => (
                    <tr key={a.id}>
                      <td>
                        <div className="cell-main cell-ellipsis" title={a.actorName}>
                          {clipText(a.actorName, 24)}
                        </div>
                        {a.mutating && <div className="cell-sub">{t('dba.mutating')}</div>}
                      </td>
                      <td>
                        <span className="tag">{a.outcome}</span>
                      </td>
                      <td>
                        <div className="cell-muted mono cell-ellipsis" title={a.sqlText}>
                          {clipText(a.sqlText, 64)}
                        </div>
                        {a.detail && (
                          <div className="cell-sub cell-ellipsis" title={a.detail}>
                            {clipText(a.detail, 40)}
                          </div>
                        )}
                      </td>
                      <td>
                        <div className="cell-muted">{a.durationMs}ms</div>
                      </td>
                      <td>
                        <div className="cell-muted cell-ellipsis" title={a.ts}>
                          {formatTime(a.ts)}
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

      {detail && (
        <TicketDetailModal
          ticket={detail}
          isReviewer={isReviewer}
          busy={busy}
          error={detailErr}
          nodeLabel={nodeLabel(detail)}
          statusLabel={statusLabel(detail.status)}
          formatTime={formatTime}
          onClose={() => !busy && setDetail(null)}
          onAction={handleDetailAction}
        />
      )}
    </div>
  )
}
