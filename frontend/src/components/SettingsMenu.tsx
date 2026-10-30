import { useEffect, useRef, useState } from 'react'
import { useI18n } from '../i18n'
import { AppMode } from '../types'

const GEAR = 'M12 8.5a3.5 3.5 0 1 0 0 7 3.5 3.5 0 0 0 0-7z M12 3v2.5 M12 18.5V21 M4.2 7.5l2.2 1.3 M17.6 15.2l2.2 1.3 M4.2 16.5l2.2-1.3 M17.6 8.8l2.2-1.3'

const PAGE_ICONS: Record<string, string> = {
  prefs: 'M4 6h16v2H4z M4 11h10v2H4z M4 16h13v2H4z',
  connections: 'M6 4h12v7H6z M6 13h12v7H6z M9 7.5h.01 M9 16.5h.01',
  query: 'M4 5h16v3H4z M4 10h16v9H4z M7 13h4 M7 16h7',
  settings: 'M12 8.5a3.5 3.5 0 1 0 0 7 3.5 3.5 0 0 0 0-7z M12 3v2.5 M12 18.5V21 M4.2 7.5l2.2 1.3 M17.6 15.2l2.2 1.3 M4.2 16.5l2.2-1.3 M17.6 8.8l2.2-1.3',
}

interface Props {
  activePage: AppMode
  onPage: (page: AppMode) => void
}

export function SettingsMenu({ activePage, onPage }: Props) {
  const { t } = useI18n()
  const [open, setOpen] = useState(false)
  const root = useRef<HTMLDivElement>(null)

  const pages: { key: AppMode; label: string; icon: string }[] = [
    { key: 'prefs', label: t('menu.page.prefs'), icon: PAGE_ICONS.prefs },
    { key: 'connections', label: t('menu.page.connections'), icon: PAGE_ICONS.connections },
    { key: 'query', label: t('menu.page.query'), icon: PAGE_ICONS.query },
    { key: 'settings', label: t('menu.page.settings'), icon: PAGE_ICONS.settings },
  ]

  const gearActive = pages.some((p) => p.key === activePage)

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
          {pages.map((p) => (
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
                <svg width="13" height="13" viewBox="0 0 24 24" aria-hidden="true">
                  <path d={p.icon} fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" />
                </svg>
                <span>{p.label}</span>
              </span>
              <span className="sm-arrow">›</span>
            </button>
          ))}
        </div>
      )}
    </div>
  )
}
