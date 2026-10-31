/**
 * SQL 编辑区滚动 + 分割线热区：源码回归。
 */
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = join(dirname(fileURLToPath(import.meta.url)), '..')
const css = readFileSync(join(root, 'App.css'), 'utf8')
const editor = readFileSync(join(root, 'components', 'SqlEditor.tsx'), 'utf8')

let failed = 0
function assert(cond, msg) {
  if (!cond) {
    failed++
    console.error('FAIL:', msg)
  } else {
    console.log('ok:', msg)
  }
}

const hostBlock = css.slice(css.indexOf('.sql-editor-host {'), css.indexOf('.sql-editor-host.is-disabled'))
assert(hostBlock.includes('position: relative'), 'sql-editor-host 为定位上下文')
assert(hostBlock.includes('sql-editor-host > *'), '直接子节点绝对铺满，不靠 height% 链')
assert(hostBlock.includes('inset: 0'), '子节点 inset:0 铺满')
assert(hostBlock.includes('.cm-scroller'), '约束 cm-scroller 可滚动')
assert(/overflow-y:\s*auto\s*!important/.test(hostBlock), 'cm-scroller overflow-y auto')

assert(/maxHeight:\s*'100%'/.test(editor) || /maxHeight: '100%'/.test(editor), 'CM theme 限制 maxHeight')
assert(/overflowY:\s*'auto'/.test(editor), 'CM theme overflowY auto')

const side = css.slice(css.indexOf('.query-side-splitter {'), css.indexOf('body.query-side-splitting'))
assert(/flex:\s*0 0 1px/.test(side), '侧栏分割线可视 1px')
assert(/left:\s*-4px/.test(side) && /width:\s*9px/.test(side), '侧栏热区约 9px')
assert(/z-index:\s*3/.test(side), '侧栏分割线抬高层级')

const row = css.slice(css.indexOf('.query-splitter {'), css.indexOf('body.query-splitting {'))
assert(/flex:\s*0 0 1px/.test(row), '横向分割线可视 1px')
assert(/top:\s*-4px/.test(row) && /height:\s*9px/.test(row), '横向热区约 9px')
assert(/z-index:\s*3/.test(row), '横向分割线抬高层级')

if (failed) {
  console.error(`\n${failed} failed`)
  process.exit(1)
}
console.log('\nall passed')
