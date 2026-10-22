import type { DriveNode, PendingUploadChunk, Upload } from './types.ts'

export type TransferState = 'queued' | 'hashing' | 'uploading' | 'committing' | 'downloading' | 'retrying' | 'browser' | 'paused' | 'needs-file' | 'verifying' | 'complete' | 'error'
export type TransferSource = Pick<DriveNode, 'id' | 'name' | 'size' | 'mime_type' | 'sha256'>
export type Transfer = {
  key: string; id?: string; name: string; size: number; mime_type: string; parent_id: string; sha256?: string
  state: TransferState; received_bytes: number; hash_progress?: number; error?: string; created_at: string
  sent_bytes?: number; stored_bytes?: number
  background?: boolean; pending_chunks?: PendingUploadChunk[]; waiting_for_storage?: boolean
  direction?: 'download' | 'copy'; source?: TransferSource; bytes_per_second?: number; eta_seconds?: number; retries?: number
}

export const runningTransfer = (item: Transfer) => ['queued', 'hashing', 'uploading', 'committing', 'downloading', 'retrying', 'verifying'].includes(item.state)
export const transferQueueLimit = 1000
export const maxUploadChunkSize = 100 * 1024 * 1024
export const uploadPartSize = 4 * 1024 * 1024
export const uploadStorageError = 'Storage could not save an accepted chunk. Retry to reuse its saved parts.'
const unfinishedTransfer = (item: Transfer) => item.state !== 'complete' && item.state !== 'browser'

export function checkTransferCapacity(items: Transfer[], incoming: number): void {
  if (!Number.isSafeInteger(incoming) || incoming < 0 || items.filter(unfinishedTransfer).length + incoming > transferQueueLimit) throw new Error(`Transfer queue holds up to ${transferQueueLimit} pending files. Finish or cancel transfers before adding more.`)
}

export function storedTransfers(items: Transfer[]): Transfer[] {
  const pending = new Set(items.filter(unfinishedTransfer).slice(0, transferQueueLimit))
  const history = new Set(items.filter(item => !unfinishedTransfer(item)).slice(-100))
  return items.filter(item => pending.has(item) || history.has(item)).map(({ sent_bytes: _sent, bytes_per_second: _speed, eta_seconds: _eta, hash_progress: _hash, ...item }) => item)
}

export function restoreTransfers(saved: unknown): Transfer[] {
  if (!Array.isArray(saved)) return []
  return storedTransfers(saved.filter(item => item && typeof item.key === 'string' && typeof item.name === 'string' && Number.isSafeInteger(item.size) && item.size >= 0 && typeof item.parent_id === 'string' && !item.parent_id.startsWith('copy:') && (!item.direction || item.direction === 'copy' || item.direction === 'download')).flatMap(item => {
    const source = item.source
    if (item.direction && (!source || typeof source.id !== 'string' || typeof source.name !== 'string' || source.size !== item.size || typeof source.mime_type !== 'string' || !/^[a-f0-9]{64}$/i.test(source.sha256))) return []
    const received_bytes = Math.min(item.size, Math.max(0, Number(item.received_bytes) || 0))
    const state = item.state === 'complete' ? 'complete' : item.state === 'browser' && item.direction === 'download' ? 'browser' : item.direction ? 'paused' : typeof item.id === 'string' && /^[a-f0-9]{64}$/i.test(item.sha256) && received_bytes === item.size ? 'committing' : 'needs-file'
    return [{ key: item.key, id: typeof item.id === 'string' ? item.id : undefined, name: item.name, size: item.size, mime_type: typeof item.mime_type === 'string' ? item.mime_type : 'application/octet-stream', parent_id: item.parent_id, sha256: typeof item.sha256 === 'string' ? item.sha256 : undefined, state, received_bytes, ...(Number.isSafeInteger(item.stored_bytes) ? { stored_bytes: Math.min(received_bytes, Math.max(0, item.stored_bytes)) } : {}), ...(item.background === true ? { background: true } : {}), created_at: typeof item.created_at === 'string' ? item.created_at : new Date().toISOString(), direction: item.direction, ...(source ? { source: { id: source.id, name: source.name, size: source.size, mime_type: source.mime_type, sha256: source.sha256 } } : {}) } as Transfer]
  }))
}

