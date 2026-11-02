import { useCallback, useEffect, useState } from 'react'
import { BodyPortal } from '../lib/portal'
import { useTipToast } from '../components/TipToast'
import { useI18n } from '../i18n'
import {
  adminCreateUser,
  adminListUsers,
  adminSetPassword,
  adminUpdateUser,
  type AdminUser,
} from '../lib/api'

// Web-only admin console: card list like LLM settings. Opened as App mode
// "admin" from the bottom-left Settings menu (admin role only).

type EditForm = {
  id: string
  username: string
  displayName: string
  email: string
  phone: string
  role: string
}

type PasswordForm = {
  id: string
  username: string
  password: string
  confirm: string
}

type CreateForm = {
  username: string
  password: string
  email: string
  phone: string
  role: string
}

const emptyCreate = (): CreateForm => ({
  username: '',
  password: '',
  email: '',
  phone: '',
  role: 'user',
})

interface Props {
  /** Current session username — used to disable self-ban in the UI. */
  selfUsername: string
}

type Tab = 'users' | 'roles'

export function AdminView({ selfUsername }: Props) {
  const { t } = useI18n()
  const { showTip, tipNode } = useTipToast()
  const [tab, setTab] = useState<Tab>('users')
  const [users, setUsers] = useState<AdminUser[]>([])
  const [busy, setBusy] = useState(false)
  const [creating, setCreating] = useState(false)
  const [createForm, setCreateForm] = useState<CreateForm>(emptyCreate)
  const [createErr, setCreateErr] = useState('')
  const [editing, setEditing] = useState<EditForm | null>(null)
  const [editErr, setEditErr] = useState('')
  const [pwdForm, setPwdForm] = useState<PasswordForm | null>(null)
  const [pwdErr, setPwdErr] = useState('')
  const [banTarget, setBanTarget] = useState<AdminUser | null>(null)

  const reload = useCallback(async () => {
    setUsers(await adminListUsers())
  }, [])

  useEffect(() => {
    void reload().catch((e) => showTip(String(e?.message || e), 'err'))
  }, [reload, showTip])

  function startNew() {
    setCreateForm(emptyCreate())
    setCreateErr('')
    setCreating(true)
  }

  function startEdit(u: AdminUser) {
    setEditing({
      id: u.id,
      username: u.username,
      displayName: u.displayName || '',
      email: u.email || '',
      phone: u.phone || '',
      role: u.role,
    })
    setEditErr('')
  }

  function startPassword(u: AdminUser) {
    setPwdForm({ id: u.id, username: u.username, password: '', confirm: '' })
    setPwdErr('')
  }

  async function saveCreate() {
    setBusy(true)
    setCreateErr('')
    try {
      const res = await adminCreateUser(
        createForm.username.trim(),
        createForm.password,
        createForm.role,
        { email: createForm.email.trim(), phone: createForm.phone.trim() }
      )
      if (!res.ok) {
        setCreateErr(res.error || t('admin.create.fail'))
        return
      }
      setCreating(false)
      await reload()
      showTip(t('admin.create.ok', { name: res.username || createForm.username }))
    } catch (e: any) {
      setCreateErr(String(e?.message || e))
    } finally {
      setBusy(false)
    }
  }

  async function saveEdit() {
    if (!editing) return
    setBusy(true)
    setEditErr('')
    try {
      const res = await adminUpdateUser(editing.id, {
        displayName: editing.displayName.trim(),
        email: editing.email.trim(),
        phone: editing.phone.trim(),
        role: editing.role,
      })
      if (!res.ok) {
        setEditErr(res.error || t('admin.save.fail'))
        return
      }
      setEditing(null)
      await reload()
      showTip(t('admin.saved'))
    } catch (e: any) {
      setEditErr(String(e?.message || e))
    } finally {
      setBusy(false)
    }
  }

  async function savePassword() {
    if (!pwdForm) return
    if (pwdForm.password.length < 6) {
      setPwdErr(t('admin.password.short'))
      return
    }
    if (pwdForm.password !== pwdForm.confirm) {
      setPwdErr(t('admin.password.mismatch'))
      return
    }
    setBusy(true)
    setPwdErr('')
    try {
      const res = await adminSetPassword(pwdForm.id, pwdForm.password)
      if (!res.ok) {
        setPwdErr(res.error || t('admin.password.fail'))
        return
      }
      setPwdForm(null)
      showTip(t('admin.password.ok'))
    } catch (e: any) {
      setPwdErr(String(e?.message || e))
    } finally {
      setBusy(false)
    }
  }

  async function confirmBanToggle() {
    if (!banTarget) return
    const nextBanned = !banTarget.banned
    setBusy(true)
    try {
      const res = await adminUpdateUser(banTarget.id, { banned: nextBanned })
      if (!res.ok) {
        showTip(res.error || t('admin.ban.fail'), 'err')
        return
      }
      setBanTarget(null)
      await reload()
      showTip(nextBanned ? t('admin.ban.ok') : t('admin.unban.ok'))
    } catch (e: any) {
      showTip(String(e?.message || e), 'err')
    } finally {
      setBusy(false)
    }
  }

  const sideItems: { key: Tab; label: string; icon: string }[] = [
    {
      key: 'users',
      label: t('admin.tab.users'),
      icon: 'M12 12a4 4 0 1 0 0-8 4 4 0 0 0 0 8z M4 20c1.5-3.5 4.5-5 8-5s6.5 1.5 8 5',
    },
    {
      key: 'roles',
      label: t('admin.tab.roles'),
      icon: 'M12 3l8 4v5c0 5-3.5 8.5-8 10-4.5-1.5-8-5-8-10V7l8-4z',
    },
  ]

  const roleCards = [
    { key: 'admin', name: t('admin.roles.admin.name'), desc: t('admin.roles.admin.desc') },
    { key: 'dba', name: t('admin.roles.dba.name'), desc: t('admin.roles.dba.desc') },
    { key: 'user', name: t('admin.roles.user.name'), desc: t('admin.roles.user.desc') },
  ]

  return (
    <div className="sub-layout">
      {tipNode}
      <aside className="sub-side" aria-label={t('admin.title')}>
        <div className="sub-side-head">
          <strong>{t('admin.title')}</strong>
          <span>{t('admin.hint')}</span>
        </div>
        <nav className="sub-side-nav">
          {sideItems.map((item) => (
            <button
              key={item.key}
              type="button"
              className={`sub-side-btn${tab === item.key ? ' on' : ''}`}
              onClick={() => setTab(item.key)}
            >
              <svg className="sub-side-ico" viewBox="0 0 24 24" aria-hidden>
                <path d={item.icon} fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" />
              </svg>
              <span>{item.label}</span>
            </button>
          ))}
        </nav>
      </aside>

      <div className="sub-main">
        {tab === 'users' && (
          <>
            <div className="data-table-toolbar">
              <div>
                <h2>{t('admin.tab.users')}</h2>
                <p>{t('admin.hint')}</p>
              </div>
              <button type="button" className="primary" onClick={startNew}>
                {t('admin.add')}
              </button>
            </div>
            <div className="data-table-wrap">
              {users.length === 0 ? (
                <div className="data-table-empty">{t('admin.empty')}</div>
              ) : (
                <table className="data-table">
                  <thead>
                    <tr>
                      <th style={{ width: '14%' }}>{t('admin.col.user')}</th>
                      <th style={{ width: '14%' }}>{t('admin.col.email')}</th>
                      <th style={{ width: '10%' }}>{t('admin.col.phone')}</th>
                      <th style={{ width: '8%' }}>{t('admin.col.role')}</th>
                      <th style={{ width: '8%' }}>{t('admin.col.status')}</th>
                      <th style={{ width: '12%' }}>{t('admin.col.created')}</th>
                      <th style={{ width: '12%' }}>{t('admin.col.active')}</th>
                      <th style={{ width: '22%' }}>{t('admin.col.actions')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {users.map((u) => {
                      const isSelf = u.username === selfUsername
                      const title = u.displayName?.trim() || u.username
                      return (
                        <tr key={u.id}>
                          <td>
                            <div className="cell-main">
                              {title}
                              {isSelf && <span className="pill">{t('admin.tag.self')}</span>}
                            </div>
                            <div className="cell-sub mono">@{u.username}</div>
                          </td>
                          <td>
                            <div className="cell-muted">{u.email || '—'}</div>
                          </td>
                          <td>
                            <div className="cell-muted mono">{u.phone || '—'}</div>
                          </td>
                          <td>
                            <span className="tag">{u.role}</span>
                          </td>
                          <td>
                            {u.banned ? (
                              <span className="tag danger">{t('admin.status.banned')}</span>
                            ) : (
                              <span className="tag ok">{t('admin.status.active')}</span>
                            )}
                          </td>
                          <td>
                            <div className="cell-muted">{u.createdAt || '—'}</div>
                          </td>
                          <td>
                            <div className="cell-muted">
                              {u.lastLoginAt || t('admin.neverLogin')}
                            </div>
                          </td>
                          <td>
                            <div className="cell-actions">
                              <button type="button" className="sm" disabled={busy} onClick={() => startEdit(u)}>
                                {t('admin.edit')}
                              </button>
                              <button type="button" className="sm" disabled={busy} onClick={() => startPassword(u)}>
                                {t('admin.password')}
                              </button>
                              <button
                                type="button"
                                className={`sm${u.banned ? '' : ' danger'}`}
                                disabled={busy || isSelf}
                                title={isSelf ? t('admin.ban.self') : undefined}
                                onClick={() => setBanTarget(u)}
                              >
                                {u.banned ? t('admin.unban') : t('admin.ban')}
                              </button>
                            </div>
                          </td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              )}
            </div>
          </>
        )}

        {tab === 'roles' && (
          <>
            <div className="data-table-toolbar">
              <div>
                <h2>{t('admin.tab.roles')}</h2>
                <p>{t('admin.roles.hint')}</p>
              </div>
            </div>
            <div className="data-table-wrap">
              <table className="data-table">
                <thead>
                  <tr>
                    <th style={{ width: '22%' }}>{t('admin.col.roleName')}</th>
                    <th style={{ width: '14%' }}>{t('admin.col.roleId')}</th>
                    <th>{t('admin.col.roleDesc')}</th>
                  </tr>
                </thead>
                <tbody>
                  {roleCards.map((r) => (
                    <tr key={r.key}>
                      <td>
                        <div className="cell-main">{r.name}</div>
                      </td>
                      <td>
                        <span className="pill" style={{ marginLeft: 0 }}>{r.key}</span>
                      </td>
                      <td>
                        <div className="cell-muted">{r.desc}</div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </>
        )}
      </div>

      {creating && (
        <BodyPortal>
          <div className="modal-backdrop" onClick={() => !busy && setCreating(false)}>
            <div className="modal card" onClick={(e) => e.stopPropagation()} style={{ maxWidth: 440 }}>
              <div className="modal-head">
                <h2>{t('admin.createTitle')}</h2>
                <button
                  type="button"
                  className="modal-close"
                  disabled={busy}
                  title={t('modal.close')}
                  aria-label={t('modal.close')}
                  onClick={() => setCreating(false)}
                >
                  ×
                </button>
              </div>
              <label>
                {t('admin.username')}
                <input
                  value={createForm.username}
                  autoComplete="off"
                  onChange={(e) => setCreateForm({ ...createForm, username: e.target.value })}
                />
              </label>
              <label>
                {t('admin.passwordNew')}
                <input
                  type="password"
                  autoComplete="new-password"
                  value={createForm.password}
                  onChange={(e) => setCreateForm({ ...createForm, password: e.target.value })}
                />
              </label>
              <label>
                {t('admin.email')}
                <input
                  type="email"
                  autoComplete="off"
                  value={createForm.email}
                  maxLength={128}
                  placeholder={t('admin.email.ph')}
                  onChange={(e) => setCreateForm({ ...createForm, email: e.target.value })}
                />
              </label>
              <label>
                {t('admin.phone')}
                <input
                  autoComplete="off"
                  value={createForm.phone}
                  maxLength={32}
                  placeholder={t('admin.phone.ph')}
                  onChange={(e) => setCreateForm({ ...createForm, phone: e.target.value })}
                />
              </label>
              <label>
                {t('admin.role')}
                <select
                  value={createForm.role}
                  onChange={(e) => setCreateForm({ ...createForm, role: e.target.value })}
                >
                  <option value="user">user</option>
                  <option value="dba">dba</option>
                  <option value="admin">admin</option>
                </select>
              </label>
              {createErr && <div className="error-text">{createErr}</div>}
              <div className="modal-actions">
                <button type="button" disabled={busy} onClick={() => setCreating(false)}>
                  {t('admin.cancel')}
                </button>
                <button type="button" className="primary" disabled={busy} onClick={() => void saveCreate()}>
                  {busy ? t('admin.saving') : t('admin.create')}
                </button>
              </div>
            </div>
          </div>
        </BodyPortal>
      )}

      {editing && (
        <BodyPortal>
          <div className="modal-backdrop" onClick={() => !busy && setEditing(null)}>
            <div className="modal card" onClick={(e) => e.stopPropagation()} style={{ maxWidth: 440 }}>
              <div className="modal-head">
                <h2>{t('admin.editTitle')}</h2>
                <button
                  type="button"
                  className="modal-close"
                  disabled={busy}
                  title={t('modal.close')}
                  aria-label={t('modal.close')}
                  onClick={() => setEditing(null)}
                >
                  ×
                </button>
              </div>
              <label>
                {t('admin.username')}
                <input type="text" value={editing.username} disabled readOnly />
              </label>
              <label>
                {t('admin.displayName')}
                <input
                  value={editing.displayName}
                  maxLength={64}
                  placeholder={t('admin.displayName.ph')}
                  onChange={(e) => setEditing({ ...editing, displayName: e.target.value })}
                />
              </label>
              <label>
                {t('admin.email')}
                <input
                  type="email"
                  autoComplete="off"
                  value={editing.email}
                  maxLength={128}
                  placeholder={t('admin.email.ph')}
                  onChange={(e) => setEditing({ ...editing, email: e.target.value })}
                />
              </label>
              <label>
                {t('admin.phone')}
                <input
                  autoComplete="off"
                  value={editing.phone}
                  maxLength={32}
                  placeholder={t('admin.phone.ph')}
                  onChange={(e) => setEditing({ ...editing, phone: e.target.value })}
                />
              </label>
              <label>
                {t('admin.role')}
                <select
                  value={editing.role}
                  onChange={(e) => setEditing({ ...editing, role: e.target.value })}
                >
                  <option value="user">user</option>
                  <option value="dba">dba</option>
                  <option value="admin">admin</option>
                </select>
              </label>
              {editErr && <div className="error-text">{editErr}</div>}
              <div className="modal-actions">
                <button type="button" disabled={busy} onClick={() => setEditing(null)}>
                  {t('admin.cancel')}
                </button>
                <button type="button" className="primary" disabled={busy} onClick={() => void saveEdit()}>
                  {busy ? t('admin.saving') : t('admin.save')}
                </button>
              </div>
            </div>
          </div>
        </BodyPortal>
      )}

      {pwdForm && (
        <BodyPortal>
          <div className="modal-backdrop" onClick={() => !busy && setPwdForm(null)}>
            <div className="modal card" onClick={(e) => e.stopPropagation()} style={{ maxWidth: 440 }}>
              <div className="modal-head">
                <h2>{t('admin.passwordTitle')}</h2>
                <button
                  type="button"
                  className="modal-close"
                  disabled={busy}
                  title={t('modal.close')}
                  aria-label={t('modal.close')}
                  onClick={() => setPwdForm(null)}
                >
                  ×
                </button>
              </div>
              <p className="muted" style={{ margin: '0 0 8px', fontSize: 13 }}>
                {t('admin.passwordHint', { name: pwdForm.username })}
              </p>
              <label>
                {t('admin.passwordNew')}
                <input
                  type="password"
                  autoComplete="new-password"
                  value={pwdForm.password}
                  onChange={(e) => setPwdForm({ ...pwdForm, password: e.target.value })}
                />
              </label>
              <label>
                {t('admin.passwordConfirm')}
                <input
                  type="password"
                  autoComplete="new-password"
                  value={pwdForm.confirm}
                  onChange={(e) => setPwdForm({ ...pwdForm, confirm: e.target.value })}
                />
              </label>
              {pwdErr && <div className="error-text">{pwdErr}</div>}
              <div className="modal-actions">
                <button type="button" disabled={busy} onClick={() => setPwdForm(null)}>
                  {t('admin.cancel')}
                </button>
                <button type="button" className="primary" disabled={busy} onClick={() => void savePassword()}>
                  {busy ? t('admin.saving') : t('admin.passwordSave')}
                </button>
              </div>
            </div>
          </div>
        </BodyPortal>
      )}

      {banTarget && (
        <BodyPortal>
          <div className="modal-backdrop" onClick={() => !busy && setBanTarget(null)}>
            <div className="modal card" onClick={(e) => e.stopPropagation()} style={{ maxWidth: 400 }}>
              <div className="modal-head">
                <h3>{banTarget.banned ? t('admin.unbanTitle') : t('admin.banTitle')}</h3>
                <button
                  type="button"
                  className="modal-close"
                  disabled={busy}
                  title={t('modal.close')}
                  aria-label={t('modal.close')}
                  onClick={() => setBanTarget(null)}
                >
                  ×
                </button>
              </div>
              <p style={{ margin: '0 0 16px', fontSize: 13, lineHeight: 1.55, color: 'var(--text-2)' }}>
                {banTarget.banned
                  ? t('admin.unbanConfirm', { name: banTarget.username })
                  : t('admin.banConfirm', { name: banTarget.username })}
              </p>
              <div className="modal-actions">
                <button type="button" className="sm" disabled={busy} onClick={() => setBanTarget(null)}>
                  {t('admin.cancel')}
                </button>
                <button
                  type="button"
                  className={`sm${banTarget.banned ? ' primary' : ' danger'}`}
                  disabled={busy}
                  onClick={() => void confirmBanToggle()}
                >
                  {banTarget.banned ? t('admin.unban') : t('admin.ban')}
                </button>
              </div>
            </div>
          </div>
        </BodyPortal>
      )}
    </div>
  )
}
