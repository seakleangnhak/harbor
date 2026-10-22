import { useCallback, useEffect, useRef, useState } from 'react'
import { ApiError, api, contentUrl } from './api'
import { errorMessage, hashFile, managedDownloadLimit, readVerifiedDownload, transferTiming, validateName } from './utils'
import { canSaveUploadInBackground, checkTransferCapacity, nextUploadAction, pendingUploadChunks, readCopyRange, restoreTransfers, storedTransfers, transferDelay, transferFromUpload, uploadPartRange, uploadStorageError, validateUploadState, type Transfer, type TransferSource } from './transfer-utils'
import type { DriveNode, Upload } from './types'

export type { Transfer, TransferState } from './transfer-utils'
const storageKey = 'harbor:transfers:v1'
const readTransfers = (): Transfer[] => {
  try { return restoreTransfers(JSON.parse(localStorage.getItem(storageKey) || '[]')) } catch { return [] }
}
const storageWaitError = 'Storage has not progressed for 10 minutes. Check its connection, or resume/cancel other unfinished uploads to free capacity. Accepted parts remain saved.'
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
  const failedChunks = useRef(new Map<string, Map<number, number>>())
  const mounted = useRef(true)
  const saved = useRef('')
  const completeRef = useRef(onComplete)
  completeRef.current = onComplete
  const change = useCallback((fn: (items: Transfer[]) => Transfer[]) => {
    current.current = fn(current.current)
    if (mounted.current) setTransfers(current.current)
  }, [])
  const update = useCallback((key: string, patch: Partial<Transfer>) => change(items => items.map(item => item.key === key ? { ...item, ...patch } : item)), [change])

  useEffect(() => {
    mounted.current = true
    const initial = new Map(current.current.map(item => [item.key, item]))
    const initialIDs = new Set(current.current.map(item => item.id))
    api.uploads().then(({ items }) => {
      if (mounted.current) change(current => {
        const merged = current.map(saved => {
          const upload = items.find(item => item.id === saved.id)
          if (!upload || initial.get(saved.key) !== saved || saved.direction || controllers.current.has(saved.key) || cancelling.current.has(saved.key)) return saved
          const restored = transferFromUpload(upload)
          return { ...saved, ...restored, key: saved.key, state: restored.state === 'needs-file' && (saved.state === 'paused' || saved.state === 'queued') ? saved.state : restored.state }
        })
        return [...merged, ...items.filter(item => !initialIDs.has(item.id) && !current.some(saved => saved.id === item.id)).map(transferFromUpload)]
      })
    }).catch(() => { /* Session errors are surfaced by the shared API client. */ })
    return () => { mounted.current = false; for (const controller of controllers.current.values()) controller.abort() }
  }, [change])
  useEffect(() => {
    try {
      const serialized = JSON.stringify(storedTransfers(transfers))
      if (serialized !== saved.current) { localStorage.setItem(storageKey, serialized); saved.current = serialized }
    } catch { /* Server upload sessions remain resumable when browser storage is full. */ }
  }, [transfers])

  useEffect(() => {
    let stopped = false, timer: ReturnType<typeof setTimeout>, controller: AbortController | undefined, lastKey = ''
    const checkpoints = new Map<string, { marker: string; since: number }>()
    const poll = async () => {
      const keys = new Set(current.current.map(item => item.key))
      for (const key of checkpoints.keys()) if (!keys.has(key)) checkpoints.delete(key)
      const pending = current.current.filter(item => item.id && !item.direction && !controllers.current.has(item.key) && !cancelling.current.has(item.key) && (item.state === 'committing' && item.received_bytes === item.size || ['paused', 'needs-file'].includes(item.state) && item.pending_chunks?.some(chunk => chunk.state === 'queued' || chunk.state === 'storing')))
      const item = pending[pending.findIndex(item => item.key === lastKey) + 1] ?? pending[0]
      if (item) {
        lastKey = item.key
        controller = new AbortController()
        const activeController = controller
        controllers.current.set(item.key, activeController)
        const task = (async () => {
          try {
            for (let attempt = 0; ; attempt++) {
              try {
                const upload = await api.upload(item.id!, activeController.signal)
                activeController.signal.throwIfAborted()
                if (upload.id !== item.id) throw new Error('Upload metadata changed. Refresh and retry.')
                const progress = validateUploadState(upload, { size: item.size, sha256: item.sha256! })
                let restored = transferFromUpload(upload)
                if (progress.received_bytes === upload.size) {
                  const action = nextUploadAction(upload, false)
                  if (action.kind === 'complete') {
                    update(item.key, { ...restored, key: item.key, state: 'verifying', error: undefined, waiting_for_storage: false, bytes_per_second: undefined, eta_seconds: undefined })
                    await api.completeUpload(upload.id, activeController.signal)
                    activeController.signal.throwIfAborted()
                    update(item.key, { state: 'complete', received_bytes: item.size, stored_bytes: item.size, sent_bytes: item.size, pending_chunks: [], error: undefined })
                    files.current.delete(item.key)
                    failedChunks.current.delete(item.key)
                    checkpoints.delete(item.key)
                    completeRef.current()
                    break
                  }
                  if (action.kind === 'queue') {
                    const retries = failedChunks.current.get(item.key) ?? new Map<number, number>()
                    failedChunks.current.set(item.key, retries)
                    const failed = pendingUploadChunks(upload).some(chunk => chunk.index === action.index && chunk.state === 'failed')
                    const count = retries.get(action.index) ?? 0
                    if (failed && count >= 2) throw new Error(uploadStorageError)
                    let accepted: Upload | undefined
                    try { accepted = await (upload.max_pending_chunks === undefined ? api.commitChunk(upload.id, action.index, activeController.signal) : api.queueChunk(upload.id, action.index, activeController.signal)) }
                    catch (error) { if (!(error instanceof ApiError && error.status === 409 && upload.max_pending_chunks !== undefined)) throw error }
                    activeController.signal.throwIfAborted()
                    if (accepted) {
                      if (accepted.id !== upload.id || accepted.chunk_size !== upload.chunk_size || accepted.part_size !== upload.part_size || accepted.max_pending_chunks !== upload.max_pending_chunks) throw new Error('Upload metadata changed. Refresh and retry.')
                      validateUploadState(accepted, { size: item.size, sha256: item.sha256! })
                      restored = transferFromUpload(accepted)
                      if (failed) {
                        retries.set(action.index, count + 1)
                        update(item.key, { retries: (current.current.find(saved => saved.key === item.key)?.retries ?? 0) + 1 })
                      }
                    }
                  }
                  const marker = JSON.stringify([restored.stored_bytes, restored.pending_chunks]), previous = checkpoints.get(item.key)
                  if (!previous || marker !== previous.marker) checkpoints.set(item.key, { marker, since: performance.now() })
                  else if (performance.now() - previous.since >= 10 * 60 * 1000) throw new Error(storageWaitError)
                  update(item.key, { ...restored, key: item.key, state: restored.received_bytes === item.size ? 'committing' : restored.state, error: restored.received_bytes === item.size ? undefined : restored.error, waiting_for_storage: false, bytes_per_second: undefined, eta_seconds: undefined })
                } else {
                  const latest = current.current.find(saved => saved.key === item.key)
                  update(item.key, { ...restored, key: item.key, state: latest?.state === 'queued' ? 'queued' : restored.state === 'needs-file' && latest?.state === 'paused' ? 'paused' : restored.state })
                }
                break
              } catch (error) {
                if (activeController.signal.aborted || attempt >= 2 || !(error instanceof TypeError || error instanceof ApiError && (error.status === 0 || error.status === 429 || error.status >= 500))) throw error
                update(item.key, { retries: (current.current.find(saved => saved.key === item.key)?.retries ?? 0) + 1 })
                await transferDelay(700 * 2 ** attempt, activeController.signal)
              }
            }
          } catch (error) {
            checkpoints.delete(item.key)
            if (!activeController.signal.aborted) update(item.key, { state: 'error', error: errorMessage(error), bytes_per_second: undefined, eta_seconds: undefined })
          } finally {
            resumeRequested.current.delete(item.key)
            controllers.current.delete(item.key)
            tasks.current.delete(item.key)
            change(items => [...items])
          }
        })()
        tasks.current.set(item.key, task)
        await task
      }
      if (!stopped) timer = setTimeout(poll, document.hidden ? 3000 : 1000)
    }
    timer = setTimeout(poll, 1000)
    return () => { stopped = true; clearTimeout(timer); controller?.abort() }
  }, [change, update])

  useEffect(() => {
    if (active.current) return
    const transfer = transfers.find(item => item.state === 'queued' && !controllers.current.has(item.key) && !cancelling.current.has(item.key))
    if (!transfer) return
    const file = files.current.get(transfer.key), source = transfer.source
    if (!file && !source && !transfer.id) { update(transfer.key, { state: 'needs-file' }); return }
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
        let digest = source?.sha256 ?? transfer!.sha256
        if (file) {
          update(transfer!.key, { state: 'hashing', error: undefined, hash_progress: 0 })
          digest = await hashFile(file, signal, percent => update(transfer!.key, { hash_progress: percent }))
          if (transfer!.sha256 && transfer!.sha256 !== digest) throw new Error('File content differs from the original. Select the original file to resume.')
        }
        if (!digest || !/^[a-f0-9]{64}$/i.test(digest)) throw new Error('File integrity metadata is missing. Refresh your drive and retry.')
        update(transfer!.key, { sha256: digest, error: undefined })
        let session = transfer!.id ? await api.upload(transfer!.id, signal) : await api.createUpload({ name: transfer!.name, size: transfer!.size, mime_type: transfer!.mime_type, parent_id: transfer!.parent_id, sha256: digest }, signal)
        if (transfer!.id && session.id !== transfer!.id) throw new Error('Upload metadata changed. Refresh and retry.')
        update(transfer!.key, { id: session.id })
        signal.throwIfAborted()
        const expected = { size: transfer!.size, sha256: digest }, initial = validateUploadState(session, expected)
        if (!file && !source && !session.part_size && initial.received_bytes !== session.size) { update(transfer!.key, { state: 'needs-file', ...initial }); return }
        const chunkSize = session.chunk_size, partSize = session.part_size, maxPending = session.max_pending_chunks, id = session.id
        let retries = 0
        const accept = (accepted: Upload) => {
          signal.throwIfAborted()
          if (accepted.id !== id || accepted.chunk_size !== chunkSize || accepted.part_size !== partSize || accepted.max_pending_chunks !== maxPending) throw new Error('Upload metadata changed. Refresh and retry.')
          const progress = validateUploadState(accepted, expected)
          session = accepted
          update(transfer!.key, { ...progress, sent_bytes: progress.received_bytes, background: accepted.max_pending_chunks !== undefined, pending_chunks: pendingUploadChunks(accepted) })
          return accepted
        }
        const retry = async <T,>(operation: () => Promise<T>, state: 'uploading' | 'committing'): Promise<T> => {
          for (let attempt = 0; ; attempt++) {
            signal.throwIfAborted()
            update(transfer!.key, { state })
            try { return await operation() } catch (error) {
              const acknowledged = validateUploadState(session, expected).received_bytes
              update(transfer!.key, { sent_bytes: acknowledged, bytes_per_second: undefined, eta_seconds: undefined })
              if (signal.aborted || attempt >= 2 || !(error instanceof TypeError || error instanceof ApiError && (error.status === 0 || error.status === 429 || error.status >= 500))) throw error
              update(transfer!.key, { state: 'retrying', retries: ++retries })
              await transferDelay(700 * 2 ** attempt, signal)
            }
          }
        }
        update(transfer!.key, { state: 'uploading', ...initial, sent_bytes: initial.received_bytes, background: session.max_pending_chunks !== undefined, pending_chunks: pendingUploadChunks(session), bytes_per_second: undefined, eta_seconds: undefined, retries })
        if (!source && session.part_size) {
          const failedRetries = failedChunks.current.get(transfer!.key) ?? new Map<number, number>()
          failedChunks.current.set(transfer!.key, failedRetries)
          let checkpoint = '', waitingSince = performance.now()
          let partCache: { index: number; part: number; body: Blob; digest: string } | undefined
          const poll = async () => {
            const marker = JSON.stringify([session.received_chunks, pendingUploadChunks(session)])
            if (marker !== checkpoint) { checkpoint = marker; waitingSince = performance.now() }
            if (performance.now() - waitingSince >= 10 * 60 * 1000) throw new Error(storageWaitError)
            update(transfer!.key, { state: 'committing', waiting_for_storage: true, bytes_per_second: undefined, eta_seconds: undefined })
            await transferDelay(document.hidden ? 3000 : 1000, signal)
            accept(await retry(() => api.upload(id, signal), 'committing'))
          }
          for (;;) {
            signal.throwIfAborted()
            if (canSaveUploadInBackground(session)) {
              update(transfer!.key, { state: 'committing', waiting_for_storage: false, bytes_per_second: undefined, eta_seconds: undefined })
              return
            }
            const action = nextUploadAction(session, !!file)
            if (action.kind === 'complete') break
            if (action.kind === 'needs-file') { update(transfer!.key, { state: 'needs-file', sent_bytes: undefined, waiting_for_storage: false }); return }
            if (action.kind === 'wait') { await poll(); continue }
            const index = action.index
            if (action.kind === 'part') {
              const part = action.part
              if (!partCache || partCache.index !== index || partCache.part !== part) {
                const range = uploadPartRange(session, index, part), body = file!.slice(range.start, range.end)
                partCache = { index, part, body, digest: await hashFile(body, signal) }
              }
              const { body, digest: partDigest } = partCache
              update(transfer!.key, { waiting_for_storage: false })
              try {
                await retry(async () => {
                  const acknowledged = validateUploadState(session, expected).received_bytes, started = performance.now()
                  let lastProgress = -Infinity
                  const progress = (sent: number) => {
                    if (signal.aborted) return
                    const now = performance.now()
                    if (sent !== body.size && now - lastProgress < 100) return
                    lastProgress = now
                    update(transfer!.key, { sent_bytes: acknowledged + sent, ...transferTiming(transfer!.size, acknowledged + sent, acknowledged, now - started) })
                  }
                  try {
                    const accepted = await api.putPart(id, index, part, body, partDigest, signal, progress)
                    accept(accepted)
                    if (!accepted.received_chunks.includes(index) && !pendingUploadChunks(accepted).some(chunk => chunk.index === index && chunk.parts.includes(part))) throw new Error('Upload part was not acknowledged. Refresh and retry.')
                  } catch (error) {
                    if (error instanceof ApiError && error.status === 409) {
                      const latest = accept(await api.upload(id, signal))
                      if (latest.received_chunks.includes(index) || pendingUploadChunks(latest).some(chunk => chunk.index === index && chunk.parts.includes(part))) return
                    }
                    throw error
                  }
                }, 'uploading')
              } catch (error) {
                if (!(error instanceof ApiError && error.status === 409 && session.max_pending_chunks !== undefined)) throw error
                await poll()
              }
            } else {
              const failed = pendingUploadChunks(session).some(chunk => chunk.index === index && chunk.state === 'failed')
              const attempts = failedRetries.get(index) ?? 0
              if (failed && attempts >= 2) throw new Error(uploadStorageError)
              update(transfer!.key, { bytes_per_second: undefined, eta_seconds: undefined })
              try {
                await retry(async () => {
                  const accepted = accept(await (session.max_pending_chunks === undefined ? api.commitChunk(id, index, signal) : api.queueChunk(id, index, signal)))
                  if (!accepted.received_chunks.includes(index) && !pendingUploadChunks(accepted).some(chunk => chunk.index === index && ['queued', 'storing', 'failed'].includes(chunk.state))) throw new Error(uploadStorageError)
                }, 'committing')
                if (failed) { failedRetries.set(index, attempts + 1); update(transfer!.key, { retries: ++retries }) }
              } catch (error) {
                if (!(error instanceof ApiError && error.status === 409 && session.max_pending_chunks !== undefined)) throw error
                await poll()
              }
            }
          }
        } else {
          const committed = new Set(session.received_chunks)
          for (let index = 0; index < session.total_chunks; index++) {
            if (committed.has(index)) continue
            signal.throwIfAborted()
            const start = index * session.chunk_size, end = Math.min((index + 1) * session.chunk_size, transfer!.size) - 1
            let chunk = file?.slice(start, end + 1)
            const started = performance.now(), startedBytes = session.received_bytes
            await retry(async () => {
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
              const accepted = accept(await api.putChunk(session.id, index, chunk, chunkDigest, signal))
              update(transfer!.key, transferTiming(transfer!.size, accepted.received_bytes, startedBytes, performance.now() - started))
            }, 'uploading')
          }
        }
        signal.throwIfAborted()
        update(transfer!.key, { state: 'verifying', waiting_for_storage: false, bytes_per_second: undefined, eta_seconds: undefined })
        await api.completeUpload(session.id, signal)
        signal.throwIfAborted()
        update(transfer!.key, { state: 'complete', received_bytes: transfer!.size, stored_bytes: transfer!.size, sent_bytes: transfer!.size, pending_chunks: [], error: undefined })
        files.current.delete(transfer!.key)
        failedChunks.current.delete(transfer!.key)
        completeRef.current()
      } catch (error) {
        if (!cancelling.current.has(transfer!.key)) update(transfer!.key, { state: signal.aborted ? resumeRequested.current.delete(transfer!.key) ? 'queued' : 'paused' : 'error', sent_bytes: undefined, error: signal.aborted ? undefined : error instanceof TypeError ? 'Transfer interrupted. Check your connection and retry.' : errorMessage(error), bytes_per_second: undefined, eta_seconds: undefined })
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
    update(key, { state: 'paused', sent_bytes: undefined, bytes_per_second: undefined, eta_seconds: undefined })
  }
  const resume = (key: string, file?: File) => {
    const transfer = current.current.find(item => item.key === key)
    if (!transfer || cancelling.current.has(key)) return
    if (file) {
      if (file.size !== transfer.size || file.name !== transfer.name) { update(key, { error: 'Select the original file with the same name and size.' }); return }
      files.current.set(key, file)
    }
    if (controllers.current.get(key)?.signal.aborted) resumeRequested.current.add(key)
    failedChunks.current.delete(key)
    update(key, { state: files.current.has(key) || transfer.source || transfer.id ? 'queued' : 'needs-file', error: undefined })
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
    update(key, { state: 'paused', sent_bytes: undefined, bytes_per_second: undefined, eta_seconds: undefined })
    try {
      await tasks.current.get(key)
      const latest = current.current.find(transfer => transfer.key === key)
      if (latest?.id && latest.direction !== 'download') {
        try { await api.cancelUpload(latest.id) } catch (error) { if (!(error instanceof ApiError && error.status === 404)) throw error }
      }
      files.current.delete(key)
      failedChunks.current.delete(key)
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