export function validateUploadState(upload: Upload, expected: { size: number; sha256: string }) {
  const invalid = () => { throw new Error('Upload metadata differs from this file. Cancel and start a new transfer.') }
  if (!upload || typeof upload.id !== 'string' || !upload.id || !Number.isSafeInteger(upload.size) || upload.size < 0 || upload.size !== expected.size || typeof upload.sha256 !== 'string' || !/^[a-f0-9]{64}$/i.test(upload.sha256) || typeof expected.sha256 !== 'string' || upload.sha256.toLowerCase() !== expected.sha256.toLowerCase() || !Number.isSafeInteger(upload.chunk_size) || upload.chunk_size < 1 || upload.chunk_size > maxUploadChunkSize || upload.total_chunks !== Math.ceil(upload.size / upload.chunk_size) || !['uploading', 'complete'].includes(upload.status) || !Array.isArray(upload.received_chunks)) invalid()
  const received = new Set<number>()
  let stored = 0
  for (const index of upload.received_chunks) {
    if (!Number.isSafeInteger(index) || index < 0 || index >= upload.total_chunks || received.has(index)) invalid()
    received.add(index)
    stored += Math.min(upload.chunk_size, upload.size - index * upload.chunk_size)
  }
  if (stored !== upload.received_bytes) invalid()
  if (upload.part_size !== undefined && upload.part_size !== Math.min(uploadPartSize, upload.chunk_size)) invalid()
  const background = upload.max_pending_chunks !== undefined
  if (!background && upload.staged_parts !== undefined && !Array.isArray(upload.staged_parts)) invalid()
  if (upload.max_pending_chunks !== undefined && (!Number.isSafeInteger(upload.max_pending_chunks) || upload.max_pending_chunks < 1 || upload.max_pending_chunks > 2)) invalid()
  if (upload.pending_chunks !== undefined && (!background || !Array.isArray(upload.pending_chunks))) invalid()
  const pending = pendingUploadChunks(upload)
  if (pending.length > (upload.max_pending_chunks ?? 1)) invalid()
  let staged = 0
  const pendingIndices = new Set<number>()
  for (const chunk of pending) {
    if (!chunk || !upload.part_size || !Number.isSafeInteger(chunk.index) || chunk.index < 0 || chunk.index >= upload.total_chunks || received.has(chunk.index) || pendingIndices.has(chunk.index) || !['receiving', 'queued', 'storing', 'failed'].includes(chunk.state) || !Array.isArray(chunk.parts) || chunk.error_code !== undefined && typeof chunk.error_code !== 'string') invalid()
    pendingIndices.add(chunk.index)
    const chunkSize = Math.min(upload.chunk_size, upload.size - chunk.index * upload.chunk_size), seen = new Set<number>()
    let bytes = 0
    for (const part of chunk.parts) {
      if (!Number.isSafeInteger(part) || part < 0 || part >= Math.ceil(chunkSize / upload.part_size!) || seen.has(part)) invalid()
      seen.add(part)
      bytes += Math.min(upload.part_size!, chunkSize - part * upload.part_size!)
    }
    if (bytes !== chunk.bytes || ['queued', 'storing'].includes(chunk.state) && bytes !== chunkSize) invalid()
    staged += bytes
  }
  if (!background && (staged !== (upload.staged_bytes ?? 0) || upload.staged_chunk === undefined && (upload.staged_parts?.length ?? 0))) invalid()
  if (upload.status === 'complete' && (stored !== upload.size || received.size !== upload.total_chunks || pending.length)) invalid()
  return { received_bytes: stored + staged, stored_bytes: stored }
}

export function pendingUploadChunks(upload: Upload): PendingUploadChunk[] {
  return upload.max_pending_chunks !== undefined ? upload.pending_chunks ?? [] : upload.staged_chunk === undefined ? [] : [{ index: upload.staged_chunk, parts: upload.staged_parts ?? [], bytes: upload.staged_bytes ?? 0, state: 'receiving' }]
}

export function transferFromUpload(upload: Upload): Transfer {
  const progress = validateUploadState(upload, upload), pending = pendingUploadChunks(upload)
  const failed = pending.some(chunk => chunk.state === 'failed')
  // Storage completion can precede file-node publication; /complete must still run.
  return {
    key: upload.id, id: upload.id, name: upload.name, size: upload.size, mime_type: upload.mime_type, parent_id: upload.parent_id,
    sha256: upload.sha256, state: failed ? 'error' : progress.received_bytes === upload.size ? 'committing' : 'needs-file',
    ...progress, background: upload.max_pending_chunks !== undefined, pending_chunks: pending, ...(failed ? { error: uploadStorageError } : {}), created_at: upload.created_at,
  }
}

export type UploadAction = { kind: 'part'; index: number; part: number } | { kind: 'queue'; index: number } | { kind: 'wait' } | { kind: 'complete' } | { kind: 'needs-file' }

