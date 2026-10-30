import { createContext, useContext, useMemo, useState, useCallback, type ReactNode } from 'react'
import { createElement } from 'react'
import { dicts, type Locale } from './locales'

const LOCALE_KEY = 'aidb.locale'

function detectLocale(): Locale {
  try {
    const saved = localStorage.getItem(LOCALE_KEY)
    if (saved === 'zh' || saved === 'en') return saved
  } catch { /* ignore */ }
  return /^en/i.test(String(navigator.language || '')) ? 'en' : 'zh'
}

function applyLocale(l: Locale) {
  document.documentElement.lang = l === 'en' ? 'en' : 'zh-CN'
}

export function tFor(locale: Locale, key: string, params?: Record<string, string | number>): string {
  let s = dicts[locale][key]
  if (s === undefined) s = dicts.zh[key]
  if (s === undefined) return key
  if (!params) return s
  return s.replace(/\{(\w+)\}/g, (_, k) => (k in params ? String(params[k]) : `{${k}}`))
}

type I18nValue = {
  locale: Locale
  setLocale: (l: Locale) => void
  t: (key: string, params?: Record<string, string | number>) => string
}

const I18nContext = createContext<I18nValue | null>(null)

export function I18nProvider({ children }: { children: ReactNode }) {
  const [locale, setLocaleState] = useState<Locale>(() => {
    const l = detectLocale()
    applyLocale(l)
    return l
  })

  const setLocale = useCallback((l: Locale) => {
    if (l !== 'zh' && l !== 'en') return
    setLocaleState(l)
    applyLocale(l)
    try { localStorage.setItem(LOCALE_KEY, l) } catch { /* ignore */ }
  }, [])

  const value = useMemo<I18nValue>(() => ({
    locale,
    setLocale,
    t: (key, params) => tFor(locale, key, params),
  }), [locale, setLocale])

  return createElement(I18nContext.Provider, { value }, children)
}

export function useI18n(): I18nValue {
  const ctx = useContext(I18nContext)
  if (!ctx) throw new Error('useI18n must be used within I18nProvider')
  return ctx
}
