import assert from 'node:assert/strict'
import { test } from 'node:test'
import { clampImagePan, codeTokens, mediaPreviewError, readPreviewText, textPreviewLimit } from './components/preview-utils.ts'
import { checkTransferCapacity, maxUploadChunkSize, readCopyRange, restoreTransfers, storedTransfers, transferDelay, transferSummary, type Transfer } from './transfer-utils.ts'

const digest = 'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad'
const source = { id: 'source', name: 'source.txt', size: 3, mime_type: 'text/plain', sha256: digest }
const transfer: Transfer = { key: 'copy', id: 'upload', name: 'copy.txt', size: 3, mime_type: 'text/plain', parent_id: '', sha256: digest, source, direction: 'copy', state: 'uploading', received_bytes: 2, created_at: '2026-10-21T00:00:00Z' }
const rangeResponse = (body: string, headers: Record<string, string> = {}, status = 206) => new Response(body, { status, headers: { 'Content-Range': 'bytes 0-2/3', 'X-Content-SHA256': digest, ...headers } })

test('copy reads one exact authenticated range and rejects fallback, mismatched and interrupted content', async () => {
  assert.equal(await (await readCopyRange(rangeResponse('abc'), source, 0, 2)).text(), 'abc')
  await assert.rejects(readCopyRange(rangeResponse('abc', {}, 200), source, 0, 2), /verified ranges/)
  await assert.rejects(readCopyRange(rangeResponse('abc', { 'Content-Range': 'bytes 1-3/4' }), source, 0, 2), /verified ranges/)
  await assert.rejects(readCopyRange(rangeResponse('abc', { 'X-Content-SHA256': '0'.repeat(64) }), source, 0, 2), /verified ranges/)
  await assert.rejects(readCopyRange(rangeResponse('abcd'), source, 0, 2), /exceeds/)
  await assert.rejects(readCopyRange(rangeResponse('ab'), source, 0, 2), /interrupted/)
  await assert.rejects(readCopyRange(rangeResponse('abc'), source, 0, 64 * 1024 * 1024), /range is invalid/)
})

test('copy cancellation closes its source stream and creates no destination blob', async () => {
  let cancelled = false
  const controller = new AbortController()
  controller.abort()
  const response = new Response(new ReadableStream({ cancel() { cancelled = true } }), { status: 206, headers: { 'Content-Range': 'bytes 0-2/3', 'X-Content-SHA256': digest } })
  await assert.rejects(readCopyRange(response, source, 0, 2, controller.signal), /aborted/i)
  assert.equal(cancelled, true)
  assert.throws(() => transferDelay(1, controller.signal), /aborted/i)
  const delayed = new AbortController(), waiting = transferDelay(10_000, delayed.signal)
  delayed.abort()
  await assert.rejects(waiting, /aborted/i)
})

test('copy bounds accept existing chunks and 100 MiB chunks, rejecting oversized ranges before allocation', async () => {
  const largeSource = { size: maxUploadChunkSize + 1, sha256: digest }
  for (const size of [4 * 1024 * 1024, 100 * 1024 * 1024]) {
    await assert.rejects(readCopyRange(new Response(null, { status: 200 }), largeSource, 0, size - 1), /verified ranges/)
  }
  await assert.rejects(readCopyRange(new Response(null, { status: 200 }), largeSource, 0, maxUploadChunkSize), /range is invalid/)
})

test('durable transfer history restores copies and downloads paused without requesting original files', () => {
  const result = restoreTransfers([transfer, { ...transfer, key: 'download', direction: 'download' }, { ...transfer, key: 'upload', source: undefined, direction: undefined }, { ...transfer, key: 'done', state: 'complete' }])
  assert.deepEqual(result.map(item => [item.key, item.state]), [['copy', 'paused'], ['download', 'paused'], ['upload', 'needs-file'], ['done', 'complete']])
  assert.equal(result[0].source?.id, 'source')
  assert.equal(result[0].id, 'upload')
  assert.equal(result[0].received_bytes, 2)
  assert.deepEqual(restoreTransfers([{ ...transfer, source: { ...source, size: 4 } }, { ...transfer, source: { ...source, sha256: 'invalid' } }]), [])
  assert.equal(restoreTransfers(Array.from({ length: 120 }, (_, key) => ({ ...transfer, key: String(key) }))).length, 120)
})

