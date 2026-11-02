/** Mirrors navAccess.ts for node specs. */

export function navPagesForRole(role) {
  const r = (role || 'user').toLowerCase()
  if (r === 'admin') {
    return ['query', 'dba', 'perms', 'admin', 'sys']
  }
  if (r === 'dba') {
    return ['query', 'dba', 'perms', 'admin']
  }
  return ['query', 'dba', 'permApply']
}

export function canOpenPage(role, page) {
  if (page === 'work' || page === 'profile' || page === 'prefs') return true
  if (page === 'connections' || page === 'settings') {
    return (role || '').toLowerCase() === 'admin'
  }
  return navPagesForRole(role).includes(page)
}
