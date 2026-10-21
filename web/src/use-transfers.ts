import { useCallback, useEffect, useRef, useState } from 'react'
import { ApiError, api, contentUrl } from './api'
import { errorMessage, hashFile, managedDownloadLimit, missingChunks, readVerifiedDownload, transferTiming, validateName } from './utils'
import { checkTransferCapacity, maxUploadChunkSize, readCopyRange, restoreTransfers, storedTransfers, transferDelay, type Transfer, type TransferSource } from './transfer-utils'
import type { DriveNode, Upload } from './types'

export type { Transfer, TransferState } from './transfer-utils'
const storageKey = 'harbor:transfers:v1'
const readTransfers = (): Transfer[] => {
  try { return restoreTransfers(JSON.parse(localStorage.getItem(storageKey) || '[]')) } catch { return [] }
}
const fromUpload = (upload: Upload): Transfer => ({
  key: upload.id, id: upload.id, name: upload.name, size: upload.size, mime_type: upload.mime_type, parent_id: upload.parent_id,
  sha256: upload.sha256, state: 'needs-file', received_bytes: upload.received_bytes, created_at: upload.created_at,
})
const sourceOf = (node: DriveNode): TransferSource => ({ id: node.id, name: node.name, size: node.size, mime_type: node.mime_type, sha256: node.sha256 })

