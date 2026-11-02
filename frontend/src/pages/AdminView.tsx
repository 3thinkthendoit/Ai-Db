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
  role: string
}

const emptyCreate = (): CreateForm => ({ username: '', password: '', role: 'user' })

interface Props {
  /** Current session username — used to disable self-ban in the UI. */
  selfUsername: string
}

export function AdminView({ selfUsername }: Props) {
  const { t } = useI18n()
  const { showTip, tipNode } = useTipToast()
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
        createForm.role
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

  return (
    <div className="panel conn-panel">
      <header className="panel-head panel-head-row">
        <div>
          <h2>{t('admin.title')}</h2>
          <p>{t('admin.hint')}</p>
        </div>
        <button type="button" className="primary" onClick={startNew}>
          {t('admin.add')}
        </button>
      </header>

      <div className="conn-list-cards">
        {users.length === 0 && <div className="card muted">{t('admin.empty')}</div>}
        {users.map((u) => {
          const isSelf = u.username === selfUsername
          const title = u.displayName?.trim() || u.username
          return (
            <div key={u.id} className="card conn-card">
              <div className="conn-card-main">
                <div className="conn-card-name">
                  {title}
                  <span className="tag">{u.role}</span>
                  {u.banned && <span className="tag danger">{t('admin.tag.banned')}</span>}
                  {isSelf && <span className="tag ok">{t('admin.tag.self')}</span>}
                </div>
                <div className="conn-card-addr mono">
                  @{u.username}
                  {u.displayName?.trim() ? ` · ${u.displayName}` : ''}
                </div>
              </div>
              <div className="conn-card-actions">
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
            </div>
          )
        })}
      </div>

      {tipNode}

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
