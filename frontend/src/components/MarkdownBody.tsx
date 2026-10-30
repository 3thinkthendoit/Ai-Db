import type { ReactNode } from 'react'
import ReactMarkdown from 'react-markdown'
import rehypeHighlight from 'rehype-highlight'

interface Props {
  children: string
  className?: string
}

const SAFE_HREF = /^(https?:|mailto:)/i

function SafeLink({ href, children }: { href?: string; children?: ReactNode }) {
  const url = (href || '').trim()
  if (!url || !SAFE_HREF.test(url)) {
    return <span>{children}</span>
  }
  return (
    <a href={url} target="_blank" rel="noreferrer noopener">
      {children}
    </a>
  )
}

/** Untagged fences that look like SQL → ```sql so highlight.js picks a grammar. */
function preferSqlFences(md: string) {
  return md.replace(/```([ \t]*)\n([\s\S]*?)```/g, (full, sp, body: string) => {
    const head = body.trimStart().slice(0, 80).toUpperCase()
    if (!/^(SELECT|WITH|INSERT|UPDATE|DELETE|CREATE|ALTER|DROP|SHOW|EXPLAIN|DESCRIBE|USE|CALL|REPLACE|TRUNCATE|GRANT|REVOKE)\b/.test(head)) {
      return full
    }
    return '```sql' + sp + '\n' + body + '```'
  })
}

/** Renders chat / agent text as Markdown with fenced-code syntax highlighting. */
export function MarkdownBody({ children, className }: Props) {
  const source = preferSqlFences(children || '')
  return (
    <div className={`md-body${className ? ` ${className}` : ''}`}>
      <ReactMarkdown
        rehypePlugins={[[rehypeHighlight, { detect: true, ignoreMissing: true }]]}
        components={{
          a: ({ href, children: c }) => <SafeLink href={href}>{c}</SafeLink>,
          // Avoid remote image fetches from model/user content.
          img: () => null,
        }}
      >
        {source}
      </ReactMarkdown>
    </div>
  )
}
