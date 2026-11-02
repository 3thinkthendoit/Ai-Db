// Transport abstraction: one frontend build serves both the desktop (Wails
// bridge) and server (HTTP) targets. On desktop it forwards to the generated
// Wails bindings; in a browser it calls the Go server over fetch with a session
// cookie.
//
// Phase 3 exposes all 21 bound methods; the 8 call sites import from this module
// instead of '../../wailsjs/go/main/App'. Every signature mirrors App.d.ts
// one-for-one, so switching a call site's import is type-transparent.
import * as bridge from '../../wailsjs/go/main/App'
import { agent, config, db, main } from '../../wailsjs/go/models'

/** True when running inside the Wails webview (desktop build). */
export function isDesktop(): boolean {
  return typeof (window as unknown as { go?: { main?: { App?: unknown } } }).go?.main?.App === 'object'
}

interface ApiErrorBody {
  error?: string
}

/** Web transport: POST /api/{Method} with positional args + session cookie. */
async function call<T>(method: string, ...args: unknown[]): Promise<T> {
  const res = await fetch(`/api/${method}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify({ args }),
  })
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as ApiErrorBody
    throw new Error(body.error || res.statusText)
  }
  return (await res.json()) as T
}

/** Route a bound-method call to the Wails bridge (desktop) or HTTP (web). */
function invoke<T>(method: string, ...args: unknown[]): Promise<T> {
  if (isDesktop()) {
    const fn = (bridge as unknown as Record<string, (...a: unknown[]) => Promise<T>>)[method]
    return fn(...args)
  }
  return call<T>(method, ...args)
}

// —— Bound methods (all 23; signatures mirror wailsjs/go/main/App.d.ts) ——

// connections
export const ListConnections = (): Promise<Array<config.Connection>> => invoke('ListConnections')
export const UpsertConnection = (arg1: config.Connection): Promise<config.Connection> => invoke('UpsertConnection', arg1)
export const DeleteConnection = (arg1: string): Promise<void> => invoke('DeleteConnection', arg1)
export const TestConnection = (arg1: config.Connection): Promise<void> => invoke('TestConnection', arg1)
export const Connect = (arg1: string): Promise<void> => invoke('Connect', arg1)
export const Disconnect = (arg1: string): Promise<void> => invoke('Disconnect', arg1)

// object tree / query
export const ListObjects = (arg1: string): Promise<Array<db.ObjectNode>> => invoke('ListObjects', arg1)
export const ExpandObject = (arg1: string, arg2: Array<string>): Promise<Array<db.ObjectNode>> => invoke('ExpandObject', arg1, arg2)
export const ExecuteQuery = (arg1: string, arg2: string): Promise<db.QueryResult> => invoke('ExecuteQuery', arg1, arg2)
export const SetActiveSchema = (arg1: string, arg2: string): Promise<void> => invoke('SetActiveSchema', arg1, arg2)
export const IsMutatingQuery = (arg1: string, arg2: string): Promise<boolean> => invoke('IsMutatingQuery', arg1, arg2)
export const WritesAllowed = (): Promise<boolean> => invoke('WritesAllowed')

// create-database dialog (MySQL / PostgreSQL connections only)
export interface CharsetOption {
  name: string
  defaultCollation: string
  collations: string[]
}

export interface CreateDatabaseOptions {
  /** MySQL charsets with collations; empty on PostgreSQL. */
  charsets: CharsetOption[]
  /** PostgreSQL candidate ENCODING values; empty on MySQL. */
  encodings: string[]
}

export interface CreateDatabaseSpec {
  name: string
  charset?: string
  collate?: string
  encoding?: string
  account?: string
  password?: string
  host?: string
}

export interface CreateDatabaseResult {
  ok: boolean
  completed: string[]
  failedStep?: string
  error?: string
}

export const CreateDatabaseOptions = (arg1: string): Promise<CreateDatabaseOptions> =>
  invoke('CreateDatabaseOptions', arg1)
export const CreateDatabase = (arg1: string, arg2: CreateDatabaseSpec): Promise<CreateDatabaseResult> =>
  invoke('CreateDatabase', arg1, arg2)

// LLM
export const GetLLMConfig = (): Promise<config.LLMConfig> => invoke('GetLLMConfig')
export const ListLLMProfiles = (): Promise<Array<config.LLMProfileView>> => invoke('ListLLMProfiles')
export const SaveLLMProfile = (arg1: main.SaveLLMProfileRequest): Promise<config.LLMProfileView> => invoke('SaveLLMProfile', arg1)
export const DeleteLLMProfile = (arg1: string): Promise<void> => invoke('DeleteLLMProfile', arg1)
export const ActivateLLMProfile = (arg1: string): Promise<void> => invoke('ActivateLLMProfile', arg1)
export const SaveLLMSafety = (arg1: boolean, arg2: number, arg3: number): Promise<void> => invoke('SaveLLMSafety', arg1, arg2, arg3)
export const TestLLMProfile = (arg1: string, arg2: string, arg3: string, arg4: string): Promise<main.LLMTestResult> => invoke('TestLLMProfile', arg1, arg2, arg3, arg4)
export const SaveLLMConfig = (arg1: config.LLMConfig): Promise<void> => invoke('SaveLLMConfig', arg1)

// agent
export const AgentChat = (arg1: main.AgentChatRequest): Promise<agent.ChatResponse> => invoke('AgentChat', arg1)

/**
 * Streaming agent chat. On the web it POSTs /api/AgentChatStream and consumes the
 * Server-Sent-Events frames live, invoking onStep for each step (thinking / tool /
 * tool_result / message) as it arrives, then resolves with the terminal
 * ChatResponse. On desktop there is no streaming binding — it falls back to the
 * one-shot AgentChat and replays res.steps through onStep, so behaviour (and the
 * generated Wails bindings) stay unchanged.
 *
 * SSE granularity is per agent step, not per token: the LLM client reads a full
 * response, so this surfaces progress as each step completes.
 */
export async function AgentChatStream(
  req: main.AgentChatRequest,
  onStep?: (step: agent.StepEvent) => void
): Promise<agent.ChatResponse> {
  if (isDesktop()) {
    const res = await AgentChat(req)
    if (onStep) for (const step of res.steps || []) onStep(step)
    return res
  }

  const resp = await fetch('/api/AgentChatStream', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Accept: 'text/event-stream' },
    credentials: 'include',
    body: JSON.stringify({ args: [req] }),
  })
  if (!resp.ok) {
    const body = (await resp.json().catch(() => ({}))) as ApiErrorBody
    throw new Error(body.error || resp.statusText)
  }
  // The server falls back to a one-shot JSON body if its ResponseWriter cannot
  // flush; honour that instead of trying to parse SSE frames.
  const ctype = resp.headers.get('Content-Type') || ''
  if (!ctype.startsWith('text/event-stream')) {
    const res = (await resp.json()) as agent.ChatResponse
    if (onStep) for (const step of res.steps || []) onStep(step)
    return res
  }
  if (!resp.body) {
    const res = (await resp.json()) as agent.ChatResponse
    if (onStep) for (const step of res.steps || []) onStep(step)
    return res
  }

  const reader = resp.body.getReader()
  const decoder = new TextDecoder()
  let buf = ''
  let final: agent.ChatResponse | null = null
  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    buf += decoder.decode(value, { stream: true })
    // Frames are separated by a blank line; process each complete one.
    let sep: number
    while ((sep = buf.indexOf('\n\n')) >= 0) {
      const raw = buf.slice(0, sep)
      buf = buf.slice(sep + 2)
      const line = raw.split('\n').find((l) => l.startsWith('data:'))
      if (!line) continue
      const payload = line.slice(5).trim()
      if (!payload) continue
      let ev: { kind?: string; step?: agent.StepEvent; final?: agent.ChatResponse; error?: string }
      try {
        ev = JSON.parse(payload)
      } catch {
        continue
      }
      if (ev.kind === 'step' && ev.step) {
        onStep?.(ev.step)
      } else if (ev.kind === 'done') {
        final = ev.final || null
      } else if (ev.kind === 'error') {
        throw new Error(ev.error || 'stream error')
      }
    }
  }
  if (!final) throw new Error('stream ended without a result')
  return final
}

// —— Admin account management (web only; requires an admin session) ——

export interface AdminUser {
  id: string
  username: string
  role: string
  displayName: string
  banned: boolean
}

export interface AdminCreateResult {
  ok: boolean
  error?: string
  id?: string
  username?: string
  role?: string
}

export interface AdminMutateResult {
  ok: boolean
  error?: string
  id?: string
  username?: string
  role?: string
  displayName?: string
  banned?: boolean
}

/** List all accounts (admin-only). */
export async function adminListUsers(): Promise<AdminUser[]> {
  const res = await fetch('/api/admin/users', { credentials: 'include' })
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as ApiErrorBody
    throw new Error(body.error || res.statusText)
  }
  return (await res.json()) as AdminUser[]
}

async function readAdminJSON<T extends { ok?: boolean; error?: string }>(
  res: Response
): Promise<T> {
  const body = (await res.json().catch(() => ({}))) as T & ApiErrorBody
  if (!res.ok) {
    return { ...(body as T), ok: false, error: body.error || res.statusText || String(res.status) }
  }
  return body
}

/** Create an account (admin-only). Role defaults to "user" server-side. */
export async function adminCreateUser(
  username: string,
  password: string,
  role: string
): Promise<AdminCreateResult> {
  const res = await fetch('/api/admin/users', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify({ username, password, role }),
  })
  return readAdminJSON<AdminCreateResult>(res)
}

/** Patch role / display name / ban flag (admin-only). */
export async function adminUpdateUser(
  id: string,
  patch: { role?: string; displayName?: string; banned?: boolean }
): Promise<AdminMutateResult> {
  const res = await fetch(`/api/admin/users/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify(patch),
  })
  return readAdminJSON<AdminMutateResult>(res)
}

