export type Session = { authenticated: true; owner: string }

export type DriveNode = {
  id: string
  parent_id: string
  name: string
  kind: 'file' | 'folder'
  size: number
  mime_type: string
  sha256: string
  favorite: boolean
  trashed_at: string | null
  created_at: string
  updated_at: string
}

export type Crumb = { id: string; name: string }
export type NodeList = { items: DriveNode[]; breadcrumbs: Crumb[]; total: number }
export type View = 'all' | 'recent' | 'favorites' | 'trash'
export type FileKind = '' | 'folder' | 'image' | 'video' | 'audio' | 'document' | 'archive' | 'other'
export type Sort = 'name' | 'updated_at' | 'size'
export type Listing = { parent_id: string; view: View; q: string; sort: Sort; direction: 'asc' | 'desc'; type: FileKind }
export type PendingUploadChunk = {
  index: number
  parts: number[]
  bytes: number
  state: 'receiving' | 'queued' | 'storing' | 'failed'
  error_code?: string
}

export type Upload = {
  id: string
  name: string
  parent_id: string
  mime_type: string
  size: number
  sha256: string
  chunk_size: number
  total_chunks: number
  received_chunks: number[]
  received_bytes: number
  part_size?: number
  staged_chunk?: number
  staged_parts?: number[]
  staged_bytes?: number
  pending_chunks?: PendingUploadChunk[]
  max_pending_chunks?: number
  status: 'uploading' | 'complete'
  created_at: string
}

export type Diagnostics = {
  adapter: 'local' | 'telegram'
  telegram_configured: boolean
  storage_connection?: { state: 'connected' | 'disconnected' | 'connecting' | 'retrying' | 'local' | 'unknown'; last_checked_at?: string; retry_count: number }
  encryption: string
  database: string
  version: string
  files: number
  folders: number
  used_bytes: number
  trash_bytes: number
  pending_uploads: number
  chunk_size: number
}

export type BulkResult = { completed: string[]; errors: { id: string; error: string }[] }
export type BulkAction = 'trash' | 'restore' | 'favorite' | 'unfavorite' | 'move'

export type CopyJob = {
  id: string
  status: 'queued' | 'running' | 'failed' | 'cancelling' | 'cancelled' | 'complete'
  total_entries: number
  completed_entries: number
  total_bytes: number
  completed_bytes: number
  error_code: string
  created_at: string
  updated_at: string
  result_ids: string[]
}
