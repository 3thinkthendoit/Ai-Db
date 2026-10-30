import { ChatMessage } from './types'

const TASKS_KEY = 'aidb.tasks'
const ACTIVE_KEY = 'aidb.activeTaskId'

/** Cap localStorage growth from long-lived chat history. */
export const MAX_TASKS = 50
export const MAX_MESSAGES_PER_TASK = 200

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
  try {
    localStorage.setItem(TASKS_KEY, JSON.stringify(normalizeTasks(tasks)))
  } catch { /* ignore quota / private mode */ }
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
