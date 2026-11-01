import { useCallback, useEffect, useState } from 'react'
import type { CSSProperties, FormEvent } from 'react'
import { adminCreateUser, adminListUsers, type AdminUser } from '../lib/api'

// Web-only admin console: lists accounts and mints new ones. It is rendered by
// LoginView solely when the session role is "admin"; the desktop build never
// reaches it (main.tsx mounts <App/> directly). All calls go over the same
// admin-only HTTP endpoints, which the server also gates with RequireAdmin.

const styles: Record<string, CSSProperties> = {
  overlay: {
    position: 'fixed',
    inset: 0,
    zIndex: 1100,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    background: 'rgba(0,0,0,.5)',
    padding: 16,
  },
  card: {
    width: '100%',
    maxWidth: 520,
    maxHeight: '85vh',
    overflow: 'auto',
    padding: 24,
    borderRadius: 12,
    background: '#1b2129',
    boxShadow: '0 8px 30px rgba(0,0,0,.45)',
    color: '#e6e6e6',
    fontFamily: 'system-ui, -apple-system, Segoe UI, sans-serif',
  },
  head: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', margin: '0 0 4px' },
  title: { margin: 0, fontSize: 18, fontWeight: 700 },
  close: {
    border: '1px solid #2c3540',
    background: 'transparent',
    color: '#9aa4b2',
    borderRadius: 8,
    padding: '4px 10px',
    cursor: 'pointer',
    fontSize: 13,
  },
  sub: { margin: '0 0 16px', fontSize: 13, color: '#9aa4b2' },
  table: { width: '100%', borderCollapse: 'collapse', fontSize: 13, marginBottom: 20 },
  th: { textAlign: 'left', color: '#9aa4b2', fontWeight: 600, padding: '6px 8px', borderBottom: '1px solid #2c3540' },
  td: { padding: '6px 8px', borderBottom: '1px solid #222a34' },
  badge: {
    display: 'inline-block',
    padding: '1px 8px',
    borderRadius: 999,
    fontSize: 11,
    background: '#2c3540',
    color: '#cbd5e1',
  },
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
  row: { display: 'flex', gap: 10 },
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
  msg: { marginTop: 12, fontSize: 13 },
}

interface Props {
  onClose: () => void
}

export function AdminView({ onClose }: Props) {
  const [users, setUsers] = useState<AdminUser[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [role, setRole] = useState('user')
  const [creating, setCreating] = useState(false)

  const reload = useCallback(async () => {
    setLoading(true)
    try {
      setUsers(await adminListUsers())
      setError('')
    } catch (e: any) {
      setError(String(e?.message || e))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void reload()
  }, [reload])

  async function onCreate(e: FormEvent) {
    e.preventDefault()
    setNotice('')
    setError('')
    if (!username.trim() || !password) {
      setError('用户名和密码不能为空')
      return
    }
    setCreating(true)
    try {
      const res = await adminCreateUser(username.trim(), password, role)
      if (res.ok) {
        setNotice(`已创建用户 ${res.username}`)
        setUsername('')
        setPassword('')
        setRole('user')
        await reload()
      } else {
        setError(res.error || '创建失败')
      }
    } catch (e: any) {
      setError(String(e?.message || e))
    } finally {
      setCreating(false)
    }
  }

  return (
    <div style={styles.overlay} onClick={onClose}>
      <div style={styles.card} onClick={(e) => e.stopPropagation()}>
        <div style={styles.head}>
          <h1 style={styles.title}>用户管理</h1>
          <button type="button" style={styles.close} onClick={onClose}>
            关闭
          </button>
        </div>
        <p style={styles.sub}>服务端账号目录（仅管理员）。首个管理员由服务端环境变量引导创建。</p>

        {loading ? (
          <p style={styles.sub}>加载中…</p>
        ) : (
          <table style={styles.table}>
            <thead>
              <tr>
                <th style={styles.th}>用户名</th>
                <th style={styles.th}>角色</th>
              </tr>
            </thead>
            <tbody>
              {users.map((u) => (
                <tr key={u.id}>
                  <td style={styles.td}>{u.username}</td>
                  <td style={styles.td}>
                    <span style={styles.badge}>{u.role}</span>
                  </td>
                </tr>
              ))}
              {users.length === 0 && (
                <tr>
                  <td style={styles.td} colSpan={2}>
                    暂无用户
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        )}

        <form onSubmit={onCreate}>
          <h2 style={{ ...styles.title, fontSize: 15, margin: '0 0 4px' }}>新建用户</h2>
          <label style={styles.label} htmlFor="nu">
            用户名
          </label>
          <input
            id="nu"
            style={styles.input}
            value={username}
            autoComplete="off"
            onChange={(e) => setUsername(e.target.value)}
          />
          <label style={styles.label} htmlFor="np">
            密码
          </label>
          <input
            id="np"
            type="password"
            style={styles.input}
            value={password}
            autoComplete="new-password"
            onChange={(e) => setPassword(e.target.value)}
          />
          <label style={styles.label} htmlFor="nr">
            角色
          </label>
          <select
            id="nr"
            style={styles.input}
            value={role}
            onChange={(e) => setRole(e.target.value)}
          >
            <option value="user">user</option>
            <option value="admin">admin</option>
          </select>
          <button type="submit" style={styles.button} disabled={creating}>
            {creating ? '创建中…' : '创建用户'}
          </button>
        </form>

        {error && <div style={{ ...styles.msg, color: '#f87171' }}>{error}</div>}
        {notice && <div style={{ ...styles.msg, color: '#4ade80' }}>{notice}</div>}
      </div>
    </div>
  )
}
