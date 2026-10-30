import { useState } from 'react'
import { useI18n } from '../i18n'
import { getTheme, setTheme, type Theme } from '../theme'

export function PrefsView() {
  const { t, locale, setLocale } = useI18n()
  const [theme, setThemeState] = useState<Theme>(() => getTheme())

  function pickTheme(v: Theme) {
    setTheme(v)
    setThemeState(v)
  }

  return (
    <div className="panel">
      <header className="panel-head">
        <h2>{t('prefs.title')}</h2>
        <p>{t('prefs.headHint')}</p>
      </header>

      <section className="block">
        <div className="block-label">{t('prefs.appearance')}</div>
        <div className="stack">
          <div className="field">
            <div className="field-meta">
              <div className="field-title">{t('prefs.theme')}</div>
              <div className="field-desc">{t('prefs.theme.hint')}</div>
            </div>
            <div className="theme-picks" role="group" aria-label={t('prefs.theme')}>
              <button type="button" className={`theme-pick${theme === 'light' ? ' on' : ''}`} onClick={() => pickTheme('light')}>
                <span className="theme-swatch light" aria-hidden="true">
                  <span className="sw-bar" />
                  <span className="sw-line" />
                  <span className="sw-line short" />
                </span>
                <span className="theme-name">{t('prefs.theme.light')}</span>
              </button>
              <button type="button" className={`theme-pick${theme === 'dark' ? ' on' : ''}`} onClick={() => pickTheme('dark')}>
                <span className="theme-swatch dark" aria-hidden="true">
                  <span className="sw-bar" />
                  <span className="sw-line" />
                  <span className="sw-line short" />
                </span>
                <span className="theme-name">{t('prefs.theme.dark')}</span>
              </button>
            </div>
          </div>

          <div className="field">
            <div className="field-meta">
              <div className="field-title">{t('prefs.language')}</div>
              <div className="field-desc">{t('prefs.lang.hint')}</div>
            </div>
            <div className="pill-seg" role="group" aria-label={t('prefs.language')}>
              <button type="button" className={locale === 'zh' ? 'on' : ''} onClick={() => setLocale('zh')}>中文</button>
              <button type="button" className={locale === 'en' ? 'on' : ''} onClick={() => setLocale('en')}>English</button>
            </div>
          </div>
        </div>
      </section>
    </div>
  )
}