export function useTransfers(onComplete: () => void) {
  const [transfers, setTransfers] = useState<Transfer[]>(readTransfers)
  const current = useRef(transfers)
  const files = useRef(new Map<string, File>())
  const active = useRef<string | null>(null)
  const resumeRequested = useRef(new Set<string>())
  const cancelling = useRef(new Set<string>())
  const controllers = useRef(new Map<string, AbortController>())
  const tasks = useRef(new Map<string, Promise<void>>())
  const mounted = useRef(true)
  const completeRef = useRef(onComplete)
  completeRef.current = onComplete
  const change = useCallback((fn: (items: Transfer[]) => Transfer[]) => {
    current.current = fn(current.current)
    if (mounted.current) setTransfers(current.current)
  }, [])
  const update = useCallback((key: string, patch: Partial<Transfer>) => change(items => items.map(item => item.key === key ? { ...item, ...patch } : item)), [change])

  useEffect(() => {
    mounted.current = true
    api.uploads().then(({ items }) => {
      if (mounted.current) change(current => [...current, ...items.filter(item => !current.some(saved => saved.id === item.id)).map(fromUpload)])
    }).catch(() => { /* Session errors are surfaced by the shared API client. */ })
    return () => { mounted.current = false; for (const controller of controllers.current.values()) controller.abort() }
  }, [change])
  useEffect(() => {
    try { localStorage.setItem(storageKey, JSON.stringify(storedTransfers(transfers))) } catch { /* Server upload sessions remain resumable when browser storage is full. */ }
  }, [transfers])

  useEffect(() => {
    if (active.current) return
    const transfer = transfers.find(item => item.state === 'queued' && !cancelling.current.has(item.key))
    if (!transfer) return
    const file = files.current.get(transfer.key), source = transfer.source
    if (!file && !source) { update(transfer.key, { state: 'needs-file' }); return }
    active.current = transfer.key
    const controller = new AbortController(), signal = controller.signal
    controllers.current.set(transfer.key, controller)

    async function run() {
      try {
        if (transfer!.direction === 'download' && source) {
          update(transfer!.key, { state: 'downloading', received_bytes: 0, error: undefined, bytes_per_second: undefined, eta_seconds: undefined })
          const started = performance.now()
          const response = await fetch(contentUrl(source.id, true), { credentials: 'same-origin', signal })
          if (response.status === 401) window.dispatchEvent(new Event('harbor:unauthorized'))
          const blob = await readVerifiedDownload(response, source, signal, received_bytes => update(transfer!.key, { received_bytes, ...transferTiming(source.size, received_bytes, 0, performance.now() - started) }))
          signal.throwIfAborted()
          const url = URL.createObjectURL(blob), link = document.createElement('a')
          link.href = url; link.download = transfer!.name; link.click()
          setTimeout(() => URL.revokeObjectURL(url), 1000)
          update(transfer!.key, { state: 'complete', received_bytes: source.size, bytes_per_second: undefined, eta_seconds: undefined })
          return
        }
        let digest = source?.sha256
        if (file) {
          update(transfer!.key, { state: 'hashing', error: undefined, hash_progress: 0 })
          digest = await hashFile(file, signal, percent => update(transfer!.key, { hash_progress: percent }))
          if (transfer!.sha256 && transfer!.sha256 !== digest) throw new Error('File content differs from the original. Select the original file to resume.')
        }
        if (!digest || !/^[a-f0-9]{64}$/i.test(digest)) throw new Error('File integrity metadata is missing. Refresh your drive and retry.')
        update(transfer!.key, { sha256: digest, error: undefined })
        const session = transfer!.id ? await api.upload(transfer!.id) : await api.createUpload({ name: transfer!.name, size: transfer!.size, mime_type: transfer!.mime_type, parent_id: transfer!.parent_id, sha256: digest }, signal)
        update(transfer!.key, { id: session.id })
        signal.throwIfAborted()
        if (session.size !== transfer!.size || !Number.isSafeInteger(session.chunk_size) || session.chunk_size < 1 || session.chunk_size > maxUploadChunkSize || session.total_chunks !== Math.ceil(session.size / session.chunk_size) || (session.sha256 && session.sha256.toLowerCase() !== digest.toLowerCase())) throw new Error('Upload metadata differs from this file. Cancel and start a new transfer.')
        const started = performance.now(), startedBytes = session.received_bytes
        let retries = 0
        update(transfer!.key, { state: 'uploading', received_bytes: session.received_bytes, bytes_per_second: undefined, eta_seconds: undefined, retries })
        for (const index of missingChunks(session.total_chunks, session.received_chunks || [])) {
          signal.throwIfAborted()
          const start = index * session.chunk_size, end = Math.min((index + 1) * session.chunk_size, transfer!.size) - 1
          let chunk = file?.slice(start, end + 1)
          for (let attempt = 0; ; attempt++) {
            try {
              if (!chunk && source) {
                update(transfer!.key, { state: 'downloading' })
                const response = await fetch(contentUrl(source.id), { credentials: 'same-origin', headers: { Range: `bytes=${start}-${end}` }, signal })
                if (response.status === 401) window.dispatchEvent(new Event('harbor:unauthorized'))
                if (!response.ok) { await response.body?.cancel(); throw new ApiError('Source file could not be read. Check your connection and retry.', response.status) }
                chunk = await readCopyRange(response, source, start, end, signal)
              }
              if (!chunk) throw new Error('Select the original file to resume.')
              const chunkDigest = await hashFile(chunk, signal)
              update(transfer!.key, { state: 'uploading' })
              const accepted = await api.putChunk(session.id, index, chunk, chunkDigest, signal)
              update(transfer!.key, { received_bytes: accepted.received_bytes, ...transferTiming(transfer!.size, accepted.received_bytes, startedBytes, performance.now() - started) })
              break
            } catch (error) {
              if (signal.aborted || attempt >= 2 || (error instanceof ApiError && error.status < 500 && error.status !== 429)) throw error
              update(transfer!.key, { state: 'retrying', retries: ++retries })
              await transferDelay(700 * 2 ** attempt, signal)
            }
          }
        }
        signal.throwIfAborted()
        update(transfer!.key, { state: 'verifying', bytes_per_second: undefined, eta_seconds: undefined })
        await api.completeUpload(session.id, signal)
        update(transfer!.key, { state: 'complete', received_bytes: transfer!.size, error: undefined })
        files.current.delete(transfer!.key)
        completeRef.current()
      } catch (error) {
        if (!cancelling.current.has(transfer!.key)) update(transfer!.key, { state: signal.aborted ? resumeRequested.current.delete(transfer!.key) ? 'queued' : 'paused' : 'error', error: signal.aborted ? undefined : error instanceof TypeError ? 'Transfer interrupted. Check your connection and retry.' : errorMessage(error), bytes_per_second: undefined, eta_seconds: undefined })
      } finally {
        controllers.current.delete(transfer!.key)
        tasks.current.delete(transfer!.key)
        active.current = null
        change(items => [...items])
      }
    }
    tasks.current.set(transfer.key, run())
  }, [transfers, update, change])

  const addFiles = (incoming: File[], parent_id: string) => {
    checkTransferCapacity(current.current, incoming.length)
    const added = incoming.map(file => {
      const key = crypto.randomUUID()
      files.current.set(key, file)
      const invalid = validateName(file.name)
      return { key, name: file.name, size: file.size, mime_type: file.type || 'application/octet-stream', parent_id, state: invalid ? 'error' as const : 'queued' as const, error: invalid || undefined, received_bytes: 0, created_at: new Date().toISOString() }
    })
    change(items => [...items, ...added])
  }
  const copyFiles = (nodes: DriveNode[], parent_id: string) => {
    if (nodes.some(node => node.kind !== 'file')) throw new Error('Copy files individually. Folder copies are not supported.')
    checkTransferCapacity(current.current, nodes.length)
    const added: Transfer[] = nodes.map(node => {
      const invalid = validateName(node.name) || (!Number.isSafeInteger(node.size) || node.size < 0 || !/^[a-f0-9]{64}$/i.test(node.sha256) ? 'Source file metadata is invalid. Refresh and retry.' : undefined)
      return { key: crypto.randomUUID(), name: node.name, size: node.size, mime_type: node.mime_type, parent_id, sha256: node.sha256, direction: 'copy', source: sourceOf(node), state: invalid ? 'error' : 'queued', error: invalid || undefined, received_bytes: 0, created_at: new Date().toISOString() }
    })
    change(items => [...items, ...added])
  }
  const pause = (key: string) => {
    if (current.current.find(item => item.key === key)?.state === 'verifying' || cancelling.current.has(key)) return
    resumeRequested.current.delete(key)
    controllers.current.get(key)?.abort()
    update(key, { state: 'paused', bytes_per_second: undefined, eta_seconds: undefined })
  }
  const resume = (key: string, file?: File) => {
    const transfer = current.current.find(item => item.key === key)
    if (!transfer || cancelling.current.has(key)) return
    if (file) {
      if (file.size !== transfer.size || file.name !== transfer.name) { update(key, { error: 'Select the original file with the same name and size.' }); return }
      files.current.set(key, file)
    }
    if (controllers.current.get(key)?.signal.aborted) resumeRequested.current.add(key)
    update(key, { state: files.current.has(key) || transfer.source ? 'queued' : 'needs-file', error: undefined })
  }
  const cancel = async (key: string) => {
    if (cancelling.current.has(key)) return
    const item = current.current.find(transfer => transfer.key === key)
    if (!item) return
    if (item.state === 'verifying') throw new Error('Integrity verification is finishing. Wait for completion before removing this file.')
    if (item.state === 'complete') return
    cancelling.current.add(key)
    resumeRequested.current.delete(key)
    controllers.current.get(key)?.abort()
    update(key, { state: 'paused', bytes_per_second: undefined, eta_seconds: undefined })
    try {
      await tasks.current.get(key)
      const latest = current.current.find(transfer => transfer.key === key)
      if (latest?.id && latest.direction !== 'download') {
        try { await api.cancelUpload(latest.id) } catch (error) { if (!(error instanceof ApiError && error.status === 404)) throw error }
      }
      files.current.delete(key)
      change(items => items.filter(transfer => transfer.key !== key))
    } catch (error) {
      update(key, { state: 'error', error: 'Cancellation could not finish. Retry cancellation to clean up the pending transfer.' })
      throw error
    } finally { cancelling.current.delete(key) }
  }
  const download = (node: DriveNode) => {
    const key = crypto.randomUUID(), native = node.size > managedDownloadLimit
    if (!native) checkTransferCapacity(current.current, 1)
    if (native) { const link = document.createElement('a'); link.href = contentUrl(node.id, true); link.download = node.name; link.click() }
    change(items => [...items, { key, id: node.id, name: node.name, size: node.size, mime_type: node.mime_type, parent_id: node.parent_id, direction: 'download', source: sourceOf(node), state: native ? 'browser' : 'queued', received_bytes: 0, created_at: new Date().toISOString() }])
  }
  const clearCompleted = () => change(items => items.filter(item => item.state !== 'complete' && item.state !== 'browser'))
  return { transfers, addFiles, copyFiles, download, pause, resume, cancel, clearCompleted }
}
