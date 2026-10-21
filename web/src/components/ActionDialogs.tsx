import { useEffect, useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, ChevronRight, Folder, FolderInput, Loader2, Trash2 } from 'lucide-react'
import { api } from '../api'
import type { Crumb, DriveNode, NodeList } from '../types'
import { errorMessage, validateName } from '../utils'
import { Button, InlineError, Modal } from './ui'

export function NameDialog({ mode, node, close, save }: { mode: 'create' | 'rename' | null; node?: DriveNode; close: () => void; save: (name: string) => Promise<void> }) {
  const [name, setName] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  useEffect(() => { setName(mode === 'rename' ? node?.name || '' : ''); setError('') }, [mode, node])
  const submit = async (event: FormEvent) => {
    event.preventDefault()
    const invalid = validateName(name)
    if (invalid) { setError(invalid); return }
    setBusy(true); setError('')
    try { await save(name); close() } catch (error) { setError(errorMessage(error)) } finally { setBusy(false) }
  }
  return <Modal open={!!mode} onOpenChange={open => { if (!open && !busy) close() }} title={mode === 'create' ? 'New folder' : 'Rename item'} description={mode === 'create' ? 'Create a folder in this location.' : 'Choose a new name for this item.'}><form onSubmit={submit}><label htmlFor="item-name">{mode === 'create' ? 'Folder name' : 'Name'}</label><input id="item-name" className="form-input" value={name} onChange={event => { setName(event.target.value); setError('') }} placeholder={mode === 'create' ? 'Untitled folder' : ''} autoFocus onFocus={event => { if (mode === 'rename') { const end = node?.kind === 'file' && name.lastIndexOf('.') > 0 ? name.lastIndexOf('.') : name.length; event.target.setSelectionRange(0, end) } }} maxLength={255} disabled={busy} />{error && <InlineError>{error}</InlineError>}<div className="dialog-actions"><Button type="button" onClick={close} disabled={busy}>Cancel</Button><Button type="submit" variant="primary" disabled={!name.trim()} busy={busy}>{mode === 'create' ? 'Create folder' : 'Save changes'}</Button></div></form></Modal>
}

export function MoveDialog({ nodes, close, move }: { nodes: DriveNode[]; close: () => void; move: (parent: string) => Promise<void> }) {
  const queryClient = useQueryClient()
  const [path, setPath] = useState<Crumb[]>([{ id: '', name: 'My drive' }])
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [loadingMore, setLoadingMore] = useState(false)
  const current = path.at(-1)!
  const folders = useQuery({ queryKey: ['move-folders', current.id], queryFn: () => api.folders(current.id) })
  const selectedIds = new Set(nodes.map(node => node.id))
  return <Modal open={nodes.length > 0} onOpenChange={open => { if (!open && !busy) close() }} title={`Move ${nodes.length === 1 ? `“${nodes[0].name}”` : `${nodes.length} items`}`} description="Choose a destination folder."><nav className="move-breadcrumb" aria-label="Move destination"><button disabled={path.length === 1 || busy} aria-label="Up one folder" onClick={() => { setPath(items => items.slice(0, -1)); setError('') }}><ArrowLeft size={16} /></button>{path.map((crumb, index) => <span key={crumb.id}>{index > 0 && <ChevronRight size={12} />}<button onClick={() => { setPath(items => items.slice(0, index + 1)); setError('') }} disabled={index === path.length - 1 || busy} aria-current={index === path.length - 1 ? 'location' : undefined}>{crumb.name}</button></span>)}</nav><div className="move-folders" aria-busy={folders.isLoading}>{folders.isLoading ? <p role="status"><Loader2 size={16} className="spin" />Loading folders…</p> : folders.error ? <InlineError>{errorMessage(folders.error)}</InlineError> : !folders.data?.items.filter(node => !selectedIds.has(node.id)).length ? <p>No folders here. You can move into this location.</p> : folders.data.items.filter(node => !selectedIds.has(node.id)).map(node => <button key={node.id} disabled={busy} onClick={() => { setPath(items => [...items, { id: node.id, name: node.name }]); setError('') }}><Folder size={20} fill="currentColor" strokeWidth={1.5} /><span>{node.name}</span><ChevronRight size={15} /></button>)}</div>{folders.data && folders.data.items.length < folders.data.total && <Button busy={loadingMore} disabled={busy} onClick={async () => { setLoadingMore(true); try { const page = await api.folders(current.id, folders.data.items.length); queryClient.setQueryData<NodeList>(['move-folders', current.id], previous => previous ? { ...page, items: [...previous.items, ...page.items] } : page) } catch (error) { setError(errorMessage(error)) } finally { setLoadingMore(false) } }}>Load more folders</Button>}{error && <InlineError>{error}</InlineError>}<div className="dialog-actions"><Button onClick={close} disabled={busy}>Cancel</Button><Button variant="primary" busy={busy} disabled={nodes.every(node => node.parent_id === current.id)} onClick={async () => { setBusy(true); setError(''); try { await move(current.id); close() } catch (error) { setError(errorMessage(error)) } finally { setBusy(false) } }}><FolderInput size={16} />Move here</Button></div></Modal>
}

export function DeleteDialog({ nodes, close, remove }: { nodes: DriveNode[]; close: () => void; remove: () => Promise<void> }) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  return <Modal open={nodes.length > 0} onOpenChange={open => { if (!open && !busy) close() }} title="Delete permanently?" description={`${nodes.length === 1 ? `“${nodes[0].name}”` : `${nodes.length} selected items`} will be permanently deleted, including any files inside selected folders. This cannot be undone.`}><div className="delete-warning"><Trash2 size={21} /><p>Encrypted content and metadata will be removed from storage.</p></div>{error && <InlineError>{error}</InlineError>}<div className="dialog-actions"><Button onClick={close} disabled={busy}>Keep in trash</Button><Button variant="danger" busy={busy} onClick={async () => { setBusy(true); setError(''); try { await remove(); close() } catch (error) { setError(errorMessage(error)) } finally { setBusy(false) } }}>Delete permanently</Button></div></Modal>
}
