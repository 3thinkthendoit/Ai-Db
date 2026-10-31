import { useEffect, useMemo, useRef, useState, type MouseEvent as ReactMouseEvent } from 'react'
import { QueryResult } from '../types'
import { useI18n } from '../i18n'
import { BodyPortal } from '../lib/portal'
import {
  cellForCopy,
  isNullCell,
  rowRange,
  rowsToCsv,
  rowsToTsv,
  sortedRowIndices,
} from '../lib/resultGrid'

const PAGE_SIZES = [20, 50, 100, 200, 500] as const
const MIN_COL_W = 56

type SortState = { col: number; dir: 'asc' | 'desc' } | null

type CtxMenu =
  | { kind: 'col'; col: number; x: number; y: number }
  | { kind: 'cell'; col: number; row: number; x: number; y: number }

async function writeClipboard(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch {
    return false
  }
}

function downloadText(filename: string, text: string, mime: string) {
  // BOM so Excel on Windows recognizes UTF-8.
  const blob = new Blob([text.startsWith('\uFEFF') ? text : `\uFEFF${text}`], { type: mime })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  a.click()
  // Delay revoke so the browser can finish reading the blob (Safari / some Chromium builds).
  window.setTimeout(() => URL.revokeObjectURL(url), 2000)
}

