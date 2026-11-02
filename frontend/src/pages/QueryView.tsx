import { useEffect, useMemo, useRef, useState, type MouseEvent as ReactMouseEvent } from 'react'
import { MarkdownBody } from '../components/MarkdownBody'
import { CreateDatabaseModal } from '../components/CreateDatabaseModal'
import { ObjectTree } from '../components/ObjectTree'
import { ResultTable } from '../components/ResultTable'
import { SqlEditor } from '../components/SqlEditor'
import { ObjectNode, QueryResult } from '../types'
import { ExecuteQuery, ExpandObject, IsMutatingQuery, SetActiveSchema, WritesAllowed } from '../lib/api'
import { useI18n } from '../i18n'
import { BodyPortal } from '../lib/portal'
import {
  buildCopyToOtherDbSql,
  buildInserts,
  escLit,
  explainSql,
  formatSql,
  parseCsv,
  quoteIdent,
  quoteRedisArg,
  tableRef,
} from '../lib/querySql'

interface Props {
  /** False when Query panel is hidden — skip object-tree network work. */
  active?: boolean
  connectionId: string
  connected: boolean
  connType: string
  objects: ObjectNode[]
  activeSchema?: string
  onRefreshObjects: () => void
  /** Run AI analysis; return markdown. Also syncs into Agent task history. */
  onAiAnalyze: (sql: string) => Promise<string>
  onActiveSchemaChange?: (schema: string) => void
}

type CtxAction =
  | 'select'
  | 'describe'
  | 'showCreate'
  | 'copyName'
  | 'addColumn'
  | 'modifyColumn'
  | 'migrateColumnExpand'
  | 'migrateTableCopy'
  | 'dropColumn'
  | 'createIndex'
  | 'modifyIndex'
  | 'dropIndex'
  | 'import'
  | 'copyToOtherDb'
  | 'useSchema'
  | 'createTable'
  | 'createView'
  | 'createProcedure'
  | 'createFunction'
  | 'createTrigger'
  | 'createEvent'
  | 'createSequence'
  | 'createDatabase'
  | 'dropObject'
  | 'callRoutine'
  | 'showIndexes'
  | 'redisPing'

interface CtxMenu {
  x: number
  y: number
  node: ObjectNode
  path: string[]
}

interface ImportTarget {
  schema: string
  table: string
  ref: string
}

/** Prompt for same-connection force-overwrite copy to another schema. */
interface CopyDbPrompt {
  sourceSchema: string
  /** Single table; omit for whole-schema (all tables under source). */
  tableName?: string
}

interface EditorTab {
  id: string
  title: string
  sql: string
  error: string
}

interface ResultTab {
  id: string
  title: string
  result: QueryResult | null
  /** When set, bottom pane shows markdown instead of a result grid. */
  kind?: 'table' | 'ai'
  markdown?: string
  loading?: boolean
}

let tabSeq = 1
let resultSeq = 1

function defaultSql(connType: string) {
  if (connType === 'redis') return 'PING'
  if (connType === 'mongo') return 'show collections'
  return 'SELECT 1'
}

function isNonSQL(connType: string) {
  return connType === 'redis' || connType === 'mongo'
}

/** mongosh collection handle: bare ident or getCollection("…"). */
function mongoColl(name: string) {
  if (/^[A-Za-z_][\w$]*$/.test(name)) return `db.${name}`
  return `db.getCollection(${JSON.stringify(name)})`
}

function makeTab(connType: string, title: string): EditorTab {
  return {
    id: `q-${Date.now()}-${tabSeq++}`,
    title,
    sql: defaultSql(connType),
    error: '',
  }
}

function makeResultTab(title: string, kind: 'table' | 'ai' = 'table'): ResultTab {
  return {
    id: `r-${Date.now()}-${resultSeq++}`,
    title,
    result: null,
    kind,
    markdown: '',
    loading: false,
  }
}

type MenuEntry = { action: CtxAction; labelKey: string } | { sep: true }

/** SQL schema/namespace for DDL/DML (PG: path is [database, schema, …]). */
function schemaOf(path: string[], node: ObjectNode, connType = '') {
  if (node.kind === 'schema') return node.name
  if (node.kind === 'database') return connType === 'postgres' ? 'public' : node.name
  if (connType === 'postgres' && path.length >= 2) return path[1] || ''
  return path[0] || ''
}

/** Workbench「当前库」value — PG uses database (path[0]), MySQL uses schema/database name. */
function workbenchCatalog(path: string[], node: ObjectNode, connType: string) {
  if (connType === 'postgres') {
    if (node.kind === 'database') return node.name
    return path[0] || ''
  }
  if (node.kind === 'schema' || node.kind === 'database') return node.name
  return path[0] || ''
}

/** Table/view name from a tree path (PG nests schema under database). */
function relationFromPath(path: string[], node: ObjectNode, connType: string) {
  if (node.kind === 'table' || node.kind === 'view') return node.name
  if (connType === 'postgres') {
    // [db, schema, tables|views, table]
    return path[3] || ''
  }
  return path[2] || ''
}

