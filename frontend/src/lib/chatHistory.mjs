/**
 * History helpers for Agent retry/edit (shared with specs).
 */

/** Messages strictly before `index` (excludes that turn and everything after). */
export function historyBeforeIndex(history, index) {
  if (!Array.isArray(history) || index < 0) return []
  return history.slice(0, index)
}

/** True when history[index] is a user turn we can retry/edit. */
export function isRetryableUserTurn(history, index) {
  if (!Array.isArray(history) || index < 0 || index >= history.length) return false
  const m = history[index]
  return !!m && m.role === 'user' && typeof m.content === 'string'
}