export function nextUploadAction(upload: Upload, hasFile: boolean): UploadAction {
  validateUploadState(upload, upload)
  const pending = pendingUploadChunks(upload), committed = new Set(upload.received_chunks)
  for (const chunk of pending) {
    if (chunk.state === 'queued' || chunk.state === 'storing') continue
    const count = Math.ceil(Math.min(upload.chunk_size, upload.size - chunk.index * upload.chunk_size) / upload.part_size!)
    const parts = new Set(chunk.parts)
    for (let part = 0; part < count; part++) if (!parts.has(part)) return hasFile ? { kind: 'part', index: chunk.index, part } : { kind: 'needs-file' }
    return { kind: 'queue', index: chunk.index }
  }
  if (committed.size === upload.total_chunks) return { kind: 'complete' }
  if (pending.length >= (upload.max_pending_chunks ?? 1)) return { kind: 'wait' }
  for (let index = 0; index < upload.total_chunks; index++) if (!committed.has(index) && !pending.some(chunk => chunk.index === index)) return hasFile ? { kind: 'part', index, part: 0 } : { kind: 'needs-file' }
  return { kind: 'wait' }
}

export function canSaveUploadInBackground(upload: Upload): boolean {
  return upload.max_pending_chunks !== undefined && validateUploadState(upload, upload).received_bytes === upload.size && pendingUploadChunks(upload).every(chunk => chunk.state === 'queued' || chunk.state === 'storing')
}

export function uploadPartRange(upload: Upload, index: number, part: number) {
  if (!upload.part_size || !Number.isSafeInteger(index) || index < 0 || index >= upload.total_chunks || !Number.isSafeInteger(part) || part < 0) throw new Error('Upload part range is invalid.')
  const start = index * upload.chunk_size + part * upload.part_size
  const end = Math.min(index * upload.chunk_size + (part + 1) * upload.part_size, (index + 1) * upload.chunk_size, upload.size)
  if (start >= end) throw new Error('Upload part range is invalid.')
  return { start, end }
}

export function transferProgress(item: Transfer) {
  const bytes = Math.min(item.size, Math.max(0, item.received_bytes, item.sent_bytes ?? 0))
  return { bytes, percent: item.state === 'complete' ? 100 : item.size ? Math.min(99, bytes / item.size * 100) : 0 }
}

export function transferSummary(items: Transfer[]) {
  const pending = items.filter(item => item.state !== 'complete' && item.state !== 'browser')
  const measured = pending.length ? pending : items.filter(item => item.state === 'complete')
  const total = measured.reduce((sum, item) => sum + item.size, 0)
  const received = measured.reduce((sum, item) => sum + transferProgress(item).bytes, 0)
  return { pending: pending.length, active: pending.filter(item => runningTransfer(item) || item.pending_chunks?.some(chunk => chunk.state === 'queued' || chunk.state === 'storing')).length, attention: pending.filter(item => item.state === 'error' || item.state === 'needs-file').length, completed: items.filter(item => item.state === 'complete').length, total, received, percent: total ? Math.min(pending.length ? 99 : 100, received / total * 100) : pending.length ? 0 : items.length ? 100 : 0, speed: pending.filter(runningTransfer).reduce((sum, item) => sum + (item.bytes_per_second || 0), 0) }
}

export async function readCopyRange(response: Response, source: Pick<TransferSource, 'size' | 'sha256'>, start: number, end: number, signal?: AbortSignal): Promise<Blob> {
  const reader = response.body?.getReader()
  try {
    const expected = end - start + 1
    if (!Number.isSafeInteger(start) || !Number.isSafeInteger(end) || start < 0 || end < start || end >= source.size || expected > maxUploadChunkSize) throw new Error('Copy range is invalid. Refresh and retry.')
    if (response.status !== 206 || response.headers.get('Content-Range') !== `bytes ${start}-${end}/${source.size}` || response.headers.get('X-Content-SHA256')?.toLowerCase() !== source.sha256.toLowerCase()) throw new Error('Source file changed or does not support verified ranges. Nothing was published.')
    if (!reader) throw new Error('Source file stream is unavailable. Retry the copy.')
    const bytes = new Uint8Array(expected)
    let received = 0
    for (;;) {
      signal?.throwIfAborted()
      const { done, value } = await reader.read()
      if (done) break
      if (received + value.byteLength > expected) throw new Error('Source chunk exceeds its expected size. Nothing was published.')
      bytes.set(value, received); received += value.byteLength
    }
    signal?.throwIfAborted()
    if (received !== expected) throw new Error('Source chunk was interrupted. Retry the copy.')
    return new Blob([bytes], { type: 'application/octet-stream' })
  } finally {
    if (reader) { await reader.cancel().catch(() => {}); reader.releaseLock() }
  }
}

export function transferDelay(milliseconds: number, signal: AbortSignal): Promise<void> {
  signal.throwIfAborted()
  return new Promise((resolve, reject) => {
    const abort = () => { clearTimeout(timer); reject(signal.reason) }
    const timer = setTimeout(() => { signal.removeEventListener('abort', abort); resolve() }, milliseconds)
    signal.addEventListener('abort', abort, { once: true })
  })
}
