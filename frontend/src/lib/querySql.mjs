/** Shared SQL/CSV/Redis helpers (ESM). Imported by querySql.ts and specs. */

export function quoteIdent(connType, name) {
  if (connType === 'postgres') return `"${name.replace(/"/g, '""')}"`
  if (connType === 'mysql') return `\`${name.replace(/`/g, '``')}\``
  return name
}

export function tableRef(connType, schema, name) {
  if (!schema) return quoteIdent(connType, name)
  return `${quoteIdent(connType, schema)}.${quoteIdent(connType, name)}`
}

/** CSV cell → SQL literal. Bare null/NULL → NULL; empty string stays ''. */
export function lit(_connType, v) {
  if (/^null$/i.test(v)) return 'NULL'
  return `'${v.replace(/'/g, "''")}'`
}

export function escLit(s) {
  return s.replace(/'/g, "''")
}

/** Quote a Redis argument if it has whitespace/special chars. */
export function quoteRedisArg(s) {
  if (/^[\w.:/@+-]+$/.test(s)) return s
  return `"${s.replace(/\\/g, '\\\\').replace(/"/g, '\\"')}"`
}

/** Minimal CSV parser: comma-separated, supports "quoted" fields. */
export function parseCsv(text) {
  const rows = []
  let row = []
  let cell = ''
  let i = 0
  let inQuotes = false
  const s = text.replace(/^\uFEFF/, '')
  while (i < s.length) {
    const ch = s[i]
    if (inQuotes) {
      if (ch === '"') {
        if (s[i + 1] === '"') {
          cell += '"'
          i += 2
          continue
        }
        inQuotes = false
        i++
        continue
      }
      cell += ch
      i++
      continue
    }
    if (ch === '"') {
      inQuotes = true
      i++
      continue
    }
    if (ch === ',') {
      row.push(cell)
      cell = ''
      i++
      continue
    }
    if (ch === '\n' || ch === '\r') {
      if (ch === '\r' && s[i + 1] === '\n') i++
      row.push(cell)
      cell = ''
      if (row.some((c) => c.trim() !== '')) rows.push(row)
      row = []
      i++
      continue
    }
    cell += ch
    i++
  }
  row.push(cell)
  if (row.some((c) => c.trim() !== '')) rows.push(row)
  return rows
}

export function buildInserts(connType, ref, headers, dataRows) {
  const cols = headers.map((h) => quoteIdent(connType, h.trim())).join(', ')
  const stmts = dataRows.map((r) => {
    const vals = headers.map((_, idx) => lit(connType, (r[idx] ?? '').trim())).join(', ')
    return `INSERT INTO ${ref} (${cols}) VALUES (${vals});`
  })
  return stmts.join('\n')
}

const SQL_CLAUSE =
  /\b(SELECT|FROM|WHERE|AND|OR|JOIN|LEFT|RIGHT|INNER|OUTER|FULL|CROSS|ON|GROUP BY|ORDER BY|HAVING|LIMIT|OFFSET|UNION|INSERT INTO|VALUES|UPDATE|SET|DELETE FROM|CREATE|ALTER|DROP|WITH)\b/gi

/** Split SQL into code vs string/comment chunks so formatters do not touch literals. */
function splitSqlChunks(sql) {
  const chunks = []
  let buf = ''
  let mode = 'code' // code | sq | dq | bt | line | block | dollar
  let dollarTag = ''
  const flush = (type) => {
    if (!buf) return
    chunks.push({ type, text: buf })
    buf = ''
  }
  for (let i = 0; i < sql.length; i++) {
    const ch = sql[i]
    const next = sql[i + 1]
    if (mode === 'code') {
      if (ch === '-' && next === '-') {
        flush('code')
        mode = 'line'
        buf = '--'
        i++
        continue
      }
      if (ch === '/' && next === '*') {
        flush('code')
        mode = 'block'
        buf = '/*'
        i++
        continue
      }
      // PostgreSQL dollar-quoting: $$...$$ or $tag$...$tag$
      if (ch === '$') {
        let j = i + 1
        while (j < sql.length && /[A-Za-z0-9_]/.test(sql[j])) j++
        if (sql[j] === '$') {
          dollarTag = sql.slice(i, j + 1)
          flush('code')
          mode = 'dollar'
          buf = dollarTag
          i = j
          continue
        }
      }
      if (ch === "'") {
        flush('code')
        mode = 'sq'
        buf = "'"
        continue
      }
      if (ch === '"') {
        flush('code')
        mode = 'dq'
        buf = '"'
        continue
      }
      if (ch === '`') {
        flush('code')
        mode = 'bt'
        buf = '`'
        continue
      }
      buf += ch
      continue
    }
    if (mode === 'dollar') {
      buf += ch
      if (ch === '$' && dollarTag && sql.startsWith(dollarTag, i)) {
        // already wrote first '$'; append the rest of the closing tag
        buf += dollarTag.slice(1)
        i += dollarTag.length - 1
        flush('keep')
        mode = 'code'
        dollarTag = ''
      }
      continue
    }
    if (mode === 'line') {
      buf += ch
      if (ch === '\n') {
        flush('keep')
        mode = 'code'
      }
      continue
    }
    if (mode === 'block') {
      buf += ch
      if (ch === '*' && next === '/') {
        buf += '/'
        i++
        flush('keep')
        mode = 'code'
      }
      continue
    }
    if (mode === 'sq') {
      buf += ch
      if (ch === "'" && next === "'") {
        buf += "'"
        i++
        continue
      }
      if (ch === "'") {
        flush('keep')
        mode = 'code'
      }
      continue
    }
    if (mode === 'dq') {
      buf += ch
      if (ch === '"' && next === '"') {
        buf += '"'
        i++
        continue
      }
      if (ch === '"') {
        flush('keep')
        mode = 'code'
      }
      continue
    }
    if (mode === 'bt') {
      buf += ch
      if (ch === '`' && next === '`') {
        buf += '`'
        i++
        continue
      }
      if (ch === '`') {
        flush('keep')
        mode = 'code'
      }
    }
  }
  if (buf) flush(mode === 'code' ? 'code' : 'keep')
  return chunks
}

function formatCodeChunk(s) {
  let t = s.replace(/\s+/g, ' ').trim()
  if (!t) return s.includes('\n') ? '\n' : s
  t = t.replace(SQL_CLAUSE, (kw) => `\n${kw.toUpperCase()}`)
  t = t.replace(/,\s*/g, ',\n  ')
  return t
    .split('\n')
    .map((l) => l.trim())
    .filter(Boolean)
    .join('\n')
}

/** Lightweight SQL pretty-print; leaves string/comment contents untouched. */
export function formatSql(sql) {
  const trimmed = sql.trim()
  if (!trimmed) return sql
  if (!/select|insert|update|delete|with|create|alter|drop|explain|call|show|describe/i.test(trimmed)) {
    return sql
  }
  const chunks = splitSqlChunks(trimmed)
  let out = chunks
    .map((c) => (c.type === 'code' ? formatCodeChunk(c.text) : c.text))
    .join('')
    .replace(/\n{3,}/g, '\n\n')
    .trim()
  if (trimmed.endsWith(';') && !out.endsWith(';')) out += ';'
  return out
}

/** Build EXPLAIN statement for the current editor SQL. */
export function explainSql(sql) {
  const body = sql.trim().replace(/;+\s*$/, '')
  if (!body) return ''
  if (/^\s*explain\b/i.test(body)) return body
  return `EXPLAIN ${body}`
}
