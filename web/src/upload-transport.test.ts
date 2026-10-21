import assert from 'node:assert/strict'
import { test, type TestContext } from 'node:test'
import { ApiError, putUploadPart } from './api.ts'
import { maxUploadChunkSize, restoreTransfers, storedTransfers, transferProgress, transferSummary, uploadPartRange, uploadPartSize, validateUploadState, type Transfer } from './transfer-utils.ts'
import type { Upload } from './types.ts'

const digest = 'a'.repeat(64)
const upload: Upload = { id: 'upload', name: 'fixture.bin', parent_id: '', size: maxUploadChunkSize + 3, mime_type: 'application/octet-stream', sha256: digest, chunk_size: maxUploadChunkSize, part_size: uploadPartSize, total_chunks: 2, received_chunks: [], received_bytes: 0, status: 'uploading', created_at: '2026-10-21T00:00:00Z' }
const expected = { size: upload.size, sha256: digest }
const transfer: Transfer = { key: 'fixture', id: upload.id, name: upload.name, size: upload.size, mime_type: upload.mime_type, parent_id: '', state: 'uploading', received_bytes: uploadPartSize, stored_bytes: 0, sent_bytes: uploadPartSize + 123, created_at: upload.created_at }

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
