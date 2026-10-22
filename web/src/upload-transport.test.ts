import assert from 'node:assert/strict'
import { test, type TestContext } from 'node:test'
import { ApiError, api, putUploadPart } from './api.ts'
import { canSaveUploadInBackground, maxUploadChunkSize, nextUploadAction, pendingUploadChunks, restoreTransfers, storedTransfers, transferFromUpload, transferProgress, transferSummary, uploadPartRange, uploadPartSize, validateUploadState, type Transfer } from './transfer-utils.ts'
import type { Upload } from './types.ts'

const digest = 'a'.repeat(64)
const upload: Upload = { id: 'upload', name: 'fixture.bin', parent_id: '', size: maxUploadChunkSize + 3, mime_type: 'application/octet-stream', sha256: digest, chunk_size: maxUploadChunkSize, part_size: uploadPartSize, total_chunks: 2, received_chunks: [], received_bytes: 0, status: 'uploading', created_at: '2026-10-21T00:00:00Z' }
const expected = { size: upload.size, sha256: digest }
const transfer: Transfer = { key: 'fixture', id: upload.id, name: upload.name, size: upload.size, mime_type: upload.mime_type, parent_id: '', state: 'uploading', received_bytes: uploadPartSize, stored_bytes: 0, sent_bytes: uploadPartSize + 123, created_at: upload.created_at }
const fullParts = Array.from({ length: maxUploadChunkSize / uploadPartSize }, (_, index) => index)
const pipeline: Upload = { ...upload, max_pending_chunks: 2 }

test('4 MiB transport geometry preserves 100 MiB storage chunks and exact final parts', () => {
  assert.deepEqual(uploadPartRange(upload, 0, 0), { start: 0, end: uploadPartSize })
  assert.deepEqual(uploadPartRange(upload, 0, 24), { start: 24 * uploadPartSize, end: maxUploadChunkSize })
  assert.deepEqual(uploadPartRange(upload, 1, 0), { start: maxUploadChunkSize, end: maxUploadChunkSize + 3 })
  for (const [index, part] of [[-1, 0], [0, -1], [0, 25], [2, 0], [0, 0.5], [1, 1]]) assert.throws(() => uploadPartRange(upload, index, part), /range is invalid/)
  const legacy = { ...upload, size: 3, chunk_size: 3, part_size: 3, total_chunks: 1 }
  assert.deepEqual(uploadPartRange(legacy, 0, 0), { start: 0, end: 3 })
})

test('server acknowledgements distinguish staged from committed bytes and reject corrupt geometry', () => {
  assert.deepEqual(validateUploadState(upload, expected), { received_bytes: 0, stored_bytes: 0 })
  const staged = { ...upload, staged_chunk: 0, staged_parts: [2, 0], staged_bytes: uploadPartSize * 2 }
  assert.deepEqual(validateUploadState(staged, expected), { received_bytes: uploadPartSize * 2, stored_bytes: 0 })
  const committed = { ...upload, received_chunks: [0], received_bytes: maxUploadChunkSize, staged_chunk: 1, staged_parts: [0], staged_bytes: 3 }
  assert.deepEqual(validateUploadState(committed, expected), { received_bytes: upload.size, stored_bytes: maxUploadChunkSize })
  const legacy = { ...upload, part_size: undefined }
  assert.deepEqual(validateUploadState(legacy, expected), { received_bytes: 0, stored_bytes: 0 })
  for (const invalid of [
    { ...upload, chunk_size: maxUploadChunkSize + 1 }, { ...upload, sha256: '' }, { ...upload, size: upload.size + 1 },
    { ...upload, part_size: 1024 }, { ...upload, received_chunks: [0, 0], received_bytes: maxUploadChunkSize * 2 },
    { ...upload, received_chunks: [0], received_bytes: 0 }, { ...upload, received_chunks: [-1] }, { ...upload, received_chunks: [2] },
    { ...staged, staged_parts: [0, 0] }, { ...staged, staged_parts: [25] }, { ...staged, staged_bytes: 1 },
    { ...upload, staged_parts: [0], staged_bytes: uploadPartSize }, { ...staged, part_size: undefined },
    { ...staged, staged_chunk: 1 }, { ...committed, staged_chunk: 0 }, { ...committed, status: 'complete' as const },
  ]) assert.throws(() => validateUploadState(invalid, expected), /Upload metadata differs/)
  assert.deepEqual(validateUploadState({ ...upload, received_chunks: [1, 0], received_bytes: upload.size, status: 'complete' }, expected), { received_bytes: upload.size, stored_bytes: upload.size })
})

