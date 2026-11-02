export type UiEdition = 'agent' | 'dbtool'

const KEY = 'aidb.uiEdition'
/** Bump once to migrate existing browsers onto DB-tool as the product default. */
const MIGRATE_KEY = 'aidb.uiEdition.migrateDbtool1'

export function getUiEdition(): UiEdition {
  try {
    ensureDbtoolDefault()
    const v = localStorage.getItem(KEY)
    if (v === 'agent') return 'agent'
    return 'dbtool'
  } catch {
    return 'dbtool'
  }
}

/** First-run / one-shot migrate: default & upgrade prior sessions to dbtool. */
export function ensureDbtoolDefault() {
  try {
    if (!localStorage.getItem(MIGRATE_KEY)) {
      localStorage.setItem(MIGRATE_KEY, '1')
      localStorage.setItem(KEY, 'dbtool')
    } else if (!localStorage.getItem(KEY)) {
      localStorage.setItem(KEY, 'dbtool')
    }
  } catch {
    /* ignore */
  }
}

export function setUiEdition(v: UiEdition) {
  try {
    localStorage.setItem(KEY, v)
  } catch {
    /* ignore */
  }
  window.dispatchEvent(new CustomEvent('aidb-ui-edition', { detail: v }))
}
