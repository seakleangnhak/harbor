import { sha256 } from '@noble/hashes/sha2.js'
import type { Diagnostics, DriveNode, FileKind } from './types.ts'
import { previewMime } from './media.ts'

export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B'
  const unit = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), 4)
  return `${(bytes / 1024 ** unit).toLocaleString('en', { maximumFractionDigits: unit === 0 ? 0 : 1 })} ${['B', 'KB', 'MB', 'GB', 'TB'][unit]}`
}

export function fileKind(node: Pick<DriveNode, 'kind' | 'mime_type' | 'name'>): Exclude<FileKind, ''> {
  if (node.kind === 'folder') return 'folder'
  const mime = previewMime(node) || node.mime_type.trim().toLowerCase()
  if (mime.startsWith('image/')) return 'image'
  if (mime.startsWith('video/')) return 'video'
  if (mime.startsWith('audio/')) return 'audio'
  if (/\.(zip|rar|7z|tar|gz|bz2|xz)$/i.test(node.name) || /zip|rar|7z|tar|gzip|bzip|xz|compressed/i.test(node.mime_type)) return 'archive'
  if (node.mime_type.startsWith('text/') || /pdf|document|sheet|presentation|msword/i.test(node.mime_type) || /\.(pdf|docx?|xlsx?|pptx?|md|json|csv|rtf|odt)$/i.test(node.name)) return 'document'
  return 'other'
}

export function relativeDate(date: string): string {
  const value = new Date(date)
  if (Number.isNaN(value.getTime())) return '—'
  const days = Math.floor((Date.now() - value.getTime()) / 86_400_000)
  if (days <= 0) return 'Today'
  if (days === 1) return 'Yesterday'
  if (days < 7) return `${days} days ago`
  return value.toLocaleDateString(undefined, { month: 'short', day: 'numeric', ...(value.getFullYear() !== new Date().getFullYear() ? { year: 'numeric' } : {}) })
}

export function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : 'Something went wrong. Try again.'
}

export const hex = (bytes: Uint8Array) => Array.from(bytes, byte => byte.toString(16).padStart(2, '0')).join('')

export async function hashFile(file: Blob, signal?: AbortSignal, progress?: (percent: number) => void): Promise<string> {
  const hash = sha256.create()
  const chunkSize = 4 * 1024 * 1024
  for (let offset = 0; offset < file.size; offset += chunkSize) {
    signal?.throwIfAborted()
    hash.update(new Uint8Array(await file.slice(offset, offset + chunkSize).arrayBuffer()))
    progress?.(Math.min(100, ((offset + chunkSize) / file.size) * 100))
    await new Promise(resolve => setTimeout(resolve, 0))
  }
  signal?.throwIfAborted()
  return hex(hash.digest())
}

export function missingChunks(total: number, received: number[]): number[] {
  const present = new Set(received)
  return Array.from({ length: total }, (_, index) => index).filter(index => !present.has(index))
}

export function storageLabel(data?: Diagnostics): string {
  if (!data) return 'Checking storage…'
  if (data.adapter === 'local' && data.storage_connection?.state === 'local') return 'Local encrypted storage'
  const labels = { connected: 'Telegram connected', disconnected: 'Telegram disconnected', connecting: 'Connecting to Telegram…', retrying: 'Telegram retrying…', local: 'Storage status unknown', unknown: 'Storage status unknown' }
  return labels[data.storage_connection?.state || 'unknown']
}

export function transferTiming(size: number, received: number, startedBytes: number, elapsedMs: number) {
  const bytes_per_second = elapsedMs > 0 && received > startedBytes ? (received - startedBytes) * 1000 / elapsedMs : undefined
  return { bytes_per_second, eta_seconds: bytes_per_second ? Math.max(0, size - received) / bytes_per_second : undefined }
}

// ponytail: keep managed downloads within 32 MiB; use browser disk streaming for larger files until File System Access is broadly supported.
export const managedDownloadLimit = 32 * 1024 * 1024

export async function readVerifiedDownload(response: Response, file: Pick<DriveNode, 'size' | 'sha256'>, signal?: AbortSignal, progress?: (received: number) => void): Promise<Blob> {
  const reader = (response.body || new Blob([]).stream()).getReader()
  try {
    if (!response.ok) throw new Error(`Download unavailable (${response.status}). Check your connection and retry.`)
    if (!Number.isSafeInteger(file.size) || file.size < 0 || file.size > managedDownloadLimit || !/^[a-f0-9]{64}$/i.test(file.sha256)) throw new Error('Download metadata is invalid. Refresh your drive and retry.')
    const hash = sha256.create(), bytes = new Uint8Array(file.size)
    let received = 0
    for (;;) {
      signal?.throwIfAborted()
      const { done, value } = await reader.read()
      if (done) break
      received += value.byteLength
      if (received > file.size || received > managedDownloadLimit) throw new Error('Download size differs from the original. Nothing was saved; retry the download.')
      hash.update(value)
      bytes.set(value, received - value.byteLength)
      progress?.(received)
    }
    signal?.throwIfAborted()
    if (received !== file.size || hex(hash.digest()) !== file.sha256.toLowerCase()) throw new Error('Download integrity check failed. Nothing was saved; retry the download.')
    return new Blob([bytes], { type: 'application/octet-stream' })
  } finally {
    await reader.cancel().catch(() => {})
    reader.releaseLock()
  }
}

export function validateName(name: string): string | null {
  if (!name.trim()) return 'Enter a name.'
  if (name !== name.trim()) return 'Names cannot start or end with spaces.'
  if (name === '.' || name === '..' || /[\x00-\x1f\x7f/\\]/.test(name)) return 'Use a name without slashes or control characters.'
  if (new TextEncoder().encode(name).length > 255) return 'Name must be 255 bytes or fewer.'
  return null
}