test('in-flight progress stays below 100 until storage and file verification complete; sent bytes are transient', () => {
  assert.equal(transferProgress(transfer).bytes, uploadPartSize + 123)
  const committing = { ...transfer, state: 'committing' as const, received_bytes: upload.size, sent_bytes: upload.size }
  assert.equal(transferProgress(committing).percent, 99)
  assert.equal(transferSummary([committing]).percent, 99)
  assert.equal(transferProgress({ ...committing, state: 'verifying' }).percent, 99)
  assert.equal(transferProgress({ ...committing, state: 'complete' }).percent, 100)
  assert.equal(transferProgress({ ...transfer, state: 'paused', sent_bytes: undefined }).bytes, uploadPartSize)
  const saved = storedTransfers([{ ...transfer, bytes_per_second: 123, eta_seconds: 42 }])
  assert.equal(saved[0].sent_bytes, undefined)
  assert.equal(saved[0].bytes_per_second, undefined)
  const restored = restoreTransfers(saved)
  assert.equal(restored[0].state, 'needs-file')
  assert.equal(restored[0].received_bytes, uploadPartSize)
  assert.equal(restored[0].stored_bytes, 0)
  assert.equal(restored[0].sent_bytes, undefined)
})

test('pipeline acknowledgements count each pending chunk once and keep remote bytes separate', () => {
  const pending: Upload = { ...pipeline, pending_chunks: [
    { index: 0, parts: fullParts, bytes: maxUploadChunkSize, state: 'storing' },
    { index: 1, parts: [0], bytes: 3, state: 'receiving' },
  ], staged_chunk: 0, staged_parts: fullParts, staged_bytes: maxUploadChunkSize }
  assert.deepEqual(validateUploadState(pending, expected), { received_bytes: upload.size, stored_bytes: 0 })
  const stored: Upload = { ...pending, received_chunks: [0], received_bytes: maxUploadChunkSize, pending_chunks: pending.pending_chunks!.slice(1) }
  assert.deepEqual(validateUploadState(stored, expected), { received_bytes: upload.size, stored_bytes: maxUploadChunkSize })
  assert.deepEqual(pendingUploadChunks(pipeline), [])
  assert.deepEqual(pendingUploadChunks({ ...upload, staged_chunk: 1, staged_parts: [0], staged_bytes: 3 }), [{ index: 1, parts: [0], bytes: 3, state: 'receiving' }])
})

test('pipeline status rejects duplicate, incomplete, overlapping and malformed pending state', () => {
  const chunk = { index: 0, parts: fullParts, bytes: maxUploadChunkSize, state: 'queued' as const }
  const pending: Upload = { ...pipeline, pending_chunks: [chunk] }
  const invalid: unknown[] = [
    { ...pending, pending_chunks: [chunk, chunk] },
    { ...pending, received_chunks: [0], received_bytes: maxUploadChunkSize },
    { ...pending, max_pending_chunks: 0 }, { ...pending, max_pending_chunks: 3 }, { ...pending, max_pending_chunks: undefined },
    { ...pending, pending_chunks: null }, { ...pending, pending_chunks: [null] },
    { ...pending, pending_chunks: [{ ...chunk, index: -1 }] }, { ...pending, pending_chunks: [{ ...chunk, index: 2 }] },
    { ...pending, pending_chunks: [{ ...chunk, parts: [0], bytes: uploadPartSize }] },
    { ...pending, pending_chunks: [{ ...chunk, state: 'storing', parts: [0], bytes: uploadPartSize }] },
    { ...pending, pending_chunks: [{ ...chunk, state: 'receiving', parts: [0, 0], bytes: 2 * uploadPartSize }] },
    { ...pending, pending_chunks: [{ ...chunk, parts: [25] }] },
    { ...pending, pending_chunks: [{ ...chunk, parts: null }] },
    { ...pending, pending_chunks: [{ ...chunk, bytes: 1 }] },
    { ...pending, pending_chunks: [{ ...chunk, state: 'complete' }] },
    { ...pending, pending_chunks: [{ ...chunk, error_code: 123 }] },
    { ...pending, pending_chunks: [chunk, { index: 1, parts: [0], bytes: 3, state: 'queued' }], max_pending_chunks: 1 },
    { ...upload, staged_bytes: Number.NaN }, { ...upload, staged_bytes: -1 },
    { ...upload, staged_parts: null }, { ...upload, staged_chunk: 0, staged_parts: [0], staged_bytes: 0 },
  ]
  for (const state of invalid) assert.throws(() => validateUploadState(state as Upload, expected), /Upload metadata differs/)
  const three: Upload = { ...pipeline, size: maxUploadChunkSize * 3, total_chunks: 3, pending_chunks: [chunk, { ...chunk, index: 1 }, { ...chunk, index: 2 }] }
  assert.throws(() => validateUploadState(three, { size: three.size, sha256: digest }), /Upload metadata differs/)
})

