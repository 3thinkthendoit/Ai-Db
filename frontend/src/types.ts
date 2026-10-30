export type ConnType = 'mysql' | 'postgres' | 'redis'
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
  children?: ObjectNode[]
}

export interface ChatMessage {
  role: 'user' | 'assistant' | 'system' | 'tool'
  content: string
}

export interface StepEvent {
  kind: string
  name?: string
  content: string
}

export function emptyConnection(type: ConnType = 'mysql'): Connection {
  const ports: Record<ConnType, number> = { mysql: 3306, postgres: 5432, redis: 6379 }
  return {
    id: '',
    name: '',
    type,
    host: '127.0.0.1',
    port: ports[type],
    user: type === 'redis' ? '' : 'root',
    password: '',
    database: type === 'postgres' ? 'postgres' : type === 'redis' ? '0' : '',
  }
}
