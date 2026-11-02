import { useEffect, useRef, useState } from 'react'
import { useI18n } from '../i18n'
import { canOpenPage, navPagesForRole } from '../lib/navAccess'
import { AppMode } from '../types'

const GEAR = 'M12 8.5a3.5 3.5 0 1 0 0 7 3.5 3.5 0 0 0 0-7z M12 3v2.5 M12 18.5V21 M4.2 7.5l2.2 1.3 M17.6 15.2l2.2 1.3 M4.2 16.5l2.2-1.3 M17.6 8.8l2.2-1.3'

/** Stroke paths for 24×24 icons (sliders / host / table / spark). */
const PAGE_ICONS: Record<string, string> = {
  prefs: 'M4 8h10 M18 8h2 M14 8a2 2 0 1 0 0-.01 M4 16h2 M10 16h10 M8 16a2 2 0 1 0 0-.01',
  query: 'M5 4h14a1 1 0 0 1 1 1v3H4V5a1 1 0 0 1 1-1z M4 10h16v9a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1v-9z M7 14h5 M7 17h8',
  sys: 'M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8z M12 2v2 M12 20v2 M4.9 4.9l1.4 1.4 M17.7 17.7l1.4 1.4 M2 12h2 M20 12h2 M4.9 19.1l1.4-1.4 M17.7 6.3l1.4-1.4',
  profile: 'M12 12a4 4 0 1 0 0-8 4 4 0 0 0 0 8z M4 20c1.5-3.5 4.5-5 8-5s6.5 1.5 8 5',
  admin: 'M8 10V8a4 4 0 1 1 8 0v2 M6 10h12v10H6z',
  dba: 'M4 6h16v4H4z M4 14h16v4H4z M8 8v0.01 M8 16v0.01',
  perms: 'M12 3l8 4v5c0 5-3.5 8.5-8 10-4.5-1.5-8-5-8-10V7l8-4z',
  permApply: 'M12 3l8 4v5c0 5-3.5 8.5-8 10-4.5-1.5-8-5-8-10V7l8-4z M9 12h6 M12 9v6',
  logout: 'M10 4H6a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h4 M15 16l4-4-4-4 M10 12h9',
}

const CHEVRON = 'M9 6l6 6-6 6'

const LABEL_KEY: Partial<Record<AppMode, string>> = {
  prefs: 'menu.page.prefs',
  query: 'menu.page.query',
  sys: 'menu.page.sys',
  dba: 'menu.page.dba',
  perms: 'menu.page.perms',
  permApply: 'menu.page.permApply',
  admin: 'menu.page.admin',
  profile: 'menu.page.profile',
}

export interface AccountMenuProps {
  username: string
  role: string
  displayName: string
  avatar: string
  onLogout: () => void
}

interface Props {
  activePage: AppMode
  onPage: (page: AppMode) => void
  account?: AccountMenuProps
}

export function SettingsMenu({ activePage, onPage, account }: Props) {
  const { t } = useI18n()
  const [open, setOpen] = useState(false)
  const root = useRef<HTMLDivElement>(null)
  const role = account?.role || 'user'

  const pages: { key: AppMode; label: string; icon: string }[] = [
    { key: 'prefs', label: t('menu.page.prefs'), icon: PAGE_ICONS.prefs },
    ...navPagesForRole(role).map((key) => ({
      key,
      label: t(LABEL_KEY[key] || `menu.page.${key}`),
      icon: PAGE_ICONS[key] || PAGE_ICONS.query,
    })),
  ]

  const gearActive =
    pages.some((p) => p.key === activePage) ||
    activePage === 'profile' ||
    activePage === 'connections' ||
    activePage === 'settings'

  useEffect(() => {
    function onDoc(e: MouseEvent) {
      if (root.current && !root.current.contains(e.target as Node)) setOpen(false)
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onDoc)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDoc)
      document.removeEventListener('keydown', onKey)
    }
  }, [])

  const label = (account?.displayName || account?.username || '').trim()
  const letter = (label || '?').charAt(0).toUpperCase()

  return (
    <div className="sm-root" ref={root}>
      <button
        type="button"
        className={`sm-trigger${gearActive ? ' active' : ''}${open ? ' open' : ''}`}
        title={t('menu.gear')}
        onClick={() => setOpen((v) => !v)}
      >
        <svg width="15" height="15" viewBox="0 0 24 24" aria-hidden="true">
          <path d={GEAR} fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
        <span>{t('menu.gear')}</span>
      </button>

      {open && (
        <div className="sm-pop" role="menu">
          {pages.filter((p) => canOpenPage(role, p.key) || p.key === 'prefs').map((p) => (
            <button
              key={p.key}
              type="button"
              className={`sm-page${activePage === p.key ? ' on' : ''}`}
              onClick={() => {
                setOpen(false)
                onPage(p.key)
              }}
            >
              <span className="sm-page-label">
                <svg className="sm-page-ico" width="16" height="16" viewBox="0 0 24 24" aria-hidden="true">
                  <path d={p.icon} fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" />
                </svg>
                <span>{p.label}</span>
              </span>
              <svg className="sm-arrow" width="14" height="14" viewBox="0 0 24 24" aria-hidden="true">
                <path d={CHEVRON} fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
              </svg>
            </button>
          ))}

          {account && (
            <>
              <div className="sm-sep" role="separator" />
              <div className="sm-account-head">
                <span className="sm-account-avatar" aria-hidden>
                  {account.avatar ? (
                    <img src={account.avatar} alt="" />
                  ) : (
                    letter
                  )}
                </span>
                <span className="sm-account-meta">
                  <span className="sm-account-name">{label || account.username}</span>
                  <span className="sm-account-user">@{account.username}</span>
                </span>
              </div>
              <button
                type="button"
                className={`sm-page${activePage === 'profile' ? ' on' : ''}`}
                onClick={() => {
                  setOpen(false)
                  onPage('profile')
                }}
              >
                <span className="sm-page-label">
                  <svg className="sm-page-ico" width="16" height="16" viewBox="0 0 24 24" aria-hidden="true">
                    <path d={PAGE_ICONS.profile} fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" />
                  </svg>
                  <span>{t('menu.page.profile')}</span>
                </span>
                <svg className="sm-arrow" width="14" height="14" viewBox="0 0 24 24" aria-hidden="true">
                  <path d={CHEVRON} fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
                </svg>
              </button>
              <button
                type="button"
                className="sm-page sm-page-danger"
                onClick={() => {
                  setOpen(false)
                  account.onLogout()
                }}
              >
                <span className="sm-page-label">
                  <svg className="sm-page-ico" width="16" height="16" viewBox="0 0 24 24" aria-hidden="true">
                    <path d={PAGE_ICONS.logout} fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" />
                  </svg>
                  <span>{t('menu.page.logout')}</span>
                </span>
              </button>
            </>
          )}
        </div>
      )}
    </div>
  )
}
