import { useCallback, useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { getProfile, login, logout, session, type LoginResult, type SessionInfo } from '../lib/api'
import { useI18n } from '../i18n'
import logoSrc from '../assets/logo.svg'
import App from '../App'

// Web entry: gates on the HTTP auth session, then mounts the full <App/> once
// every bound method has an endpoint (Phase 3). The desktop build never renders
// this component; main.tsx picks <App/> directly when isDesktop() is true.

type Phase = 'checking' | 'login' | 'done'

function IconUser() {
  return (
    <svg className="login-field-icon" viewBox="0 0 24 24" aria-hidden>
      <path
        fill="none"
        stroke="currentColor"
        strokeWidth="1.7"
        strokeLinecap="round"
        strokeLinejoin="round"
        d="M20 21a8 8 0 0 0-16 0M12 13a4 4 0 1 0 0-8 4 4 0 0 0 0 8z"
      />
    </svg>
  )
}

function IconLock() {
  return (
    <svg className="login-field-icon" viewBox="0 0 24 24" aria-hidden>
      <path
        fill="none"
        stroke="currentColor"
        strokeWidth="1.7"
        strokeLinecap="round"
        strokeLinejoin="round"
        d="M7 11V8a5 5 0 0 1 10 0v3M6 11h12v10H6z"
      />
    </svg>
  )
}

function IconEye({ off }: { off?: boolean }) {
  if (off) {
    return (
      <svg viewBox="0 0 24 24" aria-hidden>
        <path
          fill="none"
          stroke="currentColor"
          strokeWidth="1.7"
          strokeLinecap="round"
          strokeLinejoin="round"
          d="M3 3l18 18M10.6 10.6a2 2 0 0 0 2.8 2.8M9.9 5.1A10.5 10.5 0 0 1 12 5c5 0 9.3 3.1 11 7-.5 1.2-1.2 2.3-2.1 3.2M6.1 6.1C4.2 7.4 2.7 9.1 2 12c1.7 3.9 6 7 11 7 1.4 0 2.7-.2 3.9-.7"
        />
      </svg>
    )
  }
  return (
    <svg viewBox="0 0 24 24" aria-hidden>
      <path
        fill="none"
        stroke="currentColor"
        strokeWidth="1.7"
        strokeLinecap="round"
        strokeLinejoin="round"
        d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z"
      />
      <circle cx="12" cy="12" r="3" fill="none" stroke="currentColor" strokeWidth="1.7" />
    </svg>
  )
}

function IconShield() {
  return (
    <svg viewBox="0 0 24 24" aria-hidden>
      <path
        fill="none"
        stroke="currentColor"
        strokeWidth="1.7"
        strokeLinecap="round"
        strokeLinejoin="round"
        d="M12 3l8 3v6c0 5-3.4 7.8-8 9-4.6-1.2-8-4-8-9V6l8-3z"
      />
    </svg>
  )
}

export function LoginView() {
  const { t } = useI18n()
  const [phase, setPhase] = useState<Phase>('checking')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [showPwd, setShowPwd] = useState(false)
  const [error, setError] = useState('')
  const [who, setWho] = useState('')
  const [role, setRole] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [avatar, setAvatar] = useState('')

  const enter = useCallback(async (name: string, userRole: string) => {
    setWho(name)
    setRole(userRole)
    setPhase('done')
    const p = await getProfile().catch(() => null)
    if (p) {
      setDisplayName(p.displayName || '')
      setAvatar(p.avatar || '')
      if (p.role) setRole(p.role)
    }
  }, [])

  useEffect(() => {
    let alive = true
    void (async () => {
      const s = await session().catch((): SessionInfo => ({ authenticated: false }))
      if (!alive) return
      if (s.authenticated) {
        await enter(s.username || '', s.role || '')
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
    const res = await login(username, password).catch((): LoginResult => ({
      ok: false,
      error: t('login.networkError'),
    }))
    if (res.ok) {
      await enter(res.username || username, res.role || '')
    } else {
      setError(res.error || t('login.failed'))
    }
  }

  async function onLogout() {
    await logout().catch(() => undefined)
    setWho('')
    setRole('')
    setDisplayName('')
    setAvatar('')
    setUsername('')
    setPassword('')
    setError('')
    setPhase('login')
  }

  if (phase === 'done') {
    return (
      <App
        account={{
          username: who,
          role,
          displayName,
          avatar,
          onLogout: () => void onLogout(),
          onProfileUpdated: (next) => {
            setDisplayName(next.displayName)
            setAvatar(next.avatar)
          },
        }}
      />
    )
  }

  return (
    <div className="login-page">
      <aside className="login-brand">
        <div className="login-logo">
          <img src={logoSrc} alt="" />
          <span>AI-DB</span>
        </div>

        <div className="login-brand-body">
          <span className="login-badge">{t('login.badge')}</span>
          <h1 className="login-headline">{t('login.headline')}</h1>
          <p className="login-desc">{t('login.desc')}</p>

          <div className="login-flow">
            <div className="login-flow-card">{t('login.flow.connect')}</div>
            <span className="login-flow-arrow" aria-hidden>
              →
            </span>
            <div className="login-flow-card">{t('login.flow.agent')}</div>
            <span className="login-flow-arrow" aria-hidden>
              →
            </span>
            <div className="login-flow-card">{t('login.flow.workbench')}</div>
          </div>
        </div>

        <div className="login-brand-foot">
          <IconShield />
          <span>{t('login.brand.foot')}</span>
        </div>
      </aside>

      <main className="login-panel">
        <div className="login-panel-inner">
          {phase === 'checking' && (
            <div className="login-checking">
              <h2>AI-DB</h2>
              <p>{t('login.checking')}</p>
            </div>
          )}

          {phase === 'login' && (
            <form className="login-form" onSubmit={onSubmit}>
              <p className="login-welcome">{t('login.welcome')}</p>
              <h2 className="login-form-title">{t('login.title')}</h2>

              <label className="login-label" htmlFor="u">
                <span className="login-req">*</span>
                {t('login.username')}
              </label>
              <div className="login-field">
                <IconUser />
                <input
                  id="u"
                  value={username}
                  autoComplete="username"
                  placeholder={t('login.username.ph')}
                  onChange={(e) => setUsername(e.target.value)}
                />
              </div>

              <label className="login-label" htmlFor="p">
                <span className="login-req">*</span>
                {t('login.password')}
              </label>
              <div className="login-field">
                <IconLock />
                <input
                  id="p"
                  type={showPwd ? 'text' : 'password'}
                  value={password}
                  autoComplete="current-password"
                  placeholder={t('login.password.ph')}
                  onChange={(e) => setPassword(e.target.value)}
                />
                <button
                  type="button"
                  className="login-eye ghost"
                  aria-label={showPwd ? t('login.hidePassword') : t('login.showPassword')}
                  onClick={() => setShowPwd((v) => !v)}
                >
                  <IconEye off={showPwd} />
                </button>
              </div>

              <button type="submit" className="login-submit primary">
                {t('login.submit')}
              </button>

              {error && <div className="login-error">{error}</div>}

              {import.meta.env.DEV && <div className="login-dev-hint">{t('login.devHint')}</div>}
            </form>
          )}
        </div>

        <div className="login-panel-foot">{t('login.foot')}</div>
      </main>
    </div>
  )
}
