/**
 * 新建任务 / 选库：源码回归 + 续发消费逻辑单测。
 */
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = join(dirname(fileURLToPath(import.meta.url)), '..')
const app = readFileSync(join(root, 'App.tsx'), 'utf8')
const agent = readFileSync(join(root, 'pages', 'AgentWorkspace.tsx'), 'utf8')
const pick = readFileSync(join(root, 'components', 'PickConnectionModal.tsx'), 'utf8')
const locales = readFileSync(join(root, 'locales.ts'), 'utf8')

let failed = 0
function assert(cond, msg) {
  if (!cond) {
    failed++
    console.error('FAIL:', msg)
  } else {
    console.log('ok:', msg)
  }
}

/** 与 AgentWorkspace resumedPendingKey 同语义的纯函数测试。 */
function makeResumeGuard() {
  let key = ''
  return {
    tryResume(pending, connectionId) {
      if (!pending) {
        key = ''
        return { resume: false, reason: 'cleared' }
      }
      if (!connectionId) return { resume: false, reason: 'no-conn' }
      const next = `${connectionId}\0${pending}`
      if (key === next) return { resume: false, reason: 'dup' }
      key = next
      return { resume: true, reason: 'ok' }
    },
  }
}

assert(/function goHomeClearConnection\(/.test(app), '存在 goHomeClearConnection')
assert(/function handleNewTask\(/.test(app) && /goHomeClearConnection\(\)/.test(app), '新建任务走 goHomeClearConnection')
assert(/onClick=\{\(\) => goHomeClearConnection\(\)\}/.test(app), 'Home 按钮走 goHomeClearConnection')
assert(/setActiveConnId\(''\)/.test(app) && /setPendingAgentSend\(null\)/.test(app), '回首页清连接与 pending')
assert(!/NewTaskModal/.test(app), 'App 不再引用 NewTaskModal')
assert(!/if \(!activeConnId && list\?\.length\) setActiveConnId\(list\[0\]\.id\)/.test(app), 'reload 不得自动选第一个连接')

assert(/pickConnPurpose === 'query' \? \(activeConnId \|\| ''\) : ''/.test(app), 'Query 可预填当前连接，Agent 不预选')
assert(/onRequestPickConnection/.test(app) && /onRequestPickConnection/.test(agent), 'Agent 未选库发送走 App 弹窗')
assert(/!showHomeHero/.test(agent), '首页隐藏底栏选连接')
assert(/resumedPendingKey/.test(agent), '续发用 ref 防双发')

assert(!/defaultConnectionId \|\| connections\[0\]/.test(pick), 'PickConnectionModal 不回落第一个连接')
assert(!/task\.newTitle/.test(locales) && !/task\.create/.test(locales) && !/task\.noConnection/.test(locales), '已删无用 locale')
assert(/task\.needConnection/.test(locales) && /task\.connection/.test(locales), '保留仍在用的 task locale')

{
  const g = makeResumeGuard()
  assert(g.tryResume('hi', '').resume === false, '无 connectionId 不续发')
  assert(g.tryResume('hi', 'c1').resume === true, '首次选库后续发')
  assert(g.tryResume('hi', 'c1').reason === 'dup', '同一 pending+conn 不二次续发')
  assert(g.tryResume(null, 'c1').reason === 'cleared', '取消/清空 pending 重置 guard')
  assert(g.tryResume('hi', 'c1').resume === true, '清空后再选可再次续发')
  assert(g.tryResume('other', 'c1').resume === true, '不同文案可续发')
}

if (failed) {
  console.error(`\n${failed} failed`)
  process.exit(1)
}
console.log('\nall passed')