test('upload actions overlap sending with storage, then respect two-chunk capacity', () => {
  const storing: Upload = { ...pipeline, pending_chunks: [{ index: 0, parts: fullParts, bytes: maxUploadChunkSize, state: 'storing' }] }
  assert.deepEqual(nextUploadAction(storing, true), { kind: 'part', index: 1, part: 0 })
  assert.deepEqual(nextUploadAction(storing, false), { kind: 'needs-file' })
  const receiving: Upload = { ...storing, pending_chunks: [...storing.pending_chunks!, { index: 1, parts: [0], bytes: 3, state: 'receiving' }] }
  assert.deepEqual(nextUploadAction(receiving, false), { kind: 'queue', index: 1 })
  const waiting: Upload = { ...receiving, pending_chunks: receiving.pending_chunks!.map(chunk => ({ ...chunk, state: 'queued' as const })) }
  assert.deepEqual(nextUploadAction(waiting, false), { kind: 'wait' })
  const larger = { ...waiting, size: 2 * maxUploadChunkSize + 3, total_chunks: 3, pending_chunks: waiting.pending_chunks!.map(chunk => ({ ...chunk, parts: fullParts, bytes: maxUploadChunkSize })) }
  assert.deepEqual(nextUploadAction(larger, true), { kind: 'wait' })
})

test('failed saved chunks requeue without File while missing parts still require original input', () => {
  const failed: Upload = { ...pipeline, pending_chunks: [{ index: 0, parts: fullParts, bytes: maxUploadChunkSize, state: 'failed', error_code: 'storage_unavailable' }] }
  assert.deepEqual(nextUploadAction(failed, false), { kind: 'queue', index: 0 })
  assert.equal(transferFromUpload(failed).state, 'error')
  assert.match(transferFromUpload(failed).error!, /Retry to reuse/)
  const partial: Upload = { ...failed, pending_chunks: [{ ...failed.pending_chunks![0], parts: [0], bytes: uploadPartSize }] }
  assert.deepEqual(nextUploadAction(partial, false), { kind: 'needs-file' })
  assert.deepEqual(nextUploadAction(partial, true), { kind: 'part', index: 0, part: 1 })
})

test('fully accepted queued files release sender for next file while publication remains pending', () => {
  const first: Upload = { ...pipeline, size: 3, total_chunks: 1, pending_chunks: [{ index: 0, parts: [0], bytes: 3, state: 'storing' }] }
  const second: Upload = { ...first, id: 'second-upload', pending_chunks: undefined }
  assert.equal(canSaveUploadInBackground(first), true)
  assert.equal(transferFromUpload(first).state, 'committing')
  assert.equal(transferProgress(transferFromUpload(first)).percent, 99)
  assert.deepEqual(nextUploadAction(second, true), { kind: 'part', index: 0, part: 0 })
  assert.equal(canSaveUploadInBackground(second), false)
  for (const state of ['receiving', 'failed'] as const) assert.equal(canSaveUploadInBackground({ ...first, pending_chunks: [{ ...first.pending_chunks![0], state }] }), false)
  assert.equal(canSaveUploadInBackground({ ...first, pending_chunks: undefined, received_chunks: [0], received_bytes: 3, status: 'complete' }), true)
  assert.equal(canSaveUploadInBackground({ ...upload, staged_chunk: 0, staged_parts: fullParts, staged_bytes: maxUploadChunkSize }), false)
  const multipart: Upload = { ...pipeline, pending_chunks: [
    { index: 0, parts: fullParts, bytes: maxUploadChunkSize, state: 'queued' },
    { index: 1, parts: [0], bytes: 3, state: 'storing' },
  ] }
  assert.equal(canSaveUploadInBackground(multipart), true)
  assert.equal(canSaveUploadInBackground({ ...multipart, pending_chunks: multipart.pending_chunks!.slice(0, 1) }), false)
})

