/**
 * Run: node src/lib/chatHistory.spec.mjs
 */
import { historyBeforeIndex, isRetryableUserTurn } from './chatHistory.mjs'

function assert(cond, msg) {
  if (!cond) throw new Error(msg)
}

const hist = [
  { role: 'user', content: 'a' },
  { role: 'assistant', content: 'b' },
  { role: 'user', content: 'c' },
  { role: 'assistant', content: 'd' },
]

assert(historyBeforeIndex(hist, 2).length === 2, 'truncate before second user')
assert(historyBeforeIndex(hist, 2)[1].content === 'b', 'keeps prior assistant')
assert(historyBeforeIndex(hist, 0).length === 0, 'retry first turn → empty prior')
assert(historyBeforeIndex(null, 1).length === 0, 'null history → empty')
assert(isRetryableUserTurn(hist, 0) === true, 'index 0 user ok')
assert(isRetryableUserTurn(hist, 1) === false, 'assistant not retryable')
assert(isRetryableUserTurn(hist, 99) === false, 'oob not retryable')

console.log('chatHistory.spec.mjs: ok')
