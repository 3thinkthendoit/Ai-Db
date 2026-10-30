/**
 * Asserts for querySql helpers (run: node src/lib/querySql.spec.mjs).
 */
import {
  lit,
  quoteRedisArg,
  parseCsv,
  formatSql,
  explainSql,
} from './querySql.mjs'

function assert(cond, msg) {
  if (!cond) throw new Error(msg)
}

assert(lit('', '') === "''", 'empty cell → empty string literal')
assert(lit('', 'null') === 'NULL', 'null token → NULL')
assert(lit('', "a'b") === "'a''b'", 'quote escape')
assert(quoteRedisArg('foo') === 'foo', 'simple redis key')
assert(quoteRedisArg('foo bar') === '"foo bar"', 'spaced redis key')
assert(JSON.stringify(parseCsv('a,b\n1,\n2,null')) === JSON.stringify([['a', 'b'], ['1', ''], ['2', 'null']]), 'csv parse')

assert(formatSql('select a, b from t where x=1').includes('\nFROM'), 'formatSql breaks clauses')
assert(formatSql("select 'from x' as v from t").includes("'from x'"), 'formatSql keeps string literals')
assert(!formatSql("select 'from x' as v from t").match(/\nFROM x'/), 'formatSql does not split inside strings')
assert(formatSql('select 1;').endsWith(';'), 'formatSql keeps trailing semicolon')
assert(
  formatSql("create function f() returns int as $$ select 1 from t $$ language sql").includes('$$ select 1 from t $$'),
  'formatSql keeps dollar-quoted body',
)
assert(
  !formatSql("create function f() returns int as $$ select 1 from t $$ language sql").match(/\$\$[\s\S]*\nFROM/),
  'formatSql does not split keywords inside $$',
)
assert(explainSql('select 1') === 'EXPLAIN select 1', 'explainSql prefixes')
assert(explainSql('EXPLAIN select 1') === 'EXPLAIN select 1', 'explainSql idempotent')

console.log('querySql.spec.mjs: ok')
