import { useI18n } from '../i18n'
import { AppMode } from '../types'
import type { AccountMenuProps } from './SettingsMenu'
import { navPagesForRole } from '../lib/navAccess'
import logoSrc from '../assets/logo.svg'

interface Props {
  activePage: AppMode
  onPage: (page: AppMode) => void
  account?: AccountMenuProps
}

const LABEL_KEY: Partial<Record<AppMode, string>> = {
  query: 'menu.page.query',
  dba: 'menu.page.dba',
  perms: 'menu.page.perms',
  permApply: 'menu.page.permApply',
  admin: 'menu.page.admin',
  sys: 'menu.page.sys',
}

export function DbToolTopNav({ activePage, onPage, account }: Props) {
  const { t } = useI18n()
  const label = (account?.displayName || account?.username || '').trim()
  const role = account?.role || 'user'
  const letter = (label || account?.username || '?').charAt(0).toUpperCase()
  const links = navPagesForRole(role).map((key) => ({
    key,
    label: t(LABEL_KEY[key] || `menu.page.${key}`),
  }))

  return (
    <header className="dbtool-topnav">
      <button type="button" className="dbtool-brand" onClick={() => onPage('query')}>
        <img className="dbtool-brand-mark" src={logoSrc} alt="" />
        <span className="dbtool-brand-title">AI-DB</span>
      </button>

      <nav className="dbtool-nav" aria-label="DB tool">
        {links.map((l) => (
          <button
            key={l.key}
            type="button"
            className={`dbtool-nav-btn${activePage === l.key ? ' on' : ''}`}
            onClick={() => onPage(l.key)}
          >
            {l.label}
          </button>
        ))}
      </nav>

      {account && (
        <div className="dbtool-account">
          <button
            type="button"
            className={`dbtool-account-btn${activePage === 'profile' ? ' on' : ''}`}
            onClick={() => onPage('profile')}
            title={label || account.username}
          >
            <span className="dbtool-avatar" aria-hidden>
              {account.avatar ? <img src={account.avatar} alt="" /> : letter}
            </span>
            <span className="dbtool-account-name">{label || account.username}</span>
          </button>
          <button type="button" className="dbtool-logout" onClick={() => account.onLogout()}>
            {t('menu.page.logout')}
          </button>
        </div>
      )}
    </header>
  )
}
