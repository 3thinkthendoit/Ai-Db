import { useDeferredValue, useEffect, useMemo, useRef, useState } from 'react'
import logoSrc from '../assets/logo.svg'
import { MarkdownBody } from '../components/MarkdownBody'
import { historyBeforeIndex, isRetryableUserTurn } from '../lib/chatHistory'
import { ChatMessage, Connection, LLMProfileView, StepEvent } from '../types'
import {
  ActivateLLMProfile,
  AgentChat,
  ListLLMProfiles,
  WritesAllowed,
} from '../../wailsjs/go/main/App'
import { useI18n } from '../i18n'
import type { Locale } from '../locales'

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

/** Rough token estimate for context-ring UI (CJK denser than Latin). */
function estimateTokens(text: string): number {
  let cjk = 0
  let other = 0
  for (const ch of text) {
    if (/[\u4e00-\u9fff\u3040-\u30ff\uac00-\ud7af]/.test(ch)) cjk++
    else other++
  }
  return Math.max(0, Math.ceil(cjk / 1.5 + other / 4))
}

/** Default context window when profile has no explicit limit. */
function contextWindowForModel(model: string): number {
  const m = model.toLowerCase()
  if (/gemini-2|claude-3|gpt-4\.1|o3|o4/.test(m)) return 200_000
  if (/32k/.test(m)) return 32_000
  if (/8k/.test(m)) return 8_192
  return 128_000
}

function formatTokenCount(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 10_000) return `${Math.round(n / 1000)}k`
  if (n >= 1000) return `${(n / 1000).toFixed(1)}k`
  return String(n)
}

function pad2(n: number) {
  return n < 10 ? `0${n}` : String(n)
}

