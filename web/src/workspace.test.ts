import assert from 'node:assert/strict'
import test from 'node:test'
import { copyName, preferenceKey, viewPreference } from './workspace.ts'

test('location preferences validate saved data and isolate folder/view', () => {
  assert.notEqual(preferenceKey({ view: 'all', parent_id: 'a' }), preferenceKey({ view: 'all', parent_id: 'b' }))
  assert.notEqual(preferenceKey({ view: 'all', parent_id: '' }), preferenceKey({ view: 'trash', parent_id: '' }))
  assert.deepEqual(viewPreference({ grid: true, sort: 'size', direction: 'desc' }), { grid: true, sort: 'size', direction: 'desc' })
  assert.deepEqual(viewPreference({ grid: 'yes', sort: '__proto__', direction: 1 }), { grid: false, sort: 'name', direction: 'asc' })
})

test('copy names preserve extensions, folders, uniqueness and UTF-8 name bound', () => {
  const used = new Set(['report.pdf', 'report copy.pdf', 'report copy 2.pdf'])
  assert.equal(copyName('Report.pdf', used), 'Report copy 3.pdf')
  assert.equal(copyName('Report.pdf', used), 'Report copy 4.pdf')
  assert.equal(copyName('Photos.2026', new Set(['photos.2026']), true), 'Photos.2026 copy')
  assert.equal(copyName('.env', new Set(['.env'])), '.env copy')
  const long = '界'.repeat(82) + '.txt'
  const copy = copyName(long, new Set([long]))
  assert.ok(new TextEncoder().encode(copy).length <= 255)
  assert.ok(copy.endsWith(' copy.txt'))
  assert.ok(new TextEncoder().encode(copyName('x.' + 'z'.repeat(253), new Set(['x.' + 'z'.repeat(253)]))).length <= 255)
})