function menuFor(connType: string, node: ObjectNode): MenuEntry[] {
  const kind = node.kind === 'folder' ? `folder:${node.name}` : node.kind
  const copy: MenuEntry = { action: 'copyName', labelKey: 'query.ctx.copyName' }

  switch (kind) {
    case 'schema':
    case 'database': {
      if (connType === 'mongo') {
        return [
          { action: 'useSchema', labelKey: 'query.ctx.mongoUse' },
          { action: 'select', labelKey: 'query.ctx.mongoShowCollections' },
          { sep: true },
          copy,
        ]
      }
      if (connType === 'redis') {
        return [
          { action: 'redisPing', labelKey: 'query.ctx.redisPing' },
          { sep: true },
          copy,
        ]
      }
      const items: MenuEntry[] = [
        { action: 'createDatabase', labelKey: 'query.ctx.createDatabase' },
        { sep: true },
        { action: 'createTable', labelKey: 'query.ctx.createTable' },
        { action: 'createView', labelKey: 'query.ctx.createView' },
        { action: 'createProcedure', labelKey: 'query.ctx.createProcedure' },
        { action: 'createFunction', labelKey: 'query.ctx.createFunction' },
      ]
      if (connType === 'mysql') {
        items.push({ action: 'createEvent', labelKey: 'query.ctx.createEvent' })
      }
      if (connType === 'postgres') {
        items.push({ action: 'createSequence', labelKey: 'query.ctx.createSequence' })
      }
      // PG cross-database copy is not expressible as one-shot SQL; only allow schema↔schema within a DB.
      if (!(connType === 'postgres' && node.kind === 'database')) {
        items.push({ sep: true }, { action: 'copyToOtherDb', labelKey: 'query.ctx.copyToOtherDb' })
      }
      items.push({ sep: true }, copy)
      return items
    }
    case 'root':
      // Tree background right-click: catalog-level creation only.
      if (connType === 'mysql' || connType === 'postgres') {
        return [{ action: 'createDatabase', labelKey: 'query.ctx.createDatabase' }]
      }
      return []
    case 'folder:tables':
      return [
        { action: 'createTable', labelKey: 'query.ctx.createTable' },
        { sep: true },
        copy,
      ]
    case 'folder:views':
      return [
        { action: 'createView', labelKey: 'query.ctx.createView' },
        { sep: true },
        copy,
      ]
    case 'folder:procedures':
      return [
        { action: 'createProcedure', labelKey: 'query.ctx.createProcedure' },
        { sep: true },
        copy,
      ]
    case 'folder:functions':
      return [
        { action: 'createFunction', labelKey: 'query.ctx.createFunction' },
        { sep: true },
        copy,
      ]
    case 'folder:triggers':
      return [
        { action: 'createTrigger', labelKey: 'query.ctx.createTrigger' },
        { sep: true },
        copy,
      ]
    case 'folder:events':
      return connType === 'mysql'
        ? [{ action: 'createEvent', labelKey: 'query.ctx.createEvent' }, { sep: true }, copy]
        : [copy]
    case 'folder:sequences':
      return connType === 'postgres'
        ? [{ action: 'createSequence', labelKey: 'query.ctx.createSequence' }, { sep: true }, copy]
        : [copy]
    case 'folder:columns':
      return [copy]
    case 'folder:indexes':
      return [
        { action: 'createIndex', labelKey: 'query.ctx.createIndex' },
        { sep: true },
        copy,
      ]
    case 'table':
      return [
        { action: 'select', labelKey: 'query.ctx.select' },
        { action: 'describe', labelKey: 'query.ctx.describe' },
        ...((connType === 'mysql' || connType === 'postgres')
          ? [{
              action: 'showCreate' as const,
              labelKey: connType === 'postgres' ? 'query.ctx.showDefinition' : 'query.ctx.showCreate',
            }]
          : []),
        { action: 'showIndexes', labelKey: 'query.ctx.showIndexes' },
        { sep: true },
        { action: 'addColumn', labelKey: 'query.ctx.addColumn' },
        { action: 'modifyColumn', labelKey: 'query.ctx.modifyColumn' },
        { action: 'migrateColumnExpand', labelKey: 'query.ctx.migrateColumnExpand' },
        { action: 'migrateTableCopy', labelKey: 'query.ctx.migrateTableCopy' },
        { action: 'dropColumn', labelKey: 'query.ctx.dropColumn' },
        { sep: true },
        { action: 'createIndex', labelKey: 'query.ctx.createIndex' },
        { action: 'dropIndex', labelKey: 'query.ctx.dropIndex' },
        { sep: true },
        { action: 'import', labelKey: 'query.ctx.import' },
        { action: 'copyToOtherDb', labelKey: 'query.ctx.copyToOtherDb' },
        { action: 'dropObject', labelKey: 'query.ctx.dropTable' },
        { sep: true },
        copy,
      ]
    case 'view':
      return [
        { action: 'select', labelKey: 'query.ctx.select' },
        { action: 'describe', labelKey: 'query.ctx.describe' },
        ...((connType === 'mysql' || connType === 'postgres')
          ? [{
              action: 'showCreate' as const,
              labelKey: connType === 'postgres' ? 'query.ctx.showDefinitionView' : 'query.ctx.showCreateView',
            }]
          : []),
        { sep: true },
        { action: 'dropObject', labelKey: 'query.ctx.dropView' },
        { sep: true },
        copy,
      ]
    case 'procedure':
      return [
        { action: 'callRoutine', labelKey: 'query.ctx.callProcedure' },
        ...((connType === 'mysql' || connType === 'postgres')
          ? [{
              action: 'showCreate' as const,
              labelKey: connType === 'postgres' ? 'query.ctx.showDefinitionProcedure' : 'query.ctx.showCreateProcedure',
            }]
          : []),
        { sep: true },
        { action: 'dropObject', labelKey: 'query.ctx.dropProcedure' },
        { sep: true },
        copy,
      ]
    case 'function':
      return [
        { action: 'callRoutine', labelKey: 'query.ctx.callFunction' },
        ...((connType === 'mysql' || connType === 'postgres')
          ? [{
              action: 'showCreate' as const,
              labelKey: connType === 'postgres' ? 'query.ctx.showDefinitionFunction' : 'query.ctx.showCreateFunction',
            }]
          : []),
        { sep: true },
        { action: 'dropObject', labelKey: 'query.ctx.dropFunction' },
        { sep: true },
        copy,
      ]
    case 'trigger':
      return [
        ...(connType === 'mysql' ? [{ action: 'showCreate' as const, labelKey: 'query.ctx.showCreateTrigger' }] : []),
        { action: 'dropObject', labelKey: 'query.ctx.dropTrigger' },
        { sep: true },
        copy,
      ]
    case 'event':
      return [
        ...(connType === 'mysql' ? [{ action: 'showCreate' as const, labelKey: 'query.ctx.showCreateEvent' }] : []),
        { action: 'dropObject', labelKey: 'query.ctx.dropEvent' },
        { sep: true },
        copy,
      ]
    case 'sequence':
      return [
        { action: 'select', labelKey: 'query.ctx.nextval' },
        { action: 'dropObject', labelKey: 'query.ctx.dropSequence' },
        { sep: true },
        copy,
      ]
    case 'index':
      return [
        { action: 'modifyIndex', labelKey: 'query.ctx.modifyIndex' },
        { action: 'dropIndex', labelKey: 'query.ctx.dropIndex' },
        { sep: true },
        copy,
      ]
    case 'column':
      return [
        { action: 'modifyColumn', labelKey: 'query.ctx.modifyColumn' },
        { action: 'migrateColumnExpand', labelKey: 'query.ctx.migrateColumnExpand' },
        { action: 'dropColumn', labelKey: 'query.ctx.dropColumn' },
        { sep: true },
        copy,
      ]
    case 'key':
      return [
        { action: 'select', labelKey: 'query.ctx.redisGet' },
        { action: 'dropObject', labelKey: 'query.ctx.redisDel' },
        { sep: true },
        copy,
      ]
    case 'collection':
      return [
        { action: 'select', labelKey: 'query.ctx.mongoFind' },
        { action: 'describe', labelKey: 'query.ctx.mongoIndexes' },
        { action: 'dropObject', labelKey: 'query.ctx.mongoDrop' },
        { sep: true },
        copy,
      ]
    default:
      return [copy]
  }
}

