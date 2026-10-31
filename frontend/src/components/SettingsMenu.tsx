import { useEffect, useRef, useState } from 'react'
import { useI18n } from '../i18n'
import { AppMode } from '../types'

const GEAR = 'M12 8.5a3.5 3.5 0 1 0 0 7 3.5 3.5 0 0 0 0-7z M12 3v2.5 M12 18.5V21 M4.2 7.5l2.2 1.3 M17.6 15.2l2.2 1.3 M4.2 16.5l2.2-1.3 M17.6 8.8l2.2-1.3'

/** Stroke paths for 24×24 icons (sliders / host / table / spark). */
const PAGE_ICONS: Record<string, string> = {
  prefs: 'M4 8h10 M18 8h2 M14 8a2 2 0 1 0 0-.01 M4 16h2 M10 16h10 M8 16a2 2 0 1 0 0-.01',
  connections: 'M7 4h10a1 1 0 0 1 1 1v5H6V5a1 1 0 0 1 1-1z M6 12h12v7a1 1 0 0 1-1 1H7a1 1 0 0 1-1-1v-7z M9 7h.01 M9 16h.01',
  query: 'M5 4h14a1 1 0 0 1 1 1v3H4V5a1 1 0 0 1 1-1z M4 10h16v9a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1v-9z M7 14h5 M7 17h8',
  settings: 'M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8z M12 2v2 M12 20v2 M4.9 4.9l1.4 1.4 M17.7 17.7l1.4 1.4 M2 12h2 M20 12h2 M4.9 19.1l1.4-1.4 M17.7 6.3l1.4-1.4',
}

const CHEVRON = 'M9 6l6 6-6 6'

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
        </div>
      )}
    </div>
  )
}
