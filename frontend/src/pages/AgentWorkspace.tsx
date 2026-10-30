import { useEffect, useMemo, useRef, useState } from 'react'
import { MarkdownBody } from '../components/MarkdownBody'
import { ChatMessage, Connection, LLMProfileView, StepEvent } from '../types'
import {
  ActivateLLMProfile,
  AgentChat,
  ListLLMProfiles,
  WritesAllowed,
} from '../../wailsjs/go/main/App'
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

/** Empty-session intro: a few starter asks (local copy, no LLM). */
const INTRO_CHIP_KEYS = ['tables', 'describe', 'nl', 'explain', 'slow'] as const

interface Props {
  taskId: string
  connectionId: string
  connected: boolean
  connections: Connection[]
  connectionName: string
  preferredSchema?: string
  history: ChatMessage[]
  /** Bind connection id only — do not open the driver yet. */
  onSelectConnection: (id: string) => void
  /** Open driver; call on Send. Resolves true only when connected. */
  onEnsureConnected: (id: string) => Promise<boolean>
  onGotoQuery: () => void
  onGotoConnections: () => void
  onGotoSettings?: () => void
  onEnsureTask: (connectionId: string) => string
  /** Live task messages (ref-backed); use for send so concurrent AI分析 is visible. */
  onGetHistory: (taskId: string) => ChatMessage[]
  onAppendMessages: (taskId: string, messages: ChatMessage[], connectionId?: string) => void
  onRemoveUserMessage: (taskId: string, content: string) => void
  /** Absolute replace (e.g. clear). Prefer append/remove for in-flight turns. */
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
  const [profiles, setProfiles] = useState<LLMProfileView[]>([])
  const [showConnPicker, setShowConnPicker] = useState(false)
  const [showModelPicker, setShowModelPicker] = useState(false)
  const [modelBusy, setModelBusy] = useState(false)
  const chipScroll = useRef<HTMLDivElement>(null)
  const hostMenuRoot = useRef<HTMLDivElement>(null)
  const modelMenuRoot = useRef<HTMLDivElement>(null)
  const inputEl = useRef<HTMLTextAreaElement>(null)
  const prevTaskId = useRef(props.taskId)
  const sendLock = useRef(false)

  async function reloadModels() {
    try {
      const list = (await ListLLMProfiles()) as LLMProfileView[]
      setProfiles(list || [])
      const active = (list || []).find((p) => p.active)
      setModelLabel(active?.model || active?.name || t('chat.noModel'))
    } catch {
      setProfiles([])
      setModelLabel(t('chat.noModel'))
    }
  }

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
  const introChips = useMemo(
    () => INTRO_CHIP_KEYS.map((key) => ({
      key,
      label: t(`chat.chip.${key}`),
      prompt: t(`chat.prompt.${key}`),
    })),
    [t]
  )

