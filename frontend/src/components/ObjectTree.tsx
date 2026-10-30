import { useCallback, useEffect, useRef, useState } from 'react'
import { ObjectNode } from '../types'
import { ExpandObject } from '../../wailsjs/go/main/App'
import { useI18n } from '../i18n'

interface Props {
  /** When false, skip ExpandObject / refresh fetches. */
  active?: boolean
  connectionId: string
  connected: boolean
  nodes: ObjectNode[]
  onPick: (node: ObjectNode, path: string[]) => void
  onContextMenu?: (node: ObjectNode, path: string[], clientX: number, clientY: number) => void
}

function pathKey(path: string[]) {
  return path.join('\0')
}

function folderLabel(t: (k: string, vars?: Record<string, string | number>) => string, name: string) {
  const key = `query.folder.${name}`
  const label = t(key)
  return label === key ? name : label
}

function canExpand(node: ObjectNode) {
  if (node.lazy) return true
  if (node.children && node.children.length > 0) return true
  return node.kind === 'schema' || node.kind === 'database' || node.kind === 'folder' || node.kind === 'table' || node.kind === 'view'
}

const CTX_KINDS = new Set([
  'schema', 'database', 'folder',
  'table', 'view',
  'procedure', 'function', 'trigger', 'event', 'sequence',
  'index', 'column', 'key',
])

function NodeRow({
  node,
  path,
  connected,
  selectedKey,
  onPick,
  onContextMenu,
  expanded,
  childrenMap,
  loading,
  errors,
  toggle,
}: {
  node: ObjectNode
  path: string[]
  connected: boolean
  selectedKey: string
  onPick: Props['onPick']
  onContextMenu?: Props['onContextMenu']
  expanded: Set<string>
  childrenMap: Record<string, ObjectNode[]>
  loading: Set<string>
  errors: Record<string, string>
  toggle: (path: string[], node: ObjectNode) => void
}) {
  const { t } = useI18n()
  const key = pathKey(path)
  const isOpen = expanded.has(key)
  const kids = childrenMap[key] ?? node.children ?? []
  const expandable = canExpand(node)
  const isFolder = node.kind === 'folder'
  const selected = selectedKey === key
  const isLoading = loading.has(key)
  const err = errors[key] || ''

  return (
    <li className={`tree-node depth-${Math.min(path.length, 6)}`}>
      <div className={`tree-row${selected ? ' on' : ''}${isFolder ? ' folder' : ''}`}>
        {expandable ? (
          <button
            type="button"
            className="tree-twist"
            aria-expanded={isOpen}
            disabled={!connected || isLoading}
            onClick={() => toggle(path, node)}
            title={isOpen ? t('query.collapse') : t('query.expand')}
          >
            {isLoading ? '…' : isOpen ? '−' : '+'}
          </button>
        ) : (
          <span className="tree-twist spacer" />
        )}
        <button
          type="button"
          className="tree-item"
          onClick={() => {
            onPick(node, path)
            if (expandable && !isOpen && connected && !isLoading) toggle(path, node)
          }}
          onContextMenu={(e) => {
            if (!onContextMenu || !CTX_KINDS.has(node.kind)) return
            e.preventDefault()
            onPick(node, path)
            onContextMenu(node, path, e.clientX, e.clientY)
          }}
        >
          {isFolder && <span className="tree-ico folder" aria-hidden />}
          {(node.kind === 'schema' || node.kind === 'database') && <span className="tree-ico db" aria-hidden />}
          <span className="tree-name">
            {isFolder ? folderLabel(t, node.name) : node.name}
          </span>
        </button>
      </div>
      {isOpen && err && (
        <div className="tree-empty-branch error-text" title={err}>{t('query.expandFailed')}</div>
      )}
      {isOpen && !isLoading && !err && kids.length > 0 && (
        <ul className="tree-children">
          {kids.map((c) => (
            <NodeRow
              key={`${key}/${c.kind}:${c.name}`}
              node={c}
              path={[...path, c.name]}
              connected={connected}
              selectedKey={selectedKey}
              onPick={onPick}
              onContextMenu={onContextMenu}
              expanded={expanded}
              childrenMap={childrenMap}
              loading={loading}
              errors={errors}
              toggle={toggle}
            />
          ))}
        </ul>
      )}
      {isOpen && !isLoading && !err && kids.length === 0 && (
        <div className="tree-empty-branch muted">{t('query.emptyBranch')}</div>
      )}
    </li>
  )
}

