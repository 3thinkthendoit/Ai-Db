import { QueryResult } from '../types'
import { useI18n } from '../i18n'

export function ResultTable({ result }: { result: QueryResult | null }) {
  const { t } = useI18n()
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
            {(result.rows || []).map((row, i) => (
              <tr key={i}>
                {row.map((cell, j) => <td key={j}>{cell}</td>)}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}