test('reload preserves all 1000 pending copies alongside bounded completed history and refuses overflow before enqueue', () => {
  const pending = Array.from({ length: 1000 }, (_, key) => ({ ...transfer, key: `pending-${key}` }))
  const history = Array.from({ length: 120 }, (_, key) => ({ ...transfer, key: `history-${key}`, state: 'complete' as const }))
  const saved = storedTransfers([...pending, ...history])
  const restored = restoreTransfers(JSON.parse(JSON.stringify(saved)))
  assert.equal(restored.filter(item => item.state === 'paused').length, 1000)
  assert.equal(restored.filter(item => item.state === 'complete').length, 100)
  assert.ok(restored.some(item => item.key === 'pending-0' && item.id === 'upload' && item.source?.id === 'source'))
  assert.ok(restored.some(item => item.key === 'pending-999'))
  assert.equal(restored.find(item => item.state === 'complete')?.key, 'history-20')
  assert.doesNotThrow(() => checkTransferCapacity(pending.slice(0, 999), 1))
  assert.doesNotThrow(() => checkTransferCapacity(history, 1000))
  assert.throws(() => checkTransferCapacity(pending, 1), /1000 pending files/)
  assert.throws(() => checkTransferCapacity([], 1001), /1000 pending files/)
  assert.equal(pending.length, 1000)
})

test('aggregate transfer progress counts committed pending bytes independently of completed history', () => {
  const summary = transferSummary([transfer, { ...transfer, key: 'paused', state: 'paused', received_bytes: 1 }, { ...transfer, key: 'done', state: 'complete', received_bytes: 3 }])
  assert.equal(summary.pending, 2)
  assert.equal(summary.active, 1)
  assert.equal(summary.completed, 1)
  assert.equal(summary.total, 6)
  assert.equal(summary.received, 3)
  assert.equal(summary.percent, 50)
  assert.equal(transferSummary([{ ...transfer, size: 0, received_bytes: 0, state: 'verifying' }]).percent, 0)
})

test('text previews enforce response limits even if server ignores range requests', async () => {
  assert.equal(await readPreviewText(new Response('abc'), 3), 'abc')
  assert.equal(await readPreviewText(new Response(null), 0), '')
  await assert.rejects(readPreviewText(new Response('abcd'), 3), /exceeded/)
  await assert.rejects(readPreviewText(new Response('ab'), 3), /interrupted/)
  await assert.rejects(readPreviewText(new Response('abc'), textPreviewLimit + 1), /Bounded text/)
  const bounded = new Response('a'.repeat(textPreviewLimit), { status: 206, headers: { 'Content-Range': `bytes 0-${textPreviewLimit - 1}/${textPreviewLimit + 1}` } })
  assert.equal((await readPreviewText(bounded, textPreviewLimit + 1)).length, textPreviewLimit)
})

test('modest syntax tokens preserve source verbatim and cap rendered spans', () => {
  const code = 'const html = "<script>alert(1)</script>"; // private\nreturn 42'
  const tokens = codeTokens(code)
  assert.equal(tokens.map(token => token.text).join(''), code)
  assert.ok(tokens.some(token => token.kind === 'string' && token.text.includes('<script>')))
  assert.ok(tokens.some(token => token.kind === 'comment'))
  const large = 'const a = 1;\n'.repeat(20_000)
  const bounded = codeTokens(large)
  assert.equal(bounded.map(token => token.text).join(''), large)
  assert.ok(bounded.length <= 4001)
  const unterminated = '/*a'.repeat(Math.floor(textPreviewLimit / 3))
  assert.deepEqual(codeTokens(unterminated), [{ text: unterminated, kind: 'comment' }])
})

test('pointer and keyboard image panning share bounds and fit resets both axes', () => {
  assert.deepEqual(clampImagePan(40, -40, 400, 200, 2), { x: 40, y: -40 })
  assert.deepEqual(clampImagePan(1000, -1000, 400, 200, 2), { x: 200, y: -100 })
  assert.deepEqual(clampImagePan(40, -40, 400, 200, 1), { x: 0, y: 0 })
})

test('media preview errors distinguish transport failures from native codec limitations', () => {
  for (const mime of ['video/quicktime', 'audio/aac', 'image/heic']) {
    assert.match(mediaPreviewError(mime, 2), /connection was interrupted/)
    assert.doesNotMatch(mediaPreviewError(mime, 2), /codec|HEIC|Safari/)
  }
  assert.match(mediaPreviewError('video/quicktime', 4), /MOV playback depends on the browser and video codec/)
  assert.match(mediaPreviewError('video/mp4', 3), /unsupported.*damaged/)
  assert.match(mediaPreviewError('audio/ogg', 1), /Playback was interrupted/)
  for (const mime of ['image/heic', 'image/heif', 'image/heic-sequence', 'image/heif-sequence']) {
    assert.match(mediaPreviewError(mime), /HEIC\/HEIF support varies by browser and device/)
  }
  assert.match(mediaPreviewError('image/jpeg'), /could not load.*Retry/)
})

test('restored browser queue excludes internal server copy stages', () => {
  const restored = restoreTransfers([{ key: 'stage', name: 'internal', size: 1, parent_id: 'copy:job:0', state: 'uploading' }])
  assert.equal(restored.length, 0)
})
