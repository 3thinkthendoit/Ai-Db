import { useState } from 'react'
import { useI18n } from '../i18n'
import { ConnectionsView } from './ConnectionsView'
import { SettingsView } from './SettingsView'
import type { Connection } from '../types'

type Tab = 'connections' | 'llm'

interface Props {
  connections: Connection[]
  connectedIds: Set<string>
  activeId: string
  onSelect: (id: string) => void
  onEdit: (c: Connection) => void
  onDelete: (id: string) => void
  onAdd: () => void
}

export function SysSettingsView(props: Props) {
  const { t } = useI18n()
  const [tab, setTab] = useState<Tab>('connections')

  const items: { key: Tab; label: string; icon: string }[] = [
    {
      key: 'connections',
      label: t('sys.tab.connections'),
      icon: 'M7 4h10a1 1 0 0 1 1 1v5H6V5a1 1 0 0 1 1-1z M6 12h12v7a1 1 0 0 1-1 1H7a1 1 0 0 1-1-1v-7z',
    },
    {
      key: 'llm',
      label: t('sys.tab.llm'),
      icon: 'M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8z M12 2v2 M12 20v2 M4.9 4.9l1.4 1.4 M17.7 17.7l1.4 1.4 M2 12h2 M20 12h2',
    },
  ]

  return (
    <div className="sub-layout">
      <aside className="sub-side" aria-label={t('menu.page.sys')}>
        <div className="sub-side-head">
          <strong>{t('menu.page.sys')}</strong>
          <span>{t('sys.hint')}</span>
        </div>
        <nav className="sub-side-nav">
          {items.map((item) => (
            <button
              key={item.key}
              type="button"
              className={`sub-side-btn${tab === item.key ? ' on' : ''}`}
              onClick={() => setTab(item.key)}
            >
              <svg className="sub-side-ico" viewBox="0 0 24 24" aria-hidden>
                <path d={item.icon} fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" />
              </svg>
              <span>{item.label}</span>
            </button>
          ))}
        </nav>
      </aside>
      <div className="sub-main">
        {tab === 'connections' ? (
          <ConnectionsView
            connections={props.connections}
            connectedIds={props.connectedIds}
            activeId={props.activeId}
            onSelect={props.onSelect}
            onEdit={props.onEdit}
            onDelete={props.onDelete}
            onAdd={props.onAdd}
          />
        ) : (
          <SettingsView />
        )}
      </div>
    </div>
  )
}
