import assert from 'node:assert/strict'
import { test } from 'node:test'
import { fileKind, formatBytes, hashFile, managedDownloadLimit, missingChunks, readVerifiedDownload, storageLabel, transferTiming, validateName } from './utils.ts'
import type { Diagnostics } from './types.ts'

test('resume skips received indices, including unsorted retries', () => {
  assert.deepEqual(missingChunks(5, [3, 0, 3]), [1, 2, 4])
  assert.deepEqual(missingChunks(0, []), [])
})

test('incremental file hash matches SHA-256 and handles empty files', async () => {
  assert.equal(await hashFile(new Blob(['abc'])), 'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad')
  assert.equal(await hashFile(new Blob([])), 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855')
  const bytes = new Uint8Array(4 * 1024 * 1024 + 19).fill(91)
  const { createHash } = await import('node:crypto')
  assert.equal(await hashFile(new Blob([bytes])), createHash('sha256').update(bytes).digest('hex'))
  await assert.rejects(hashFile(new Blob(['abc']), AbortSignal.abort()))
})

test('filenames reject path traversal and preserve valid international names', () => {
  for (const name of ['..', '.', 'a/b', 'a\\b', 'bad\u0000name', '', ' name', 'name ', '界'.repeat(86)]) assert.ok(validateName(name))
  assert.equal(validateName('ឯកសារ.pdf'), null)
  assert.equal(validateName('Meeting notes.txt'), null)
})

test('file metadata labels stay consistent', () => {
  assert.equal(formatBytes(0), '0 B')
  assert.equal(formatBytes(1536), '1.5 KB')
  assert.equal(fileKind({ kind: 'file', mime_type: 'application/pdf', name: 'notes.pdf' }), 'document')
  assert.equal(fileKind({ kind: 'folder', mime_type: '', name: 'photos' }), 'folder')
  assert.equal(fileKind({ kind: 'file', mime_type: 'application/vnd.msword', name: 'document' }), 'document')
  assert.equal(fileKind({ kind: 'file', mime_type: 'application/zip', name: 'backup' }), 'archive')
})

test('connection labels require observed status, not configured credentials', () => {
  const configured = { adapter: 'telegram', telegram_configured: true } as Diagnostics
  assert.equal(storageLabel(configured), 'Storage status unknown')
  assert.equal(storageLabel({ ...configured, storage_connection: { state: 'disconnected', retry_count: 0 } }), 'Telegram disconnected')
  assert.equal(storageLabel({ ...configured, storage_connection: { state: 'connected', retry_count: 0 } }), 'Telegram connected')
  assert.equal(storageLabel({ ...configured, storage_connection: { state: 'retrying', retry_count: 2 } }), 'Telegram retrying…')
  assert.equal(storageLabel({ ...configured, adapter: 'local', storage_connection: { state: 'local', retry_count: 0 } }), 'Local encrypted storage')
})

test('transfer speed uses newly committed bytes and excludes prior resumed progress', () => {
  assert.deepEqual(transferTiming(1000, 300, 300, 1000), { bytes_per_second: undefined, eta_seconds: undefined })
  assert.deepEqual(transferTiming(1000, 400, 300, 1000), { bytes_per_second: 100, eta_seconds: 6 })
  assert.deepEqual(transferTiming(1000, 1000, 300, 2000), { bytes_per_second: 350, eta_seconds: 0 })
  assert.deepEqual(transferTiming(1000, 400, 300, 0), { bytes_per_second: undefined, eta_seconds: undefined })
})

test('managed downloads verify streamed length and SHA-256 before producing a saveable blob', async () => {
  const file = { size: 3, sha256: 'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad' }
  const progress: number[] = []
  const body = new ReadableStream({ start(controller) { controller.enqueue(new TextEncoder().encode('a')); controller.enqueue(new TextEncoder().encode('bc')); controller.close() } })
  assert.equal(await (await readVerifiedDownload(new Response(body), file, undefined, bytes => progress.push(bytes))).text(), 'abc')
  assert.deepEqual(progress, [1, 3])
  const empty = { size: 0, sha256: await hashFile(new Blob([])) }
  assert.equal((await readVerifiedDownload(new Response(null), empty)).size, 0)
  for (const text of ['ab', 'abd', 'abcd']) await assert.rejects(readVerifiedDownload(new Response(text), file), /integrity|size/)
  await assert.rejects(readVerifiedDownload(new Response('abc'), { ...file, size: managedDownloadLimit + 1 }), /metadata/)
  await assert.rejects(readVerifiedDownload(new Response('abc'), { ...file, sha256: 'invalid' }), /metadata/)
  await assert.rejects(readVerifiedDownload(new Response('synthetic-secret', { status: 503 }), file), /Download unavailable \(503\)/)
})

test('aborted or interrupted downloads cancel their stream and produce no file', async () => {
  const controller = new AbortController()
  let canceled = false
  const stream = new ReadableStream({ start(source) { source.enqueue(new TextEncoder().encode('a')) }, cancel() { canceled = true } })
  await assert.rejects(readVerifiedDownload(new Response(stream), { size: 3, sha256: await hashFile(new Blob(['abc'])) }, controller.signal, () => controller.abort()), /aborted/i)
  assert.equal(canceled, true)
  const broken = new ReadableStream({ start(source) { source.error(new Error('fixture: connection interrupted')) } })
  await assert.rejects(readVerifiedDownload(new Response(broken), { size: 3, sha256: await hashFile(new Blob(['abc'])) }), /connection interrupted/)
})
