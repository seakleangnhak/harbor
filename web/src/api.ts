import type { BulkAction, BulkResult, CopyJob, Diagnostics, DriveNode, Listing, NodeList, Upload } from './types'

export class ApiError extends Error {
  constructor(message: string, public status: number, public code = '') {
    super(message)
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
export const api = {
  session: () => request<{ authenticated: true; owner: string }>('/auth/session'),
  login: (password: string) => request<{ authenticated: true; owner: string }>('/auth/login', json('POST', { password })),
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
  upload: (id: string) => request<Upload>(`/uploads/${encodeURIComponent(id)}`),
  createUpload: (input: Pick<Upload, 'name' | 'parent_id' | 'mime_type' | 'size' | 'sha256'>, signal?: AbortSignal) => request<Upload>('/uploads', { ...json('POST', input), signal }),
  putChunk: (id: string, index: number, body: Blob, digest: string, signal: AbortSignal) => request<Upload>(`/uploads/${encodeURIComponent(id)}/chunks/${index}`, { method: 'PUT', headers: { 'Content-Type': 'application/octet-stream', 'X-Chunk-SHA256': digest }, body, signal }),
  completeUpload: (id: string, signal?: AbortSignal) => request<DriveNode>(`/uploads/${encodeURIComponent(id)}/complete`, { ...json('POST', {}), signal }),
  cancelUpload: (id: string) => request<void>(`/uploads/${encodeURIComponent(id)}`, json('DELETE')),
  copyJobs: () => request<{ items: CopyJob[] }>('/copy-jobs'),
  createCopyJob: (operation_id: string, source_ids: string[], parent_id: string) => request<CopyJob>('/copy-jobs', json('POST', { operation_id, source_ids, parent_id })),
  retryCopyJob: (id: string) => request<CopyJob>(`/copy-jobs/${encodeURIComponent(id)}/retry`, json('POST', {})),
  cancelCopyJob: (id: string) => request<void>(`/copy-jobs/${encodeURIComponent(id)}`, json('DELETE')),
  diagnostics: () => request<Diagnostics>('/diagnostics'),
}

export const contentUrl = (id: string, download = false) => `/api/v1/nodes/${encodeURIComponent(id)}/content${download ? '?download=1' : ''}`
