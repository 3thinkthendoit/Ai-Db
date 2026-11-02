import { useI18n } from '../i18n'
import { AppMode } from '../types'
import type { AccountMenuProps } from './SettingsMenu'
import logoSrc from '../assets/logo.svg'

interface Props {
  activePage: AppMode
  onPage: (page: AppMode) => void
  account?: AccountMenuProps
}

export function DbToolTopNav({ activePage, onPage, account }: Props) {
  const { t } = useI18n()
  const label = (account?.displayName || account?.username || '').trim()

  const links: { key: AppMode; label: string }[] = [
    { key: 'query', label: t('menu.page.query') },
    { key: 'connections', label: t('menu.page.connections') },
    { key: 'settings', label: t('menu.page.settings') },
    { key: 'prefs', label: t('menu.page.prefs') },
  ]
  if (account?.role === 'dba') {
    links.push({ key: 'dba', label: t('menu.page.dba') })
  }
  if (account?.role === 'admin') {
    links.push({ key: 'admin', label: t('menu.page.admin') })
  }

  return (
    <header className="dbtool-topnav">
      <button type="button" className="dbtool-brand" onClick={() => onPage('query')}>
        <img className="dbtool-brand-mark" src={logoSrc} alt="" />
        <span className="dbtool-brand-title">AI-DB</span>
        <span className="dbtool-brand-tag">{t('prefs.uiEdition.dbtool')}</span>
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
            className={`dbtool-nav-btn${activePage === 'profile' ? ' on' : ''}`}
            onClick={() => onPage('profile')}
          >
            {label || account.username}
          </button>
          <button type="button" className="dbtool-nav-btn danger" onClick={() => account.onLogout()}>
            {t('menu.page.logout')}
          </button>
        </div>
      )}
    </header>
  )
}
