/**
 * Run: node src/lib/resultGrid.spec.mjs
 */
import {
  isNullCell,
  cellForCopy,
  escapeCsvField,
  rowsToTsv,
  rowsToCsv,
  compareCell,
  sortedRowIndices,
  rowRange,
} from './resultGrid.mjs'

function assert(cond, msg) {
  if (!cond) throw new Error(msg)
}

assert(isNullCell('NULL') === true, 'NULL literal is null cell')
assert(isNullCell('') === false, 'empty string is not null')
assert(cellForCopy('NULL') === '', 'null copies as empty')
assert(escapeCsvField('a,b') === '"a,b"', 'csv quotes comma')
assert(escapeCsvField('say "hi"') === '"say ""hi"""', 'csv escapes quotes')

const cols = ['id', 'name']
const rows = [
  ['2', 'b'],
  ['NULL', 'a'],
  ['10', 'c'],
]
assert(rowsToTsv(cols, [rows[0]], true) === 'id\tname\n2\tb', 'tsv with header')
assert(rowsToCsv(cols, [['1', 'x,y']]).includes('"x,y"'), 'csv quotes field')
assert(compareCell('2', '10') < 0, 'numeric compare 2 < 10')
assert(compareCell('NULL', '1') > 0, 'null sorts after values in asc compare')

const asc = sortedRowIndices(rows, 0, 'asc')
assert(asc[0] === 0 && asc[1] === 2 && asc[2] === 1, 'sort asc by id with null last')
const desc = sortedRowIndices(rows, 1, 'desc')
assert(desc[0] === 2 && desc[desc.length - 1] === 1, 'sort desc by name')

assert(rowRange(3, 1).join(',') === '1,2,3', 'row range inclusive')

const csv = rowsToCsv(['名'], [['中文']])
assert(csv.includes('中文'), 'csv keeps unicode')
assert(escapeCsvField('NULL') === '', 'NULL exports empty')

console.log('resultGrid.spec.mjs: ok')
