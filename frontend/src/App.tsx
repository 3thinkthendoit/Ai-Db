import { useCallback, useEffect, useMemo, useState } from 'react'
import './App.css'
import logoSrc from './assets/logo.svg'
import { ConnectionForm } from './components/ConnectionForm'
import { NewTaskModal } from './components/NewTaskModal'
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
  Connect,
  DeleteConnection,
  ListConnections,
  ListObjects,
  TestConnection,
  UpsertConnection,
} from '../wailsjs/go/main/App'

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
  const [activeTaskId, setActiveTaskId] = useState(() => loadActiveTaskId())
  const [showNewTask, setShowNewTask] = useState(false)
  const [workSurface, setWorkSurface] = useState<'agent' | 'sql'>('agent')

  const persistTasks = useCallback((next: Task[]) => {
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

  function openWorkPage(page: AppMode) {
    if (page === 'query') {
      setMode('work')
      setWorkSurface('sql')
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

  const activeTask = useMemo(
    () => tasks.find((x) => x.id === activeTaskId) || null,
    [tasks, activeTaskId]
  )

  const workConnId = activeTask?.connectionId || activeConnId

  const activeConn = useMemo(
    () => connections.find((c) => c.id === workConnId) || null,
    [connections, workConnId]
  )

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

  // SQL surface needs a live driver.
  useEffect(() => {
    if (mode !== 'work' || workSurface !== 'sql' || !workConnId || connectedIds.has(workConnId)) return
    void handleConnect(workConnId)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mode, workSurface, workConnId])

  async function handleConnect(id: string): Promise<boolean> {
    setBanner('')
    try {
      await Connect(id)
      setConnectedIds((prev) => new Set(prev).add(id))
      setActiveConnId(id)
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
    void handleConnect(connectionId)
  }

  function ensureTask(connectionId: string): string {
    if (activeTask) {
      if (connectionId && activeTask.connectionId !== connectionId && activeTask.messages.length === 0) {
        setTasks((prev) => {
          const next = prev.map((task) =>
            task.id === activeTask.id ? touchTask(task, { connectionId }) : task
          )
          saveTasks(next)
          return next
        })
        setActiveConnId(connectionId)
      }
      return activeTask.id
    }
    if (!connectionId) return ''
    const task = createTask('', connectionId)
    setTasks((prev) => {
      const next = normalizeTasks([task, ...prev])
      saveTasks(next)
      return next
    })
    setActiveTaskId(task.id)
    saveActiveTaskId(task.id)
    setActiveConnId(connectionId)
    return task.id
  }

  function updateTaskHistoryFor(taskId: string, history: ChatMessage[], connectionId?: string) {
    if (!taskId) return
    setTasks((prev) => {
      const idx = prev.findIndex((task) => task.id === taskId)
      let next: Task[]
      if (idx < 0) {
        next = normalizeTasks([
          {
            id: taskId,
            name: '',
            connectionId: connectionId || '',
            updatedAt: new Date().toISOString(),
            messages: history,
          },
          ...prev,
        ])
      } else {
        next = prev.map((task) =>
          task.id === taskId ? touchTask(task, { messages: history }) : task
        )
      }
      saveTasks(next)
      return next
    })
  }

  const workspaceHistory = activeTask?.messages || []

  return (
    <div className="layout">
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
              persistTasks(tasks.map((task) => (task.id === id ? touchTask(task, { name }) : task)))
            }}
            onDelete={(id) => {
              const next = tasks.filter((task) => task.id !== id)
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

      <main className="main">
        {banner && <div className="banner" onClick={() => setBanner('')}>{banner}</div>}

        {mode === 'work' && (
          <div className="work-surface">
            {/* 对齐 Ai-Shell：空欢迎首页不露顶栏；有对话或切到 SQL 才显示 Agent/SQL 切换 */}
            {(workSurface === 'sql' || workspaceHistory.length > 0) && (
              <header className="work-bar">
                <div className="work-bar-left">
                  {activeConn ? (
                    <span
                      className="work-bar-host"
                      title={`${activeConn.name} · ${activeConn.type} · ${activeConn.host}:${activeConn.port}`}
                    >
                      {activeConn.name}
                      {' · '}
                      {activeConn.host}:{activeConn.port}
                    </span>
                  ) : (
                    <span className="work-bar-host muted">{t('app.noConnection')}</span>
                  )}
                </div>
                <div className="seg" role="tablist" aria-label="Agent / SQL">
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
              </header>
            )}
            <div className="work-surface-body">
              {workSurface === 'agent' ? (
                <AgentWorkspace
                  taskId={activeTaskId}
                  connectionId={workConnId}
                  connected={!!workConnId && connectedIds.has(workConnId)}
                  connections={connections}
                  connectionName={activeConn?.name || ''}
                  taskName={activeTask?.name || ''}
                  history={workspaceHistory}
                  onPickConnection={async (id) => {
                    setActiveConnId(id)
                    if (activeTask && activeTask.messages.length === 0) {
                      persistTasks(tasks.map((task) =>
                        task.id === activeTask.id ? touchTask(task, { connectionId: id }) : task
                      ))
                    }
                    return handleConnect(id)
                  }}
                  onGotoQuery={() => setWorkSurface('sql')}
                  onGotoConnections={() => setMode('connections')}
                  onEnsureTask={ensureTask}
                  onHistoryChangeFor={updateTaskHistoryFor}
                />
              ) : (
                <QueryView
                  connectionId={workConnId}
                  connected={!!workConnId && connectedIds.has(workConnId)}
                  connType={activeConn?.type || 'mysql'}
                  objects={objects}
                  onRefreshObjects={refreshObjects}
                />
              )}
            </div>
          </div>
        )}
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