export function QueryView({ active = true, connectionId, connected, connType, objects, activeSchema = '', onRefreshObjects, onAiAnalyze, onActiveSchemaChange }: Props) {
  const { t } = useI18n()
  const [tabs, setTabs] = useState<EditorTab[]>(() => [makeTab(connType, 'Query 1')])
  const [activeTabId, setActiveTabId] = useState(() => '')
  const [tabCounter, setTabCounter] = useState(1)
  const [resultTabs, setResultTabs] = useState<ResultTab[]>([])
  const [activeResultId, setActiveResultId] = useState('')
  const [resultCounter, setResultCounter] = useState(0)
  const [runningTabId, setRunningTabId] = useState<string | null>(null)
  const [ctx, setCtx] = useState<CtxMenu | null>(null)
  const [createDbOpen, setCreateDbOpen] = useState(false)
  const [toast, setToast] = useState('')
  const [importTarget, setImportTarget] = useState<ImportTarget | null>(null)
  const [importCsv, setImportCsv] = useState('')
  const [importErr, setImportErr] = useState('')
  const [copyDbPrompt, setCopyDbPrompt] = useState<CopyDbPrompt | null>(null)
  const [copyDbTarget, setCopyDbTarget] = useState('')
  const [copyDbErr, setCopyDbErr] = useState('')
  const [copyDbBusy, setCopyDbBusy] = useState(false)
  const [splitPct, setSplitPct] = useState(50)
  const [sideWidth, setSideWidth] = useState(260)
  const menuRef = useRef<HTMLDivElement>(null)
  const editorPaneRef = useRef<HTMLElement>(null)
  const queryViewRef = useRef<HTMLDivElement>(null)
  const connRef = useRef(connectionId)
  useEffect(() => {
    setCopyDbPrompt(null)
    setCopyDbTarget('')
    setCopyDbErr('')
    setCopyDbBusy(false)
    setCreateDbOpen(false)
  }, [connectionId])
  const resultTabsRef = useRef(resultTabs)
  const resultCounterRef = useRef(resultCounter)
  const runningLockRef = useRef(false)
  const aiLockRef = useRef(false)
  const [aiBusy, setAiBusy] = useState(false)
  connRef.current = connectionId
  resultTabsRef.current = resultTabs
  resultCounterRef.current = resultCounter
  const busy = runningTabId !== null || aiBusy

  const resolvedActiveId = activeTabId && tabs.some((x) => x.id === activeTabId) ? activeTabId : tabs[0]?.id
  const activeTab = tabs.find((x) => x.id === resolvedActiveId) || tabs[0]
  const sql = activeTab?.sql ?? ''
  const error = activeTab?.error ?? ''

  const resolvedResultId =
    activeResultId && resultTabs.some((x) => x.id === activeResultId) ? activeResultId : resultTabs[0]?.id
  const activeResultTab = resultTabs.find((x) => x.id === resolvedResultId) || resultTabs[0]
  const result = activeResultTab?.result ?? null

  function patchTab(id: string, patch: Partial<EditorTab>) {
    setTabs((list) => list.map((tab) => (tab.id === id ? { ...tab, ...patch } : tab)))
  }

  function patchResultTab(id: string, patch: Partial<ResultTab>) {
    setResultTabs((list) => list.map((tab) => (tab.id === id ? { ...tab, ...patch } : tab)))
  }

  function setSql(next: string) {
    if (!activeTab) return
    patchTab(activeTab.id, { sql: next })
  }

  function setError(next: string) {
    if (!activeTab) return
    patchTab(activeTab.id, { error: next })
  }

  function addTab() {
    const n = tabCounter + 1
    setTabCounter(n)
    const tab = makeTab(connType, t('query.tabName', { n }))
    setTabs((list) => [...list, tab])
    setActiveTabId(tab.id)
  }

  function closeTab(id: string) {
    if (tabs.length <= 1) return
    const idx = tabs.findIndex((x) => x.id === id)
    const next = tabs.filter((x) => x.id !== id)
    setTabs(next)
    if (resolvedActiveId === id) {
      const fallback = next[Math.max(0, idx - 1)] || next[0]
      setActiveTabId(fallback.id)
    }
  }

  function addResultTab() {
    const n = resultCounterRef.current + 1
    resultCounterRef.current = n
    setResultCounter(n)
    const tab = makeResultTab(t('query.resultTabName', { n }))
    setResultTabs((list) => [...list, tab])
    setActiveResultId(tab.id)
  }

  /** Write a successful result into the active result tab, or create one. No empty shell tabs. */
  function commitQueryResult(preferredId: string | undefined, result: QueryResult) {
    const list = resultTabsRef.current
    if (preferredId && list.some((x) => x.id === preferredId)) {
      patchResultTab(preferredId, { result, kind: 'table', markdown: '', loading: false })
      setActiveResultId(preferredId)
      return
    }
    const n = resultCounterRef.current + 1
    resultCounterRef.current = n
    setResultCounter(n)
    const tab = { ...makeResultTab(t('query.resultTabName', { n }), 'table'), result }
    setResultTabs((prev) => [...prev, tab])
    setActiveResultId(tab.id)
  }

  function commitAiResult(markdown: string, loading: boolean) {
    const list = resultTabsRef.current
    const existing = list.find((x) => x.kind === 'ai')
    if (existing) {
      patchResultTab(existing.id, { markdown, loading, result: null, title: t('query.aiResultTab') })
      setActiveResultId(existing.id)
      return existing.id
    }
    const n = resultCounterRef.current + 1
    resultCounterRef.current = n
    setResultCounter(n)
    const tab: ResultTab = {
      ...makeResultTab(t('query.aiResultTab'), 'ai'),
      markdown,
      loading,
    }
    setResultTabs((prev) => [...prev, tab])
    setActiveResultId(tab.id)
    return tab.id
  }

  async function runAiAnalyze() {
    const text = sql.trim()
    if (!text || runningTabId !== null || aiLockRef.current) return
    aiLockRef.current = true
    setAiBusy(true)
    setError('')
    commitAiResult('', true)
    try {
      const md = await onAiAnalyze(text)
      commitAiResult(md || '', false)
    } catch (e: any) {
      const msg = String(e?.message || e)
      commitAiResult(`**Error**\n\n\`\`\`\n${msg}\n\`\`\``, false)
      setError(msg)
    } finally {
      aiLockRef.current = false
      setAiBusy(false)
    }
  }

  function closeResultTab(id: string) {
    const idx = resultTabs.findIndex((x) => x.id === id)
    const next = resultTabs.filter((x) => x.id !== id)
    setResultTabs(next)
    if (resolvedResultId === id) {
      const fallback = next[Math.max(0, idx - 1)] || next[0]
      setActiveResultId(fallback?.id || '')
    }
  }

  useEffect(() => {
    const first = makeTab(connType, t('query.tabName', { n: 1 }))
    setTabs([first])
    setActiveTabId(first.id)
    setTabCounter(1)
    setResultTabs([])
    setActiveResultId('')
    setResultCounter(0)
    resultCounterRef.current = 0
    setRunningTabId(null)
    runningLockRef.current = false
    setCtx(null)
    setImportTarget(null)
    // Schema selection is owned by App work-bar; do not clear it here (avoids race with defaults).
    // Only reset editors when the DB connection changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connectionId, connType])

  useEffect(() => {
    if (!ctx) return
    function onDoc(e: MouseEvent) {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) setCtx(null)
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') setCtx(null)
    }
    document.addEventListener('mousedown', onDoc)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDoc)
      document.removeEventListener('keydown', onKey)
    }
  }, [ctx])

  function flash(msg: string) {
    setToast(msg)
    window.setTimeout(() => setToast(''), 1600)
  }

  async function runSql(nextSql: string) {
    const tabId = activeTab?.id
    if (!tabId) return
    const runConn = connectionId
    if (!connected || !runConn) {
      patchTab(tabId, { error: t('query.needConnect') })
      return
    }
    if (runningLockRef.current) return
    runningLockRef.current = true
    const preferredResultId = activeResultTab?.id
    patchTab(tabId, { sql: nextSql, error: '' })
    setRunningTabId(tabId)
    try {
      const mutating = await IsMutatingQuery(runConn, nextSql)
      if (connRef.current !== runConn) return
      if (mutating) {
        const allowed = await WritesAllowed()
        if (connRef.current !== runConn) return
        if (!allowed) {
          patchTab(tabId, { error: t('query.writeBlocked') })
          return
        }
        if (!window.confirm(t('query.writeConfirm'))) {
          return
        }
      }
      // Apply workbench schema on the live session before execute (avoid Connect→effect race).
      const schema = activeSchema.trim()
      if (schema) {
        try {
          await SetActiveSchema(runConn, schema)
        } catch { /* Execute may still work; surface driver errors from query */ }
        if (connRef.current !== runConn) return
      }
      const res = await ExecuteQuery(runConn, nextSql)
      if (connRef.current !== runConn) return
      patchTab(tabId, { error: '' })
      commitQueryResult(preferredResultId, res as unknown as QueryResult)
      // Keep work-bar schema in sync after mongosh `use <db>`.
      if (connType === 'mongo') {
        const useMatch = nextSql.trim().match(/^use\s+([A-Za-z_][\w$-]*)\s*;?\s*$/i)
        if (useMatch) onActiveSchemaChange?.(useMatch[1])
      }
      // 建表/删对象等写操作成功后刷新左侧对象树。
      if (mutating) onRefreshObjects()
    } catch (e: any) {
      if (connRef.current !== runConn) return
      // Failures stay on the editor toolbar — do not spawn empty result tabs.
      patchTab(tabId, { error: String(e?.message || e) })
    } finally {
      runningLockRef.current = false
      setRunningTabId((cur) => (cur === tabId ? null : cur))
    }
  }

  function fillSql(next: string) {
    setSql(next)
    setError('')
    flash(t('query.ctx.filled'))
  }

  function onPick(node: ObjectNode, path: string[]) {
    const catalog = workbenchCatalog(path, node, connType)
    if (catalog) onActiveSchemaChange?.(catalog)
    if (connType === 'redis') {
      if (node.kind === 'key') setSql(`GET ${quoteRedisArg(node.name)}`)
      return
    }
    if (connType === 'mongo') {
      if (node.kind === 'collection') setSql(`${mongoColl(node.name)}.find({}).limit(100)`)
      return
    }
    if (node.kind === 'table' || node.kind === 'view') {
      const schema = schemaOf(path, node, connType)
      setSql(`SELECT * FROM ${tableRef(connType, schema || path[0] || '', node.name)} LIMIT 100`)
    }
  }

  function buildActionSql(action: CtxAction, node: ObjectNode, path: string[]): { mode: 'run' | 'fill'; sql: string } | null {
    const schema = schemaOf(path, node, connType)
    const qi = (n: string) => quoteIdent(connType, n)
    const hint = `-- ${t('query.ctx.editHint')}`
    const schemaRef = schema ? qi(schema) : ''

    // Column display names may include " · type" — use raw leaf for DDL when possible.
    const colName = node.kind === 'column' ? node.name.split(' · ')[0].trim() : node.name
    const indexName = node.kind === 'index' ? node.name.replace(/\s*\(.*\)$/, '').trim() : node.name

    switch (action) {
      case 'redisPing':
        return { mode: 'run', sql: 'PING' }
      case 'select':
        if (node.kind === 'sequence' && connType === 'postgres') {
          return { mode: 'run', sql: `SELECT nextval('${escLit(schema)}.${escLit(node.name)}') AS next_value;` }
        }
        if (node.kind === 'key') return { mode: 'run', sql: `GET ${quoteRedisArg(node.name)}` }
        if (node.kind === 'collection') return { mode: 'run', sql: `${mongoColl(node.name)}.find({}).limit(100)` }
        if (connType === 'mongo' && (node.kind === 'database' || node.kind === 'schema')) {
          return { mode: 'run', sql: 'show collections' }
        }
        return { mode: 'run', sql: `SELECT * FROM ${tableRef(connType, schema, node.name)} LIMIT 100` }
      case 'describe':
        if (node.kind === 'collection') {
          return { mode: 'run', sql: `${mongoColl(node.name)}.getIndexes()` }
        }
        if (connType === 'mysql') return { mode: 'run', sql: `DESCRIBE ${tableRef(connType, schema, node.name)}` }
        if (connType === 'postgres') {
          return {
            mode: 'run',
            sql:
              `SELECT column_name, data_type, is_nullable, column_default ` +
              `FROM information_schema.columns ` +
              `WHERE table_schema = '${escLit(schema)}' AND table_name = '${escLit(node.name)}' ` +
              `ORDER BY ordinal_position`,
          }
        }
        return null
      case 'showIndexes':
        if (connType === 'mysql') return { mode: 'run', sql: `SHOW INDEX FROM ${tableRef(connType, schema, node.name)}` }
        if (connType === 'postgres') {
          return {
            mode: 'run',
            sql:
              `SELECT indexname, indexdef FROM pg_indexes ` +
              `WHERE schemaname = '${escLit(schema)}' AND tablename = '${escLit(node.name)}' ORDER BY indexname`,
          }
        }
        return null
      case 'showCreate': {
        const ref = tableRef(connType, schema, node.name)
        if (connType === 'postgres') {
          if (node.kind === 'view') {
            return {
              mode: 'run',
              sql: `SELECT pg_get_viewdef('${escLit(schema)}.${escLit(node.name)}'::regclass, true) AS definition`,
            }
          }
          if (node.kind === 'procedure' || node.kind === 'function') {
            return {
              mode: 'run',
              sql:
                `SELECT pg_get_functiondef(p.oid) AS definition ` +
                `FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace ` +
                `WHERE n.nspname = '${escLit(schema)}' AND p.proname = '${escLit(node.name)}'`,
            }
          }
          // No stock SHOW CREATE TABLE — dump columns + indexdefs as a practical definition view.
          return {
            mode: 'run',
            sql:
              `SELECT 'COLUMN' AS part, c.column_name AS name, ` +
              `c.data_type || CASE WHEN c.is_nullable = 'NO' THEN ' NOT NULL' ELSE '' END || ` +
              `COALESCE(' DEFAULT ' || c.column_default, '') AS detail ` +
              `FROM information_schema.columns c ` +
              `WHERE c.table_schema = '${escLit(schema)}' AND c.table_name = '${escLit(node.name)}' ` +
              `UNION ALL ` +
              `SELECT 'INDEX', i.indexname, i.indexdef ` +
              `FROM pg_indexes i ` +
              `WHERE i.schemaname = '${escLit(schema)}' AND i.tablename = '${escLit(node.name)}' ` +
              `ORDER BY 1, 2`,
          }
        }
        if (connType !== 'mysql') return null
        if (node.kind === 'view') return { mode: 'run', sql: `SHOW CREATE VIEW ${ref}` }
        if (node.kind === 'procedure') return { mode: 'run', sql: `SHOW CREATE PROCEDURE ${ref}` }
        if (node.kind === 'function') return { mode: 'run', sql: `SHOW CREATE FUNCTION ${ref}` }
        if (node.kind === 'trigger') return { mode: 'run', sql: `SHOW CREATE TRIGGER ${qi(node.name)}` }
        if (node.kind === 'event') return { mode: 'run', sql: `SHOW CREATE EVENT ${ref}` }
        return { mode: 'run', sql: `SHOW CREATE TABLE ${ref}` }
      }
      case 'useSchema':
        if (connType === 'mongo') return { mode: 'run', sql: `use ${node.name}` }
        return null
      case 'createTable':
        return {
          mode: 'fill',
          sql:
            `${hint}\nCREATE TABLE ${schemaRef ? `${schemaRef}.` : ''}${qi('new_table')} (\n` +
            `  ${qi('id')} BIGINT PRIMARY KEY,\n` +
            `  ${qi('name')} VARCHAR(64) NOT NULL\n);`,
        }
      case 'createView':
        return {
          mode: 'fill',
          sql: `${hint}\nCREATE VIEW ${schemaRef ? `${schemaRef}.` : ''}${qi('new_view')} AS\nSELECT 1 AS ${qi('col')};`,
        }
      case 'createProcedure':
        if (connType === 'mysql') {
          // 单语句过程体，避免 DELIMITER（当前执行器不支持）。
          return {
            mode: 'fill',
            sql: `${hint}\nCREATE PROCEDURE ${schemaRef ? `${schemaRef}.` : ''}${qi('new_proc')}()\nSELECT 1 AS ok;`,
          }
        }
        return {
          mode: 'fill',
          sql: `${hint}\nCREATE OR REPLACE PROCEDURE ${schemaRef ? `${schemaRef}.` : ''}${qi('new_proc')}()\nLANGUAGE plpgsql\nAS $$\nBEGIN\n  -- body\nEND;\n$$;`,
        }
      case 'createFunction':
        if (connType === 'mysql') {
          return {
            mode: 'fill',
            sql:
              `${hint}\nCREATE FUNCTION ${schemaRef ? `${schemaRef}.` : ''}${qi('new_fn')}() ` +
              `RETURNS INT DETERMINISTIC\nRETURN 1;`,
          }
        }
        return {
          mode: 'fill',
          sql:
            `${hint}\nCREATE OR REPLACE FUNCTION ${schemaRef ? `${schemaRef}.` : ''}${qi('new_fn')}()\nRETURNS INT\nLANGUAGE sql\nAS $$ SELECT 1 $$;`,
        }
      case 'createTrigger':
        if (connType === 'mysql') {
          return {
            mode: 'fill',
            sql:
              `${hint}\nCREATE TRIGGER ${qi('trg_name')} BEFORE INSERT ON ${schemaRef ? `${schemaRef}.` : ''}${qi('table_name')}\n` +
              `FOR EACH ROW SET NEW.${qi('id')} = NEW.${qi('id')};`,
          }
        }
        return {
          mode: 'fill',
          sql:
            `${hint}\nCREATE OR REPLACE FUNCTION ${qi('trg_fn')}() RETURNS trigger AS $$\nBEGIN\n  RETURN NEW;\nEND;\n$$ LANGUAGE plpgsql;\n\n` +
            `CREATE TRIGGER ${qi('trg_name')} BEFORE INSERT ON ${schemaRef ? `${schemaRef}.` : ''}${qi('table_name')}\n` +
            `FOR EACH ROW EXECUTE FUNCTION ${qi('trg_fn')}();`,
        }
      case 'createEvent':
        return {
          mode: 'fill',
          sql:
            `${hint}\nCREATE EVENT ${schemaRef ? `${schemaRef}.` : ''}${qi('evt_name')}\n` +
            `ON SCHEDULE EVERY 1 DAY\nDO\n  SELECT 1;`,
        }
      case 'createSequence':
        return {
          mode: 'fill',
          sql: `${hint}\nCREATE SEQUENCE ${schemaRef ? `${schemaRef}.` : ''}${qi('seq_name')} START 1;`,
        }
      case 'callRoutine':
        if (node.kind === 'procedure') {
          if (connType === 'mysql') return { mode: 'fill', sql: `${hint}\nCALL ${tableRef(connType, schema, node.name)}();` }
          return { mode: 'fill', sql: `${hint}\nCALL ${tableRef(connType, schema, node.name)}();` }
        }
        if (node.kind === 'function') {
          return { mode: 'fill', sql: `${hint}\nSELECT ${tableRef(connType, schema, node.name)}();` }
        }
        return null
      case 'addColumn': {
        const table = node.kind === 'table' ? node.name : relationFromPath(path, node, connType) || node.name
        const ref = tableRef(connType, schema, table)
        if (connType === 'postgres') return { mode: 'fill', sql: `${hint}\nALTER TABLE ${ref} ADD COLUMN ${qi('new_col')} VARCHAR(64);` }
        return { mode: 'fill', sql: `${hint}\nALTER TABLE ${ref} ADD COLUMN ${qi('new_col')} VARCHAR(64) NULL;` }
      }
      case 'modifyColumn': {
        const table = node.kind === 'column' ? relationFromPath(path, node, connType) : node.name
        const col = node.kind === 'column' ? colName : 'col_name'
        const ref = tableRef(connType, schema, table)
        const curType =
          node.kind === 'column' && node.name.includes(' · ')
            ? node.name.split(' · ').slice(1).join(' · ').trim()
            : ''
        // Target type must be edited — never default to current type (avoids no-op DDL).
        const newType = connType === 'postgres' ? 'bigint' : 'BIGINT'
        const newBase = newType.split('(')[0].trim()
        const colHint = `-- ${t('query.ctx.modifyColumnHint')}`
        const curNote = curType ? `-- ${t('query.ctx.modifyColumnCurrent', { type: curType })}\n` : ''
        if (connType === 'postgres') {
          return {
            mode: 'fill',
            sql:
              `${colHint}\n` +
              curNote +
              `-- ${t('query.ctx.modifyColumnEditType')}\n` +
              `-- ${t('query.ctx.modifyColumnCheck')}\n` +
              `-- SELECT COUNT(*) FROM ${ref} WHERE ${qi(col)} IS NOT NULL AND ${qi(col)}::text !~ '^[0-9]+$';\n` +
              `ALTER TABLE ${ref}\n` +
              `  ALTER COLUMN ${qi(col)} TYPE ${newType} USING ${qi(col)}::${newBase};`,
          }
        }
        return {
          mode: 'fill',
          sql:
            `${colHint}\n` +
            curNote +
            `-- ${t('query.ctx.modifyColumnEditType')}\n` +
            `-- ${t('query.ctx.modifyColumnCheck')}\n` +
            `-- SELECT COUNT(*) FROM ${ref} WHERE ${qi(col)} IS NOT NULL AND ${qi(col)} NOT REGEXP '^[0-9]+$';\n` +
            `-- ${t('query.ctx.modifyColumnInplace')}\n` +
            `ALTER TABLE ${ref}\n` +
            `  MODIFY COLUMN ${qi(col)} ${newType} NULL;\n` +
            `-- , ALGORITHM=INPLACE, LOCK=NONE  /* ${t('query.ctx.modifyColumnAlgo')} */`,
        }
      }
      case 'migrateColumnExpand': {
        const table = node.kind === 'column'
          ? relationFromPath(path, node, connType)
          : (node.kind === 'table' ? node.name : relationFromPath(path, node, connType))
        const col = node.kind === 'column' ? colName : 'col_name'
        const ref = tableRef(connType, schema, table || 'table_name')
        const curType =
          node.kind === 'column' && node.name.includes(' · ')
            ? node.name.split(' · ').slice(1).join(' · ').trim()
            : ''
        const newType = connType === 'postgres' ? 'bigint' : 'BIGINT'
        const newBase = newType.split('(')[0].trim()
        const neu = `${col}_new`
        const oneByOne = `-- ${t('query.ctx.migrateRunOneByOne')}`
        const head = `-- ${t('query.ctx.migrateColumnExpandHint')}`
        const curNote = curType ? `-- ${t('query.ctx.modifyColumnCurrent', { type: curType })}\n` : ''
        if (connType === 'postgres') {
          return {
            mode: 'fill',
            sql:
              `${head}\n${oneByOne}\n` +
              curNote +
              `-- ${t('query.ctx.modifyColumnEditType')}\n` +
              `-- 1) ${t('query.ctx.migrateStepAdd')}\n` +
              `ALTER TABLE ${ref} ADD COLUMN ${qi(neu)} ${newType};\n` +
              `\n-- 2) ${t('query.ctx.migrateStepBackfill')}\n` +
              `UPDATE ${ref} SET ${qi(neu)} = ${qi(col)}::${newBase} WHERE ${qi(neu)} IS NULL /* AND id > ? LIMIT 1000 */;\n` +
              `\n-- 3) ${t('query.ctx.migrateStepCutover')}\n` +
              `\n-- 4) ${t('query.ctx.migrateStepSwap')}\n` +
              `ALTER TABLE ${ref} DROP COLUMN ${qi(col)};\n` +
              `ALTER TABLE ${ref} RENAME COLUMN ${qi(neu)} TO ${qi(col)};`,
          }
        }
        return {
          mode: 'fill',
          sql:
            `${head}\n${oneByOne}\n` +
            curNote +
            `-- ${t('query.ctx.modifyColumnEditType')}\n` +
            `-- 1) ${t('query.ctx.migrateStepAdd')}\n` +
            `ALTER TABLE ${ref} ADD COLUMN ${qi(neu)} ${newType} NULL;\n` +
            `\n-- 2) ${t('query.ctx.migrateStepBackfill')}\n` +
            `UPDATE ${ref} SET ${qi(neu)} = CAST(${qi(col)} AS ${newBase}) WHERE ${qi(neu)} IS NULL /* AND id > ? LIMIT 1000 */;\n` +
            `\n-- 3) ${t('query.ctx.migrateStepCutover')}\n` +
            `\n-- 4) ${t('query.ctx.migrateStepSwap')} (${t('query.ctx.migrateMysql8Rename')})\n` +
            `ALTER TABLE ${ref} DROP COLUMN ${qi(col)};\n` +
            `ALTER TABLE ${ref} RENAME COLUMN ${qi(neu)} TO ${qi(col)};`,
        }
      }
      case 'migrateTableCopy': {
        const table = node.kind === 'table' ? node.name : (relationFromPath(path, node, connType) || 'table_name')
        const ref = tableRef(connType, schema, table)
        const neuName = `${table}_new`
        const oldName = `${table}_old`
        const neu = tableRef(connType, schema, neuName)
        const old = tableRef(connType, schema, oldName)
        const head = `-- ${t('query.ctx.migrateTableCopyHint')}`
        const oneByOne = `-- ${t('query.ctx.migrateRunOneByOne')}`
        const noSwap = `-- !!! ${t('query.ctx.migrateStepCopyNoSwap')} !!!`
        if (connType === 'postgres') {
          return {
            mode: 'fill',
            sql:
              `${head}\n${oneByOne}\n` +
              `-- 1) ${t('query.ctx.migrateStepCopyCreate')}\n` +
              `CREATE TABLE ${neu} (LIKE ${ref} INCLUDING ALL);\n` +
              `\n-- 2) ${t('query.ctx.migrateStepCopyAlter')}\n` +
              `-- ALTER TABLE ${neu} ALTER COLUMN ${qi('col_name')} TYPE bigint USING ${qi('col_name')}::bigint;\n` +
              `\n-- 3) ${t('query.ctx.migrateStepCopyData')}\n` +
              `INSERT INTO ${neu} SELECT * FROM ${ref} /* WHERE id > ? ORDER BY id LIMIT 1000 */;\n` +
              `\n-- ${t('query.ctx.migrateStepCopyCatchup')}\n` +
              `${noSwap}\n` +
              `\n-- 4) ${t('query.ctx.migrateStepCopySwap')}\n` +
              `BEGIN;\n` +
              `ALTER TABLE ${ref} RENAME TO ${qi(oldName)};\n` +
              `ALTER TABLE ${neu} RENAME TO ${qi(table)};\n` +
              `COMMIT;\n` +
              `\n-- 5) ${t('query.ctx.migrateStepCopyDrop')}\n` +
              `-- DROP TABLE ${old};`,
          }
        }
        return {
          mode: 'fill',
          sql:
            `${head}\n${oneByOne}\n` +
            `-- 1) ${t('query.ctx.migrateStepCopyCreate')}\n` +
            `CREATE TABLE ${neu} LIKE ${ref};\n` +
            `\n-- 2) ${t('query.ctx.migrateStepCopyAlter')}\n` +
            `-- ALTER TABLE ${neu} MODIFY COLUMN ${qi('col_name')} BIGINT NULL;\n` +
            `\n-- 3) ${t('query.ctx.migrateStepCopyData')}\n` +
            `INSERT INTO ${neu} SELECT * FROM ${ref} /* WHERE id > ? ORDER BY id LIMIT 1000 */;\n` +
            `\n-- ${t('query.ctx.migrateStepCopyCatchup')}\n` +
            `${noSwap}\n` +
            `\n-- 4) ${t('query.ctx.migrateStepCopySwap')}\n` +
            `RENAME TABLE ${ref} TO ${old}, ${neu} TO ${ref};\n` +
            `\n-- 5) ${t('query.ctx.migrateStepCopyDrop')}\n` +
            `-- DROP TABLE ${old};`,
        }
      }
      case 'dropColumn': {
        const table = node.kind === 'column' ? relationFromPath(path, node, connType) : node.name
        const col = node.kind === 'column' ? colName : 'col_name'
        return { mode: 'fill', sql: `${hint}\nALTER TABLE ${tableRef(connType, schema, table)} DROP COLUMN ${qi(col)};` }
      }
      case 'createIndex': {
        const table = node.kind === 'folder'
          ? relationFromPath(path, node, connType)
          : (node.kind === 'table' ? node.name : (relationFromPath(path, node, connType) || node.name))
        const ref = tableRef(connType, schema, table)
        return { mode: 'fill', sql: `${hint}\nCREATE INDEX ${qi('idx_name')} ON ${ref} (${qi('col_name')});` }
      }
      case 'modifyIndex': {
        // Cannot ALTER index columns in place. Prefer create-temp → drop-old → rename
        // so the table is never without a covering index (same name cannot coexist).
        const table = relationFromPath(path, node, connType)
        const ref = tableRef(connType, schema, table)
        const m = node.name.match(/^(.*?)\s*\((.*)\)\s*$/)
        const idx = (m ? m[1] : indexName).trim() || 'idx_name'
        const rawCols = (m ? m[2] : 'col_name').split(',').map((c) => c.trim()).filter(Boolean)
        const colsSql = (rawCols.length ? rawCols : ['col_name']).map((c) => qi(c)).join(', ')
        const unique = node.badge === 'UNI'
        const recreateHint = `-- ${t('query.ctx.modifyIndexHint')}`
        const oneByOne = `-- ${t('query.ctx.migrateRunOneByOne')}`
        if (idx.toUpperCase() === 'PRIMARY') {
          // Only one PRIMARY KEY is allowed — must drop then add (brief gap unavoidable).
          return {
            mode: 'fill',
            sql:
              `${recreateHint}\n${oneByOne}\n` +
              `ALTER TABLE ${ref} DROP PRIMARY KEY, ADD PRIMARY KEY (${colsSql});`,
          }
        }
        const tmp = `${idx}_tmp`
        if (connType === 'postgres') {
          const qOld = schema ? `${qi(schema)}.${qi(idx)}` : qi(idx)
          const qTmp = schema ? `${qi(schema)}.${qi(tmp)}` : qi(tmp)
          return {
            mode: 'fill',
            sql:
              `${recreateHint}\n${oneByOne}\n` +
              `CREATE ${unique ? 'UNIQUE ' : ''}INDEX ${qi(tmp)} ON ${ref} (${colsSql});\n` +
              `\nDROP INDEX IF EXISTS ${qOld};\n` +
              `\nALTER INDEX ${qTmp} RENAME TO ${qi(idx)};`,
          }
        }
        return {
          mode: 'fill',
          sql:
            `${recreateHint}\n${oneByOne}\n` +
            `CREATE ${unique ? 'UNIQUE ' : ''}INDEX ${qi(tmp)} ON ${ref} (${colsSql});\n` +
            `\nDROP INDEX ${qi(idx)} ON ${ref};\n` +
            `\nALTER TABLE ${ref} RENAME INDEX ${qi(tmp)} TO ${qi(idx)};`,
        }
      }
      case 'dropIndex': {
        if (node.kind === 'index') {
          const table = relationFromPath(path, node, connType)
          if (connType === 'postgres') return { mode: 'fill', sql: `${hint}\nDROP INDEX IF EXISTS ${schema ? `${qi(schema)}.` : ''}${qi(indexName)};` }
          return { mode: 'fill', sql: `${hint}\nDROP INDEX ${qi(indexName)} ON ${tableRef(connType, schema, table)};` }
        }
        const ref = tableRef(connType, schema, node.name)
        if (connType === 'postgres') return { mode: 'fill', sql: `${hint}\nDROP INDEX IF EXISTS ${qi('idx_name')};` }
        return { mode: 'fill', sql: `${hint}\nDROP INDEX ${qi('idx_name')} ON ${ref};` }
      }
      case 'dropObject': {
        const ref = tableRef(connType, schema, node.name)
        if (node.kind === 'key') return { mode: 'fill', sql: `DEL ${quoteRedisArg(node.name)}` }
        if (node.kind === 'collection') return { mode: 'fill', sql: `${hint}\n${mongoColl(node.name)}.drop()` }
        if (node.kind === 'table') return { mode: 'fill', sql: `${hint}\nDROP TABLE ${ref};` }
        if (node.kind === 'view') return { mode: 'fill', sql: `${hint}\nDROP VIEW ${ref};` }
        if (node.kind === 'procedure') return { mode: 'fill', sql: `${hint}\nDROP PROCEDURE ${ref};` }
        if (node.kind === 'function') return { mode: 'fill', sql: `${hint}\nDROP FUNCTION ${ref};` }
        if (node.kind === 'trigger') {
          if (connType === 'mysql') return { mode: 'fill', sql: `${hint}\nDROP TRIGGER ${schema ? `${qi(schema)}.` : ''}${qi(node.name)};` }
          return { mode: 'fill', sql: `${hint}\nDROP TRIGGER IF EXISTS ${ref};` }
        }
        if (node.kind === 'event') return { mode: 'fill', sql: `${hint}\nDROP EVENT ${ref};` }
        if (node.kind === 'sequence') return { mode: 'fill', sql: `${hint}\nDROP SEQUENCE ${ref};` }
        return null
      }
      default:
        return null
    }
  }

  async function onCtxAction(action: CtxAction) {
    if (!ctx) return
    const { node, path } = ctx
    setCtx(null)

    if (action === 'createDatabase') {
      setCreateDbOpen(true)
      return
    }

    if (action === 'copyName') {
      let text = node.name
      if (node.kind === 'folder') text = path[0] ? `${path[0]}.${node.name}` : node.name
      else if (node.kind !== 'schema' && node.kind !== 'database' && path[0]) text = `${path[0]}.${node.name}`
      if (node.kind === 'column') text = node.name.split(' · ')[0].trim()
      if (node.kind === 'index') text = node.name.replace(/\s*\(.*\)$/, '').trim()
      try {
        await navigator.clipboard.writeText(text)
        flash(t('query.ctx.copyOk'))
      } catch {
        flash(t('query.ctx.copyFail'))
      }
      return
    }

    if (action === 'import') {
      const schema = schemaOf(path, node, connType)
      const ref = tableRef(connType, schema, node.name)
      setImportCsv('')
      setImportErr('')
      setImportTarget({ schema, table: node.name, ref })
      return
    }

    if (action === 'copyToOtherDb') {
      if (isNonSQL(connType)) return
      if (connType === 'postgres' && node.kind === 'database') {
        flash(t('query.copyDb.pgNoCrossDb'))
        return
      }
      // MySQL: database name. PG: schema namespace within the current database.
      const source = schemaOf(path, node, connType)
      if (!source) return
      setCopyDbErr('')
      setCopyDbTarget('')
      setCopyDbPrompt({
        sourceSchema: source,
        tableName: node.kind === 'table' ? node.name : undefined,
      })
      return
    }

    const built = buildActionSql(action, node, path)
    if (!built) return
    if (built.mode === 'fill') fillSql(built.sql)
    else await runSql(built.sql)
  }

  function applyImport() {
    if (!importTarget) return
    const rows = parseCsv(importCsv)
    if (rows.length < 2) {
      setImportErr(t('query.import.needRows'))
      return
    }
    const headers = rows[0].map((h) => h.trim()).filter(Boolean)
    if (!headers.length) {
      setImportErr(t('query.import.needHeader'))
      return
    }
    const data = rows.slice(1).filter((r) => r.some((c) => c.trim() !== ''))
    if (!data.length) {
      setImportErr(t('query.import.needRows'))
      return
    }
    const next = buildInserts(connType, importTarget.ref, headers, data)
    setSql(next)
    setImportTarget(null)
    setImportErr('')
    flash(t('query.import.filled'))
  }

  const [copyDbSchemaOptions, setCopyDbSchemaOptions] = useState<string[]>([])
  useEffect(() => {
    if (!copyDbPrompt || !connectionId) {
      setCopyDbSchemaOptions([])
      return
    }
    let cancelled = false
    void (async () => {
      if (connType === 'postgres') {
        const db = activeSchema || objects.find((n) => n.kind === 'database')?.name || ''
        if (!db) {
          if (!cancelled) setCopyDbSchemaOptions([])
          return
        }
        try {
          const kids = (await ExpandObject(connectionId, [db])) as unknown as ObjectNode[]
          if (cancelled) return
          setCopyDbSchemaOptions(
            (kids || [])
              .filter((n) => n.kind === 'schema' && n.name && n.name !== copyDbPrompt.sourceSchema)
              .map((n) => n.name),
          )
        } catch {
          if (!cancelled) setCopyDbSchemaOptions([])
        }
        return
      }
      if (!cancelled) {
        setCopyDbSchemaOptions(
          objects
            .filter((n) => n.kind === 'schema' || n.kind === 'database')
            .map((n) => n.name)
            .filter((name) => name && name !== copyDbPrompt.sourceSchema),
        )
      }
    })()
    return () => { cancelled = true }
  }, [copyDbPrompt, connectionId, connType, activeSchema, objects])

  const copyDbSchemas = copyDbSchemaOptions

  async function applyCopyDb() {
    if (!copyDbPrompt || copyDbBusy) return
    const target = copyDbTarget.trim()
    if (!target || target === copyDbPrompt.sourceSchema || !copyDbSchemas.includes(target)) {
      setCopyDbErr(t('query.copyDb.needTarget'))
      return
    }
    const runConn = connectionId
    setCopyDbBusy(true)
    setCopyDbErr('')
    try {
      let tables: string[] = []
      if (copyDbPrompt.tableName) {
        tables = [copyDbPrompt.tableName]
      } else {
        const tablePath = connType === 'postgres'
          ? [activeSchema || objects.find((n) => n.kind === 'database')?.name || '', copyDbPrompt.sourceSchema, 'tables']
          : [copyDbPrompt.sourceSchema, 'tables']
        if (connType === 'postgres' && !tablePath[0]) {
          setCopyDbErr(t('query.copyDb.needTarget'))
          return
        }
        const kids = (await ExpandObject(runConn, tablePath)) as unknown as ObjectNode[]
        if (connRef.current !== runConn) {
          setCopyDbPrompt(null)
          return
        }
        tables = (kids || []).filter((n) => n.kind === 'table' && n.name).map((n) => n.name)
      }
      if (connRef.current !== runConn) {
        setCopyDbPrompt(null)
        return
      }
      if (!tables.length) {
        setCopyDbErr(t('query.copyDb.noTables'))
        return
      }
      const sqlText = buildCopyToOtherDbSql(connType, copyDbPrompt.sourceSchema, target, tables, {
        head: t('query.ctx.copyToOtherDbHint'),
        oneByOne: t('query.ctx.migrateRunOneByOne'),
        stepDrop: t('query.ctx.copyToOtherDbStepDrop'),
        stepCreate: t('query.ctx.copyToOtherDbStepCreate'),
        stepData: t('query.ctx.copyToOtherDbStepData'),
        fkHint: t('query.ctx.copyToOtherDbFkHint'),
      })
      if (!sqlText) {
        setCopyDbErr(t('query.copyDb.needTarget'))
        return
      }
      setSql(sqlText)
      setError('')
      setCopyDbPrompt(null)
      setCopyDbTarget('')
      flash(t('query.copyDb.filled'))
    } catch (e: any) {
      if (connRef.current !== runConn) {
        setCopyDbPrompt(null)
        return
      }
      setCopyDbErr(String(e?.message || e))
    } finally {
      setCopyDbBusy(false)
    }
  }

  const menuItems = useMemo(() => (ctx ? menuFor(connType, ctx.node) : []), [connType, ctx])

  const menuStyle = ctx
    ? {
        left: Math.min(ctx.x, Math.max(8, window.innerWidth - 200)),
        top: Math.min(ctx.y, Math.max(8, window.innerHeight - 280)),
      }
    : undefined

  function onSplitDown(e: ReactMouseEvent) {
    e.preventDefault()
    const pane = editorPaneRef.current
    if (!pane) return
    const onMove = (ev: MouseEvent) => {
      const rect = pane.getBoundingClientRect()
      if (rect.height <= 0) return
      const y = ev.clientY - rect.top
      setSplitPct(Math.min(80, Math.max(20, (y / rect.height) * 100)))
    }
    const onUp = () => {
      document.body.classList.remove('query-splitting')
      window.removeEventListener('mousemove', onMove)
      window.removeEventListener('mouseup', onUp)
    }
    document.body.classList.add('query-splitting')
    window.addEventListener('mousemove', onMove)
    window.addEventListener('mouseup', onUp)
  }

  function onSideSplitDown(e: ReactMouseEvent) {
    e.preventDefault()
    const root = queryViewRef.current
    if (!root) return
    const onMove = (ev: MouseEvent) => {
      const rect = root.getBoundingClientRect()
      if (rect.width <= 0) return
      const x = ev.clientX - rect.left
      setSideWidth(Math.min(Math.floor(rect.width * 0.55), Math.max(160, Math.floor(x))))
    }
    const onUp = () => {
      document.body.classList.remove('query-side-splitting')
      window.removeEventListener('mousemove', onMove)
      window.removeEventListener('mouseup', onUp)
    }
    document.body.classList.add('query-side-splitting')
    window.addEventListener('mousemove', onMove)
    window.addEventListener('mouseup', onUp)
  }

  return (
    <div className="query-view" ref={queryViewRef}>
      <aside className="objects-pane" style={{ width: sideWidth, flexBasis: sideWidth }}>
        <div className="pane-title">
          <span>{t('query.objects')}</span>
          <button type="button" className="sm" disabled={!connected} onClick={onRefreshObjects}>{t('query.refresh')}</button>
        </div>
        <ObjectTree
          active={active}
          connectionId={connectionId}
          connected={connected}
          nodes={objects}
          selectedSchema={activeSchema}
          onPick={onPick}
          onContextMenu={(node, path, x, y) => {
            const catalog = workbenchCatalog(path, node, connType)
            if (catalog) onActiveSchemaChange?.(catalog)
            setCtx({ x, y, node, path })
          }}
        />
      </aside>
      <div
        className="query-side-splitter"
        role="separator"
        aria-orientation="vertical"
        aria-valuenow={sideWidth}
        title={t('query.sideSplitDrag')}
        onMouseDown={onSideSplitDown}
      />
      <section className="editor-pane" ref={editorPaneRef}>
        <div className="query-top" style={{ flex: `0 0 ${splitPct}%` }}>
          <div className="query-tabs" role="tablist" aria-label={t('query.tabs')}>
            {tabs.map((tab) => (
              <div
                key={tab.id}
                className={`query-tab${tab.id === activeTab?.id ? ' on' : ''}`}
                role="tab"
                aria-selected={tab.id === activeTab?.id}
                onClick={() => setActiveTabId(tab.id)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault()
                    setActiveTabId(tab.id)
                  }
                }}
                tabIndex={0}
              >
                <span className="query-tab-label">{tab.title || t('query.tabName', { n: 1 })}</span>
                {tabs.length > 1 && (
                  <button
                    type="button"
                    className="query-tab-close"
                    title={t('query.tabClose')}
                    aria-label={t('query.tabClose')}
                    onClick={(e) => {
                      e.stopPropagation()
                      closeTab(tab.id)
                    }}
                  >
                    ×
                  </button>
                )}
              </div>
            ))}
            <button
              type="button"
              className="query-tab-add"
              title={t('query.tabAdd')}
              aria-label={t('query.tabAdd')}
              onClick={addTab}
            >
              +
            </button>
          </div>
          <SqlEditor
            value={sql}
            onChange={setSql}
            connType={connType}
            connectionId={connectionId}
            connected={connected}
            objects={objects}
            activeSchema={activeSchema}
            disabled={busy}
            placeholder={
              connType === 'redis'
                ? t('query.placeholder.redis')
                : connType === 'mongo'
                  ? t('query.placeholder.mongo')
                  : t('query.placeholder.sql')
            }
            onRun={() => {
              if (!busy && connected && sql.trim()) void runSql(sql)
            }}
          />
          <div className="editor-toolbar">
            <div className="editor-toolbar-left">
              {error && <span className="error-text">{error}</span>}
              {toast && <span className="muted">{toast}</span>}
            </div>
            <div className="editor-toolbar-right">
              <button
                type="button"
                className="sm danger"
                disabled={busy}
                title={t('query.clear')}
                onClick={() => {
                  if (!activeTab) return
                  patchTab(activeTab.id, { sql: '', error: '' })
                  if (activeResultTab) patchResultTab(activeResultTab.id, { result: null })
                }}
              >
                {t('query.clear')}
              </button>
              <button
                type="button"
                className="sm"
                disabled={busy || !sql.trim() || isNonSQL(connType)}
                title={t('query.format')}
                onClick={() => {
                  setSql(formatSql(sql))
                  flash(t('query.formatOk'))
                }}
              >
                {t('query.format')}
              </button>
              <button
                type="button"
                className="sm"
                disabled={busy || !connected || !sql.trim() || isNonSQL(connType)}
                title={t('query.explain')}
                onClick={() => {
                  const q = explainSql(sql)
                  if (q) void runSql(q)
                }}
              >
                {t('query.explain')}
              </button>
              <button
                type="button"
                className="sm ai-action"
                disabled={busy || !sql.trim()}
                title={t('query.agentExplain')}
                onClick={() => void runAiAnalyze()}
              >
                {aiBusy ? t('query.aiAnalyzing') : t('query.agentExplain')}
              </button>
              <button type="button" className="primary" disabled={busy || !connected} onClick={() => void runSql(sql)}>
                {runningTabId === activeTab?.id ? t('query.running') : t('query.run')}
              </button>
            </div>
          </div>
        </div>
        <div
          className="query-splitter"
          role="separator"
          aria-orientation="horizontal"
          aria-valuenow={Math.round(splitPct)}
          title={t('query.splitDrag')}
          onMouseDown={onSplitDown}
        />
        <div className="query-bottom">
          {resultTabs.length > 0 && (
            <div className="query-tabs result-tabs" role="tablist" aria-label={t('query.resultTabs')}>
              {resultTabs.map((tab) => (
                <div
                  key={tab.id}
                  className={`query-tab${tab.id === activeResultTab?.id ? ' on' : ''}`}
                  role="tab"
                  aria-selected={tab.id === activeResultTab?.id}
                  onClick={() => setActiveResultId(tab.id)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault()
                      setActiveResultId(tab.id)
                    }
                  }}
                  tabIndex={0}
                >
                  <span className="query-tab-label">{tab.title || t('query.resultTabName', { n: 1 })}</span>
                  <button
                    type="button"
                    className="query-tab-close"
                    title={t('query.tabClose')}
                    aria-label={t('query.tabClose')}
                    onClick={(e) => {
                      e.stopPropagation()
                      closeResultTab(tab.id)
                    }}
                  >
                    ×
                  </button>
                </div>
              ))}
              <button
                type="button"
                className="query-tab-add"
                title={t('query.resultTabAdd')}
                aria-label={t('query.resultTabAdd')}
                onClick={addResultTab}
              >
                +
              </button>
            </div>
          )}
          {activeResultTab?.kind === 'ai' ? (
            <div className="result-ai-wrap">
              {activeResultTab.loading ? (
                <div className="result-ai-loading muted">{t('query.aiAnalyzing')}</div>
              ) : activeResultTab.markdown ? (
                <div className="result-ai-body">
                  <MarkdownBody>{activeResultTab.markdown}</MarkdownBody>
                </div>
              ) : (
                <div className="result-empty">{t('query.resultEmpty')}</div>
              )}
            </div>
          ) : (
            <ResultTable result={result} />
          )}
        </div>
      </section>

      {createDbOpen && (
        <CreateDatabaseModal
          connectionId={connectionId}
          connType={connType}
          onClose={() => setCreateDbOpen(false)}
          onCreated={() => {
            onRefreshObjects()
            flash(t('query.createDb.done'))
          }}
        />
      )}

      {ctx && menuItems.length > 0 && (
        <div className="ctx-menu" ref={menuRef} style={menuStyle} role="menu">
          {menuItems.map((item, i) =>
            'sep' in item ? (
              <div key={`sep-${i}`} className="ctx-sep" />
            ) : (
              <button
                key={item.action + item.labelKey}
                type="button"
                className="ctx-item"
                role="menuitem"
                onClick={() => void onCtxAction(item.action)}
              >
                {t(item.labelKey)}
              </button>
            )
          )}
        </div>
      )}

      {importTarget && (
        <BodyPortal>
          <div className="modal-backdrop" onMouseDown={(e) => { if (e.target === e.currentTarget) setImportTarget(null) }}>
            <div className="modal import-modal" role="dialog" aria-modal="true">
              <div className="modal-head">
                <h3>{t('query.import.title')}</h3>
                <button type="button" className="modal-close" onClick={() => setImportTarget(null)} aria-label={t('modal.close')}>×</button>
              </div>
              <p>
                {t('query.import.hint', { table: importTarget.schema ? `${importTarget.schema}.${importTarget.table}` : importTarget.table })}
              </p>
              <textarea
                className="import-csv"
                value={importCsv}
                onChange={(e) => setImportCsv(e.target.value)}
                placeholder={t('query.import.placeholder')}
                spellCheck={false}
              />
              {importErr && <p className="error-text">{importErr}</p>}
              <div className="modal-actions">
                <button type="button" onClick={() => setImportTarget(null)}>{t('conn.form.cancel')}</button>
                <button type="button" className="primary" onClick={applyImport}>{t('query.import.generate')}</button>
              </div>
            </div>
          </div>
        </BodyPortal>
      )}

      {copyDbPrompt && (
        <BodyPortal>
          <div
            className="modal-backdrop"
            onMouseDown={(e) => {
              if (e.target === e.currentTarget && !copyDbBusy) setCopyDbPrompt(null)
            }}
          >
            <div className="modal copy-db-modal" role="dialog" aria-modal="true">
              <div className="modal-head">
                <h3>{t('query.copyDb.title')}</h3>
                <button
                  type="button"
                  className="modal-close"
                  disabled={copyDbBusy}
                  onClick={() => setCopyDbPrompt(null)}
                  aria-label={t('modal.close')}
                >
                  ×
                </button>
              </div>
              <p>
                {t('query.copyDb.source', {
                  source: copyDbPrompt.tableName
                    ? `${copyDbPrompt.sourceSchema}.${copyDbPrompt.tableName}`
                    : copyDbPrompt.sourceSchema,
                })}
              </p>
              <p className="modal-hint">{t('query.copyDb.warn')}</p>
              <label>
                <span>{t('query.copyDb.target')}</span>
                <select
                  value={copyDbTarget}
                  disabled={copyDbBusy || copyDbSchemas.length === 0}
                  onChange={(e) => {
                    setCopyDbTarget(e.target.value)
                    setCopyDbErr('')
                  }}
                >
                  <option value="">{t('query.copyDb.targetPick')}</option>
                  {copyDbSchemas.map((name) => (
                    <option key={name} value={name}>{name}</option>
                  ))}
                </select>
              </label>
              {copyDbSchemas.length === 0 && <p className="error-text">{t('query.copyDb.noSchemas')}</p>}
              {copyDbBusy && <p className="muted">{t('query.copyDb.loading')}</p>}
              {copyDbErr && <p className="error-text">{copyDbErr}</p>}
              <div className="modal-actions">
                <button type="button" disabled={copyDbBusy} onClick={() => setCopyDbPrompt(null)}>{t('conn.form.cancel')}</button>
                <button
                  type="button"
                  className="primary"
                  disabled={copyDbBusy || copyDbSchemas.length === 0}
                  onClick={() => void applyCopyDb()}
                >
                  {t('query.copyDb.generate')}
                </button>
              </div>
            </div>
          </div>
        </BodyPortal>
      )}
    </div>
  )
}
