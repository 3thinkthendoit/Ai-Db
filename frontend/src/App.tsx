import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import './App.css'
import logoSrc from './assets/logo.svg'
import { ConnectionForm } from './components/ConnectionForm'
import { NewTaskModal } from './components/NewTaskModal'
import { PickConnectionModal } from './components/PickConnectionModal'
import { SettingsMenu } from './components/SettingsMenu'
import { TasksSidebar } from './components/TasksSidebar'
import { AgentWorkspace } from './pages/AgentWorkspace'
import { ConnectionsView } from './pages/ConnectionsView'
import { PrefsView } from './pages/PrefsView'
import { QueryView } from './pages/QueryView'
import { SettingsView } from './pages/SettingsView'
import { AppMode, ChatMessage, Connection, ObjectNode, emptyConnection } from './types'
import { useI18n } from './i18n'
import {
  createTask,
  loadActiveTaskId,
  loadTasks,
  normalizeTasks,
  saveActiveTaskId,
  saveTasks,
  touchTask,
  Task,
} from './tasks'
import {
  AgentChat,
  Connect,
  DeleteConnection,
  ListConnections,
  ListObjects,
  SetActiveSchema,
  TestConnection,
  UpsertConnection,
  WritesAllowed,
} from '../wailsjs/go/main/App'
import { WindowSetTitle } from '../wailsjs/runtime/runtime'

const NEW_TASK_ICON =
  'M4 6h11a3 3 0 0 1 3 3v5a3 3 0 0 1-3 3H9l-4 3v-3H7a3 3 0 0 1-3-3V9a3 3 0 0 1 3-3z M19 4v6 M16 7h6'

