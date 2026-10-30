import { useEffect, useMemo, useRef, useState } from 'react'
import { Connection } from '../types'
import { Task } from '../tasks'
import { useI18n } from '../i18n'

interface Props {
  tasks: Task[]
  connections: Connection[]
  activeTaskId: string
  onOpen: (taskId: string) => void
  onRename: (taskId: string, name: string) => void
  onDelete: (taskId: string) => void
}

function timeAgo(iso: string, t: (k: string, p?: Record<string, string | number>) => string) {
  if (!iso) return ''
  const ts = new Date(iso).getTime()
  if (Number.isNaN(ts)) return ''
  const diff = Date.now() - ts
  if (diff < 60_000) return t('ss.time.justNow')
  if (diff < 3_600_000) return t('ss.time.min', { n: Math.floor(diff / 60_000) })
  if (diff < 86_400_000) return t('ss.time.hour', { n: Math.floor(diff / 3_600_000) })
  if (diff < 30 * 86_400_000) return t('ss.time.day', { n: Math.floor(diff / 86_400_000) })
  const d = new Date(ts)
  return t('ss.time.date', { m: d.getMonth() + 1, d: d.getDate() })
}

export function TasksSidebar({ tasks, connections, activeTaskId, onOpen, onRename, onDelete }: Props) {
  const { t } = useI18n()
  const [collapsed, setCollapsed] = useState(false)
  const [menuFor, setMenuFor] = useState('')
  const [renaming, setRenaming] = useState<{ id: string; name: string } | null>(null)
  const [deleting, setDeleting] = useState<Task | null>(null)
  const renameEl = useRef<HTMLInputElement>(null)

  const connMap = useMemo(() => {
    const m = new Map<string, Connection>()
    connections.forEach((c) => m.set(c.id, c))
    return m
  }, [connections])

  const groups = useMemo(() => {
    const byConn = new Map<string, Task[]>()
    for (const task of tasks) {
      const list = byConn.get(task.connectionId) || []
      list.push(task)
      byConn.set(task.connectionId, list)
    }
    return [...byConn.entries()]
      .map(([connectionId, list]) => ({
        connectionId,
        conn: connMap.get(connectionId),
        list: [...list].sort((a, b) => b.updatedAt.localeCompare(a.updatedAt)),
      }))
      .filter((g) => g.list.length > 0)
  }, [tasks, connMap])

  useEffect(() => {
    function onDoc(e: MouseEvent) {
      const el = e.target as HTMLElement
      if (!el.closest('.ssb-item') && !el.closest('.ssb-menu')) setMenuFor('')
    }
    document.addEventListener('mousedown', onDoc)
    return () => document.removeEventListener('mousedown', onDoc)
  }, [])

  useEffect(() => {
    if (renaming) renameEl.current?.focus()
  }, [renaming])

  function label(task: Task) {
    const base = task.name || t('ss.unnamedTask')
    const n = task.messages.filter((m) => m.role === 'user').length
    return n > 0 ? `${base}${t('ss.turns', { n })}` : base
  }

  return (
    <div className="ssb">
      <button type="button" className="ssb-head" aria-expanded={!collapsed} onClick={() => setCollapsed((v) => !v)}>
        <span>{t('ss.head', { n: tasks.length })}</span>
        <svg className={`ssb-caret${collapsed ? '' : ' open'}`} width="9" height="6" viewBox="0 0 10 6" aria-hidden="true">
          <path d="M1 1l4 4 4-4" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      </button>

      {!collapsed && (
        <>
          {groups.length === 0 && <div className="ssb-empty">{t('ss.empty.none')}</div>}
          {groups.map((g) => (
            <div key={g.connectionId} className="ssb-group">
              <div className="ssb-group-head">
                <span className="ssb-group-name">{g.conn?.name || t('ss.unknownConn')}</span>
                <span className="ssb-group-meta mono">
                  {g.conn ? `${g.conn.type} · ${g.conn.host}` : g.connectionId.slice(0, 8)}
                </span>
                <span className="ssb-group-count">{g.list.length}</span>
              </div>
              {g.list.map((task) => (
                <div
                  key={task.id}
                  className={`ssb-item ssb-item-nested${task.id === activeTaskId ? ' active' : ''}`}
                  onClick={() => onOpen(task.id)}
                >
                  <span className="ssb-name" title={label(task)}>{label(task)}</span>
                  <span className="ssb-time" title={task.updatedAt}>{timeAgo(task.updatedAt, t)}</span>
                  <button
                    type="button"
                    className="ssb-more"
                    title={t('ss.more')}
                    aria-expanded={menuFor === task.id}
                    onClick={(e) => {
                      e.stopPropagation()
                      setMenuFor(menuFor === task.id ? '' : task.id)
                    }}
                  >
                    ⋯
                  </button>
                  {menuFor === task.id && (
                    <div className="ssb-menu" onClick={(e) => e.stopPropagation()}>
                      <button
                        type="button"
                        onClick={() => {
                          setMenuFor('')
                          setRenaming({ id: task.id, name: task.name })
                        }}
                      >
                        {t('ss.rename')}
                      </button>
                      <button
                        type="button"
                        className="ssb-menu-danger"
                        onClick={() => {
                          setMenuFor('')
                          setDeleting(task)
                        }}
                      >
                        {t('ss.delete')}
                      </button>
                    </div>
                  )}
                </div>
              ))}
            </div>
          ))}
        </>
      )}

      {renaming && (
        <div className="overlay">
          <div className="modal" role="dialog" aria-modal="true">
            <div className="modal-head">
              <h3>{t('ss.renameTitle')}</h3>
              <button
                type="button"
                className="modal-close"
                title={t('modal.close')}
                aria-label={t('modal.close')}
                onClick={() => setRenaming(null)}
              >
                ×
              </button>
            </div>
            <input
              ref={renameEl}
              value={renaming.name}
              placeholder={t('ss.namePh')}
              onChange={(e) => setRenaming({ ...renaming, name: e.target.value })}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  onRename(renaming.id, renaming.name.trim())
                  setRenaming(null)
                }
              }}
            />
            <div className="modal-actions">
              <button type="button" className="sm" onClick={() => setRenaming(null)}>{t('ss.cancel')}</button>
              <button
                type="button"
                className="sm primary"
                onClick={() => {
                  onRename(renaming.id, renaming.name.trim())
                  setRenaming(null)
                }}
              >
                {t('ss.confirm')}
              </button>
            </div>
          </div>
        </div>
      )}

      {deleting && (
        <div className="overlay">
          <div className="modal" role="dialog" aria-modal="true">
            <div className="modal-head">
              <h3>{t('ss.deleteTitle')}</h3>
              <button
                type="button"
                className="modal-close"
                title={t('modal.close')}
                aria-label={t('modal.close')}
                onClick={() => setDeleting(null)}
              >
                ×
              </button>
            </div>
            <p>
              {t('ss.delete.p1')}<b>{label(deleting)}</b>{t('ss.delete.p2')}
            </p>
            <div className="modal-actions">
              <button type="button" className="sm" onClick={() => setDeleting(null)}>{t('ss.cancel')}</button>
              <button
                type="button"
                className="sm danger"
                onClick={() => {
                  onDelete(deleting.id)
                  setDeleting(null)
                }}
              >
                {t('ss.deleteBtn')}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
