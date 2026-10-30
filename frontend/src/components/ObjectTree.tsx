import { ObjectNode } from '../types'
import { useI18n } from '../i18n'

interface Props {
  nodes: ObjectNode[]
  onPick: (node: ObjectNode, parent?: ObjectNode) => void
}

function Node({ node, parent, onPick }: { node: ObjectNode; parent?: ObjectNode; onPick: Props['onPick'] }) {
  return (
    <li>
      <button type="button" className="tree-item" onClick={() => onPick(node, parent)}>
        <span className="kind">{node.kind}</span> {node.name}
      </button>
      {node.children && node.children.length > 0 && (
        <ul>
          {node.children.map((c) => (
            <Node key={`${node.name}/${c.name}`} node={c} parent={node} onPick={onPick} />
          ))}
        </ul>
      )}
    </li>
  )
}

export function ObjectTree({ nodes, onPick }: Props) {
  const { t } = useI18n()
  if (!nodes.length) {
    return <div className="tree-empty">{t('query.needConnectHint')}</div>
  }
  return (
    <ul className="object-tree">
      {nodes.map((n) => (
        <Node key={n.name} node={n} onPick={onPick} />
      ))}
    </ul>
  )
}
