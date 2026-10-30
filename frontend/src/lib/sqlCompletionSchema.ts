import type { SQLNamespace } from '@codemirror/lang-sql'
import { ExpandObject } from '../../wailsjs/go/main/App'
import type { ObjectNode } from '../types'

/** Cap metadata fan-out — only the focused schema, limited tables with columns. */
const MAX_TABLES = 80
const MAX_COLS_TABLES = 24

function colName(raw: string) {
  return raw.split(' · ')[0].trim()
}

async function expand(connectionId: string, path: string[]): Promise<ObjectNode[]> {
  try {
    return ((await ExpandObject(connectionId, path)) as unknown as ObjectNode[]) || []
  } catch {
    return []
  }
}

/** Build CodeMirror SQL schema map for keyword/table/column completion. */
export async function buildSqlCompletionSchema(
  connectionId: string,
  connType: string,
  rootObjects: ObjectNode[],
  activeSchema: string,
): Promise<SQLNamespace> {
  if (!connectionId || connType === 'redis') return {}

  const schemas = rootObjects
    .filter((n) => n.kind === 'schema' || n.kind === 'database')
    .map((n) => n.name)
    .filter(Boolean)

  // Only the workbench schema (or first root) — avoid N-schema ExpandObject storms.
  const focus =
    activeSchema && schemas.includes(activeSchema)
      ? activeSchema
      : schemas[0] || ''
  if (!focus) return {}

  await expand(connectionId, [focus])
  const [tableNodes, viewNodes] = await Promise.all([
    expand(connectionId, [focus, 'tables']),
    expand(connectionId, [focus, 'views']),
  ])
  const relations = [...tableNodes, ...viewNodes]
    .filter((n) => n.kind === 'table' || n.kind === 'view')
    .slice(0, MAX_TABLES)

  const tables: Record<string, string[]> = {}
  for (const rel of relations) {
    tables[rel.name] = []
  }

  const withCols = relations.slice(0, MAX_COLS_TABLES)
  await Promise.all(
    withCols.map(async (rel) => {
      const folder = rel.kind === 'view' ? 'views' : 'tables'
      await expand(connectionId, [focus, folder, rel.name])
      const colNodes = await expand(connectionId, [focus, folder, rel.name, 'columns'])
      tables[rel.name] = colNodes
        .filter((n) => n.kind === 'column')
        .map((n) => colName(n.name))
        .filter(Boolean)
    }),
  )

  const out: Record<string, SQLNamespace> = { [focus]: tables }
  // Unqualified completion for the active schema.
  Object.assign(out, tables)
  return out
}
