import { useEffect, useMemo, useRef, useState } from 'react'
import { ChatMessage, Connection, StepEvent } from '../types'
import { AgentChat, GetLLMConfig, WritesAllowed } from '../../wailsjs/go/main/App'
import { useI18n } from '../i18n'

const TABS = [
  { id: 'query', labelKey: 'chat.tab.query' },
  { id: 'schema', labelKey: 'chat.tab.schema' },
  { id: 'perf', labelKey: 'chat.tab.perf' },
] as const

const CHIP_KEYS: Record<string, string[]> = {
  query: ['tables', 'sample', 'count', 'explain_result', 'nl'],
  schema: ['schemas', 'describe', 'indexes', 'relations'],
  perf: ['explain', 'slow', 'index_hint'],
}

interface Props {
  taskId: string
  connectionId: string
  connected: boolean
  connections: Connection[]
  connectionName: string
  taskName: string
  history: ChatMessage[]
  /** Bumps when Query seeds a message; consume via takePendingMessage (once). */
  seedTick?: number
  takePendingMessage?: () => string
  /** Select + Connect; resolves true only when the connection is open. */
  onPickConnection: (id: string) => Promise<boolean>
  onGotoQuery: () => void
  onGotoConnections: () => void
  onEnsureTask: (connectionId: string) => string
  onHistoryChangeFor: (taskId: string, history: ChatMessage[], connectionId?: string) => void
}

