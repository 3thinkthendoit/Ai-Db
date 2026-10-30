export type Theme = 'light' | 'dark'

const THEME_KEY = 'aidb.theme'

export function initTheme(): Theme {
  let v: Theme = 'light'
  try {
    v = localStorage.getItem(THEME_KEY) === 'dark' ? 'dark' : 'light'
  } catch { /* ignore */ }
  document.documentElement.dataset.theme = v
  return v
}

export function setTheme(v: Theme) {
  if (v !== 'light' && v !== 'dark') return
  document.documentElement.dataset.theme = v
  try { localStorage.setItem(THEME_KEY, v) } catch { /* ignore */ }
}

export function getTheme(): Theme {
  return document.documentElement.dataset.theme === 'dark' ? 'dark' : 'light'
}