export function ResultTable({ result }: { result: QueryResult | null }) {
  const { t } = useI18n()
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState<number>(50)
  const [selectedCol, setSelectedCol] = useState<number | null>(null)
  const [selectedRows, setSelectedRows] = useState<Set<number>>(() => new Set())
  const [anchorRow, setAnchorRow] = useState<number | null>(null)
  const [sort, setSort] = useState<SortState>(null)
  const [colWidths, setColWidths] = useState<Record<number, number>>({})
  const [ctx, setCtx] = useState<CtxMenu | null>(null)
  const [tip, setTip] = useState('')
  const tipTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const wrapRef = useRef<HTMLDivElement>(null)
  const menuRef = useRef<HTMLDivElement>(null)
  const dragRef = useRef<{ col: number; startX: number; startW: number } | null>(null)

  const total = result?.rows?.length || 0
  const totalPages = Math.max(1, Math.ceil(total / pageSize) || 1)

  useEffect(() => {
    setPage(1)
    setSelectedCol(null)
    setSelectedRows(new Set())
    setAnchorRow(null)
    setSort(null)
    setColWidths({})
    setCtx(null)
  }, [result])

  useEffect(() => {
    setPage((p) => (p > totalPages ? totalPages : p))
  }, [totalPages])

  useEffect(() => {
    if (!ctx) return
    const onDown = (e: MouseEvent) => {
      if (menuRef.current?.contains(e.target as Node)) return
      setCtx(null)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setCtx(null)
    }
    window.addEventListener('mousedown', onDown)
    window.addEventListener('keydown', onKey)
    return () => {
      window.removeEventListener('mousedown', onDown)
      window.removeEventListener('keydown', onKey)
    }
  }, [ctx])

  const orderedIndices = useMemo(() => {
    if (!result?.rows) return [] as number[]
    if (!sort) return result.rows.map((_, i) => i)
    return sortedRowIndices(result.rows, sort.col, sort.dir)
  }, [result, sort])

  const pageIndices = useMemo(() => {
    const start = (page - 1) * pageSize
    return orderedIndices.slice(start, start + pageSize)
  }, [orderedIndices, page, pageSize])

  const rangeFrom = total === 0 ? 0 : (page - 1) * pageSize + 1
  const rangeTo = Math.min(page * pageSize, total)

  function flash(msg: string) {
    setTip(msg)
    if (tipTimer.current) clearTimeout(tipTimer.current)
    tipTimer.current = setTimeout(() => setTip(''), 1600)
  }

  function selectedRowData(): string[][] {
    if (!result?.rows) return []
    const idxs = [...selectedRows].sort((a, b) => a - b)
    return idxs.map((i) => result.rows[i] || [])
  }

  async function copyColumn(col: number, withHeader: boolean) {
    if (!result?.columns?.[col]) return
    const lines = result.rows.map((row) => cellForCopy(row[col]))
    const text = withHeader ? [result.columns[col], ...lines].join('\n') : lines.join('\n')
    const ok = await writeClipboard(text)
    flash(ok ? t('query.copy.ok') : t('query.copy.fail'))
    setCtx(null)
  }

  async function copyCell(col: number, absoluteRow: number) {
    const row = result?.rows?.[absoluteRow]
    if (!row) return
    const ok = await writeClipboard(cellForCopy(row[col]))
    flash(ok ? t('query.copy.ok') : t('query.copy.fail'))
    setCtx(null)
  }

  async function copyRowsAsTsv(withHeader: boolean) {
    if (!result || selectedRows.size === 0) return
    const text = rowsToTsv(result.columns, selectedRowData(), withHeader)
    const ok = await writeClipboard(text)
    flash(ok ? t('query.copy.ok') : t('query.copy.fail'))
    setCtx(null)
  }

  function exportCsv() {
    if (!result) return
    const rows =
      selectedRows.size > 0
        ? selectedRowData()
        : result.rows
    const text = rowsToCsv(result.columns, rows)
    downloadText('query-result.csv', text, 'text/csv;charset=utf-8')
    flash(
      selectedRows.size > 0
        ? t('query.export.csvSelected', { n: selectedRows.size })
        : t('query.export.csvAll', { n: rows.length }),
    )
  }

  function cycleSort(col: number) {
    setSort((prev) => {
      if (!prev || prev.col !== col) return { col, dir: 'asc' }
      if (prev.dir === 'asc') return { col, dir: 'desc' }
      return null
    })
  }

  function onRowClick(absRow: number, e: ReactMouseEvent) {
    wrapRef.current?.focus()
    if (e.shiftKey && anchorRow != null) {
      // Range follows current visual order (sorted), not raw index arithmetic.
      const a = orderedIndices.indexOf(anchorRow)
      const b = orderedIndices.indexOf(absRow)
      if (a >= 0 && b >= 0) {
        const lo = Math.min(a, b)
        const hi = Math.max(a, b)
        setSelectedRows(new Set(orderedIndices.slice(lo, hi + 1)))
      } else {
        setSelectedRows(new Set(rowRange(anchorRow, absRow)))
      }
      return
    }
    if (e.metaKey || e.ctrlKey) {
      setSelectedRows((prev) => {
        const next = new Set(prev)
        if (next.has(absRow)) next.delete(absRow)
        else next.add(absRow)
        return next
      })
      setAnchorRow(absRow)
      return
    }
    setSelectedRows(new Set([absRow]))
    setAnchorRow(absRow)
  }

  function onResizeStart(col: number, e: ReactMouseEvent) {
    e.preventDefault()
    e.stopPropagation()
    const th = (e.currentTarget as HTMLElement).parentElement
    const startW = colWidths[col] || th?.getBoundingClientRect().width || 120
    dragRef.current = { col, startX: e.clientX, startW }
    const onMove = (ev: MouseEvent) => {
      const d = dragRef.current
      if (!d) return
      const w = Math.max(MIN_COL_W, d.startW + (ev.clientX - d.startX))
      setColWidths((prev) => ({ ...prev, [d.col]: w }))
    }
    const onUp = () => {
      dragRef.current = null
      window.removeEventListener('mousemove', onMove)
      window.removeEventListener('mouseup', onUp)
    }
    window.addEventListener('mousemove', onMove)
    window.addEventListener('mouseup', onUp)
  }

  useEffect(() => {
    if (!result) return
    const onKey = (e: KeyboardEvent) => {
      if (!(e.ctrlKey || e.metaKey) || e.key.toLowerCase() !== 'c') return
      const root = wrapRef.current
      const active = document.activeElement
      // Only intercept when focus is inside the result grid (don't steal editor copy).
      if (!root || !active || !root.contains(active)) return
      const sel = window.getSelection()
      if (sel && !sel.isCollapsed && root.contains(sel.anchorNode)) return
      if (selectedRows.size > 0) {
        e.preventDefault()
        void copyRowsAsTsv(false)
        return
      }
      if (selectedCol != null && result.columns?.[selectedCol]) {
        e.preventDefault()
        void copyColumn(selectedCol, false)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [selectedCol, selectedRows, result, orderedIndices])

  if (!result) {
    return <div className="result-empty">{t('query.resultEmpty')}</div>
  }

  const menuStyle = ctx
    ? {
        left: Math.min(ctx.x, typeof window !== 'undefined' ? window.innerWidth - 220 : ctx.x),
        top: Math.min(ctx.y, typeof window !== 'undefined' ? window.innerHeight - 180 : ctx.y),
      }
    : undefined

  return (
    <div className="result-wrap" ref={wrapRef} tabIndex={0}>
      <div className="result-meta">
        <span>{t('query.rows', { n: result.rowCount })}</span>
        <span>{result.durationMs} ms</span>
        {result.truncated && <span className="warn">{t('query.truncated')}</span>}
        {result.message && <span>{result.message}</span>}
        {selectedRows.size > 0 && (
          <span className="muted">{t('query.copy.rowHint', { n: selectedRows.size })}</span>
        )}
        {selectedRows.size === 0 && selectedCol != null && result.columns?.[selectedCol] && (
          <span className="muted">{t('query.copy.colHint', { name: result.columns[selectedCol] })}</span>
        )}
        {tip && <span className="result-copy-tip">{tip}</span>}
        <button
          type="button"
          className="sm result-export-btn"
          disabled={total === 0}
          onClick={exportCsv}
          title={t('query.export.csvTitle')}
        >
          {t('query.export.csv')}
        </button>
      </div>
      <div className="table-scroll">
        <table className="result-grid">
          <thead>
            <tr>
              {result.columns?.map((c, col) => {
                const sorted = sort?.col === col ? sort.dir : null
                return (
                  <th
                    key={`${c}-${col}`}
                    className={[selectedCol === col ? 'col-on' : '', sorted ? `sort-${sorted}` : '']
                      .filter(Boolean)
                      .join(' ') || undefined}
                    style={
                      colWidths[col]
                        ? { width: colWidths[col], minWidth: colWidths[col], maxWidth: colWidths[col] }
                        : undefined
                    }
                    title={t('query.copy.thTitle')}
                    onClick={() => {
                      setSelectedCol(col)
                      cycleSort(col)
                      wrapRef.current?.focus()
                    }}
                    onContextMenu={(e: ReactMouseEvent) => {
                      e.preventDefault()
                      setSelectedCol(col)
                      wrapRef.current?.focus()
                      setCtx({ kind: 'col', col, x: e.clientX, y: e.clientY })
                    }}
                  >
                    <span className="th-label">
                      {c}
                      {sorted === 'asc' ? ' ↑' : sorted === 'desc' ? ' ↓' : ''}
                    </span>
                    <span
                      className="col-resize"
                      onMouseDown={(e) => onResizeStart(col, e)}
                      onClick={(e) => e.stopPropagation()}
                    />
                  </th>
                )
              })}
            </tr>
          </thead>
          <tbody>
            {pageIndices.map((absRow) => {
              const row = result.rows[absRow] || []
              const rowOn = selectedRows.has(absRow)
              return (
                <tr key={absRow} className={rowOn ? 'row-on' : undefined}>
                  {row.map((cell, col) => {
                    const nullish = isNullCell(cell)
                    return (
                      <td
                        key={col}
                        className={[
                          selectedCol === col ? 'col-on' : '',
                          nullish ? 'is-null' : '',
                        ]
                          .filter(Boolean)
                          .join(' ') || undefined}
                        style={
                      colWidths[col]
                        ? { width: colWidths[col], minWidth: colWidths[col], maxWidth: colWidths[col] }
                        : undefined
                    }
                        onClick={(e) => {
                          setSelectedCol(col)
                          onRowClick(absRow, e)
                        }}
                        onDoubleClick={() => void copyCell(col, absRow)}
                        onContextMenu={(e: ReactMouseEvent) => {
                          e.preventDefault()
                          setSelectedCol(col)
                          if (!selectedRows.has(absRow)) {
                            setSelectedRows(new Set([absRow]))
                            setAnchorRow(absRow)
                          }
                          setCtx({ kind: 'cell', col, row: absRow, x: e.clientX, y: e.clientY })
                        }}
                      >
                        {nullish ? 'NULL' : cell}
                      </td>
                    )
                  })}
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
      {total > 0 && (
        <div className="result-pager" role="navigation" aria-label={t('query.pager')}>
          <span className="result-pager-range">
            {t('query.pager.range', { from: rangeFrom, to: rangeTo, total })}
          </span>
          <label className="result-pager-size">
            <span>{t('query.pager.pageSize')}</span>
            <select
              value={pageSize}
              onChange={(e) => {
                setPageSize(Number(e.target.value))
                setPage(1)
              }}
            >
              {PAGE_SIZES.map((n) => (
                <option key={n} value={n}>{n}</option>
              ))}
            </select>
          </label>
          <div className="result-pager-nav">
            <button type="button" className="sm" disabled={page <= 1} onClick={() => setPage(1)} title={t('query.pager.first')}>
              «
            </button>
            <button type="button" className="sm" disabled={page <= 1} onClick={() => setPage((p) => Math.max(1, p - 1))} title={t('query.pager.prev')}>
              ‹
            </button>
            <span className="result-pager-page">
              {t('query.pager.page', { page, pages: totalPages })}
            </span>
            <button type="button" className="sm" disabled={page >= totalPages} onClick={() => setPage((p) => Math.min(totalPages, p + 1))} title={t('query.pager.next')}>
              ›
            </button>
            <button type="button" className="sm" disabled={page >= totalPages} onClick={() => setPage(totalPages)} title={t('query.pager.last')}>
              »
            </button>
          </div>
        </div>
      )}
      {ctx && (
        <BodyPortal>
          <div className="ctx-menu" ref={menuRef} style={menuStyle} role="menu">
            {ctx.kind === 'cell' && (
              <>
                <button type="button" className="ctx-item" role="menuitem" onClick={() => void copyCell(ctx.col, ctx.row)}>
                  {t('query.copy.cell')}
                </button>
                <button type="button" className="ctx-item" role="menuitem" onClick={() => void copyRowsAsTsv(false)}>
                  {t('query.copy.rows')}
                </button>
                <button type="button" className="ctx-item" role="menuitem" onClick={() => void copyRowsAsTsv(true)}>
                  {t('query.copy.rowsHeader')}
                </button>
                <div className="ctx-sep" />
              </>
            )}
            <button type="button" className="ctx-item" role="menuitem" onClick={() => void copyColumn(ctx.col, false)}>
              {t('query.copy.column')}
            </button>
            <button type="button" className="ctx-item" role="menuitem" onClick={() => void copyColumn(ctx.col, true)}>
              {t('query.copy.columnHeader')}
            </button>
          </div>
        </BodyPortal>
      )}
    </div>
  )
}
