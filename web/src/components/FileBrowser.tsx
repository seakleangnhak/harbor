import { useEffect, useRef, useState, type DragEvent, type MouseEvent, type KeyboardEvent, type PointerEvent } from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import * as ContextMenu from '@radix-ui/react-context-menu'
import * as DropdownMenu from '@radix-ui/react-dropdown-menu'
import { ArrowDownToLine, Check, ChevronDown, CircleAlert, ClipboardPaste, Clock3, Copy, Eye, FolderInput, FolderOpen, FolderPlus, Image, Info, Loader2, MoreHorizontal, Pencil, RotateCcw, Scissors, Search, ShieldCheck, Star, Trash2, Upload, X } from 'lucide-react'
import type { DriveNode, Listing, Sort } from '../types'
import { canShowThumbnail, draggedNodeIds, nextFileIndex, nodeDragType } from '../file-manager'
import { errorMessage, fileKind, formatBytes, relativeDate, validateName } from '../utils'
import { Button, Checkbox, cn } from './ui'
import { FileIcon } from './FileIcon'

export type NodeAction = 'open' | 'download' | 'rename' | 'move' | 'favorite' | 'info' | 'trash' | 'restore' | 'permanent' | 'copy' | 'cut' | 'paste'
type Clipboard = { mode: 'copy' | 'cut'; ids: string[] }
type BrowserProps = {
  items: DriveNode[]; listing: Listing; grid: boolean; selected: Set<string>
  onSelect: (id: string, extend: boolean, range: boolean) => void; onSelectAll: () => void
  onAction: (action: NodeAction, node: DriveNode) => void
  loading: boolean; error?: string; retry: () => void; upload: () => void; createFolder: () => void; clearFilters: () => void
  onRename?: (node: DriveNode, name: string) => Promise<void>; renameId?: string | null; onRenameEnd?: () => void
  onMove?: (ids: string[], parent: string) => Promise<void>; onSort?: (sort: Sort) => void; clipboard?: Clipboard
}

function ActionItems({ node, onAction, context = false, clipboard }: { node: DriveNode; onAction: BrowserProps['onAction']; context?: boolean; clipboard?: Clipboard }) {
  const Item = context ? ContextMenu.Item : DropdownMenu.Item
  const Separator = context ? ContextMenu.Separator : DropdownMenu.Separator
  const item = (action: NodeAction, label: string, Icon: typeof Eye, danger = false) => <Item key={action} className={cn('menu-item', danger && 'menu-danger')} onSelect={() => onAction(action, node)}><Icon size={15} /><span>{label}</span></Item>
  return node.trashed_at ? <>{item('restore', 'Restore', RotateCcw)}<Separator className="menu-separator" />{item('permanent', 'Delete permanently', Trash2, true)}</> : <>
    {item('open', node.kind === 'folder' ? 'Open folder' : 'Preview', node.kind === 'folder' ? FolderOpen : Eye)}
    {node.kind === 'file' && item('download', 'Download', ArrowDownToLine)}
    <Separator className="menu-separator" />
    {clipboard && <>{item('copy', 'Copy', Copy)}{item('cut', 'Cut', Scissors)}{node.kind === 'folder' && clipboard.ids.length > 0 && !clipboard.ids.includes(node.id) && item('paste', 'Paste into folder', ClipboardPaste)}<Separator className="menu-separator" /></>}
    {item('favorite', node.favorite ? 'Remove from favorites' : 'Add to favorites', Star)}{item('rename', 'Rename', Pencil)}{item('move', 'Move to…', FolderInput)}{item('info', 'File details', Info)}
    <Separator className="menu-separator" />{item('trash', 'Move to trash', Trash2, true)}
  </>
}

