import { useEffect, useMemo, useRef, useState } from 'react'
import CodeMirror, { type ReactCodeMirrorRef } from '@uiw/react-codemirror'
import { EditorView, keymap } from '@codemirror/view'
import { defaultKeymap, history, historyKeymap } from '@codemirror/commands'
import { HighlightStyle, syntaxHighlighting } from '@codemirror/language'
import { tags as t } from '@lezer/highlight'
import { MySQL, PostgreSQL, StandardSQL, sql, type SQLConfig, type SQLNamespace } from '@codemirror/lang-sql'
import type { ObjectNode } from '../types'
import { buildSqlCompletionSchema } from '../lib/sqlCompletionSchema'
import { getTheme } from '../theme'

interface Props {
  value: string
  onChange: (next: string) => void
  connType: string
  connectionId: string
  connected: boolean
  objects: ObjectNode[]
  activeSchema: string
  placeholder?: string
  disabled?: boolean
  onRun?: () => void
}

function dialectFor(connType: string) {
  if (connType === 'mysql') return MySQL
  if (connType === 'postgres') return PostgreSQL
  return StandardSQL
}

const baseChrome = EditorView.theme({
  '&': {
    height: '100%',
    fontSize: '13px',
    backgroundColor: 'var(--surface)',
    color: 'var(--text)',
  },
  '.cm-scroller': {
    fontFamily: 'var(--mono)',
    lineHeight: '1.45',
  },
  '.cm-content': {
    padding: '12px 0',
    caretColor: 'var(--accent)',
  },
  '.cm-gutters': {
    backgroundColor: 'var(--surface-2)',
    color: 'var(--text-3)',
    border: 'none',
    borderRight: '1px solid var(--border)',
  },
  '.cm-activeLineGutter': {
    backgroundColor: 'color-mix(in srgb, var(--accent-bg) 55%, var(--surface-2))',
  },
  '.cm-activeLine': {
    backgroundColor: 'color-mix(in srgb, var(--hover-overlay) 80%, transparent)',
  },
  '&.cm-focused .cm-cursor': {
    borderLeftColor: 'var(--accent)',
  },
  '&.cm-focused .cm-selectionBackground, .cm-selectionBackground': {
    backgroundColor: 'color-mix(in srgb, var(--accent) 22%, transparent) !important',
  },
})

const highlightLight = HighlightStyle.define([
  { tag: t.keyword, color: '#185fa5', fontWeight: '600' },
  { tag: t.typeName, color: '#854f0b' },
  { tag: t.string, color: '#3b6d11' },
  { tag: t.number, color: '#a32d2d' },
  { tag: t.comment, color: '#8a8983', fontStyle: 'italic' },
  { tag: t.operator, color: '#5f5e5a' },
  { tag: t.punctuation, color: '#5f5e5a' },
  { tag: t.name, color: '#2c2c2a' },
  { tag: t.bool, color: '#185fa5' },
  { tag: t.null, color: '#185fa5' },
])

const highlightDark = HighlightStyle.define([
  { tag: t.keyword, color: '#6cb6ff', fontWeight: '600' },
  { tag: t.typeName, color: '#f5b04d' },
  { tag: t.string, color: '#4dd47c' },
  { tag: t.number, color: '#ff8f80' },
  { tag: t.comment, color: '#5e6f5e', fontStyle: 'italic' },
  { tag: t.operator, color: '#8fa38f' },
  { tag: t.punctuation, color: '#8fa38f' },
  { tag: t.name, color: '#d6e2d6' },
  { tag: t.bool, color: '#6cb6ff' },
  { tag: t.null, color: '#6cb6ff' },
])

export function SqlEditor(props: Props) {
  const {
    value,
    onChange,
    connType,
    connectionId,
    connected,
    objects,
    activeSchema,
    placeholder,
    disabled,
    onRun,
  } = props
  const cmRef = useRef<ReactCodeMirrorRef>(null)
  const [schema, setSchema] = useState<SQLNamespace>({})
  const [isDark, setIsDark] = useState(() => getTheme() === 'dark')
  const onRunRef = useRef(onRun)
  onRunRef.current = onRun

  useEffect(() => {
    const sync = () => setIsDark(getTheme() === 'dark')
    sync()
    const obs = new MutationObserver(sync)
    obs.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] })
    return () => obs.disconnect()
  }, [])

  useEffect(() => {
    if (!connected || !connectionId || connType === 'redis') {
      setSchema({})
      return
    }
    let cancelled = false
    void buildSqlCompletionSchema(connectionId, connType, objects, activeSchema).then((next) => {
      if (!cancelled) setSchema(next)
    })
    return () => {
      cancelled = true
    }
  }, [connected, connectionId, connType, objects, activeSchema])

  const extensions = useMemo(() => {
    const runKey = keymap.of([
      {
        key: 'Mod-Enter',
        run: () => {
          onRunRef.current?.()
          return true
        },
      },
    ])
    const chrome = [
      history(),
      keymap.of([...defaultKeymap, ...historyKeymap]),
      runKey,
      baseChrome,
      syntaxHighlighting(isDark ? highlightDark : highlightLight),
      EditorView.lineWrapping,
      EditorView.editable.of(!disabled),
    ]

    if (connType === 'redis') return chrome

    const sqlConf: SQLConfig = {
      dialect: dialectFor(connType),
      schema,
      upperCaseKeywords: true,
      defaultSchema: activeSchema || undefined,
    }
    // sql() already wires keyword + schema completions.
    return [...chrome, sql(sqlConf)]
  }, [connType, schema, activeSchema, disabled, isDark])

  return (
    <div className={`sql-editor-host${disabled ? ' is-disabled' : ''}`}>
      <CodeMirror
        ref={cmRef}
        value={value}
        height="100%"
        theme="none"
        placeholder={placeholder}
        basicSetup={{
          lineNumbers: true,
          foldGutter: false,
          highlightActiveLine: true,
          highlightSelectionMatches: false,
          bracketMatching: true,
          closeBrackets: true,
          autocompletion: true,
          searchKeymap: true,
          // Avoid fighting with our dialect keyword completions.
          defaultKeymap: false,
        }}
        extensions={extensions}
        onChange={(next) => onChange(next)}
        editable={!disabled}
      />
    </div>
  )
}
