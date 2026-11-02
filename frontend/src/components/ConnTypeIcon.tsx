import { ConnType } from '../types'

/** Compact brand-tint marks for mysql / postgres / redis / mongo. */
export function connTypeLabel(type: string) {
  if (type === 'mysql') return 'MySQL'
  if (type === 'postgres') return 'PostgreSQL'
  if (type === 'redis') return 'Redis'
  if (type === 'mongo') return 'MongoDB'
  return type || 'DB'
}

export function ConnTypeIcon({
  type,
  size = 14,
  className = '',
}: {
  type?: string
  size?: number
  className?: string
}) {
  const t = ((type || 'mysql') as ConnType)
  const common = {
    width: size,
    height: size,
    viewBox: '0 0 16 16',
    className: `conn-type-ico conn-type-ico-${t}${className ? ` ${className}` : ''}`,
    'aria-hidden': true as const,
  }

  if (t === 'postgres') {
    return (
      <svg {...common}>
        <rect width="16" height="16" rx="3.5" fill="#336791" />
        <path
          fill="#fff"
          d="M8.1 2.6c-1.7 0-2.9.7-3.4 1.9-.3.7-.2 1.6.3 2.5.4.7 1 1.3 1.6 1.7l-.3 1.1c-.1.3 0 .6.3.7l1 .4c.3.1.6 0 .7-.3l.3-1.1c.7.1 1.4 0 2-.3.8-.4 1.4-1.1 1.6-2 .3-1.1 0-2.2-.8-2.9-.7-.7-1.9-1.1-3.3-1.1zm-.1 1.3c1.1 0 1.9.3 2.3.7.4.4.5 1 .4 1.6-.2.6-.6 1.1-1.2 1.3-.5.2-1.1.3-1.7.2l.5-1.7c.1-.3-.1-.6-.4-.6-.3 0-.5.2-.6.4l-.5 1.8c-.5-.3-1-.7-1.3-1.2-.3-.6-.4-1.1-.2-1.5.3-.8 1.2-1.3 2.7-1.3z"
        />
      </svg>
    )
  }

  if (t === 'redis') {
    return (
      <svg {...common}>
        <rect width="16" height="16" rx="3.5" fill="#c0392b" />
        <path
          fill="#fff"
          d="M3.2 5.1 8 3.2l4.8 1.9-4.8 1.9-4.8-1.9zm0 2.6 4.8 1.9 4.8-1.9v1.5L8 11.2 3.2 9.2V7.7zm0 3.1 4.8 1.9 4.8-1.9v1.4L8 14.2l-4.8-2V10.8z"
        />
      </svg>
    )
  }

  if (t === 'mongo') {
    return (
      <svg {...common}>
        <rect width="16" height="16" rx="3.5" fill="#13aa52" />
        <path
          fill="#fff"
          d="M8.1 2.4c-.2 1.4-1.1 2.5-1.9 3.5-.9 1.1-1.7 2.1-1.7 3.5 0 2.1 1.5 3.4 3.5 3.6v.6h.4v-.6c2-.2 3.5-1.5 3.5-3.6 0-1.4-.8-2.4-1.7-3.5-.8-1-1.7-2.1-1.9-3.5h-.2zm0 9.4c-1.4-.2-2.4-1.1-2.4-2.4 0-.9.5-1.7 1.2-2.6.3-.4.7-.8 1-1.3.3.5.7.9 1 1.3.7.9 1.2 1.7 1.2 2.6 0 1.3-1 2.2-2.4 2.4h-.6z"
        />
      </svg>
    )
  }

  // mysql (default)
  return (
    <svg {...common}>
      <rect width="16" height="16" rx="3.5" fill="#e67e22" />
      <path
        fill="#fff"
        d="M3.2 10.2c.4-2.2 1.8-3.9 3.6-4.7.4-.2.7-.6.7-1.1V3.4h1.1v1c0 .8.4 1.4 1.1 1.7 1.4.6 2.5 2 2.9 3.7.1.4-.2.8-.6.8H3.8c-.4 0-.7-.3-.6-.8zm1.3.3h5.9c-.3-1.2-1.1-2.2-2.1-2.7-.5.4-1.1.7-1.8.7-.8 0-1.5-.3-2-.8-1 .6-1.7 1.6-2 2.8z"
      />
    </svg>
  )
}
