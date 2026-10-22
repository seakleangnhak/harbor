import type { BulkAction, BulkResult, CopyJob, Diagnostics, DriveNode, Listing, NodeList, Session, Upload } from './types'

export class ApiError extends Error {
  status: number
  code: string
  constructor(message: string, status: number, code = '') {
    super(message)
    this.status = status
    this.code = code
  }
}

export async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const response = await fetch(`/api/v1${path}`, {
    credentials: 'same-origin',
    ...init,
    headers: { ...(typeof init.body === 'string' ? { 'Content-Type': 'application/json' } : {}), ...init.headers },
  })
  if (!response.ok) {
    const body = await response.json().catch(() => ({}))
    if (response.status === 401 && path !== '/auth/login' && path !== '/auth/session') window.dispatchEvent(new Event('harbor:unauthorized'))
    throw new ApiError(body.error || `Request failed (${response.status})`, response.status, body.code)
  }
  return response.status === 204 ? undefined as T : response.json()
}

const json = (method: string, body?: unknown): RequestInit => ({ method, ...(body !== undefined ? { body: JSON.stringify(body) } : {}) })

export function putUploadPart(id: string, index: number, part: number, body: Blob, digest: string, signal: AbortSignal, progress: (sent: number) => void): Promise<Upload> {
  return new Promise((resolve, reject) => {
    if (signal.aborted) { reject(signal.reason); return }
    const xhr = new XMLHttpRequest()
    let settled = false
    const finish = (error?: unknown, upload?: Upload) => {
      if (settled) return
      settled = true
      signal.removeEventListener('abort', abort)
      xhr.upload.onprogress = null
      xhr.onload = xhr.onerror = xhr.ontimeout = xhr.onabort = null
      if (error !== undefined) reject(error)
      else resolve(upload!)
    }
    const abort = () => { xhr.abort(); finish(signal.reason ?? new DOMException('Upload aborted', 'AbortError')) }
    xhr.open('PUT', `/api/v1/uploads/${encodeURIComponent(id)}/chunks/${index}/parts/${part}`)
    xhr.responseType = 'json'
    xhr.timeout = 120_000
    xhr.setRequestHeader('Content-Type', 'application/octet-stream')
    xhr.setRequestHeader('X-Part-SHA256', digest)
    xhr.upload.onprogress = event => { if (!settled && !signal.aborted) progress(Math.min(body.size, Math.max(0, event.loaded))) }
    xhr.onload = () => {
      if (signal.aborted) { finish(signal.reason); return }
      if (xhr.status === 401) window.dispatchEvent(new Event('harbor:unauthorized'))
      if (xhr.status < 200 || xhr.status >= 300) {
        const response = xhr.response
        finish(new ApiError(typeof response?.error === 'string' ? response.error : `Request failed (${xhr.status})`, xhr.status, typeof response?.code === 'string' ? response.code : ''))
      } else if (!xhr.response || typeof xhr.response !== 'object') finish(new ApiError('Upload response was interrupted. Retry this part.', 502))
      else { progress(body.size); finish(undefined, xhr.response as Upload) }
    }
    xhr.onerror = () => finish(new ApiError('Transfer interrupted. Check your connection and retry.', 0, 'connection_failed'))
    xhr.ontimeout = () => finish(new ApiError('Upload part timed out. Check your connection and retry.', 0, 'request_timeout'))
    xhr.onabort = () => finish(signal.reason ?? new DOMException('Upload aborted', 'AbortError'))
    signal.addEventListener('abort', abort, { once: true })
    try { xhr.send(body) } catch (error) { finish(error) }
  })
}

export const api = {
  session: () => request<Session>('/auth/session'),
  login: (password: string) => request<Session>('/auth/login', json('POST', { password })),
  updateProfile: (display_name: string) => request<Session>('/auth/profile', json('PATCH', { display_name })),
  changePassword: (current_password: string, new_password: string) => request<void>('/auth/password', json('POST', { current_password, new_password })),
  logout: () => request<void>('/auth/logout', json('POST')),
  nodes: (listing: Listing, signal?: AbortSignal, offset = 0) => request<NodeList>(`/nodes?${new URLSearchParams({ ...listing, limit: '50000', offset: String(offset) })}`, { signal }),
  folders: (parent_id: string, offset = 0) => request<NodeList>(`/nodes?${new URLSearchParams({ parent_id, type: 'folder', limit: '50000', offset: String(offset) })}`),
  createFolder: (name: string, parent_id: string) => request<DriveNode>('/folders', json('POST', { name, parent_id })),
  patch: (id: string, changes: Partial<Pick<DriveNode, 'name' | 'parent_id' | 'favorite'>>) => request<DriveNode>(`/nodes/${encodeURIComponent(id)}`, json('PATCH', changes)),
  trash: (id: string) => request<void>(`/nodes/${encodeURIComponent(id)}`, json('DELETE')),
  restore: (id: string) => request<DriveNode>(`/nodes/${encodeURIComponent(id)}/restore`, json('POST')),
  permanent: (id: string) => request<void>(`/nodes/${encodeURIComponent(id)}/permanent`, json('DELETE')),
  bulk: (ids: string[], action: BulkAction, parent_id?: string) => request<BulkResult>('/nodes/bulk', json('POST', { ids, action, ...(parent_id !== undefined ? { parent_id } : {}) })),
  uploads: () => request<{ items: Upload[] }>('/uploads'),
  upload: (id: string, signal?: AbortSignal) => request<Upload>(`/uploads/${encodeURIComponent(id)}`, { signal }),
  createUpload: (input: Pick<Upload, 'name' | 'parent_id' | 'mime_type' | 'size' | 'sha256'>, signal?: AbortSignal) => request<Upload>('/uploads', { ...json('POST', input), signal }),
  putChunk: (id: string, index: number, body: Blob, digest: string, signal: AbortSignal) => request<Upload>(`/uploads/${encodeURIComponent(id)}/chunks/${index}`, { method: 'PUT', headers: { 'Content-Type': 'application/octet-stream', 'X-Chunk-SHA256': digest }, body, signal }),
  putPart: putUploadPart,
  commitChunk: (id: string, index: number, signal: AbortSignal) => request<Upload>(`/uploads/${encodeURIComponent(id)}/chunks/${index}/commit`, { ...json('POST', {}), signal }),
  queueChunk: (id: string, index: number, signal: AbortSignal) => request<Upload>(`/uploads/${encodeURIComponent(id)}/chunks/${index}/commit`, { ...json('POST', { background: true }), signal }),
  completeUpload: (id: string, signal?: AbortSignal) => request<DriveNode>(`/uploads/${encodeURIComponent(id)}/complete`, { ...json('POST', {}), signal }),
  cancelUpload: (id: string) => request<void>(`/uploads/${encodeURIComponent(id)}`, json('DELETE')),
  copyJobs: () => request<{ items: CopyJob[] }>('/copy-jobs'),
  createCopyJob: (operation_id: string, source_ids: string[], parent_id: string) => request<CopyJob>('/copy-jobs', json('POST', { operation_id, source_ids, parent_id })),
  retryCopyJob: (id: string) => request<CopyJob>(`/copy-jobs/${encodeURIComponent(id)}/retry`, json('POST', {})),
  cancelCopyJob: (id: string) => request<void>(`/copy-jobs/${encodeURIComponent(id)}`, json('DELETE')),
  diagnostics: () => request<Diagnostics>('/diagnostics'),
}

export const contentUrl = (id: string, download = false) => `/api/v1/nodes/${encodeURIComponent(id)}/content${download ? '?download=1' : ''}`
