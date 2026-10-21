import assert from 'node:assert/strict'
import test from 'node:test'
import { canShowThumbnail, draggedNodeIds, nextFileIndex, topLevelSelection } from './file-manager.ts'
import type { DriveNode } from './types.ts'

test('copy and move roots exclude overlapping descendants while retaining unrelated selections', () => {
  const node = (id: string, parent_id: string, kind: DriveNode['kind']): DriveNode => ({ id, parent_id, kind, name: id, size: 0, mime_type: '', sha256: '', favorite: false, trashed_at: null, created_at: '', updated_at: '' })
  const parent = node('parent', '', 'folder'), child = node('child', 'parent', 'folder'), grandchild = node('grandchild', 'unselected-folder', 'file'), other = node('other', '', 'file')
  const ancestors = new Map([['unselected-folder', ['parent', 'child', 'unselected-folder']]])
  assert.deepEqual(topLevelSelection([child, parent, grandchild, other, parent], ancestors).map(item => item.id), ['parent', 'other'])
  assert.deepEqual(topLevelSelection([grandchild, other], ancestors).map(item => item.id), ['grandchild', 'other'])
})

test('internal drag payload rejects malformed or oversized data and deduplicates IDs', () => {
  assert.deepEqual(draggedNodeIds('["a","b","a"]'), ['a', 'b'])
  for (const value of ['null', '{}', '[1]', '[""]', '["bad\\nvalue"]', '[', JSON.stringify(['x'.repeat(129)]), JSON.stringify(Array(10001).fill('a'))]) assert.deepEqual(draggedNodeIds(value), [])
})

test('grid keyboard movement respects columns and directory boundaries', () => {
  assert.equal(nextFileIndex(2, 'ArrowDown', 3, 8), 5)
  assert.equal(nextFileIndex(6, 'ArrowDown', 3, 8), 7)
  assert.equal(nextFileIndex(1, 'ArrowUp', 3, 8), 0)
  assert.equal(nextFileIndex(4, 'ArrowLeft', 3, 8), 3)
  assert.equal(nextFileIndex(4, 'ArrowRight', 3, 8), 5)
  assert.equal(nextFileIndex(4, 'Home', 3, 8), 0)
  assert.equal(nextFileIndex(4, 'End', 3, 8), 7)
  assert.equal(nextFileIndex(0, 'Enter', 3, 8), undefined)
  assert.equal(nextFileIndex(0, 'ArrowDown', 3, 0), undefined)
  assert.equal(nextFileIndex(0, 'ArrowUp', 3, 8), undefined)
  assert.equal(nextFileIndex(7, 'ArrowRight', 3, 8), undefined)
})

test('thumbnails permit bounded raster content only', () => {
  const image = { mime_type: 'image/png', size: 8 * 1024 * 1024, trashed_at: null }
  assert.equal(canShowThumbnail(image), true)
  for (const node of [{ ...image, size: image.size + 1 }, { ...image, size: 0 }, { ...image, mime_type: 'image/svg+xml' }, { ...image, trashed_at: '2026-10-21' }]) assert.equal(canShowThumbnail(node), false)
})
