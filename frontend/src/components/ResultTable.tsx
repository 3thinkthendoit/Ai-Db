import { useEffect, useMemo, useState } from 'react'
import { QueryResult } from '../types'
import { useI18n } from '../i18n'

const PAGE_SIZES = [20, 50, 100, 200, 500] as const

export function ResultTable({ result }: { result: QueryResult | null }) {
  const { t } = useI18n()
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState<number>(50)

  const total = result?.rows?.length || 0
  const totalPages = Math.max(1, Math.ceil(total / pageSize) || 1)

  // New result set → back to first page.
  useEffect(() => {
    setPage(1)
  }, [result])

  // Keep page in range when totalPages shrinks (e.g. fewer rows, same result ref).
  useEffect(() => {
    setPage((p) => (p > totalPages ? totalPages : p))
  }, [totalPages])

  const pageRows = useMemo(() => {
    if (!result?.rows?.length) return []
    const start = (page - 1) * pageSize
    return result.rows.slice(start, start + pageSize)
  }, [result, page, pageSize])

  const rangeFrom = total === 0 ? 0 : (page - 1) * pageSize + 1
  const rangeTo = Math.min(page * pageSize, total)

  if (!result) {
    return <div className="result-empty">{t('query.resultEmpty')}</div>
  }

  return (
    <div className="result-wrap">
      <div className="result-meta">
        <span>{t('query.rows', { n: result.rowCount })}</span>
        <span>{result.durationMs} ms</span>
        {result.truncated && <span className="warn">{t('query.truncated')}</span>}
        {result.message && <span>{result.message}</span>}
      </div>
      <div className="table-scroll">
        <table>
          <thead>
            <tr>
              {result.columns?.map((c) => <th key={c}>{c}</th>)}
            </tr>
          </thead>
          <tbody>
            {pageRows.map((row, i) => (
              <tr key={(page - 1) * pageSize + i}>
                {row.map((cell, j) => <td key={j}>{cell}</td>)}
              </tr>
            ))}
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
            <button
              type="button"
              className="sm"
              disabled={page <= 1}
              onClick={() => setPage(1)}
              title={t('query.pager.first')}
            >
              «
            </button>
            <button
              type="button"
              className="sm"
              disabled={page <= 1}
              onClick={() => setPage((p) => Math.max(1, p - 1))}
              title={t('query.pager.prev')}
            >
              ‹
            </button>
            <span className="result-pager-page">
              {t('query.pager.page', { page, pages: totalPages })}
            </span>
            <button
              type="button"
              className="sm"
              disabled={page >= totalPages}
              onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
              title={t('query.pager.next')}
            >
              ›
            </button>
            <button
              type="button"
              className="sm"
              disabled={page >= totalPages}
              onClick={() => setPage(totalPages)}
              title={t('query.pager.last')}
            >
              »
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
