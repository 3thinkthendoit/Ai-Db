import { useCallback, useEffect, useState } from 'react'
import type { CSSProperties, FormEvent } from 'react'
import { login, logout, session, type LoginResult, type SessionInfo } from '../lib/api'
import App from '../App'
import { AdminView } from './AdminView'

// Web entry: gates on the HTTP auth session, then mounts the full <App/> once
// every bound method has an endpoint (Phase 3). The desktop build never renders
// this component; main.tsx picks <App/> directly when isDesktop() is true.

type Phase = 'checking' | 'login' | 'done'

const styles: Record<string, CSSProperties> = {
  wrap: {
    minHeight: '100vh',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    background: '#12161c',
    color: '#e6e6e6',
    fontFamily: 'system-ui, -apple-system, Segoe UI, sans-serif',
    padding: 16,
  },
  card: {
    width: '100%',
    maxWidth: 420,
    padding: 24,
    borderRadius: 12,
    background: '#1b2129',
    boxShadow: '0 8px 30px rgba(0,0,0,.45)',
  },
  title: { margin: '0 0 4px', fontSize: 20, fontWeight: 700 },
  sub: { margin: '0 0 20px', fontSize: 13, color: '#9aa4b2' },
  label: { display: 'block', fontSize: 12, color: '#9aa4b2', margin: '12px 0 4px' },
  input: {
    width: '100%',
    boxSizing: 'border-box',
    padding: '9px 10px',
    borderRadius: 8,
    border: '1px solid #2c3540',
    background: '#12161c',
    color: '#e6e6e6',
    fontSize: 14,
  },
  button: {
    marginTop: 18,
    width: '100%',
    padding: '10px 12px',
    borderRadius: 8,
    border: 'none',
    background: '#3b82f6',
    color: 'white',
    fontSize: 14,
    fontWeight: 600,
    cursor: 'pointer',
  },
  ghost: {
    marginTop: 10,
    width: '100%',
    padding: '9px 12px',
    borderRadius: 8,
    border: '1px solid #2c3540',
    background: 'transparent',
    color: '#9aa4b2',
    fontSize: 13,
    cursor: 'pointer',
  },
  error: { marginTop: 12, fontSize: 13, color: '#f87171' },
  hint: { marginTop: 16, fontSize: 12, color: '#6b7280', lineHeight: 1.6 },
  fabRow: {
    position: 'fixed',
    top: 10,
    right: 12,
    zIndex: 1000,
    display: 'flex',
    gap: 8,
  },
  fabBtn: {
    padding: '6px 12px',
    borderRadius: 8,
    border: '1px solid #2c3540',
    background: 'rgba(27,33,41,.92)',
    color: '#9aa4b2',
    fontSize: 12,
    cursor: 'pointer',
  },
}

export function LoginView() {
  const [phase, setPhase] = useState<Phase>('checking')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [who, setWho] = useState('')
  const [role, setRole] = useState('')
  const [showAdmin, setShowAdmin] = useState(false)

  const enter = useCallback((name: string, userRole: string) => {
    setWho(name)
    setRole(userRole)
    setPhase('done')
  }, [])

  // On load, resume an existing session if the cookie is still valid.
  useEffect(() => {
    let alive = true
    void (async () => {
      const s = await session().catch((): SessionInfo => ({ authenticated: false }))
      if (!alive) return
      if (s.authenticated) {
        enter(s.username || '', s.role || '')
      } else {
        setPhase('login')
      }
    })()
    return () => {
      alive = false
    }
  }, [enter])

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setError('')
    const res = await login(username, password).catch((): LoginResult => ({ ok: false, error: '网络错误' }))
    if (res.ok) {
      enter(res.username || username, res.role || '')
    } else {
      setError(res.error || '登录失败')
    }
  }

  async function onLogout() {
    await logout().catch(() => undefined)
    setWho('')
    setRole('')
    setShowAdmin(false)
    setUsername('')
    setPassword('')
    setError('')
    setPhase('login')
  }

  // Authenticated: mount the full app (all 21 methods go over HTTP) with a
  // floating logout control. The desktop build reaches <App/> via main.tsx.
  if (phase === 'done') {
    return (
      <>
        <App />
        <div style={styles.fabRow}>
          {role === 'admin' && (
            <button type="button" style={styles.fabBtn} onClick={() => setShowAdmin(true)}>
              用户管理
            </button>
          )}
          <button type="button" style={styles.fabBtn} onClick={onLogout}>
            退出登录{who ? `（${who}）` : ''}
          </button>
        </div>
        {showAdmin && role === 'admin' && <AdminView onClose={() => setShowAdmin(false)} />}
      </>
    )
  }

  return (
    <div style={styles.wrap}>
      <div style={styles.card}>
        {phase === 'checking' && (
          <>
            <h1 style={styles.title}>AI-DB</h1>
            <p style={styles.sub}>正在检查会话…</p>
          </>
        )}

        {phase === 'login' && (
          <form onSubmit={onSubmit}>
            <h1 style={styles.title}>登录 AI-DB</h1>
            <p style={styles.sub}>服务端版 · 账号密码登录</p>
            <label style={styles.label} htmlFor="u">
              用户名
            </label>
            <input
              id="u"
              style={styles.input}
              value={username}
              autoComplete="username"
              onChange={(e) => setUsername(e.target.value)}
            />
            <label style={styles.label} htmlFor="p">
              密码
            </label>
            <input
              id="p"
              type="password"
              style={styles.input}
              value={password}
              autoComplete="current-password"
              onChange={(e) => setPassword(e.target.value)}
            />
            <button type="submit" style={styles.button}>
              登录
            </button>
            {error && <div style={styles.error}>{error}</div>}
            {import.meta.env.DEV && (
              <div style={styles.hint}>
                开发提示：服务端设 AIDB_SEED_DEMO=true 会种子演示账号 alice / bob，
                <br />
                口令取自 AIDB_SEED_*_PWD 环境变量或服务端启动日志。
              </div>
            )}
          </form>
        )}
      </div>
    </div>
  )
}