export function AgentWorkspace(props: Props) {
  const { t } = useI18n()
  const [draft, setDraft] = useState('')
  const [activeTab, setActiveTab] = useState<string>('query')
  const [steps, setSteps] = useState<StepEvent[]>([])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [modelLabel, setModelLabel] = useState('')
  const [showConnPicker, setShowConnPicker] = useState(false)
  const chipScroll = useRef<HTMLDivElement>(null)
  const hostMenuRoot = useRef<HTMLDivElement>(null)
  const inputEl = useRef<HTMLTextAreaElement>(null)
  const prevTaskId = useRef(props.taskId)
  const sendLock = useRef(false)

  const hasHistory = props.history.length > 0
  // 侧栏点进已有任务 = 对话模式（即使还没有气泡）；欢迎首页仅无任务时出现。
  const inSession = !!props.taskId
  const showHomeHero = !inSession
  const conversing = inSession || hasHistory
  const chips = useMemo(
    () => (CHIP_KEYS[activeTab] || CHIP_KEYS.query).map((key) => ({
      key,
      label: t(`chat.chip.${key}`),
      prompt: t(`chat.prompt.${key}`),
    })),
    [activeTab, t]
  )

  useEffect(() => {
    void GetLLMConfig()
      .then((c: any) => setModelLabel(c?.model ? String(c.model) : t('chat.noModel')))
      .catch(() => setModelLabel(t('chat.noModel')))
  }, [t])

  useEffect(() => {
    function onDoc(e: MouseEvent) {
      if (hostMenuRoot.current && !hostMenuRoot.current.contains(e.target as Node)) {
        setShowConnPicker(false)
      }
    }
    document.addEventListener('mousedown', onDoc)
    return () => document.removeEventListener('mousedown', onDoc)
  }, [])

  // Reset local UI when switching tasks — but not home → first create (same send in flight).
  useEffect(() => {
    const prev = prevTaskId.current
    if (prev === props.taskId) return
    prevTaskId.current = props.taskId
    if (!prev && props.taskId) return
    setSteps([])
    setError('')
    setDraft('')
    setBusy(false)
  }, [props.taskId])

  // Auto-send seeded message from Query → Agent 解释 (ref consume = StrictMode-safe).
  useEffect(() => {
    if (!props.seedTick) return
    const msg = props.takePendingMessage?.()?.trim()
    if (!msg) return
    void send(msg)
    // eslint-disable-next-line react-hooks/exhaustive-deps -- once per seedTick
  }, [props.seedTick])

  function applyChip(prompt: string) {
    setDraft(prompt)
    inputEl.current?.focus()
  }

  function scrollChips(dir: number) {
    chipScroll.current?.scrollBy({ left: dir * 160, behavior: 'smooth' })
  }

  async function send(textRaw?: string) {
    const text = (textRaw ?? draft).trim()
    if (!text || sendLock.current) return
    sendLock.current = true
    setError('')

    try {
      let connId = props.connectionId
      if (!connId) {
        if (props.connections.length === 1) {
          connId = props.connections[0].id
        } else if (props.connections.length === 0) {
          setError(t('chat.pickConn.empty'))
          return
        } else {
          setShowConnPicker(true)
          setError(t('chat.pickConn.hint'))
          return
        }
      }

      setBusy(true)

      const alreadyLive = props.connected && connId === props.connectionId
      if (!alreadyLive) {
        const ok = await props.onPickConnection(connId)
        if (!ok) {
          setError(t('agent.needConnect'))
          return
        }
      }

      let confirmWrites = false
      try {
        const allowed = await WritesAllowed()
        if (allowed) {
          if (!window.confirm(t('agent.writeConfirm'))) return
          confirmWrites = true
        }
      } catch { /* read-only */ }

      const taskId = props.onEnsureTask(connId)
      if (!taskId) {
        setError(t('agent.needConnect'))
        return
      }

      const priorHistory = props.history
      const nextHistory = [...priorHistory, { role: 'user' as const, content: text }]
      props.onHistoryChangeFor(taskId, nextHistory, connId)
      setDraft('')
      try {
        const res = await AgentChat({
          connectionId: connId,
          history: priorHistory.map((m) => ({ role: m.role, content: m.content })),
          message: text,
          confirmWrites,
        } as any)
        props.onHistoryChangeFor(
          taskId,
          [...nextHistory, { role: 'assistant', content: res.final || '' }],
          connId,
        )
        setSteps((res.steps || []) as StepEvent[])
      } catch (e: any) {
        props.onHistoryChangeFor(taskId, priorHistory, connId)
        setError(String(e?.message || e))
      }
    } finally {
      sendLock.current = false
      setBusy(false)
    }
  }

  return (
    <div className={`chat${showHomeHero ? ' empty' : ''}`}>
      {conversing && (
        <div className="log archive">
          <div className="log-col">
            <div className="agent-header inline">
              <div>
                <strong>{props.taskName || t('ss.unnamedTask')}</strong>
                <span className="muted"> · {props.connectionName || t('agent.noConnection')}</span>
              </div>
              {hasHistory && (
                <button
                  type="button"
                  className="sm"
                  onClick={() => {
                    const tid = props.taskId || props.onEnsureTask(props.connectionId || '')
                    if (tid) props.onHistoryChangeFor(tid, [])
                    setSteps([])
                    setError('')
                  }}
                >
                  {t('agent.clear')}
                </button>
              )}
            </div>
            {!hasHistory && !busy && (
              <div className="session-empty muted">{t('chat.sessionEmpty')}</div>
            )}
            {props.history.map((m, i) => (
              <div key={i} className={`bubble ${m.role}`}>
                <div className="bubble-role">{m.role}</div>
                <pre>{m.content}</pre>
              </div>
            ))}
            {busy && <div className="bubble assistant"><em>{t('agent.thinking')}</em></div>}
            {steps.length > 0 && (
              <details className="steps" open={!busy}>
                <summary>{t('agent.steps', { n: steps.length })}</summary>
                <ul>
                  {steps.map((s, i) => (
                    <li key={i}>
                      <code>{s.kind}{s.name ? `:${s.name}` : ''}</code>
                      <pre>{s.content}</pre>
                    </li>
                  ))}
                </ul>
              </details>
            )}
          </div>
        </div>
      )}

      <div className={`home${conversing ? ' docked conversing' : ''}`}>
        {showHomeHero && (
          <>
            <h1 className="home-title">{t('chat.title')}</h1>
            <p className="home-sub">{t('chat.subtitle')}</p>
            <button type="button" className="home-term-link" onClick={props.onGotoQuery}>
              {t('chat.gotoQuery')}
            </button>

            <div className="tabs">
              {TABS.map((tab) => (
                <button
                  key={tab.id}
                  type="button"
                  className={`tab${activeTab === tab.id ? ' on' : ''}`}
                  onClick={() => setActiveTab(tab.id)}
                >
                  {t(tab.labelKey)}
                </button>
              ))}
            </div>

            <div className="chips-row">
              <div className="chips" ref={chipScroll}>
                {chips.map((c) => (
                  <button key={c.key} type="button" className="chip" onClick={() => applyChip(c.prompt)}>
                    {c.label}
                  </button>
                ))}
              </div>
              <button type="button" className="chip-more" title={t('chat.chip.more')} onClick={() => scrollChips(1)}>›</button>
            </div>
          </>
        )}

        <div className="composer-shell">
          <div className="composer-card">
            <textarea
              ref={inputEl}
              className="composer-input"
              rows={3}
              value={draft}
              placeholder={t('chat.placeholder')}
              disabled={busy}
              onChange={(e) => setDraft(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
                  e.preventDefault()
                  void send()
                }
              }}
            />
            <div className="composer-toolbar">
              <div className="composer-left">
                <div className="host-menu-wrap" ref={hostMenuRoot}>
                  <button
                    type="button"
                    className={`tool-pill meta-btn${showConnPicker ? ' open' : ''}${inSession || hasHistory ? ' host-bound' : ''}`}
                    title={props.connectionName || t('chat.workspace')}
                    onClick={() => {
                      // 任务已绑定或已有对话：换库走顶栏下拉，底栏只读展示。
                      if (inSession || hasHistory) return
                      if (props.connections.length === 0) {
                        props.onGotoConnections()
                        return
                      }
                      setShowConnPicker((v) => !v)
                    }}
                  >
                    <svg className="meta-ico" width="14" height="14" viewBox="0 0 24 24" aria-hidden="true">
                      <path d="M4 4h16v6H4z M4 14h16v6H4z M7 7h.01 M7 17h.01" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" />
                    </svg>
                    <span className={`conn-dot${props.connected ? ' on' : ''}`} title={props.connected ? t('app.live') : t('app.offline')} />
                    <span className="meta-label">{props.connectionName || t('chat.workspace')}</span>
                    {!inSession && !hasHistory && <span className="caret-down">{showConnPicker ? '▴' : '▾'}</span>}
                  </button>
                  {showConnPicker && !inSession && !hasHistory && (
                    <div className="host-menu" role="listbox">
                      {props.connections.length === 0 ? (
                        <button type="button" className="model-menu-item" onClick={props.onGotoConnections}>
                          {t('chat.pickConn.add')}
                        </button>
                      ) : (
                        props.connections.map((c) => (
                          <button
                            key={c.id}
                            type="button"
                            className={`model-menu-item${c.id === props.connectionId ? ' on' : ''}`}
                            onClick={() => {
                              void props.onPickConnection(c.id)
                              setShowConnPicker(false)
                            }}
                          >
                            {c.name} · {c.type}
                          </button>
                        ))
                      )}
                    </div>
                  )}
                </div>
              </div>
              <div className="composer-right">
                <span className="model-pill" title={modelLabel}>
                  <span className="model-pill-label">{modelLabel || t('chat.noModel')}</span>
                </span>
                <button
                  type="button"
                  className="send-btn"
                  disabled={busy || !draft.trim()}
                  title={t('chat.send')}
                  onClick={() => void send()}
                >
                  ↑
                </button>
              </div>
            </div>
          </div>
          {error && <div className="error-text pad home-err">{error}</div>}
        </div>
      </div>
    </div>
  )
}
