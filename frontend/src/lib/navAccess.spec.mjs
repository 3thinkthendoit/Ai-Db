import assert from 'node:assert/strict'
import { canOpenPage, navPagesForRole } from './navAccess.mjs'

assert.deepEqual(navPagesForRole('admin'), ['query', 'dba', 'perms', 'admin', 'sys'])
assert.deepEqual(navPagesForRole('dba'), ['query', 'dba', 'perms', 'admin'])
assert.deepEqual(navPagesForRole('user'), ['query', 'dba', 'permApply'])
assert.deepEqual(navPagesForRole(''), ['query', 'dba', 'permApply'])

assert.equal(canOpenPage('admin', 'sys'), true)
assert.equal(canOpenPage('dba', 'sys'), false)
assert.equal(canOpenPage('user', 'perms'), false)
assert.equal(canOpenPage('user', 'permApply'), true)
assert.equal(canOpenPage('user', 'prefs'), true)
assert.equal(canOpenPage('user', 'profile'), true)
assert.equal(canOpenPage('dba', 'admin'), true)
assert.equal(canOpenPage('user', 'connections'), false)
assert.equal(canOpenPage('admin', 'connections'), true)

console.log('navAccess.spec.mjs ok')
