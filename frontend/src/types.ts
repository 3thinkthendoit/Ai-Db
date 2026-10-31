export type ConnType = 'mysql' | 'postgres' | 'redis' | 'mongo'
export type AppMode = 'work' | 'query' | 'prefs' | 'connections' | 'settings'

export interface Connection {
  id: string
  name: string
  type: ConnType
  host: string
  port: number
  user: string
  password: string
  passwordSet?: boolean
  database: string
  /** MongoDB authentication database (e.g. admin). */
  authSource?: string
  /** PostgreSQL sslmode; empty → prefer. */
  sslMode?: string
}

export interface LLMConfig {
  baseUrl: string
  apiKey: string
  model: string
  allowWrites: boolean
  maxRows: number
  maxAgentSteps: number
}

export interface LLMProfileView {
  id: string
  name: string
  baseUrl: string
  model: string
  hasApiKey: boolean
  active: boolean
}

export interface QueryResult {
  columns: string[]
  rows: string[][]
  rowCount: number
  durationMs: number
  message: string
  truncated: boolean
}

export interface ObjectNode {
  name: string
  kind: string
  lazy?: boolean
  /** Short UI tag from backend, e.g. "PK". */
  badge?: string
  children?: ObjectNode[]
}

export interface StepEvent {
  kind: string
  name?: string
  content: string
}

export interface ChatMessage {
  role: 'user' | 'assistant' | 'system' | 'tool'
  content: string
  /** Epoch ms when the message was added (optional for older persisted turns). */
  createdAt?: number
  /** Agent loop steps for this assistant turn (thinking / tool / tool_result). */
  steps?: StepEvent[]
  /** Model id/label shown under the brand for this turn. */
  model?: string
}

export function emptyConnection(type: ConnType = 'mysql'): Connection {
  const ports: Record<ConnType, number> = { mysql: 3306, postgres: 5432, redis: 6379, mongo: 27017 }
  return {
    id: '',
    name: '',
    type,
    host: '127.0.0.1',
    port: ports[type],
    user: type === 'redis' ? '' : type === 'mongo' ? '' : 'root',
    password: '',
    database: type === 'postgres' ? 'postgres' : type === 'redis' ? '' : type === 'mongo' ? 'test' : '',
    authSource: type === 'mongo' ? 'admin' : '',
    sslMode: type === 'postgres' ? 'prefer' : '',
  }
}