function App() {
  const { t } = useI18n()
  const [mode, setMode] = useState<AppMode>('work')
  const [connections, setConnections] = useState<Connection[]>([])
  const [activeConnId, setActiveConnId] = useState('')
  const [connectedIds, setConnectedIds] = useState<Set<string>>(new Set())
  const [objects, setObjects] = useState<ObjectNode[]>([])
  const [editing, setEditing] = useState<Connection | null | undefined>(undefined)
  const [banner, setBanner] = useState('')
  const [tasks, setTasks] = useState<Task[]>(() => loadTasks())
  const [activeTaskId, setActiveTaskId] = useState(() => {
    const id = loadActiveTaskId()
    if (!id) return ''
    const list = loadTasks()
    if (list.some((t) => t.id === id)) return id
    saveActiveTaskId('')
    return ''
  })
  const [showNewTask, setShowNewTask] = useState(false)
  const [showPickConnForQuery, setShowPickConnForQuery] = useState(false)
  const [workSurface, setWorkSurface] = useState<'agent' | 'sql'>('agent')
  const [activeSchema, setActiveSchema] = useState('')
  /** Source of truth for in-flight history reads; avoid `ref = state` each render (clobbers optimistic writes). */
  const tasksRef = useRef(tasks)
  const [schemaMenuOpen, setSchemaMenuOpen] = useState(false)
  const schemaMenuRoot = useRef<HTMLDivElement>(null)

  const persistTasks = useCallback((next: Task[]) => {
    tasksRef.current = next
    setTasks(next)
    saveTasks(next)
  }, [])

  const selectTask = useCallback((id: string) => {
    setActiveTaskId(id)
    saveActiveTaskId(id)
    const task = tasks.find((x) => x.id === id)
    if (task) setActiveConnId(task.connectionId)
    setMode('work')
    setWorkSurface('agent')
  }, [tasks])

  function openQuerySurface(connectionId?: string) {
    setMode('work')
    setWorkSurface('sql')
    if (connectionId) {
      setActiveConnId(connectionId)
      void handleConnect(connectionId)
    }
  }

  /** Home / Settings → Query: bind a connection first when no task is open. */
  function requestOpenQuery() {
    if (activeTask) {
      openQuerySurface(activeTask.connectionId)
      return
    }
    if (connections.length === 0) {
      setMode('connections')
      return
    }
    setShowPickConnForQuery(true)
  }

  function openWorkPage(page: AppMode) {
    if (page === 'query') {
      requestOpenQuery()
      return
    }
    setMode(page)
  }

  const menuActivePage: AppMode = mode === 'work' && workSurface === 'sql' ? 'query' : mode

  const reload = useCallback(async () => {
    const list = (await ListConnections()) as unknown as Connection[]
    setConnections(list || [])
    if (!activeConnId && list?.length) setActiveConnId(list[0].id)
  }, [activeConnId])

  useEffect(() => {
    void reload().catch((e) => setBanner(String(e)))
  }, [reload])

  useEffect(() => {
    function onDoc(e: MouseEvent) {
      if (schemaMenuRoot.current && !schemaMenuRoot.current.contains(e.target as Node)) {
        setSchemaMenuOpen(false)
      }
    }
    document.addEventListener('mousedown', onDoc)
    return () => document.removeEventListener('mousedown', onDoc)
  }, [])

  const activeTask = useMemo(
    () => tasks.find((x) => x.id === activeTaskId) || null,
    [tasks, activeTaskId]
  )

  // 任务列表被清理后，丢掉孤儿 activeTaskId，避免空白「对话模式」。
  useEffect(() => {
    if (activeTaskId && !activeTask) {
      setActiveTaskId('')
      saveActiveTaskId('')
    }
  }, [activeTask, activeTaskId])

  const workConnId = activeTask?.connectionId || activeConnId

  const activeConn = useMemo(
    () => connections.find((c) => c.id === workConnId) || null,
    [connections, workConnId]
  )

  // Native window title bar: "AI-DB — 未命名任务 · jd-test"
  useEffect(() => {
    if (typeof window === 'undefined' || !(window as unknown as { runtime?: unknown }).runtime) return
    const base = 'AI-DB'
    const name = activeTask?.name || (activeTask ? t('ss.unnamedTask') : '')
    const title = !activeTask
      ? base
      : activeConn?.name
        ? `${base} — ${name} · ${activeConn.name}`
        : `${base} — ${name}`
    WindowSetTitle(title)
  }, [activeTask, activeConn, t])

  const refreshObjects = useCallback(async () => {
    if (!workConnId || !connectedIds.has(workConnId)) {
      setObjects([])
      return
    }
    try {
      const nodes = (await ListObjects(workConnId)) as unknown as ObjectNode[]
      setObjects(nodes || [])
    } catch (e: any) {
      setBanner(String(e?.message || e))
      setObjects([])
    }
  }, [workConnId, connectedIds])

  useEffect(() => {
    void refreshObjects()
  }, [refreshObjects])

  // Query 需要对象树，进入 SQL 面再连；Agent 选节点不连，发送时再连。
  useEffect(() => {
    if (mode !== 'work' || workSurface !== 'sql' || !workConnId || connectedIds.has(workConnId)) return
    void handleConnect(workConnId)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mode, workSurface, workConnId])

  const schemaOptions = useMemo(
    () =>
      objects
        .filter((n) => n.kind === 'schema' || n.kind === 'database')
        .map((n) => n.name)
        .filter(Boolean),
    [objects]
  )

  // Keep / default the selected schema when objects or connection change.
  useEffect(() => {
    if (!workConnId) {
      setActiveSchema('')
      return
    }
    if (schemaOptions.length === 0) {
      // Objects still loading / empty — seed from connection default (App owns schema; QueryView must not clear).
      setActiveSchema(activeConn?.database || '')
      return
    }
    setActiveSchema((prev) => {
      if (prev && schemaOptions.includes(prev)) return prev
      const prefer = activeConn?.database || ''
      if (prefer && schemaOptions.includes(prefer)) return prefer
      return schemaOptions[0]
    })
  }, [workConnId, schemaOptions, activeConn?.database])

  // Push UI-selected schema into the live driver (USE / search_path / SELECT).
  // Wait until schemaOptions is known so we don't apply a PG database name as search_path.
  useEffect(() => {
    if (!workConnId || !connectedIds.has(workConnId) || !activeSchema) return
    if (schemaOptions.length === 0 || !schemaOptions.includes(activeSchema)) return
    void SetActiveSchema(workConnId, activeSchema).catch((e: any) => {
      setBanner(String(e?.message || e))
    })
  }, [workConnId, connectedIds, activeSchema, schemaOptions])

  async function handleConnect(id: string): Promise<boolean> {
    setBanner('')
    try {
      await Connect(id)
      setConnectedIds((prev) => new Set(prev).add(id))
      setActiveConnId(id)
      // Apply known workbench schema immediately (Query/Agent before the options effect).
      // Only when already validated against schemaOptions — avoid PG database-as-search_path.
      const schema = activeSchema.trim()
      if (schema && schemaOptions.includes(schema)) {
        try {
          await SetActiveSchema(id, schema)
        } catch { /* effect retries when options settle */ }
      }
      return true
    } catch (e: any) {
      setBanner(String(e?.message || e))
      return false
    }
  }

  async function handleDeleteConn(id: string) {
    if (!window.confirm(t('conn.deleteConfirm'))) return
    await DeleteConnection(id)
    setConnectedIds((prev) => {
      const n = new Set(prev)
      n.delete(id)
      return n
    })
    if (activeConnId === id) setActiveConnId('')
    const nextTasks = tasks.filter((task) => task.connectionId !== id)
    persistTasks(nextTasks)
    if (activeTask && activeTask.connectionId === id) {
      setActiveTaskId('')
      saveActiveTaskId('')
    }
    await reload()
  }

  function handleCreateTask(name: string, connectionId: string) {
    const task = createTask(name, connectionId)
    persistTasks([task, ...tasks])
    setActiveTaskId(task.id)
    saveActiveTaskId(task.id)
    setActiveConnId(connectionId)
    setMode('work')
    setWorkSurface('agent')
    setShowNewTask(false)
    // 不在创建时连接：等 Agent 首次发送再 Connect，避免选节点就报错横幅。
  }

  function ensureTask(connectionId: string): string {
    if (activeTask) {
      if (connectionId && activeTask.connectionId !== connectionId && activeTask.messages.length === 0) {
        persistTasks(
          tasksRef.current.map((task) =>
            task.id === activeTask.id ? touchTask(task, { connectionId }) : task
          )
        )
        setActiveConnId(connectionId)
      }
      return activeTask.id
    }
    if (!connectionId) return ''
    const task = createTask('', connectionId)
    persistTasks(normalizeTasks([task, ...tasksRef.current]))
    setActiveTaskId(task.id)
    saveActiveTaskId(task.id)
    setActiveConnId(connectionId)
    return task.id
  }

  function upsertTaskMessages(list: Task[], taskId: string, history: ChatMessage[], connectionId?: string): Task[] {
    const idx = list.findIndex((task) => task.id === taskId)
    if (idx < 0) {
      return normalizeTasks([
        {
          id: taskId,
          name: '',
          connectionId: connectionId || '',
          updatedAt: new Date().toISOString(),
          messages: history,
        },
        ...list,
      ])
    }
    return list.map((task) =>
      task.id === taskId ? touchTask(task, { messages: history }) : task
    )
  }

  /** Replace task history (clear / absolute). Syncs tasksRef immediately. */
  function updateTaskHistoryFor(taskId: string, history: ChatMessage[], connectionId?: string) {
    if (!taskId) return
    persistTasks(upsertTaskMessages(tasksRef.current, taskId, history, connectionId))
  }

  function getTaskMessages(taskId: string): ChatMessage[] {
    return tasksRef.current.find((task) => task.id === taskId)?.messages || []
  }

  /** Append without clobbering concurrent turns (Agent ↔ AI分析). */
  function appendTaskMessages(taskId: string, messages: ChatMessage[], connectionId?: string) {
    if (!taskId || messages.length === 0) return
    const list = tasksRef.current
    const existing = list.find((task) => task.id === taskId)
    const prior = existing?.messages || []
    persistTasks(
      upsertTaskMessages(list, taskId, [...prior, ...messages], connectionId || existing?.connectionId)
    )
  }

  /** Drop one user turn by content (last match); keeps messages after it. */
  function removeTaskUserMessage(taskId: string, content: string) {
    if (!taskId) return
    const list = tasksRef.current
    const existing = list.find((task) => task.id === taskId)
    if (!existing) return
    let idx = -1
    for (let i = existing.messages.length - 1; i >= 0; i--) {
      const m = existing.messages[i]
      if (m.role === 'user' && m.content === content) {
        idx = i
        break
      }
    }
    if (idx < 0) return
    const history = [...existing.messages.slice(0, idx), ...existing.messages.slice(idx + 1)]
    persistTasks(upsertTaskMessages(list, taskId, history, existing.connectionId))
  }

  /** Query「AI分析」：留在结果区展示，同时把对话写入当前任务（Agent 模式可见）。 */
  async function runAiAnalyze(sql: string): Promise<string> {
    const prompt = t('query.agentExplainPrompt', { sql })
    let connId = workConnId
    if (!connId) {
      if (connections.length === 1) connId = connections[0].id
      else if (connections.length === 0) throw new Error(t('chat.pickConn.empty'))
      else throw new Error(t('chat.pickConn.hint'))
    }
    if (!connectedIds.has(connId)) {
      const ok = await handleConnect(connId)
      if (!ok) throw new Error(t('agent.needConnect'))
    }
    let confirmWrites = false
    try {
      if (await WritesAllowed()) {
        if (!window.confirm(t('agent.writeConfirm'))) {
          throw new Error(t('query.aiCancelled'))
        }
        confirmWrites = true
      }
    } catch (e: any) {
      if (String(e?.message || e) === t('query.aiCancelled')) throw e
      /* read-only config */
    }
    const tid = ensureTask(connId)
    if (!tid) throw new Error(t('agent.needConnect'))
    const prior = getTaskMessages(tid)
    appendTaskMessages(tid, [{ role: 'user', content: prompt, createdAt: Date.now() }], connId)
    try {
      const res = await AgentChat({
        connectionId: connId,
        history: prior.map((m) => ({ role: m.role, content: m.content })),
        message: prompt,
        confirmWrites,
        preferredSchema: activeSchema || '',
      } as any)
      const final = String(res?.final || '')
      appendTaskMessages(tid, [{ role: 'assistant', content: final, createdAt: Date.now() }], connId)
      return final
    } catch (e: any) {
      removeTaskUserMessage(tid, prompt)
      throw e
    }
  }

  const workspaceHistory = activeTask?.messages || []

  // SQL 查询占满主区：隐藏任务侧栏（新建任务 / 会话列表 / 设置入口）。
  const hideTaskSidebar = mode === 'work' && workSurface === 'sql'

  return (
    <div className={`layout${hideTaskSidebar ? ' sql-focus' : ''}`}>
      {!hideTaskSidebar && (
        <aside className="sidebar">
          <div className="brand">
            <img className="brand-mark" src={logoSrc} alt="AI-DB" />
            <div>
              <div className="brand-title">AI-DB</div>
              <div className="brand-sub">{t('app.brand.sub')}</div>
            </div>
          </div>

          <nav className="sidebar-nav">
            <button
              type="button"
              className={`nav-btn${showNewTask ? ' active' : ''}`}
              onClick={() => {
                setMode('work')
                setWorkSurface('agent')
                setShowNewTask(true)
              }}
            >
              <svg className="nav-icon" width="15" height="15" viewBox="0 0 24 24" aria-hidden="true">
                <path d={NEW_TASK_ICON} fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" />
              </svg>
              {t('app.nav.newTask')}
            </button>
          </nav>

          <div className="ssb-wrap">
            <TasksSidebar
              tasks={tasks}
              connections={connections}
              activeTaskId={activeTaskId}
              onOpen={(id) => selectTask(id)}
              onRename={(id, name) => {
                persistTasks(
                  tasksRef.current.map((task) => (task.id === id ? touchTask(task, { name }) : task))
                )
              }}
              onDelete={(id) => {
                const next = tasksRef.current.filter((task) => task.id !== id)
                persistTasks(next)
                if (activeTaskId === id) {
                  setActiveTaskId('')
                  saveActiveTaskId('')
                }
              }}
            />
          </div>

          <div className="posture">
            <SettingsMenu activePage={menuActivePage} onPage={openWorkPage} />
          </div>
        </aside>
      )}

      <main className="main">
        {banner && <div className="banner" onClick={() => setBanner('')}>{banner}</div>}

        {/* Keep workbench mounted across Settings/Connections/Prefs so Query tabs survive. */}
        <div className={`work-surface${mode === 'work' ? '' : ' is-hidden'}`} aria-hidden={mode !== 'work'}>
            {/* 对齐 Ai-Shell：无任务的欢迎首页不露顶栏；点进有效任务或 SQL 显示工作栏 */}
            {(workSurface === 'sql' || !!activeTask) && (
              <header className="work-bar">
                <div className="work-bar-left">
                  {hideTaskSidebar && (
                    <button
                      type="button"
                      className="work-home-btn"
                      title={t('work.home.hint')}
                      onClick={() => {
                        setMode('work')
                        setWorkSurface('agent')
                        setActiveTaskId('')
                        saveActiveTaskId('')
                        setSchemaMenuOpen(false)
                      }}
                    >
                      <svg width="15" height="15" viewBox="0 0 24 24" aria-hidden="true">
                        <path
                          d="M4 11.5 12 4l8 7.5 M7 10.5V20h10v-9.5"
                          fill="none"
                          stroke="currentColor"
                          strokeWidth="1.7"
                          strokeLinecap="round"
                          strokeLinejoin="round"
                        />
                      </svg>
                      <span>{t('work.home')}</span>
                    </button>
                  )}
                  <div
                    className={`work-bar-node${activeConn ? '' : ' muted'}`}
                    title={
                      activeConn
                        ? `${activeConn.name} · ${activeConn.type} · ${activeConn.host}:${activeConn.port}`
                        : t('work.node.offline')
                    }
                  >
                    <svg className="meta-ico" width="14" height="14" viewBox="0 0 24 24" aria-hidden="true">
                      <ellipse cx="12" cy="6" rx="7" ry="3" fill="none" stroke="currentColor" strokeWidth="1.7" />
                      <path d="M5 6v6c0 1.7 3.1 3 7 3s7-1.3 7-3V6" fill="none" stroke="currentColor" strokeWidth="1.7" />
                      <path d="M5 12v6c0 1.7 3.1 3 7 3s7-1.3 7-3v-6" fill="none" stroke="currentColor" strokeWidth="1.7" />
                    </svg>
                    <span className={`conn-dot${workConnId && connectedIds.has(workConnId) ? ' on' : ''}`} />
                    <span className="work-bar-node-label">
                      {activeConn
                        ? `${activeConn.name} · ${activeConn.host}:${activeConn.port}`
                        : t('work.node.offline')}
                    </span>
                  </div>
                  <div className="host-menu-wrap work-bar-schema-pick" ref={schemaMenuRoot}>
                    <button
                      type="button"
                      className={`tool-pill meta-btn work-schema-btn${schemaMenuOpen ? ' open' : ''}`}
                      title={activeSchema ? t('work.schema.hint', { name: activeSchema }) : t('work.schema.pick')}
                      disabled={!workConnId || !connectedIds.has(workConnId)}
                      onClick={() => setSchemaMenuOpen((v) => !v)}
                    >
                      <span className="work-bar-schema-label">{t('work.schema')}</span>
                      <strong className="work-bar-schema-name">{activeSchema || t('work.schema.pick')}</strong>
                      <span className="caret-down">{schemaMenuOpen ? '▴' : '▾'}</span>
                    </button>
                    {schemaMenuOpen && (
                      <div className="host-menu" role="listbox">
                        {schemaOptions.length === 0 ? (
                          <button type="button" className="model-menu-item" disabled>
                            {t('work.schema.empty')}
                          </button>
                        ) : (
                          schemaOptions.map((name) => (
                            <button
                              key={name}
                              type="button"
                              className={`model-menu-item${name === activeSchema ? ' on' : ''}`}
                              onClick={() => {
                                setActiveSchema(name)
                                setSchemaMenuOpen(false)
                              }}
                            >
                              {name}
                            </button>
                          ))
                        )}
                      </div>
                    )}
                  </div>
                </div>
                <div className="work-bar-right">
                  <div className="seg" role="tablist" aria-label="Agent / Query">
                    <button
                      type="button"
                      className={workSurface === 'agent' ? 'on' : ''}
                      onClick={() => setWorkSurface('agent')}
                    >
                      {t('work.view.agent')}
                    </button>
                    <button
                      type="button"
                      className={workSurface === 'sql' ? 'on' : ''}
                      onClick={() => setWorkSurface('sql')}
                    >
                      {t('work.view.sql')}
                    </button>
                  </div>
                </div>
              </header>
            )}
            <div className="work-surface-body">
              {/* Keep both mounted so Agent↔Query switch does not wipe editor/result tabs. */}
              <div className={`work-surface-panel${workSurface === 'agent' ? '' : ' is-hidden'}`}>
                <AgentWorkspace
                  taskId={activeTaskId}
                  connectionId={workConnId}
                  connected={!!workConnId && connectedIds.has(workConnId)}
                  connections={connections}
                  connectionName={activeConn?.name || ''}
                  preferredSchema={activeSchema}
                  history={workspaceHistory}
                  onSelectConnection={(id) => {
                    setBanner('')
                    setActiveConnId(id)
                    if (activeTask && activeTask.messages.length === 0) {
                      persistTasks(tasksRef.current.map((task) =>
                        task.id === activeTask.id ? touchTask(task, { connectionId: id }) : task
                      ))
                    }
                  }}
                  onEnsureConnected={handleConnect}
                  onGotoQuery={requestOpenQuery}
                  onGotoConnections={() => setMode('connections')}
                  onGotoSettings={() => setMode('settings')}
                  onEnsureTask={ensureTask}
                  onGetHistory={getTaskMessages}
                  onAppendMessages={appendTaskMessages}
                  onRemoveUserMessage={removeTaskUserMessage}
                  onHistoryChangeFor={updateTaskHistoryFor}
                />
              </div>
              <div className={`work-surface-panel${workSurface === 'sql' ? '' : ' is-hidden'}`}>
                <QueryView
                  active={mode === 'work' && workSurface === 'sql'}
                  connectionId={workConnId}
                  connected={!!workConnId && connectedIds.has(workConnId)}
                  connType={activeConn?.type || 'mysql'}
                  objects={objects}
                  activeSchema={activeSchema}
                  onRefreshObjects={refreshObjects}
                  onActiveSchemaChange={setActiveSchema}
                  onAiAnalyze={runAiAnalyze}
                />
              </div>
            </div>
          </div>
        {mode === 'connections' && (
          <ConnectionsView
            connections={connections}
            connectedIds={connectedIds}
            activeId={activeConnId}
            onSelect={setActiveConnId}
            onEdit={(c) => setEditing(c)}
            onDelete={handleDeleteConn}
            onAdd={() => setEditing(emptyConnection())}
          />
        )}
        {mode === 'prefs' && <PrefsView />}
        {mode === 'settings' && <SettingsView />}
      </main>

      {showNewTask && (
        <NewTaskModal
          connections={connections}
          defaultConnectionId={activeConnId || connections[0]?.id || ''}
          onClose={() => setShowNewTask(false)}
          onCreate={handleCreateTask}
        />
      )}

      {showPickConnForQuery && (
        <PickConnectionModal
          connections={connections}
          defaultConnectionId={activeConnId || connections[0]?.id || ''}
          onClose={() => setShowPickConnForQuery(false)}
          onManage={() => {
            setShowPickConnForQuery(false)
            setMode('connections')
          }}
          onPick={(id) => {
            setShowPickConnForQuery(false)
            // Reuse an empty unnamed task on the same connection to avoid sidebar spam.
            const existing = tasks.find(
              (task) => !task.name.trim() && task.connectionId === id && task.messages.length === 0
            )
            const task = existing || createTask('', id)
            if (!existing) persistTasks([task, ...tasks])
            setActiveTaskId(task.id)
            saveActiveTaskId(task.id)
            openQuerySurface(id)
          }}
        />
      )}

      {editing !== undefined && (
        <ConnectionForm
          initial={editing}
          onClose={() => setEditing(undefined)}
          onSave={async (c) => {
            const saved = (await UpsertConnection(c as any)) as unknown as Connection
            await reload()
            setActiveConnId(saved.id)
          }}
          onTest={async (c) => {
            await TestConnection(c as any)
          }}
        />
      )}
    </div>
  )
}

export default App