function formatMsgTime(ts: number | undefined, locale: Locale): string {
  if (!ts) return ''
  const d = new Date(ts)
  if (Number.isNaN(d.getTime())) return ''
  if (locale === 'zh') {
    return `${d.getMonth() + 1}月${d.getDate()}日 ${pad2(d.getHours())}:${pad2(d.getMinutes())}`
  }
  return d.toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

function profileAvatarLetter(p: { name?: string; model?: string }): string {
  const raw = (p.name || p.model || '?').trim()
  const ch = raw.charAt(0)
  return ch ? ch.toUpperCase() : '?'
}

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
  const { t, locale } = useI18n()
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

  const activeProfile = useMemo(() => profiles.find((p) => p.active) || null, [profiles])
  const modelDisplay = useMemo(() => {
    if (!activeProfile) return modelLabel || t('chat.noModel')
    const name = (activeProfile.name || '').trim()
    const model = (activeProfile.model || '').trim()
    if (name && model && name !== model) return `${name} · ${model}`
    return model || name || t('chat.noModel')
  }, [activeProfile, modelLabel, t])
  const modelAvatar = useMemo(() => {
    const raw = (activeProfile?.name || activeProfile?.model || modelLabel || '?').trim()
    const ch = raw.charAt(0)
    return ch ? ch.toUpperCase() : '?'
  }, [activeProfile, modelLabel])
  const deferredDraft = useDeferredValue(draft)
  const deferredHistory = useDeferredValue(props.history)
  const contextUsage = useMemo(() => {
    const histText = deferredHistory.map((m) => m.content || '').join('\n')
    const used = estimateTokens(histText) + estimateTokens(deferredDraft)
    const windowSize = contextWindowForModel(activeProfile?.model || '')
    const pct = windowSize > 0 ? Math.min(100, (used / windowSize) * 100) : 0
    return { used, windowSize, pct }
  }, [deferredHistory, deferredDraft, activeProfile])

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
      const now = Date.now()
      props.onAppendMessages(taskId, [{ role: 'user', content: text, createdAt: now }], connId)
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
          [{ role: 'assistant', content: res.final || '', createdAt: Date.now() }],
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

  async function copyMessage(content: string) {
    try {
      await navigator.clipboard.writeText(content)
    } catch {
      /* ignore */
    }
  }

  /** Edit: drop this user turn and everything after, put text back in the composer. */
  function editUserMessage(index: number) {
    if (!isRetryableUserTurn(props.history, index) || busy) return
    const m = props.history[index]
    const taskId = props.taskId
    if (taskId) {
      props.onHistoryChangeFor(taskId, historyBeforeIndex(props.history, index))
      setSteps([])
      setError('')
    }
    setDraft(m.content)
    inputEl.current?.focus()
  }

  /** Retry: truncate to before this turn, then re-send (no duplicate user bubble). */
  function retryUserMessage(index: number) {
    if (!isRetryableUserTurn(props.history, index) || busy || sendLock.current) return
    const m = props.history[index]
    const taskId = props.taskId
    if (!taskId) return
    props.onHistoryChangeFor(taskId, historyBeforeIndex(props.history, index))
    setSteps([])
    setError('')
    void send(m.content)
  }

  return (
    <div className={`chat${showHomeHero ? ' empty' : ''}`}>
      {conversing && (
        <div className="log archive">
          <div className="log-col">
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
            {props.history.map((m, i) => {
              if (m.role === 'user') {
                const timeLabel = formatMsgTime(m.createdAt, locale)
                return (
                  <div key={i} className="msg user">
                    <div className="msg-user-bubble">
                      <MarkdownBody>{m.content}</MarkdownBody>
                    </div>
                    <div className="msg-meta">
                      {timeLabel && <span className="msg-time">{timeLabel}</span>}
                      <div className="msg-actions">
                        <button type="button" className="msg-action" title={t('chat.msg.copy')} onClick={() => void copyMessage(m.content)}>
                          <svg width="14" height="14" viewBox="0 0 24 24" aria-hidden="true">
                            <rect x="9" y="9" width="11" height="11" rx="2" fill="none" stroke="currentColor" strokeWidth="1.7" />
                            <path d="M6 15H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h8a2 2 0 0 1 2 2v1" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" />
                          </svg>
                        </button>
                        <button type="button" className="msg-action" title={t('chat.msg.retry')} disabled={busy} onClick={() => retryUserMessage(i)}>
                          <svg width="14" height="14" viewBox="0 0 24 24" aria-hidden="true">
                            <path d="M21 12a9 9 0 1 1-2.6-6.3M21 3v6h-6" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" />
                          </svg>
                        </button>
                        <button type="button" className="msg-action" title={t('chat.msg.edit')} disabled={busy} onClick={() => editUserMessage(i)}>
                          <svg width="14" height="14" viewBox="0 0 24 24" aria-hidden="true">
                            <path d="M12 20h9M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4 12.5-12.5z" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" />
                          </svg>
                        </button>
                      </div>
                    </div>
                  </div>
                )
              }
              if (m.role !== 'assistant') return null
              const outTokens = estimateTokens(m.content || '')
              return (
                <div key={i} className="msg assistant">
                  <div className="msg-brand">
                    <img className="msg-brand-logo" src={logoSrc} alt="" />
                    <span className="msg-brand-name">{t('chat.title')}</span>
                  </div>
                  <div className="msg-assistant-body">
                    <MarkdownBody>{m.content}</MarkdownBody>
                  </div>
                  {outTokens > 0 && (
                    <div className="msg-token-badge">{t('chat.msg.outTokens', { n: outTokens })}</div>
                  )}
                </div>
              )
            })}
            {busy && (
              <div className="msg assistant">
                <div className="msg-brand">
                  <img className="msg-brand-logo" src={logoSrc} alt="" />
                  <span className="msg-brand-name">{t('chat.title')}</span>
                </div>
                <div className="msg-assistant-body muted"><em>{t('agent.thinking')}</em></div>
              </div>
            )}
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
                <span
                  className="ctx-usage"
                  title={t('chat.model.contextTip', {
                    pct: Math.round(contextUsage.pct),
                    used: formatTokenCount(contextUsage.used),
                    max: formatTokenCount(contextUsage.windowSize),
                  })}
                >
                  <span className="ctx-ring" aria-hidden>
                    <svg viewBox="0 0 20 20" width="16" height="16">
                      <circle className="ctx-ring-track" cx="10" cy="10" r="7.5" />
                      <circle
                        className="ctx-ring-fill"
                        cx="10"
                        cy="10"
                        r="7.5"
                        pathLength="100"
                        strokeDasharray={`${Math.max(contextUsage.pct, contextUsage.pct > 0 ? 2 : 0)} 100`}
                      />
                    </svg>
                  </span>
                </span>
                <div className="host-menu-wrap model-menu-wrap" ref={modelMenuRoot}>
                  <button
                    type="button"
                    className={`model-select${showModelPicker ? ' open' : ''}`}
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
                    <span className="model-avatar" aria-hidden>{modelAvatar}</span>
                    <span className="model-select-label">{modelDisplay}</span>
                    <span className="caret-down">{showModelPicker ? '▴' : '▾'}</span>
                  </button>
                  {showModelPicker && (
                    <div className="host-menu model-picker-menu" role="listbox">
                      {profiles.length === 0 ? (
                        <button
                          type="button"
                          className="model-menu-manage"
                          onClick={() => {
                            setShowModelPicker(false)
                            props.onGotoSettings?.()
                          }}
                        >
                          <svg width="14" height="14" viewBox="0 0 24 24" aria-hidden="true">
                            <path d="M12 20h9M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4L16.5 3.5z" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" />
                          </svg>
                          {t('chat.model.configure')}
                        </button>
                      ) : (
                        <>
                          <div className="model-picker-list">
                            {profiles.map((p) => {
                              const title = (p.name || p.model || t('chat.noModel')).trim()
                              const sub = p.name && p.model && p.name !== p.model ? p.model : ''
                              return (
                                <button
                                  key={p.id}
                                  type="button"
                                  className={`model-menu-row${p.active ? ' on' : ''}`}
                                  disabled={modelBusy}
                                  onClick={() => void switchModel(p)}
                                >
                                  <span className={`model-menu-avatar${p.active ? ' accent' : ''}`} aria-hidden>
                                    {profileAvatarLetter(p)}
                                  </span>
                                  <span className="model-menu-text">
                                    <span className="model-menu-title">{title}</span>
                                    {sub ? <span className="model-menu-sub">{sub}</span> : null}
                                  </span>
                                  {p.active && (
                                    <svg className="model-menu-check" width="16" height="16" viewBox="0 0 24 24" aria-hidden="true">
                                      <path d="M5 13l4 4L19 7" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" />
                                    </svg>
                                  )}
                                </button>
                              )
                            })}
                          </div>
                          <button
                            type="button"
                            className="model-menu-manage"
                            onClick={() => {
                              setShowModelPicker(false)
                              props.onGotoSettings?.()
                            }}
                          >
                            <svg width="14" height="14" viewBox="0 0 24 24" aria-hidden="true">
                              <path d="M12 20h9M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4L16.5 3.5z" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" />
                            </svg>
                            {t('chat.model.configure')}
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
                  <svg width="16" height="16" viewBox="0 0 24 24" aria-hidden="true">
                    <path
                      d="M12 19V5M5 12l7-7 7 7"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="2.4"
                      strokeLinecap="round"
                      strokeLinejoin="round"
                    />
                  </svg>
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