test('queue-only reload finalizes without File and storage completion still waits for node publication', () => {
  const queued: Upload = { ...pipeline, pending_chunks: [
    { index: 0, parts: fullParts, bytes: maxUploadChunkSize, state: 'storing' },
    { index: 1, parts: [0], bytes: 3, state: 'queued' },
  ] }
  const saved = transferFromUpload(queued)
  assert.equal(saved.state, 'committing')
  assert.equal(saved.received_bytes, upload.size)
  assert.equal(saved.stored_bytes, 0)
  const restored = restoreTransfers(storedTransfers([saved]))[0]
  assert.equal(restored.state, 'committing')
  assert.equal(restored.sha256, digest)
  assert.equal(restored.background, true)
  assert.equal(restored.pending_chunks, undefined)
  assert.deepEqual(nextUploadAction(queued, false), { kind: 'wait' })
  assert.equal(transferProgress(saved).percent, 99)
  const complete: Upload = { ...pipeline, received_chunks: [0, 1], received_bytes: upload.size, status: 'complete' }
  const unpublished = transferFromUpload(complete)
  assert.equal(unpublished.state, 'committing')
  assert.deepEqual(nextUploadAction(complete, false), { kind: 'complete' })
  assert.equal(transferProgress(unpublished).percent, 99)
  assert.equal(restoreTransfers([unpublished])[0].state, 'committing')
  assert.equal(restoreTransfers([{ ...unpublished, state: 'complete' }])[0].state, 'complete')
  assert.equal(restoreTransfers([{ ...saved, received_bytes: upload.size - 1 }])[0].state, 'needs-file')
  assert.equal(transferSummary([{ ...saved, state: 'paused' }]).active, 1)
})

test('background commit uses explicit opt-in JSON and accepts 202; legacy commit stays synchronous', async t => {
  const originalFetch = globalThis.fetch
  t.after(() => { globalThis.fetch = originalFetch })
  const calls: { path: RequestInfo | URL; init?: RequestInit }[] = []
  globalThis.fetch = async (path, init) => {
    calls.push({ path, init })
    return new Response(JSON.stringify(pipeline), { status: calls.length === 1 ? 202 : 200, headers: { 'Content-Type': 'application/json' } })
  }
  const signal = new AbortController().signal
  assert.deepEqual(await api.queueChunk('upload/id', 1, signal), pipeline)
  assert.deepEqual(await api.commitChunk('upload/id', 1, signal), pipeline)
  for (const call of calls) {
    assert.equal(call.path, '/api/v1/uploads/upload%2Fid/chunks/1/commit')
    assert.equal(call.init?.method, 'POST')
    assert.equal(call.init?.credentials, 'same-origin')
    assert.equal(call.init?.signal, signal)
    assert.deepEqual(call.init?.headers, { 'Content-Type': 'application/json' })
  }
  assert.deepEqual(JSON.parse(calls[0].init!.body as string), { background: true })
  assert.deepEqual(JSON.parse(calls[1].init!.body as string), {})
})

