import { useEffect, useState } from 'react'
import { useTipToast } from '../components/TipToast'
import { useI18n } from '../i18n'
import { getTheme, setTheme, type Theme } from '../theme'
import { LLMConfig } from '../types'
import { GetLLMConfig, SaveLLMSafety } from '../../wailsjs/go/main/App'

const DEFAULT_MAX_ROWS = 500
const DEFAULT_MAX_STEPS = 12

function clampPositive(n: number, fallback: number) {
  if (!Number.isFinite(n) || n <= 0) return fallback
  return Math.floor(n)
}

export function PrefsView() {
  const { t, locale, setLocale } = useI18n()
  const { showTip, tipNode } = useTipToast()
  const [theme, setThemeState] = useState<Theme>(() => getTheme())
  const [safety, setSafety] = useState({
    allowWrites: false,
    maxRows: DEFAULT_MAX_ROWS,
    maxAgentSteps: DEFAULT_MAX_STEPS,
  })
  const [safetyLoaded, setSafetyLoaded] = useState(false)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    void GetLLMConfig()
      .then((cfg) => {
        const c = cfg as unknown as LLMConfig
        setSafety({
          allowWrites: !!c.allowWrites,
          maxRows: clampPositive(Number(c.maxRows), DEFAULT_MAX_ROWS),
          maxAgentSteps: clampPositive(Number(c.maxAgentSteps), DEFAULT_MAX_STEPS),
        })
        setSafetyLoaded(true)
      })
      .catch((e) => {
        setSafetyLoaded(false)
        showTip(String(e?.message || e), 'err')
      })
  }, [showTip])

  function pickTheme(v: Theme) {
    setTheme(v)
    setThemeState(v)
  }

  async function saveSafety() {
    if (!safetyLoaded) {
      showTip(t('prefs.safety.loadFirst'), 'err')
      return
    }
    const maxRows = clampPositive(safety.maxRows, DEFAULT_MAX_ROWS)
    const maxAgentSteps = clampPositive(safety.maxAgentSteps, DEFAULT_MAX_STEPS)
    if (maxRows !== safety.maxRows || maxAgentSteps !== safety.maxAgentSteps) {
      setSafety({ ...safety, maxRows, maxAgentSteps })
    }
    setBusy(true)
    try {
      await SaveLLMSafety(safety.allowWrites, maxRows, maxAgentSteps)
      showTip(t('prefs.safety.saved'))
    } catch (e: any) {
      showTip(String(e?.message || e), 'err')
    } finally {
      setBusy(false)
    }
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

      <section className="block">
        <div className="block-label">{t('prefs.safety')}</div>
        <div className="stack">
          <div className="field">
            <div className="field-meta">
              <div className="field-title">{t('prefs.maxRows')}</div>
              <div className="field-desc">{t('prefs.maxRows.hint')}</div>
            </div>
            <input
              style={{ width: 120 }}
              type="number"
              min={1}
              disabled={!safetyLoaded}
              value={safety.maxRows}
              onChange={(e) => setSafety({ ...safety, maxRows: Number(e.target.value) })}
              onBlur={() => setSafety((s) => ({
                ...s,
                maxRows: clampPositive(s.maxRows, DEFAULT_MAX_ROWS),
              }))}
            />
          </div>
          <div className="field">
            <div className="field-meta">
              <div className="field-title">{t('prefs.maxSteps')}</div>
              <div className="field-desc">{t('prefs.maxSteps.hint')}</div>
            </div>
            <input
              style={{ width: 120 }}
              type="number"
              min={1}
              disabled={!safetyLoaded}
              value={safety.maxAgentSteps}
              onChange={(e) => setSafety({ ...safety, maxAgentSteps: Number(e.target.value) })}
              onBlur={() => setSafety((s) => ({
                ...s,
                maxAgentSteps: clampPositive(s.maxAgentSteps, DEFAULT_MAX_STEPS),
              }))}
            />
          </div>
          <div className="field">
            <label className="checkbox-row">
              <input
                type="checkbox"
                disabled={!safetyLoaded}
                checked={safety.allowWrites}
                onChange={(e) => setSafety({ ...safety, allowWrites: e.target.checked })}
              />
              {t('prefs.allowWrites')}
            </label>
          </div>
        </div>
        <button
          type="button"
          className="primary"
          style={{ marginTop: 14 }}
          disabled={busy || !safetyLoaded}
          onClick={() => void saveSafety()}
        >
          {t('prefs.saveSafety')}
        </button>
        {tipNode}
      </section>
    </div>
  )
}
