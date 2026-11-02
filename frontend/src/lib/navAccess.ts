import type { AppMode } from '../types'

/** Top-nav / settings pages allowed for a session role (web). */
export function navPagesForRole(role: string): AppMode[] {
  const r = (role || 'user').toLowerCase()
  if (r === 'admin') {
    return ['query', 'dba', 'perms', 'admin', 'sys']
  }
  if (r === 'dba') {
    return ['query', 'dba', 'perms', 'admin']
  }
  // ordinary user
  return ['query', 'dba', 'permApply']
}

export function canOpenPage(role: string, page: AppMode): boolean {
  if (page === 'work' || page === 'profile' || page === 'prefs') return true
  // legacy aliases: connections / LLM settings live under sys for admin
  if (page === 'connections' || page === 'settings') {
    return (role || '').toLowerCase() === 'admin'
  }
  return navPagesForRole(role).includes(page)
}
