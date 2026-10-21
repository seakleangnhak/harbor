import type { DriveNode } from './types.ts'

export type TransferState = 'queued' | 'hashing' | 'uploading' | 'downloading' | 'retrying' | 'browser' | 'paused' | 'needs-file' | 'verifying' | 'complete' | 'error'
export type TransferSource = Pick<DriveNode, 'id' | 'name' | 'size' | 'mime_type' | 'sha256'>
export type Transfer = {
  key: string; id?: string; name: string; size: number; mime_type: string; parent_id: string; sha256?: string
  state: TransferState; received_bytes: number; hash_progress?: number; error?: string; created_at: string
  direction?: 'download' | 'copy'; source?: TransferSource; bytes_per_second?: number; eta_seconds?: number; retries?: number
}

export const runningTransfer = (item: Transfer) => ['queued', 'hashing', 'uploading', 'downloading', 'retrying', 'verifying'].includes(item.state)
export const transferQueueLimit = 1000
export const maxUploadChunkSize = 100 * 1024 * 1024
const unfinishedTransfer = (item: Transfer) => item.state !== 'complete' && item.state !== 'browser'

export function checkTransferCapacity(items: Transfer[], incoming: number): void {
  if (!Number.isSafeInteger(incoming) || incoming < 0 || items.filter(unfinishedTransfer).length + incoming > transferQueueLimit) throw new Error(`Transfer queue holds up to ${transferQueueLimit} pending files. Finish or cancel transfers before adding more.`)
}

export function storedTransfers(items: Transfer[]): Transfer[] {
  const pending = new Set(items.filter(unfinishedTransfer).slice(0, transferQueueLimit))
  const history = new Set(items.filter(item => !unfinishedTransfer(item)).slice(-100))
  return items.filter(item => pending.has(item) || history.has(item))
}

export function restoreTransfers(saved: unknown): Transfer[] {
  if (!Array.isArray(saved)) return []
  return storedTransfers(saved.filter(item => item && typeof item.key === 'string' && typeof item.name === 'string' && Number.isSafeInteger(item.size) && item.size >= 0 && typeof item.parent_id === 'string' && !item.parent_id.startsWith('copy:') && (!item.direction || item.direction === 'copy' || item.direction === 'download')).flatMap(item => {
    const source = item.source
    if (item.direction && (!source || typeof source.id !== 'string' || typeof source.name !== 'string' || source.size !== item.size || typeof source.mime_type !== 'string' || !/^[a-f0-9]{64}$/i.test(source.sha256))) return []
    const state = item.state === 'complete' ? 'complete' : item.state === 'browser' && item.direction === 'download' ? 'browser' : item.direction ? 'paused' : 'needs-file'
    return [{ key: item.key, id: typeof item.id === 'string' ? item.id : undefined, name: item.name, size: item.size, mime_type: typeof item.mime_type === 'string' ? item.mime_type : 'application/octet-stream', parent_id: item.parent_id, sha256: typeof item.sha256 === 'string' ? item.sha256 : undefined, state, received_bytes: Math.min(item.size, Math.max(0, Number(item.received_bytes) || 0)), created_at: typeof item.created_at === 'string' ? item.created_at : new Date().toISOString(), direction: item.direction, ...(source ? { source: { id: source.id, name: source.name, size: source.size, mime_type: source.mime_type, sha256: source.sha256 } } : {}) } as Transfer]
  }))
}

export function transferSummary(items: Transfer[]) {
  const pending = items.filter(item => item.state !== 'complete' && item.state !== 'browser')
  const measured = pending.length ? pending : items.filter(item => item.state === 'complete')
  const total = measured.reduce((sum, item) => sum + item.size, 0)
  const received = measured.reduce((sum, item) => sum + Math.min(item.size, Math.max(0, item.received_bytes)), 0)
  return { pending: pending.length, active: pending.filter(runningTransfer).length, attention: pending.filter(item => item.state === 'error' || item.state === 'needs-file').length, completed: items.filter(item => item.state === 'complete').length, total, received, percent: total ? received / total * 100 : pending.length ? 0 : items.length ? 100 : 0, speed: pending.filter(runningTransfer).reduce((sum, item) => sum + (item.bytes_per_second || 0), 0) }
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
