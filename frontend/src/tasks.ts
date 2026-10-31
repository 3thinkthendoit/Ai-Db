import { ChatMessage, StepEvent } from './types'

const TASKS_KEY = 'aidb.tasks'
const ACTIVE_KEY = 'aidb.activeTaskId'

/** Cap localStorage growth from long-lived chat history. */
export const MAX_TASKS = 50
export const MAX_MESSAGES_PER_TASK = 200
/** Per-step content cap when persisting (tool_result can be 12KB from the agent). */
export const MAX_STEP_CONTENT_CHARS = 2000

const PERSIST_STEP_KINDS = new Set(['thinking', 'tool', 'tool_result', 'error'])

/** Keep UI-relevant steps only; drop duplicate `message` bodies; truncate large payloads. */
export function sanitizeStepsForStorage(steps: unknown): StepEvent[] | undefined {
  if (!Array.isArray(steps)) return undefined
  const out: StepEvent[] = []
  for (const raw of steps) {
    if (!raw || typeof raw !== 'object') continue
    const s = raw as Record<string, unknown>
    const kind = typeof s.kind === 'string' ? s.kind : ''
    if (!PERSIST_STEP_KINDS.has(kind) || typeof s.content !== 'string') continue
    let content = s.content
    if (content.length > MAX_STEP_CONTENT_CHARS) {
      content = `${content.slice(0, MAX_STEP_CONTENT_CHARS)}…(truncated)`
    }
    out.push({
      kind,
      content,
      ...(typeof s.name === 'string' && s.name ? { name: s.name } : {}),
    })
  }
  return out.length ? out : undefined
}

export type Task = {
  id: string
  name: string
  connectionId: string
  updatedAt: string
  messages: ChatMessage[]
}

function uid() {
  return crypto.randomUUID?.() || `t_${Date.now()}_${Math.random().toString(16).slice(2)}`
}

function isTask(x: unknown): x is Task {
  if (!x || typeof x !== 'object') return false
  const t = x as Record<string, unknown>
  return typeof t.id === 'string'
    && typeof t.connectionId === 'string'
    && Array.isArray(t.messages)
}

/** Drop invalid entries, trim message tails, keep newest tasks. */
export function normalizeTasks(list: unknown): Task[] {
  if (!Array.isArray(list)) return []
  const cleaned: Task[] = []
  for (const item of list) {
    if (!isTask(item)) continue
    const messages = item.messages
      .filter((m): m is ChatMessage => !!m && typeof m === 'object'
        && (m.role === 'user' || m.role === 'assistant')
        && typeof m.content === 'string')
      .map((m) => {
        const steps = sanitizeStepsForStorage(m.steps)
        return {
          role: m.role,
          content: m.content,
          ...(typeof m.createdAt === 'number' ? { createdAt: m.createdAt } : {}),
          ...(steps ? { steps } : {}),
          ...(typeof m.model === 'string' && m.model.trim() ? { model: m.model.trim() } : {}),
        }
      })
      .slice(-MAX_MESSAGES_PER_TASK)
    cleaned.push({
      id: item.id,
      name: typeof item.name === 'string' ? item.name : '',
      connectionId: item.connectionId,
      updatedAt: typeof item.updatedAt === 'string' ? item.updatedAt : new Date(0).toISOString(),
      messages,
    })
  }
  cleaned.sort((a, b) => (a.updatedAt < b.updatedAt ? 1 : a.updatedAt > b.updatedAt ? -1 : 0))
  return cleaned.slice(0, MAX_TASKS)
}

export function loadTasks(): Task[] {
  try {
    const raw = localStorage.getItem(TASKS_KEY)
    if (!raw) return []
    return normalizeTasks(JSON.parse(raw))
  } catch {
    return []
  }
}

export function saveTasks(tasks: Task[]) {
  const normalized = normalizeTasks(tasks)
  try {
    localStorage.setItem(TASKS_KEY, JSON.stringify(normalized))
    return
  } catch {
    /* quota / private mode — retry without bulky tool payloads */
  }
  try {
    const slim = normalized.map((task) => ({
      ...task,
      messages: task.messages.map((m) => {
        const base: ChatMessage = {
          role: m.role,
          content: m.content,
          ...(m.createdAt != null ? { createdAt: m.createdAt } : {}),
          ...(m.model ? { model: m.model } : {}),
        }
        if (!m.steps?.length) return base
        const steps = m.steps
          .filter((s) => s.kind === 'thinking' || s.kind === 'tool' || s.kind === 'error')
          .map((s) => ({
            ...s,
            content: s.content.length > 400 ? `${s.content.slice(0, 400)}…(truncated)` : s.content,
          }))
        return steps.length ? { ...base, steps } : base
      }),
    }))
    localStorage.setItem(TASKS_KEY, JSON.stringify(slim))
  } catch { /* give up */ }
}

export function loadActiveTaskId(): string {
  try {
    return localStorage.getItem(ACTIVE_KEY) || ''
  } catch {
    return ''
  }
}

export function saveActiveTaskId(id: string) {
  try {
    if (id) localStorage.setItem(ACTIVE_KEY, id)
    else localStorage.removeItem(ACTIVE_KEY)
  } catch { /* ignore */ }
}

export function createTask(name: string, connectionId: string): Task {
  return {
    id: uid(),
    name: name.trim() || '',
    connectionId,
    updatedAt: new Date().toISOString(),
    messages: [],
  }
}

export function touchTask(task: Task, patch: Partial<Task>): Task {
  return { ...task, ...patch, updatedAt: new Date().toISOString() }
}