function Thumbnail({ node }: { node: DriveNode }) {
  const [loaded, setLoaded] = useState(false), [failed, setFailed] = useState(false)
  return <>{!loaded && <span className="thumbnail-fallback"><FileIcon node={node} large /></span>}{!failed && <img className={cn('thumbnail-image', loaded && 'thumbnail-image-loaded')} loading="lazy" decoding="async" draggable={false} src={`/api/v1/nodes/${encodeURIComponent(node.id)}/content`} alt="" onLoad={() => setLoaded(true)} onError={() => setFailed(true)} />}</>
}

export function FileBrowser(props: BrowserProps) {
  const { items, listing, grid, selected, onSelect, loading, error, onRename, onMove, clipboard } = props
  const scrollRef = useRef<HTMLDivElement>(null), dragIds = useRef<string[]>([])
  const [columns, setColumns] = useState(4), [mobile, setMobile] = useState(() => matchMedia('(max-width: 700px)').matches)
  const [rename, setRename] = useState<{ node: DriveNode; name: string; busy: boolean } | null>(null)
  const [notice, setNotice] = useState(''), [moving, setMoving] = useState(false), [dropTarget, setDropTarget] = useState<string | null>(null)
  const [thumbnails, setThumbnails] = useState(false)
  const press = useRef<{ timer: ReturnType<typeof setTimeout>; x: number; y: number } | null>(null), pressed = useRef<string | null>(null)
  useEffect(() => {
    const element = scrollRef.current
    if (!element) return
    const observer = new ResizeObserver(([entry]) => setColumns(matchMedia('(max-width: 767px)').matches ? 2 : Math.max(1, Math.floor((entry.contentRect.width + 12) / 212))))
    observer.observe(element)
    const media = matchMedia('(max-width: 700px)'), change = () => setMobile(media.matches)
    media.addEventListener('change', change)
    return () => { observer.disconnect(); media.removeEventListener('change', change); if (press.current) clearTimeout(press.current.timer) }
  }, [])
  useEffect(() => { scrollRef.current?.scrollTo({ top: 0 }); setRename(null); setNotice(''); setThumbnails(false) }, [listing.parent_id, listing.view, listing.q, listing.type])
  const beginRename = (node: DriveNode) => { if (!onRename || node.trashed_at) return; setNotice(''); setRename({ node, name: node.name, busy: false }) }
  useEffect(() => { if (props.renameId) { const node = items.find(item => item.id === props.renameId); if (node) beginRename(node) } }, [props.renameId])
  const endRename = () => { const id = rename?.node.id; setRename(null); props.onRenameEnd?.(); requestAnimationFrame(() => { if (id) document.getElementById(`node-${id}`)?.focus() }) }
  const saveRename = async () => {
    if (!rename || rename.busy || !onRename) return
    const invalid = validateName(rename.name)
    if (invalid) { setNotice(invalid); return }
    if (rename.name === rename.node.name) { endRename(); return }
    setRename({ ...rename, busy: true }); setNotice('')
    try { await onRename(rename.node, rename.name); endRename() } catch (error) { setRename({ ...rename, busy: false }); setNotice(errorMessage(error)) }
  }
  const onAction: BrowserProps['onAction'] = (action, node) => action === 'rename' && onRename ? beginRename(node) : props.onAction(action, node)
  const virtual = items.length > 100
  const rowCount = grid ? Math.ceil(items.length / columns) : items.length
  const virtualizer = useVirtualizer({ count: rowCount, getScrollElement: () => scrollRef.current, estimateSize: () => grid ? 184 : mobile ? 68 : 56, overscan: 6, enabled: virtual })
  useEffect(() => { virtualizer.measure() }, [grid, mobile, columns])
  const select = (event: MouseEvent | KeyboardEvent, node: DriveNode) => onSelect(node.id, event.metaKey || event.ctrlKey, event.shiftKey)
  const keyboard = (event: KeyboardEvent, node: DriveNode) => {
    if (event.target !== event.currentTarget) return
    if (event.key === 'F2' && onRename && !node.trashed_at) { event.preventDefault(); event.stopPropagation(); beginRename(node); return }
    if (event.key === 'Enter') { event.preventDefault(); onAction('open', node) }
    if (event.key === ' ') { event.preventDefault(); select(event, node) }
    const index = nextFileIndex(items.findIndex(item => item.id === node.id), event.key, grid ? columns : 1, items.length)
    if (index !== undefined && (grid || !['ArrowLeft', 'ArrowRight'].includes(event.key))) {
      event.preventDefault()
      onSelect(items[index].id, event.metaKey || event.ctrlKey, event.shiftKey)
      if (virtual) virtualizer.scrollToIndex(grid ? Math.floor(index / columns) : index)
      requestAnimationFrame(() => document.getElementById(`node-${items[index].id}`)?.focus())
    }
  }
  const cancelPress = () => { if (press.current) clearTimeout(press.current.timer); press.current = null }
  const startPress = (event: PointerEvent, node: DriveNode) => {
    if (event.pointerType !== 'touch' || (event.target as HTMLElement).closest('button:not(.file-name),input')) return
    cancelPress(); pressed.current = null
    press.current = { x: event.clientX, y: event.clientY, timer: setTimeout(() => { onSelect(node.id, true, false); pressed.current = node.id; press.current = null }, 450) }
  }
  const dragStart = (event: DragEvent, node: DriveNode) => {
    if (!onMove || node.trashed_at || rename || moving) { event.preventDefault(); return }
    dragIds.current = selected.has(node.id) ? items.filter(item => selected.has(item.id) && !item.trashed_at).map(item => item.id) : [node.id]
    if (!selected.has(node.id)) onSelect(node.id, false, false)
    event.dataTransfer.setData(nodeDragType, JSON.stringify(dragIds.current)); event.dataTransfer.effectAllowed = 'move'
  }
  const drop = async (event: DragEvent, node: DriveNode) => {
    if (!onMove || node.kind !== 'folder' || node.trashed_at || !event.dataTransfer.types.includes(nodeDragType)) return
    event.preventDefault(); event.stopPropagation(); setDropTarget(null)
    if (moving) return
    const activeIds = new Set(items.filter(item => !item.trashed_at).map(item => item.id))
    const ids = draggedNodeIds(event.dataTransfer.getData(nodeDragType)).filter(id => activeIds.has(id))
    dragIds.current = []
    if (!ids.length || ids.includes(node.id)) return
    setMoving(true); setNotice('')
    try { await onMove(ids, node.id) } catch (error) { setNotice(errorMessage(error)) } finally { setMoving(false) }
  }
  const name = (node: DriveNode) => rename?.node.id === node.id ? <form className="inline-rename" onSubmit={event => { event.preventDefault(); void saveRename() }} onClick={event => event.stopPropagation()} onDoubleClick={event => event.stopPropagation()} onKeyDown={event => { event.stopPropagation(); if (event.key === 'Escape') { event.preventDefault(); if (!rename.busy) endRename() } }}><input autoFocus className="inline-rename-input" aria-label={`Rename ${node.name}`} value={rename.name} disabled={rename.busy} maxLength={255} onChange={event => { setRename({ ...rename, name: event.target.value }); setNotice('') }} onFocus={event => event.target.setSelectionRange(0, node.kind === 'file' && node.name.lastIndexOf('.') > 0 ? node.name.lastIndexOf('.') : node.name.length)} /><span className="inline-rename-actions"><button className="icon-button" type="submit" aria-label="Save name" disabled={rename.busy}>{rename.busy ? <Loader2 className="spin" size={14} /> : <Check size={14} />}</button><button className="icon-button" type="button" aria-label="Cancel rename" disabled={rename.busy} onClick={endRename}><X size={14} /></button></span></form> : <button className="file-name" onClick={event => { event.stopPropagation(); if (pressed.current === node.id) { pressed.current = null; event.preventDefault(); return } if (event.metaKey || event.ctrlKey || event.shiftKey) select(event, node); else onAction('open', node) }}>{node.name}</button>
  const row = (node: DriveNode) => <ContextMenu.Root key={node.id} modal={false}><ContextMenu.Trigger asChild>
    <div id={`node-${node.id}`} className={cn(grid ? 'file-card' : 'file-row', grid && node.kind === 'folder' && 'file-card-folder', selected.has(node.id) && 'file-selected', clipboard?.mode === 'cut' && clipboard.ids.includes(node.id) && 'file-cut', dropTarget === node.id && 'file-drop-target')} role={grid ? 'listitem' : 'row'} tabIndex={0} aria-label={`${node.name}, ${node.kind === 'folder' ? 'folder' : formatBytes(node.size)}${selected.has(node.id) ? ', selected' : ''}`} aria-selected={grid ? undefined : selected.has(node.id)} aria-busy={rename?.node.id === node.id && rename.busy || moving || undefined}
      draggable={!!onMove && !node.trashed_at && !rename && !node.id.startsWith('pending-')}
      onDragStart={event => dragStart(event, node)} onDragEnd={() => { dragIds.current = []; setDropTarget(null) }}
      onDragOver={event => { if (onMove && node.kind === 'folder' && !node.trashed_at && !moving && event.dataTransfer.types.includes(nodeDragType) && !dragIds.current.includes(node.id)) { event.preventDefault(); event.stopPropagation(); event.dataTransfer.dropEffect = 'move'; setDropTarget(node.id) } }}
      onDragLeave={event => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setDropTarget(null) }} onDrop={event => { void drop(event, node) }}
      onPointerDown={event => startPress(event, node)} onPointerMove={event => { if (press.current && Math.hypot(event.clientX - press.current.x, event.clientY - press.current.y) > 10) cancelPress() }} onPointerUp={cancelPress} onPointerCancel={() => { cancelPress(); pressed.current = null }}
      onClick={event => { if (pressed.current === node.id) { pressed.current = null; event.preventDefault(); return } select(event, node) }} onDoubleClick={() => { if (!rename) onAction('open', node) }} onContextMenu={() => { cancelPress(); if (!selected.has(node.id)) onSelect(node.id, false, false) }} onKeyDown={event => keyboard(event, node)}>
      <div className="file-select-cell" role={grid ? undefined : 'gridcell'}><Checkbox checked={selected.has(node.id)} label={`Select ${node.name}`} onChange={() => onSelect(node.id, true, false)} /></div>
      <div className={grid ? 'file-card-visual' : 'file-main-cell'} role={grid ? undefined : 'gridcell'}>{grid && thumbnails && canShowThumbnail(node) ? <Thumbnail node={node} /> : <FileIcon node={node} large={grid} />}{!grid && <div className="file-name-wrap">{name(node)}{node.favorite && <Star className="favorite-star" size={12} fill="currentColor" aria-label="Favorite" />}<span className="file-location">{node.kind === 'folder' ? 'Folder' : node.name.includes('.') ? `${node.name.split('.').at(-1)?.toUpperCase()} file` : 'File'}</span></div>}</div>
      {grid ? <div className="file-card-meta">{name(node)}<span>{node.kind === 'folder' ? 'Folder' : formatBytes(node.size)}<span>·</span>{relativeDate(node.updated_at)}</span>{node.favorite && <Star className="favorite-star" size={12} fill="currentColor" aria-label="Favorite" />}</div> : <><div className="file-date-cell" role="gridcell" title={new Date(node.trashed_at || node.updated_at).toLocaleString()}>{relativeDate(node.trashed_at || node.updated_at)}</div><div className="file-size-cell" role="gridcell">{node.kind === 'folder' ? '—' : formatBytes(node.size)}</div><div className="file-kind-cell" role="gridcell">{node.kind === 'folder' ? 'Folder' : fileKind(node) === 'other' ? 'File' : fileKind(node).replace(/^./, character => character.toUpperCase())}</div></>}
      <div className="file-action-cell" role={grid ? undefined : 'gridcell'} onClick={event => event.stopPropagation()}><DropdownMenu.Root modal={false}><DropdownMenu.Trigger asChild><button className="icon-button" aria-label={`Actions for ${node.name}`}><MoreHorizontal size={18} /></button></DropdownMenu.Trigger><DropdownMenu.Portal><DropdownMenu.Content className="menu-content" align="end" sideOffset={4}><ActionItems node={node} onAction={onAction} clipboard={clipboard} /></DropdownMenu.Content></DropdownMenu.Portal></DropdownMenu.Root></div>
    </div>
  </ContextMenu.Trigger><ContextMenu.Portal><ContextMenu.Content className="menu-content"><ActionItems node={node} onAction={onAction} clipboard={clipboard} context /></ContextMenu.Content></ContextMenu.Portal></ContextMenu.Root>
  const sortHeading = (sort: Sort, title: string) => <button role="columnheader" aria-sort={listing.sort === sort ? listing.direction === 'asc' ? 'ascending' : 'descending' : 'none'} onClick={() => { if (props.onSort) props.onSort(sort); else if (sort === 'name') document.dispatchEvent(new CustomEvent('harbor:sort-name')) }} disabled={!props.onSort && sort !== 'name'}>{title}{listing.sort === sort && <ChevronDown size={12} className={listing.direction === 'asc' ? 'sort-ascending' : undefined} />}</button>

  return <div className={cn('file-browser', grid && 'file-browser-grid')} role={grid ? undefined : 'grid'} aria-label={grid ? undefined : 'Files'} aria-multiselectable={grid ? undefined : true}>
    {!grid && <div className="table-heading" role="row"><div className="file-select-cell" role="columnheader"><Checkbox checked={items.length > 0 && items.every(node => selected.has(node.id))} indeterminate={items.some(node => selected.has(node.id)) && !items.every(node => selected.has(node.id))} label="Select all visible files" onChange={props.onSelectAll} /></div>{sortHeading('name', 'Name')}{sortHeading('updated_at', listing.view === 'trash' ? 'Date deleted' : 'Date modified')}{sortHeading('size', 'File size')}<span role="columnheader">Type</span><div role="columnheader" aria-label="Actions" /></div>}
    {grid && items.some(canShowThumbnail) && <div className="file-browser-tools"><button className={cn('thumbnail-toggle', thumbnails && 'active')} aria-pressed={thumbnails} onClick={() => setThumbnails(!thumbnails)}><Image size={14} />{thumbnails ? 'Hide thumbnails' : 'Show thumbnails'}</button><span>Images up to 8 MB · loaded as you scroll</span></div>}
    {notice && <div className="file-browser-notice" role="alert"><CircleAlert size={15} /><span>{notice}</span><button className="icon-button" aria-label="Dismiss file action error" onClick={() => setNotice('')}><X size={14} /></button></div>}
    {moving && <div className="file-browser-notice" role="status"><Loader2 size={15} className="spin" /><span>Moving selected items…</span></div>}
    <div className="file-scroll" ref={scrollRef}>
      {loading ? <div className="file-skeletons" aria-label="Loading files" aria-busy="true">{Array.from({ length: 6 }, (_, index) => <div className="file-skeleton" key={index}><span /><span /><span /></div>)}</div> : error ? <div className="empty-state" role="alert"><span className="empty-illustration error-illustration"><CircleAlert size={34} /></span><h2>Files couldn't load</h2><p>{error}</p><Button onClick={props.retry}>Try again</Button></div> : items.length === 0 ? <EmptyState listing={listing} upload={props.upload} createFolder={props.createFolder} clearFilters={props.clearFilters} /> : virtual ? <div className={grid ? 'virtual-grid' : 'file-table'} role={grid ? 'list' : 'rowgroup'} aria-label={grid ? 'Files' : undefined} style={{ height: virtualizer.getTotalSize(), position: 'relative' }}>{virtualizer.getVirtualItems().map(virtualRow => <div key={virtualRow.key} className={grid ? 'file-grid-row' : undefined} style={{ position: 'absolute', top: 0, left: 0, width: '100%', height: virtualRow.size, transform: `translateY(${virtualRow.start}px)`, ...(grid ? { gridTemplateColumns: `repeat(${columns}, minmax(0, 1fr))` } : {}) }}>{grid ? items.slice(virtualRow.index * columns, (virtualRow.index + 1) * columns).map(row) : row(items[virtualRow.index])}</div>)}</div> : <div className={grid ? 'file-grid' : 'file-table'} role={grid ? 'list' : 'rowgroup'} aria-label={grid ? 'Files' : undefined}>{items.map(row)}</div>}
    </div>
  </div>
}