class FixtureXHR {
  static latest: FixtureXHR
  upload = { onprogress: null as ((event: { loaded: number }) => void) | null }
  onload: (() => void) | null = null
  onerror: (() => void) | null = null
  ontimeout: (() => void) | null = null
  onabort: (() => void) | null = null
  status = 200
  response: unknown = upload
  responseType = ''
  timeout = 0
  method = ''; url = ''; body?: Blob; aborted = false
  headers: Record<string, string> = {}
  constructor() { FixtureXHR.latest = this }
  open(method: string, url: string) { this.method = method; this.url = url }
  setRequestHeader(name: string, value: string) { this.headers[name] = value }
  send(body: Blob) { this.body = body }
  abort() { this.aborted = true; this.onabort?.() }
}

function useXHR(t: TestContext) {
  const original = Object.getOwnPropertyDescriptor(globalThis, 'XMLHttpRequest')
  Object.defineProperty(globalThis, 'XMLHttpRequest', { configurable: true, value: FixtureXHR })
  t.after(() => { if (original) Object.defineProperty(globalThis, 'XMLHttpRequest', original); else Reflect.deleteProperty(globalThis, 'XMLHttpRequest') })
}

test('XHR sends exact part, checksum and real progress; completion removes handlers', async t => {
  useXHR(t)
  const body = new Blob(['abc']), controller = new AbortController(), progress: number[] = []
  const pending = putUploadPart('upload/id', 2, 3, body, digest, controller.signal, sent => progress.push(sent)), xhr = FixtureXHR.latest
  assert.equal(xhr.method, 'PUT')
  assert.equal(xhr.url, '/api/v1/uploads/upload%2Fid/chunks/2/parts/3')
  assert.equal(xhr.body, body)
  assert.deepEqual(xhr.headers, { 'Content-Type': 'application/octet-stream', 'X-Part-SHA256': digest })
  xhr.upload.onprogress?.({ loaded: 1 }); xhr.upload.onprogress?.({ loaded: 2 }); xhr.onload?.()
  assert.deepEqual(await pending, upload)
  assert.deepEqual(progress, [1, 2, 3])
  assert.equal(xhr.upload.onprogress, null)
  assert.equal(xhr.onload, null)
  controller.abort()
  assert.equal(xhr.aborted, false)
})

test('XHR abort cancels request, rejects with AbortError and suppresses stale progress', async t => {
  useXHR(t)
  const controller = new AbortController(), progress: number[] = []
  const pending = putUploadPart('upload', 0, 0, new Blob(['abc']), digest, controller.signal, sent => progress.push(sent)), xhr = FixtureXHR.latest
  const staleProgress = xhr.upload.onprogress
  controller.abort()
  staleProgress?.({ loaded: 3 })
  await assert.rejects(pending, { name: 'AbortError' })
  assert.equal(xhr.aborted, true)
  assert.deepEqual(progress, [])
  await assert.rejects(putUploadPart('upload', 0, 0, new Blob([]), digest, controller.signal, () => {}), { name: 'AbortError' })
  assert.equal(FixtureXHR.latest, xhr)
})

test('XHR preserves safe HTTP errors and unauthorized event; network failures remain retryable', async t => {
  useXHR(t)
  const originalWindow = Object.getOwnPropertyDescriptor(globalThis, 'window'), events = new EventTarget()
  Object.defineProperty(globalThis, 'window', { configurable: true, value: events })
  t.after(() => { if (originalWindow) Object.defineProperty(globalThis, 'window', originalWindow); else Reflect.deleteProperty(globalThis, 'window') })
  let unauthorized = 0
  events.addEventListener('harbor:unauthorized', () => unauthorized++)
  for (const status of [401, 409, 429, 503]) {
    const pending = putUploadPart('upload', 0, 0, new Blob(['abc']), digest, new AbortController().signal, () => {}), xhr = FixtureXHR.latest
    xhr.status = status; xhr.response = { error: 'Safe error', code: 'safe_code' }; xhr.onload?.()
    await assert.rejects(pending, error => error instanceof ApiError && error.status === status && error.code === 'safe_code' && error.message === 'Safe error')
  }
  assert.equal(unauthorized, 1)
  for (const fail of ['onerror', 'ontimeout'] as const) {
    const pending = putUploadPart('upload', 0, 0, new Blob(['abc']), digest, new AbortController().signal, () => {}), xhr = FixtureXHR.latest
    xhr[fail]?.()
    await assert.rejects(pending, error => error instanceof ApiError && error.status === 0)
  }
})
