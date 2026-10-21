import assert from 'node:assert/strict'
import { test } from 'node:test'
import { previewMime } from './media.ts'

test('previews recognize generic uploads and normalized browser-native media without enabling active documents', () => {
  const media = (name: string, mime_type = 'application/octet-stream') => previewMime({ kind: 'file', name, mime_type })
  assert.equal(media('holiday.JPEG'), 'image/jpeg')
  assert.equal(media('photo.HEIC', ''), 'image/heic')
  assert.equal(media('clip.MOV'), 'video/quicktime')
  assert.equal(media('guide.pdf'), 'application/pdf')
  assert.equal(media('photo', ' IMAGE/JPEG; charset=binary '), 'image/jpeg')
  assert.equal(media('clip.m4v'), 'video/mp4')
  assert.equal(media('photo.jpg', 'text/html'), '')
  assert.equal(media('photo.jpg', 'image/svg+xml'), '')
  assert.equal(media('drawing.svg'), '')
  assert.equal(media('page.html'), '')
  assert.equal(media('jpg'), '')
  assert.equal(media('file.constructor'), '')
  assert.equal(previewMime({ kind: 'folder', name: 'photo.jpg', mime_type: 'image/jpeg' }), '')
})
