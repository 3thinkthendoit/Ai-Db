import { useEffect, useRef, useState } from 'react'
import type { ChangeEvent, FormEvent } from 'react'
import { useTipToast } from '../components/TipToast'
import { useI18n } from '../i18n'
import { changePassword, getProfile, updateProfile } from '../lib/api'

const MAX_AVATAR_BYTES = 180_000

interface Props {
  onUpdated?: (next: { displayName: string; avatar: string }) => void
}

function readFileAsDataURL(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result || ''))
    reader.onerror = () => reject(new Error('read failed'))
    reader.readAsDataURL(file)
  })
}

export function ProfileView({ onUpdated }: Props) {
  const { t } = useI18n()
  const { showTip, tipNode } = useTipToast()
  const fileRef = useRef<HTMLInputElement>(null)

  const [loading, setLoading] = useState(true)
  const [username, setUsername] = useState('')
  const [role, setRole] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [avatar, setAvatar] = useState('')
  const [savingProfile, setSavingProfile] = useState(false)

  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [savingPassword, setSavingPassword] = useState(false)

  useEffect(() => {
    let alive = true
    void (async () => {
      try {
        const p = await getProfile()
        if (!alive) return
        setUsername(p.username || '')
        setRole(p.role || '')
        setDisplayName(p.displayName || '')
        setAvatar(p.avatar || '')
      } catch (e: any) {
        if (alive) showTip(String(e?.message || e), 'err')
      } finally {
        if (alive) setLoading(false)
      }
    })()
    return () => {
      alive = false
    }
  }, [showTip])

  async function onPickAvatar(e: ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    e.target.value = ''
    if (!file) return
    if (!/^image\/(png|jpe?g)$/i.test(file.type)) {
      showTip(t('profile.avatar.type'), 'err')
      return
    }
    if (file.size > MAX_AVATAR_BYTES) {
      showTip(t('profile.avatar.size'), 'err')
      return
    }
    try {
      const dataUrl = await readFileAsDataURL(file)
      setSavingProfile(true)
      const res = await updateProfile({ avatar: dataUrl })
      if (!res.ok) {
        showTip(res.error || t('profile.save.fail'), 'err')
        return
      }
      const nextAvatar = res.avatar || dataUrl
      setAvatar(nextAvatar)
      onUpdated?.({ displayName: res.displayName ?? displayName, avatar: nextAvatar })
      showTip(t('profile.avatar.saved'))
    } catch (err: any) {
      showTip(String(err?.message || err), 'err')
    } finally {
      setSavingProfile(false)
    }
  }

  async function clearAvatar() {
    setSavingProfile(true)
    try {
      const res = await updateProfile({ avatar: '' })
      if (!res.ok) {
        showTip(res.error || t('profile.save.fail'), 'err')
        return
      }
      setAvatar('')
      onUpdated?.({ displayName: res.displayName ?? displayName, avatar: '' })
      showTip(t('profile.avatar.cleared'))
    } catch (err: any) {
      showTip(String(err?.message || err), 'err')
    } finally {
      setSavingProfile(false)
    }
  }

  async function onSaveProfile(e: FormEvent) {
    e.preventDefault()
    setSavingProfile(true)
    try {
      const res = await updateProfile({ displayName: displayName.trim() })
      if (!res.ok) {
        showTip(res.error || t('profile.save.fail'), 'err')
        return
      }
      const nextName = res.displayName ?? displayName.trim()
      setDisplayName(nextName)
      onUpdated?.({ displayName: nextName, avatar: res.avatar ?? avatar })
      showTip(t('profile.saved'))
    } catch (err: any) {
      showTip(String(err?.message || err), 'err')
    } finally {
      setSavingProfile(false)
    }
  }

  async function onSavePassword(e: FormEvent) {
    e.preventDefault()
    if (newPassword.length < 6) {
      showTip(t('profile.password.short'), 'err')
      return
    }
    if (newPassword !== confirmPassword) {
      showTip(t('profile.password.mismatch'), 'err')
      return
    }
    setSavingPassword(true)
    try {
      const res = await changePassword(currentPassword, newPassword)
      if (!res.ok) {
        showTip(res.error || t('profile.password.fail'), 'err')
        return
      }
      setCurrentPassword('')
      setNewPassword('')
      setConfirmPassword('')
      showTip(t('profile.password.ok'))
    } catch (err: any) {
      showTip(String(err?.message || err), 'err')
    } finally {
      setSavingPassword(false)
    }
  }

  const letter = (displayName || username || '?').trim().charAt(0).toUpperCase() || '?'

  return (
    <div className="panel">
      {tipNode}
      <div className="panel-head">
        <div>
          <h2>{t('profile.title')}</h2>
          <p className="muted" style={{ margin: '6px 0 0', fontSize: 13, lineHeight: 1.55 }}>
            {t('profile.hint')}
          </p>
        </div>
      </div>

      {loading ? (
        <div className="card muted">{t('profile.loading')}</div>
      ) : (
        <>
          <form className="card" onSubmit={onSaveProfile} style={{ display: 'grid', gap: 14, maxWidth: 480 }}>
            <div className="profile-avatar-row">
              <button
                type="button"
                className="profile-avatar-btn"
                onClick={() => fileRef.current?.click()}
                disabled={savingProfile}
                title={t('profile.avatar.change')}
              >
                {avatar ? (
                  <img src={avatar} alt="" className="profile-avatar-img" />
                ) : (
                  <span className="profile-avatar-letter">{letter}</span>
                )}
              </button>
              <div className="profile-avatar-actions">
                <button type="button" className="sm" disabled={savingProfile} onClick={() => fileRef.current?.click()}>
                  {t('profile.avatar.change')}
                </button>
                {avatar && (
                  <button type="button" className="sm" disabled={savingProfile} onClick={() => void clearAvatar()}>
                    {t('profile.avatar.clear')}
                  </button>
                )}
                <p className="muted" style={{ margin: 0, fontSize: 12 }}>
                  {t('profile.avatar.hint')}
                </p>
              </div>
              <input
                ref={fileRef}
                type="file"
                accept="image/png,image/jpeg"
                hidden
                onChange={(e) => void onPickAvatar(e)}
              />
            </div>

            <label>
              {t('profile.username')}
              <input type="text" value={username} disabled readOnly />
            </label>
            <label>
              {t('profile.role')}
              <input type="text" value={role} disabled readOnly />
            </label>
            <label>
              {t('profile.displayName')}
              <input
                type="text"
                value={displayName}
                maxLength={64}
                placeholder={t('profile.displayName.ph')}
                onChange={(e) => setDisplayName(e.target.value)}
              />
            </label>
            <div>
              <button type="submit" className="primary" disabled={savingProfile}>
                {savingProfile ? t('profile.saving') : t('profile.save')}
              </button>
            </div>
          </form>

          <form className="card" onSubmit={onSavePassword} style={{ display: 'grid', gap: 14, maxWidth: 480, marginTop: 16 }}>
            <h3 style={{ margin: 0, fontSize: 15 }}>{t('profile.password.title')}</h3>
            <label>
              {t('profile.password.current')}
              <input
                type="password"
                autoComplete="current-password"
                value={currentPassword}
                onChange={(e) => setCurrentPassword(e.target.value)}
              />
            </label>
            <label>
              {t('profile.password.new')}
              <input
                type="password"
                autoComplete="new-password"
                value={newPassword}
                onChange={(e) => setNewPassword(e.target.value)}
              />
            </label>
            <label>
              {t('profile.password.confirm')}
              <input
                type="password"
                autoComplete="new-password"
                value={confirmPassword}
                onChange={(e) => setConfirmPassword(e.target.value)}
              />
            </label>
            <div>
              <button type="submit" className="primary" disabled={savingPassword}>
                {savingPassword ? t('profile.saving') : t('profile.password.save')}
              </button>
            </div>
          </form>
        </>
      )}
    </div>
  )
}
