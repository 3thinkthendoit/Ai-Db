import { useCallback, useEffect, useState } from 'react'
import { useTipToast } from '../components/TipToast'
import { useI18n } from '../i18n'
import {
  ApproveMutation,
  ListMutations,
  ListQueryAudit,
  RejectMutation,
  RollbackMutation,
  type MutationView,
  type QueryAuditView,
} from '../lib/api'

type Tab = 'pending' | 'executed' | 'audit'

export function DbaReviewView() {
  const { t } = useI18n()
  const { showTip, tipNode } = useTipToast()
  const [tab, setTab] = useState<Tab>('pending')
  const [mutations, setMutations] = useState<MutationView[]>([])
  const [audit, setAudit] = useState<QueryAuditView[]>([])
  const [busy, setBusy] = useState(false)

  const reload = useCallback(async () => {
    if (tab === 'audit') {
      setAudit(await ListQueryAudit(200))
      return
    }
    if (tab === 'pending') {
      const [pending, executing, failed] = await Promise.all([
        ListMutations('pending', 200),
        ListMutations('executing', 50),
        ListMutations('failed', 50),
      ])
      setMutations([...pending, ...executing, ...failed])
      return
    }
    setMutations(await ListMutations('executed', 200))
  }, [tab])

  useEffect(() => {
    void reload().catch((e) => showTip(String(e?.message || e), 'err'))
  }, [reload, showTip])

  async function act(kind: 'approve' | 'reject' | 'rollback', id: string) {
    setBusy(true)
    try {
      if (kind === 'approve') await ApproveMutation(id, '')
      else if (kind === 'reject') await RejectMutation(id, '')
      else await RollbackMutation(id, '')
      showTip(t('dba.ok'))
      await reload()
    } catch (e: any) {
      showTip(String(e?.message || e), 'err')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="page-pad">
      {tipNode}
      <div className="page-head">
        <h2>{t('dba.title')}</h2>
        <p>{t('dba.hint')}</p>
      </div>
      <div className="seg" style={{ marginBottom: 12 }}>
        <button type="button" className={tab === 'pending' ? 'on' : ''} onClick={() => setTab('pending')}>
          {t('dba.tab.pending')}
        </button>
        <button type="button" className={tab === 'executed' ? 'on' : ''} onClick={() => setTab('executed')}>
          {t('dba.tab.executed')}
        </button>
        <button type="button" className={tab === 'audit' ? 'on' : ''} onClick={() => setTab('audit')}>
          {t('dba.tab.audit')}
        </button>
      </div>

      {tab !== 'audit' && (
        <div className="card-list">
          {mutations.length === 0 && <div className="card muted">{t('dba.empty')}</div>}
          {mutations.map((m) => (
            <div className="card" key={m.id}>
              <div className="card-row">
                <strong>{m.ownerName}</strong>
                <span className="tag">{m.stmtClass}</span>
                <span className="tag">{m.status}</span>
                {m.rollbackable && <span className="tag ok">{t('dba.rollbackable')}</span>}
              </div>
              <pre className="sql-preview">{m.sqlText}</pre>
              <div className="muted" style={{ fontSize: 12 }}>
                {m.createdAt} · conn {m.connectionId.slice(0, 8)}…
              </div>
              <div className="card-actions">
                {tab === 'pending' && m.status === 'pending' && (
                  <>
                    <button type="button" className="primary" disabled={busy} onClick={() => void act('approve', m.id)}>
                      {t('dba.approve')}
                    </button>
                    <button type="button" disabled={busy} onClick={() => void act('reject', m.id)}>
                      {t('dba.reject')}
                    </button>
                  </>
                )}
                {tab === 'executed' && m.status === 'executed' && m.rollbackable && (
                  <button type="button" disabled={busy} onClick={() => void act('rollback', m.id)}>
                    {t('dba.rollback')}
                  </button>
                )}
              </div>
            </div>
          ))}
        </div>
      )}

      {tab === 'audit' && (
        <div className="card-list">
          {audit.length === 0 && <div className="card muted">{t('dba.empty')}</div>}
          {audit.map((a) => (
            <div className="card" key={a.id}>
              <div className="card-row">
                <strong>{a.actorName}</strong>
                <span className="tag">{a.outcome}</span>
                {a.mutating && <span className="tag">{t('dba.mutating')}</span>}
              </div>
              <pre className="sql-preview">{a.sqlText}</pre>
              <div className="muted" style={{ fontSize: 12 }}>
                {a.ts} · {a.durationMs}ms {a.detail ? `· ${a.detail}` : ''}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