  useEffect(() => {
    void reloadModels()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [t])

  useEffect(() => {
    function onDoc(e: MouseEvent) {
      if (hostMenuRoot.current && !hostMenuRoot.current.contains(e.target as Node)) {
        setShowConnPicker(false)
      }
      if (modelMenuRoot.current && !modelMenuRoot.current.contains(e.target as Node)) {
        setShowModelPicker(false)
      }
    }
    document.addEventListener('mousedown', onDoc)
    return () => document.removeEventListener('mousedown', onDoc)
  }, [])

  async function switchModel(p: LLMProfileView) {
    if (p.active || modelBusy) {
      setShowModelPicker(false)
      return
    }
    setModelBusy(true)
    try {
      await ActivateLLMProfile(p.id)
      await reloadModels()
      setShowModelPicker(false)
    } catch (e: any) {
      setError(String(e?.message || e))
    } finally {
      setModelBusy(false)
    }
  }

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
        const ok = await props.onEnsureConnected(connId)
        if (!ok) {
          // 顶部 banner 已有详细错误；本地再给一句提示。
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

      const priorHistory = props.onGetHistory(taskId)
      props.onAppendMessages(taskId, [{ role: 'user', content: text }], connId)
      setDraft('')
      try {
        const res = await AgentChat({
          connectionId: connId,
          history: priorHistory.map((m) => ({ role: m.role, content: m.content })),
          message: text,
          confirmWrites,
          preferredSchema: props.preferredSchema || '',
        } as any)
        props.onAppendMessages(
          taskId,
          [{ role: 'assistant', content: res.final || '' }],
          connId,
        )
        setSteps((res.steps || []) as StepEvent[])
      } catch (e: any) {
        props.onRemoveUserMessage(taskId, text)
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
            {hasHistory && (
              <div className="agent-header inline agent-header-actions">
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
              </div>
            )}
            {!hasHistory && !busy && (
              <div className="agent-intro" role="status">
                <div className="agent-intro-role">{t('chat.intro.role')}</div>
                <h3 className="agent-intro-title">{t('chat.intro.title')}</h3>
                <p className="agent-intro-lead">{t('chat.intro.lead')}</p>
                <ul className="agent-intro-caps">
                  <li>{t('chat.intro.cap.query')}</li>
                  <li>{t('chat.intro.cap.schema')}</li>
                  <li>{t('chat.intro.cap.sql')}</li>
                  <li>{t('chat.intro.cap.write')}</li>
                </ul>
                {(props.connectionName || props.preferredSchema) && (
                  <p className="agent-intro-ctx muted">
                    {props.preferredSchema
                      ? t('chat.intro.ctx', {
                          conn: props.connectionName || t('agent.noConnection'),
                          schema: props.preferredSchema,
                        })
                      : t('chat.intro.ctxConn', {
                          conn: props.connectionName || t('agent.noConnection'),
                        })}
                  </p>
                )}
                <p className="agent-intro-hint muted">{t('chat.intro.hint')}</p>
                <div className="agent-intro-chips">
                  {introChips.map((c) => (
                    <button
                      key={c.key}
                      type="button"
                      className="chip"
                      onClick={() => applyChip(c.prompt)}
                    >
                      {c.label}
                    </button>
                  ))}
                </div>
              </div>
            )}
            {props.history.map((m, i) => (
              <div key={i} className={`bubble ${m.role}`}>
                <div className="bubble-role">{m.role}</div>
                <MarkdownBody>{m.content}</MarkdownBody>
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
                      <MarkdownBody className="md-body-steps">{s.content}</MarkdownBody>
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
                              props.onSelectConnection(c.id)
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
                <div className="host-menu-wrap model-menu-wrap" ref={modelMenuRoot}>
                  <button
                    type="button"
                    className={`tool-pill model-pill${showModelPicker ? ' open' : ''}`}
                    title={t('chat.model.switch')}
                    disabled={busy || modelBusy}
                    onClick={() => {
                      if (profiles.length === 0) {
                        props.onGotoSettings?.()
                        return
                      }
                      setShowModelPicker((v) => !v)
                    }}
                  >
                    <span className="model-pill-label">{modelLabel || t('chat.noModel')}</span>
                    <span className="caret-down">{showModelPicker ? '▴' : '▾'}</span>
                  </button>
                  {showModelPicker && (
                    <div className="host-menu model-picker-menu" role="listbox">
                      {profiles.length === 0 ? (
                        <button
                          type="button"
                          className="model-menu-item"
                          onClick={() => {
                            setShowModelPicker(false)
                            props.onGotoSettings?.()
                          }}
                        >
                          {t('chat.model.manage')}
                        </button>
                      ) : (
                        <>
                          {profiles.map((p) => (
                            <button
                              key={p.id}
                              type="button"
                              className={`model-menu-item${p.active ? ' on' : ''}`}
                              disabled={modelBusy}
                              onClick={() => void switchModel(p)}
                            >
                              <span className="model-menu-primary">{p.model || p.name}</span>
                              {p.name && p.model && p.name !== p.model && (
                                <span className="model-menu-secondary">{p.name}</span>
                              )}
                            </button>
                          ))}
                          <button
                            type="button"
                            className="model-menu-item model-menu-manage"
                            onClick={() => {
                              setShowModelPicker(false)
                              props.onGotoSettings?.()
                            }}
                          >
                            {t('chat.model.manage')}
                          </button>
                        </>
                      )}
                    </div>
                  )}
                </div>
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