function EmptyState({ listing, upload, createFolder, clearFilters }: { listing: Listing; upload: () => void; createFolder: () => void; clearFilters: () => void }) {
  const filtered = !!listing.q || !!listing.type
  const Icon = filtered ? Search : listing.view === 'trash' ? Trash2 : listing.view === 'favorites' ? Star : listing.view === 'recent' ? Clock3 : FolderOpen
  const title = filtered ? 'No matching files' : listing.view === 'trash' ? 'Trash is empty' : listing.view === 'favorites' ? 'Your favorites, within reach' : listing.view === 'recent' ? 'No recent files yet' : 'A place for your files'
  const description = filtered ? 'Try another search or clear your filters.' : listing.view === 'trash' ? 'Deleted files stay here until you restore or remove them.' : listing.view === 'favorites' ? 'Star a file or folder to find it here.' : listing.view === 'recent' ? 'Files you add and update will appear here.' : 'Drop files to upload, or create a folder to get started.'
  return <div className="empty-state"><div className="empty-art" aria-hidden="true"><span className="empty-art-back" /><span className="empty-illustration"><Icon size={38} strokeWidth={1.3} /></span><span className="empty-art-dot" /></div><span className="eyebrow">{filtered ? 'SEARCH YOUR DRIVE' : listing.view === 'all' ? 'PRIVATE BY DEFAULT' : viewLabel(listing.view)}</span><h2>{title}</h2><p>{description}</p><div className="empty-actions">{filtered ? <Button onClick={clearFilters}>Clear filters<X size={14} /></Button> : listing.view === 'all' ? <><Button variant="primary" onClick={upload}><Upload size={16} />Upload files</Button><Button onClick={createFolder}><FolderPlus size={16} />New folder</Button></> : null}</div>{listing.view === 'all' && !filtered && <span className="empty-security"><ShieldCheck size={14} />Encrypted before storage</span>}</div>
}
const viewLabel = (view: Listing['view']) => view === 'trash' ? 'RECOVERY' : view === 'favorites' ? 'FAVORITES' : 'RECENT FILES'

export function SelectionBar({ count, clear, action, trash }: { count: number; clear: () => void; action: (action: 'move' | 'favorite' | 'trash' | 'restore' | 'permanent') => void; trash: boolean }) {
  return <div className="selection-bar" role="toolbar" aria-label="Selected file actions"><span className="selection-count"><Check size={15} />{count} selected</span><span className="selection-divider" />{trash ? <><button onClick={() => action('restore')}><RotateCcw size={16} />Restore</button><button className="selection-danger" onClick={() => action('permanent')}><Trash2 size={16} /><span>Delete forever</span></button></> : <><button onClick={() => action('move')}><FolderInput size={16} /><span>Move</span></button><button onClick={() => action('favorite')}><Star size={16} /><span>Favorite</span></button><button onClick={() => action('trash')}><Trash2 size={16} /><span>Trash</span></button></>}<span className="selection-divider" /><button aria-label="Clear selection" onClick={clear}><X size={16} /></button></div>
}