export function ObjectTree({ active = true, connectionId, connected, nodes, onPick, onContextMenu }: Props) {
  const { t } = useI18n()
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set())
  const [childrenMap, setChildrenMap] = useState<Record<string, ObjectNode[]>>({})
  const [loading, setLoading] = useState<Set<string>>(() => new Set())
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [selectedKey, setSelectedKey] = useState('')

  const expandedRef = useRef(expanded)
  const childrenRef = useRef(childrenMap)
  const loadingRef = useRef(loading)
  const errorsRef = useRef(errors)
  const treeConnRef = useRef(connectionId)
  expandedRef.current = expanded
  childrenRef.current = childrenMap
  loadingRef.current = loading
  errorsRef.current = errors

  // 换连接：同步清空 ref，避免同轮「按 nodes 重拉」仍读到旧展开路径。
  useEffect(() => {
    const empty = new Set<string>()
    expandedRef.current = empty
    treeConnRef.current = connectionId
    setExpanded(empty)
    setChildrenMap({})
    setLoading(new Set())
    setErrors({})
    setSelectedKey('')
  }, [connectionId])

  // 刷新对象列表：保留展开；先标 loading 再替换 children，避免闪「（空）」。
  useEffect(() => {
    const forConn = connectionId
    // 连接已切换则由上一个 effect 负责重置，这里不再用旧路径打新连接。
    if (treeConnRef.current !== forConn) return
    if (!active) return

    const paths = Array.from(expandedRef.current)
      .map((k) => k.split('\0').filter(Boolean))
      .filter((p) => p.length > 0)
      .sort((a, b) => a.length - b.length)

    if (!forConn || !connected || paths.length === 0) {
      setErrors({})
      return
    }

    const keys = paths.map(pathKey)
    setLoading(new Set(keys))
    setErrors({})

    let cancelled = false
    ;(async () => {
      const nextMap: Record<string, ObjectNode[]> = {}
      const nextErrors: Record<string, string> = {}
      for (const path of paths) {
        if (cancelled || treeConnRef.current !== forConn) return
        const key = pathKey(path)
        try {
          const kids = (await ExpandObject(forConn, path)) as unknown as ObjectNode[]
          nextMap[key] = kids || []
        } catch (e: any) {
          nextErrors[key] = String(e?.message || e)
        }
      }
      if (cancelled || treeConnRef.current !== forConn) return
      setChildrenMap(nextMap)
      setErrors(nextErrors)
      setLoading(new Set())
    })()
    return () => { cancelled = true }
  }, [nodes, connectionId, connected, active])

  const loadChildren = useCallback(async (path: string[], node: ObjectNode, key: string) => {
    if (!active) return
    if (Object.prototype.hasOwnProperty.call(childrenRef.current, key)) return
    if (node.children && node.children.length > 0) {
      setChildrenMap((prev) => ({ ...prev, [key]: node.children || [] }))
      return
    }
    if (!connectionId || !connected) return
    if (loadingRef.current.has(key)) return

    setLoading((prev) => new Set(prev).add(key))
    setErrors((prev) => {
      if (!(key in prev)) return prev
      const next = { ...prev }
      delete next[key]
      return next
    })
    try {
      const kids = (await ExpandObject(connectionId, path)) as unknown as ObjectNode[]
      if (treeConnRef.current !== connectionId) return
      setChildrenMap((prev) => ({ ...prev, [key]: kids || [] }))
    } catch (e: any) {
      if (treeConnRef.current !== connectionId) return
      setErrors((prev) => ({ ...prev, [key]: String(e?.message || e) }))
    } finally {
      setLoading((prev) => {
        const next = new Set(prev)
        next.delete(key)
        return next
      })
    }
  }, [active, connected, connectionId])

  const toggle = useCallback(async (path: string[], node: ObjectNode) => {
    const key = pathKey(path)
    if (!canExpand(node)) return
    if (!active || !connectionId || !connected) return
    if (loadingRef.current.has(key)) return

    if (expandedRef.current.has(key)) {
      if (errorsRef.current[key]) {
        // 失败重试：同步清缓存后再拉，避免 loadChildren 命中旧 ref。
        const cleared = { ...childrenRef.current }
        delete cleared[key]
        childrenRef.current = cleared
        setChildrenMap(cleared)
        await loadChildren(path, node, key)
        return
      }
      setExpanded((prev) => {
        const next = new Set(prev)
        next.delete(key)
        return next
      })
      return
    }

    setExpanded((prev) => new Set(prev).add(key))
    await loadChildren(path, node, key)
  }, [active, connected, connectionId, loadChildren])

  if (!nodes.length) {
    return <div className="tree-empty">{t('query.needConnectHint')}</div>
  }

  return (
    <ul className="object-tree">
      {nodes.map((n) => (
        <NodeRow
          key={`${n.kind}:${n.name}`}
          node={n}
          path={[n.name]}
          connected={connected}
          selectedKey={selectedKey}
          onPick={(node, path) => {
            setSelectedKey(pathKey(path))
            onPick(node, path)
          }}
          onContextMenu={onContextMenu}
          expanded={expanded}
          childrenMap={childrenMap}
          loading={loading}
          errors={errors}
          toggle={toggle}
        />
      ))}
    </ul>
  )
}
