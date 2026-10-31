/**
 * Pure helpers for result-grid copy / export / sort (shared with node specs).
 */

/** Backend stringify uses the literal "NULL" for SQL NULL. */
export function isNullCell(v) {
  return v == null || v === 'NULL'
}

/** Clipboard / export value: SQL NULL → empty. */
export function cellForCopy(v) {
  if (isNullCell(v)) return ''
  return String(v)
}

export function escapeCsvField(v) {
  const s = cellForCopy(v)
  if (/[",\r\n]/.test(s)) return `"${s.replace(/"/g, '""')}"`
  return s
}

/** Build TSV (tab-separated). Optional header row. */
export function rowsToTsv(columns, rows, withHeader = false) {
  const lines = []
  if (withHeader && Array.isArray(columns)) {
    lines.push(columns.map((c) => cellForCopy(c)).join('\t'))
  }
  for (const row of rows || []) {
    lines.push((row || []).map((c) => cellForCopy(c)).join('\t'))
  }
  return lines.join('\n')
}

/** Build CSV with RFC-style quoting. Always includes header when columns provided. */
export function rowsToCsv(columns, rows) {
  const lines = []
  if (Array.isArray(columns) && columns.length) {
    lines.push(columns.map((c) => escapeCsvField(c)).join(','))
  }
  for (const row of rows || []) {
    lines.push((row || []).map((c) => escapeCsvField(c)).join(','))
  }
  return `${lines.join('\n')}\n`
}

const NUM_RE = /^-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?$/

/** Compare two cell strings for sort. Nulls last in asc. */
export function compareCell(a, b) {
  const na = isNullCell(a)
  const nb = isNullCell(b)
  if (na && nb) return 0
  if (na) return 1
  if (nb) return -1
  const sa = String(a)
  const sb = String(b)
  if (NUM_RE.test(sa) && NUM_RE.test(sb)) {
    const da = Number(sa)
    const db = Number(sb)
    if (da < db) return -1
    if (da > db) return 1
    return 0
  }
  return sa.localeCompare(sb, undefined, { numeric: true, sensitivity: 'base' })
}

/**
 * Return row indices 0..n-1 sorted by column `col`.
 * @param {string[][]} rows
 * @param {number} col
 * @param {'asc'|'desc'} dir
 */
export function sortedRowIndices(rows, col, dir) {
  const n = Array.isArray(rows) ? rows.length : 0
  const idxs = Array.from({ length: n }, (_, i) => i)
  if (col < 0 || !dir) return idxs
  const sign = dir === 'desc' ? -1 : 1
  idxs.sort((i, j) => sign * compareCell(rows[i]?.[col], rows[j]?.[col]))
  return idxs
}

/** Inclusive range of absolute row indices between a and b. */
export function rowRange(a, b) {
  const lo = Math.min(a, b)
  const hi = Math.max(a, b)
  const out = []
  for (let i = lo; i <= hi; i++) out.push(i)
  return out
}