/** Admin reset of another account's password (no current password required). */
export async function adminSetPassword(
  id: string,
  password: string
): Promise<AdminMutateResult> {
  const res = await fetch(`/api/admin/users/${encodeURIComponent(id)}/password`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify({ password }),
  })
  return readAdminJSON<AdminMutateResult>(res)
}

// —— Auth endpoints (web only; the desktop build never calls these) ——

export interface LoginResult {
  ok: boolean
  error?: string
  username?: string
  role?: string
}

export interface SessionInfo {
  authenticated: boolean
  username?: string
  role?: string
}

export async function login(username: string, password: string): Promise<LoginResult> {
  const res = await fetch('/api/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify({ username, password }),
  })
  return (await res.json()) as LoginResult
}

export async function logout(): Promise<void> {
  await fetch('/api/logout', { method: 'POST', credentials: 'include' })
}

export async function session(): Promise<SessionInfo> {
  const res = await fetch('/api/session', { credentials: 'include' })
  return (await res.json()) as SessionInfo
}

// —— Self-service profile (web only) ——

export interface ProfileInfo {
  username: string
  role: string
  displayName: string
  avatar: string
}

export interface ProfileUpdateResult {
  ok: boolean
  error?: string
  username?: string
  role?: string
  displayName?: string
  avatar?: string
}

export async function getProfile(): Promise<ProfileInfo> {
  const res = await fetch('/api/me', { credentials: 'include' })
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as ApiErrorBody
    throw new Error(body.error || res.statusText)
  }
  return (await res.json()) as ProfileInfo
}

export async function updateProfile(patch: {
  displayName?: string
  avatar?: string
}): Promise<ProfileUpdateResult> {
  const res = await fetch('/api/me', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify(patch),
  })
  return (await res.json()) as ProfileUpdateResult
}

export async function changePassword(
  currentPassword: string,
  newPassword: string
): Promise<{ ok: boolean; error?: string }> {
  const res = await fetch('/api/me/password', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify({ currentPassword, newPassword }),
  })
  return (await res.json()) as { ok: boolean; error?: string }
}
