import { lazy, Suspense, useCallback, useEffect, useRef, useState, type DragEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, CircleAlert, Clipboard, CloudUpload, LockKeyhole, ShieldCheck, Upload, X } from 'lucide-react'
import { api, ApiError } from './api'
import type { BulkAction, DriveNode, Listing, NodeList, View } from './types'
import { errorMessage, formatBytes } from './utils'
import { useTransfers } from './use-transfers'
import { Login } from './components/Login'
import { Sidebar } from './components/Sidebar'
import { FileToolbar, Topbar } from './components/Toolbar'
import { FileBrowser, SelectionBar, type NodeAction } from './components/FileBrowser'
import { Inspector, Preview } from './components/Preview'
import { Transfers } from './components/Transfers'
import { DeleteDialog, MoveDialog, NameDialog } from './components/ActionDialogs'
import { Button } from './components/ui'
import { copyName, preferenceKey, viewPreference } from './workspace'
import { topLevelSelection } from './file-manager'
import { setupStatus } from './onboarding'

const Onboarding = lazy(() => import('./components/Onboarding').then(module => ({ default: module.Onboarding })))

const Settings = lazy(() => import('./components/Settings').then(module => ({ default: module.Settings })))
const Help = lazy(() => import('./components/Settings').then(module => ({ default: module.Help })))
const CommandPalette = lazy(() => import('./components/CommandPalette'))
const initialListing: Listing = { parent_id: '', view: 'all', q: '', sort: 'name', direction: 'asc', type: '' }
function preferences(listing: Listing) {
  try { return viewPreference(JSON.parse(localStorage.getItem(preferenceKey(listing)) || 'null'), localStorage.getItem('harbor:view') === 'grid') } catch { return viewPreference(null) }
}

export default function App() {
  const queryClient = useQueryClient()
  const setup = useQuery({ queryKey: ['setup'], queryFn: ({ signal }) => setupStatus(signal), staleTime: Infinity, retry: 3 })
  const session = useQuery({ queryKey: ['session'], queryFn: api.session, retry: false, enabled: setup.data?.required === false })
  useEffect(() => {
    const unauthorized = () => { queryClient.setQueryData(['session'], null) }
    window.addEventListener('harbor:unauthorized', unauthorized)
    return () => window.removeEventListener('harbor:unauthorized', unauthorized)
  }, [queryClient])
  if (setup.isPending) return <div className="app-loading"><span className="loading-brand">H</span><p>Opening your space…</p></div>
  if (setup.error) return <div className="connection-error"><CircleAlert size={30} /><h1>Harbor is out of reach</h1><p>{errorMessage(setup.error)}</p><Button onClick={() => { void setup.refetch() }}>Try again</Button></div>
  if (setup.data?.required) return <Suspense fallback={<div className="app-loading">Preparing setup…</div>}><Onboarding onComplete={() => { void queryClient.invalidateQueries({ queryKey: ['setup'] }) }} /></Suspense>
  if (session.isPending) return <div className="app-loading"><span className="loading-brand">H</span><p>Opening your space…</p></div>
  if (session.error && !(session.error instanceof ApiError && session.error.status === 401)) return <div className="connection-error"><CircleAlert size={30} /><h1>Harbor is out of reach</h1><p>{errorMessage(session.error)}</p><Button onClick={() => { void session.refetch() }}>Try again</Button></div>
  if (!session.data) return <Login onLogin={() => { void queryClient.invalidateQueries({ queryKey: ['session'] }) }} />
  return <Workspace logout={async () => { await api.logout(); queryClient.setQueryData(['session'], null); queryClient.removeQueries({ predicate: query => query.queryKey[0] !== 'session' }) }} />
}

function Workspace({ logout }: { logout: () => Promise<void> }) {
  const queryClient = useQueryClient()
  const [listing, setListing] = useState<Listing>(() => ({ ...initialListing, sort: preferences(initialListing).sort, direction: preferences(initialListing).direction }))
  const [search, setSearch] = useState('')
  const [grid, setGrid] = useState(() => preferences(initialListing).grid)
  const [collapsed, setCollapsed] = useState(() => localStorage.getItem('harbor:sidebar') === 'collapsed')
  const [commands, setCommands] = useState(false)
  const [renameId, setRenameId] = useState<string | null>(null)
  const [clipboard, setClipboard] = useState<{ mode: 'copy' | 'cut'; nodes: DriveNode[] }>({ mode: 'copy', nodes: [] })
  const [pasting, setPasting] = useState(false)
  const copyOperation = useRef<{ key: string; id: string } | null>(null)
  const [dark, setDark] = useState(() => localStorage.getItem('harbor:theme') === 'dark' || (!localStorage.getItem('harbor:theme') && matchMedia('(prefers-color-scheme: dark)').matches))
  const [selected, setSelected] = useState(new Set<string>())
  const [preview, setPreview] = useState<DriveNode | null>(null)
  const [inspector, setInspector] = useState<DriveNode | null>(null)
  const [nameDialog, setNameDialog] = useState<{ mode: 'create' | 'rename'; node?: DriveNode } | null>(null)
  const [moveNodes, setMoveNodes] = useState<DriveNode[]>([])
  const [deleteNodes, setDeleteNodes] = useState<DriveNode[]>([])
  const [settings, setSettings] = useState(false)
  const [help, setHelp] = useState(false)
  const [mobileOpen, setMobileOpen] = useState(false)
  const [transfersOpen, setTransfersOpen] = useState(false)
  const [dragging, setDragging] = useState(false)
  const [loadingMore, setLoadingMore] = useState(false)
  const [toast, setToast] = useState<{ message: string; error?: boolean } | null>(null)
  const fileInput = useRef<HTMLInputElement>(null)
  const searchInput = useRef<HTMLInputElement>(null)
  const dragDepth = useRef(0)
  const lastSelected = useRef<string | null>(null)
  const toastTimeout = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const notify = useCallback((message: string, error = false) => { clearTimeout(toastTimeout.current); setToast({ message, error }); toastTimeout.current = setTimeout(() => setToast(null), error ? 8000 : 4500) }, [])
  useEffect(() => () => clearTimeout(toastTimeout.current), [])
  const refresh = useCallback(() => { void queryClient.invalidateQueries({ queryKey: ['nodes'] }); void queryClient.invalidateQueries({ queryKey: ['diagnostics'] }); void queryClient.invalidateQueries({ queryKey: ['move-folders'] }) }, [queryClient])
  const transfers = useTransfers(refresh)
  const nodeQuery = useQuery({ queryKey: ['nodes', listing], queryFn: ({ signal }) => api.nodes(listing, signal) })
  const copyJobs = useQuery({ queryKey: ['copy-jobs'], queryFn: api.copyJobs, refetchInterval: query => query.state.data?.items.some(job => ['queued', 'running', 'cancelling'].includes(job.status)) ? 1000 : 5000 })
  const previousJobs = useRef(new Map<string, string>())
  useEffect(() => {
    for (const job of copyJobs.data?.items || []) {
      const previous = previousJobs.current.get(job.id)
      if (job.status === 'complete' && previous !== 'complete') refresh()
      if (job.status === 'complete' && previous && previous !== 'complete') notify('Folder copy complete')
      previousJobs.current.set(job.id, job.status)
    }
  }, [copyJobs.data, refresh, notify])
  const diagnostics = useQuery({ queryKey: ['diagnostics'], queryFn: api.diagnostics, refetchInterval: 5000, staleTime: 4000 })
  const items = nodeQuery.data?.items || []
  const activeTransfers = transfers.transfers.filter(item => item.state !== 'complete' && item.state !== 'browser').length + (copyJobs.data?.items.filter(job => !['complete', 'cancelled'].includes(job.status)).length || 0)
  const currentFolder = listing.view === 'all' ? listing.parent_id : ''
  const savePreference = (next: Listing, nextGrid: boolean) => { try { localStorage.setItem(preferenceKey(next), JSON.stringify({ grid: nextGrid, sort: next.sort, direction: next.direction })) } catch { /* Preferences are optional when storage is unavailable. */ } }
  const updateListing = (changes: Partial<Listing>) => {
    const next = { ...listing, ...changes }
    if (preferenceKey(next) !== preferenceKey(listing)) { const saved = preferences(next); next.sort = saved.sort; next.direction = saved.direction; setGrid(saved.grid) }
    else if (changes.sort || changes.direction) savePreference(next, grid)
    setListing(next); setSelected(new Set()); setInspector(null); setRenameId(null)
  }
  const changeGrid = (next: boolean) => { setGrid(next); savePreference(listing, next) }
  const navigateFolder = (id: string) => { updateListing({ parent_id: id, view: 'all', q: '', type: '' }); setSearch('') }
  const navigateView = (view: View) => { updateListing({ parent_id: '', view, q: '', type: '' }); setSearch('') }
  const upload = () => fileInput.current?.click()
  const addFiles = (files: File[]) => { if (!files.length) return; try { transfers.addFiles(files, currentFolder); setTransfersOpen(true) } catch (error) { notify(errorMessage(error), true) } }
  const download = (node: DriveNode) => { try { transfers.download(node); setTransfersOpen(true); setPreview(null) } catch (error) { notify(errorMessage(error), true) } }

  useEffect(() => { const timer = setTimeout(() => setListing(value => value.q === search ? value : { ...value, q: search }), 200); return () => clearTimeout(timer) }, [search])
  useEffect(() => { document.documentElement.classList.toggle('dark', dark); localStorage.setItem('harbor:theme', dark ? 'dark' : 'light') }, [dark])
  useEffect(() => { localStorage.setItem('harbor:sidebar', collapsed ? 'collapsed' : 'expanded') }, [collapsed])
  useEffect(() => { setSelected(new Set()); setInspector(null) }, [listing.q])

  const select = (id: string, extend: boolean, range: boolean) => {
    setSelected(current => {
      if (range && lastSelected.current) {
        const from = items.findIndex(item => item.id === lastSelected.current), to = items.findIndex(item => item.id === id)
        if (from >= 0 && to >= 0) return new Set([...current, ...items.slice(Math.min(from, to), Math.max(from, to) + 1).map(item => item.id)])
      }
      if (extend) { const next = new Set(current); next.has(id) ? next.delete(id) : next.add(id); return next }
      return new Set([id])
    })
    lastSelected.current = id
  }

  const selectionRoots = async (nodes: DriveNode[]) => {
    const ancestors = new Map<string, string[]>()
    if (nodes.some(node => node.kind === 'folder')) for (const parent of new Set(nodes.map(node => node.parent_id))) {
      if (parent) ancestors.set(parent, (await api.nodes({ ...initialListing, parent_id: parent })).breadcrumbs.map(crumb => crumb.id))
    }
    return topLevelSelection(nodes, ancestors)
  }
  const runBulk = async (nodes: DriveNode[], action: BulkAction, parent_id?: string) => {
    if (action === 'move') nodes = await selectionRoots(nodes)
    let completed = 0
    const errors: string[] = []
    try {
      for (let offset = 0; offset < nodes.length; offset += 100) {
        const result = await api.bulk(nodes.slice(offset, offset + 100).map(node => node.id), action, parent_id)
        completed += result.completed.length; errors.push(...result.errors.map(item => item.error))
      }
    } catch (error) { throw new Error(`${completed ? `${completed} updated. ` : ''}${errorMessage(error)}`) }
    finally { refresh(); setSelected(new Set()); setInspector(null) }
    if (errors.length) throw new Error(`${completed} updated. ${errors.length} failed: ${errors[0]}`)
    notify(action === 'trash' ? `${completed} item${completed === 1 ? '' : 's'} moved to trash` : action === 'restore' ? `${completed} item${completed === 1 ? '' : 's'} restored` : action === 'move' ? 'Moved to new folder' : 'Favorites updated')
  }

  const rename = async (node: DriveNode, name: string) => {
    await queryClient.cancelQueries({ queryKey: ['nodes'] })
    const previous = queryClient.getQueriesData<NodeList>({ queryKey: ['nodes'] })
    queryClient.setQueriesData<NodeList>({ queryKey: ['nodes'] }, data => data ? { ...data, items: data.items.map(item => item.id === node.id ? { ...item, name } : item) } : data)
    try { await api.patch(node.id, { name }); notify('Name updated') }
    catch (error) { for (const [key, value] of previous) queryClient.setQueryData(key, value); throw error }
    finally { refresh() }
  }

  const paste = async (parent = currentFolder) => {
    if (!clipboard.nodes.length || pasting) return
    setPasting(true)
    try {
      const destination = await api.nodes({ ...initialListing, parent_id: parent })
      if (destination.breadcrumbs.some(crumb => clipboard.nodes.some(node => node.kind === 'folder' && node.id === crumb.id))) throw new Error('Choose a destination outside the selected folder.')
      if (clipboard.mode === 'cut') {
        await runBulk(clipboard.nodes, 'move', parent)
        setClipboard({ mode: 'copy', nodes: [] })
      } else {
        const roots = await selectionRoots(clipboard.nodes)
        if (roots.some(node => node.kind === 'folder')) {
          if (roots.length > 100) throw new Error('Copy up to 100 top-level selections per job.')
          const sourceIDs = roots.map(node => node.id).sort(), key = JSON.stringify([parent, sourceIDs])
          if (copyOperation.current?.key !== key) copyOperation.current = { key, id: crypto.randomUUID().replaceAll('-', '') }
          await api.createCopyJob(copyOperation.current.id, sourceIDs, parent)
          copyOperation.current = null
          await queryClient.invalidateQueries({ queryKey: ['copy-jobs'] })
          setTransfersOpen(true)
          notify('Folder copy queued. Continues safely on the server.')
        } else {
          const occupied = new Set([...destination.items.map(node => node.name.toLocaleLowerCase()), ...transfers.transfers.filter(item => item.parent_id === parent && item.direction !== 'download' && item.state !== 'complete').map(item => item.name.toLocaleLowerCase())])
          transfers.copyFiles(roots.map(node => ({ ...node, name: copyName(node.name, occupied) })), parent)
          setTransfersOpen(true)
          notify('Copy queued. Originals stay in place.')
        }
      }
    } catch (error) { refresh(); notify(errorMessage(error), true) } finally { setPasting(false) }
  }

  const favorite = async (node: DriveNode) => {
    const queries = queryClient.getQueriesData<NodeList>({ queryKey: ['nodes'] })
    queryClient.setQueriesData<NodeList>({ queryKey: ['nodes'] }, previous => previous ? { ...previous, items: previous.items.map(item => item.id === node.id ? { ...item, favorite: !item.favorite } : item) } : previous)
    if (inspector?.id === node.id) setInspector({ ...node, favorite: !node.favorite })
    try { await api.patch(node.id, { favorite: !node.favorite }) } catch (error) { for (const [key, data] of queries) queryClient.setQueryData(key, data); setInspector(inspector); throw error } finally { refresh() }
  }

  const action = (action: NodeAction, node: DriveNode) => {
    if (action === 'open') { if (node.trashed_at) { setInspector(node); return } node.kind === 'folder' ? navigateFolder(node.id) : setPreview(node) }
    else if (action === 'download') download(node)
    else if (action === 'rename') setRenameId(node.id)
    else if (action === 'copy' || action === 'cut') { const nodes = selected.has(node.id) ? items.filter(item => selected.has(item.id)) : [node]; setClipboard({ mode: action, nodes }); notify(`${nodes.length} item${nodes.length === 1 ? '' : 's'} ready to ${action === 'cut' ? 'move' : 'copy'}`) }
    else if (action === 'paste') void paste(node.id)
    else if (action === 'move') setMoveNodes([node])
    else if (action === 'info') setInspector(node)
    else if (action === 'permanent') setDeleteNodes([node])
    else if (action === 'favorite') void favorite(node).catch(error => notify(errorMessage(error), true))
    else void runBulk([node], action).catch(error => notify(errorMessage(error), true))
  }
  const selectedNodes = items.filter(item => selected.has(item.id))
  const selectionAction = (name: 'move' | 'favorite' | 'trash' | 'restore' | 'permanent') => {
    if (name === 'move') setMoveNodes(selectedNodes)
    else if (name === 'permanent') setDeleteNodes(selectedNodes)
    else void runBulk(selectedNodes, name).catch(error => notify(errorMessage(error), true))
  }

  useEffect(() => {
    const keyboard = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement
      const typing = ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName) || target.isContentEditable
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k' && (!document.querySelector('[role="dialog"]') || commands)) { event.preventDefault(); setCommands(!commands); return }
      if (typing || document.querySelector('[role="dialog"], [role="menu"]')) return
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'u') { event.preventDefault(); upload() }
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'a') { event.preventDefault(); setSelected(new Set(items.map(item => item.id))) }
      if (event.key === '/') { event.preventDefault(); searchInput.current?.focus() }
      if ((event.metaKey || event.ctrlKey) && ['c', 'x'].includes(event.key.toLowerCase()) && selectedNodes.length && listing.view !== 'trash') { event.preventDefault(); setClipboard({ mode: event.key.toLowerCase() === 'x' ? 'cut' : 'copy', nodes: selectedNodes }); notify(`${selectedNodes.length} ready to ${event.key.toLowerCase() === 'x' ? 'move' : 'copy'}`) }
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'v' && clipboard.nodes.length && listing.view === 'all') { event.preventDefault(); void paste() }
      if (event.key === 'Escape') { setSelected(new Set()); setInspector(null) }
      if (event.key === 'F2' && selectedNodes.length === 1 && !selectedNodes[0].trashed_at) { event.preventDefault(); setRenameId(selectedNodes[0].id) }
      if ((event.key === 'Enter' || event.key === ' ') && selectedNodes.length === 1 && target === document.body) { event.preventDefault(); action('open', selectedNodes[0]) }
      if (event.key === 'Delete' && selectedNodes.length && listing.view !== 'trash') { event.preventDefault(); selectionAction('trash') }
    }
    const sortName = () => updateListing({ sort: 'name', direction: listing.direction === 'asc' && listing.sort === 'name' ? 'desc' : 'asc' })
    window.addEventListener('keydown', keyboard); document.addEventListener('harbor:sort-name', sortName)
    return () => { window.removeEventListener('keydown', keyboard); document.removeEventListener('harbor:sort-name', sortName) }
  })

  const onDrop = (event: DragEvent) => {
    event.preventDefault(); dragDepth.current = 0; setDragging(false)
    if (event.dataTransfer.files.length) addFiles(Array.from(event.dataTransfer.files))
  }
  const previewFiles = items.filter(node => node.kind === 'file' && !node.trashed_at)
  const previewIndex = previewFiles.findIndex(node => node.id === preview?.id)

  return <div className={`app-shell ${collapsed ? 'sidebar-collapsed' : ''}`} onDragEnter={event => { if (event.dataTransfer.types.includes('Files')) { event.preventDefault(); dragDepth.current++; setDragging(true) } }} onDragLeave={event => { event.preventDefault(); if (--dragDepth.current <= 0) { dragDepth.current = 0; setDragging(false) } }} onDragOver={event => { if (event.dataTransfer.types.includes('Files')) { event.preventDefault(); event.dataTransfer.dropEffect = 'copy' } }} onDrop={onDrop}>
    <a className="skip-link" href="#main-content">Skip to files</a><Sidebar view={listing.view} navigate={navigateView} upload={upload} settings={() => setSettings(true)} help={() => setHelp(true)} diagnostics={diagnostics.data} connectionError={diagnostics.isError} dark={dark} toggleTheme={() => setDark(!dark)} mobileOpen={mobileOpen} closeMobile={() => setMobileOpen(false)} collapsed={collapsed} toggleCollapsed={() => setCollapsed(!collapsed)} />
    <div className="workspace"><Topbar query={search} setQuery={setSearch} onMobileMenu={() => setMobileOpen(true)} openTransfers={() => setTransfersOpen(!transfersOpen)} activeTransfers={activeTransfers} openHelp={() => setHelp(true)} searchRef={searchInput} openCommands={() => setCommands(true)} />
      <div className="workspace-body"><main id="main-content" className="main-content"><FileToolbar listing={listing} setListing={updateListing} breadcrumbs={nodeQuery.data?.breadcrumbs || [{ id: '', name: 'My drive' }]} navigateFolder={navigateFolder} grid={grid} setGrid={changeGrid} createFolder={() => setNameDialog({ mode: 'create' })} upload={upload} count={nodeQuery.data?.total || 0} />
        {listing.view === 'trash' && <div className="trash-info"><TrashIcon /><span>Files stay here until you restore or permanently delete them.</span></div>}
        {clipboard.nodes.length > 0 && listing.view === 'all' && <div className="clipboard-bar" role="status"><Clipboard size={15} /><span>{clipboard.nodes.length} ready to {clipboard.mode === 'cut' ? 'move' : 'copy'}</span><div className="clipboard-actions"><Button busy={pasting} onClick={() => { void paste() }}>Paste here</Button><button aria-label="Clear clipboard" onClick={() => setClipboard({ mode: 'copy', nodes: [] })}><X size={15} /></button></div></div>}
        <FileBrowser items={items} listing={listing} grid={grid} selected={selected} onSelect={select} onSelectAll={() => setSelected(selected.size === items.length ? new Set() : new Set(items.map(item => item.id)))} onAction={action} onRename={rename} renameId={renameId} onRenameEnd={() => setRenameId(null)} onMove={async (ids, parent) => { await runBulk(items.filter(node => ids.includes(node.id)), 'move', parent) }} onSort={sort => updateListing({ sort, direction: listing.sort === sort && listing.direction === 'asc' ? 'desc' : 'asc' })} clipboard={{ mode: clipboard.mode, ids: clipboard.nodes.map(node => node.id) }} loading={nodeQuery.isPending} error={nodeQuery.error ? errorMessage(nodeQuery.error) : undefined} retry={() => { void nodeQuery.refetch() }} upload={upload} createFolder={() => setNameDialog({ mode: 'create' })} clearFilters={() => { updateListing({ q: '', type: '' }); setSearch('') }} />
        {nodeQuery.data && items.length < nodeQuery.data.total && <div className="load-more"><span>Showing {items.length.toLocaleString()} of {nodeQuery.data.total.toLocaleString()} items</span><Button busy={loadingMore} onClick={async () => { setLoadingMore(true); try { const page = await api.nodes(listing, undefined, items.length); queryClient.setQueryData<NodeList>(['nodes', listing], previous => previous ? { ...page, items: [...new Map([...previous.items, ...page.items].map(node => [node.id, node])).values()] } : page) } catch (error) { notify(errorMessage(error), true) } finally { setLoadingMore(false) } }}>Load more files</Button></div>}
        <footer className="files-footer"><span>{nodeQuery.isError ? 'Files unavailable' : nodeQuery.data ? `${nodeQuery.data.total} item${nodeQuery.data.total === 1 ? '' : 's'}` : 'Loading…'}{selected.size > 0 && ` · ${selected.size} selected`}</span><span className="footer-encrypted"><ShieldCheck size={13} />Your files. Only yours.<span className="footer-dot">·</span><span>{diagnostics.data?.adapter === 'telegram' ? 'Telegram storage' : diagnostics.data?.adapter === 'local' ? 'Local encrypted storage' : 'Connecting…'}</span></span></footer>
      </main>{inspector && <Inspector node={inspector} close={() => setInspector(null)} onAction={action} notify={notify} />}</div>
    </div>
    {selected.size > 0 && <SelectionBar count={selected.size} clear={() => setSelected(new Set())} action={selectionAction} trash={listing.view === 'trash'} />}
    <input ref={fileInput} type="file" multiple className="sr-only" aria-label="Choose files to upload" onChange={event => { addFiles(Array.from(event.target.files || [])); event.target.value = '' }} />
    {dragging && <div className="drop-overlay"><div><span><CloudUpload size={47} strokeWidth={1.4} /></span><h2>A safe landing for your files.</h2><p>Drop to upload {currentFolder ? 'into this folder' : 'to My drive'}.</p><small><LockKeyhole size={13} />Encrypted before storage</small></div></div>}
    <NameDialog mode={nameDialog?.mode || null} node={nameDialog?.node} close={() => setNameDialog(null)} save={async name => {
      if (nameDialog?.mode === 'rename' && nameDialog.node) await rename(nameDialog.node, name)
      else {
        const key = ['nodes', listing], previous = queryClient.getQueryData<NodeList>(key), temporaryId = `pending-${crypto.randomUUID()}`
        if (previous && listing.view === 'all' && !listing.q && (!listing.type || listing.type === 'folder')) queryClient.setQueryData<NodeList>(key, { ...previous, items: [{ id: temporaryId, parent_id: currentFolder, name, kind: 'folder', size: 0, mime_type: '', sha256: '', favorite: false, trashed_at: null, created_at: new Date().toISOString(), updated_at: new Date().toISOString() }, ...previous.items], total: previous.total + 1 })
        try { await api.createFolder(name, currentFolder); notify('Folder created') } catch (error) { queryClient.setQueryData(key, previous); throw error }
      }
      refresh()
    }} />
    {moveNodes.length > 0 && <MoveDialog nodes={moveNodes} close={() => setMoveNodes([])} move={parent => runBulk(moveNodes, 'move', parent)} />}
    {deleteNodes.length > 0 && <DeleteDialog nodes={deleteNodes} close={() => setDeleteNodes([])} remove={async () => { let count = 0; try { for (const node of deleteNodes) { await api.permanent(node.id); count++ } } finally { refresh(); setSelected(new Set()); setInspector(null) } notify(`${count} item${count === 1 ? '' : 's'} permanently deleted`) }} />}
    <Preview node={preview} files={previewFiles} onNavigate={setPreview} close={() => setPreview(null)} onDownload={download} previous={previewIndex > 0 ? () => setPreview(previewFiles[previewIndex - 1]) : undefined} next={previewIndex >= 0 && previewIndex < previewFiles.length - 1 ? () => setPreview(previewFiles[previewIndex + 1]) : undefined} />
    <Suspense fallback={null}>{settings && <Settings open={settings} close={() => setSettings(false)} logout={async () => { try { await logout() } catch (error) { notify(errorMessage(error), true) } }} />}{help && <Help open={help} close={() => setHelp(false)} />}</Suspense>
    <Suspense fallback={null}>{commands && <CommandPalette close={() => setCommands(false)} items={items} navigate={navigateView} open={node => action('open', node)} upload={upload} createFolder={() => setNameDialog({ mode: 'create' })} settings={() => setSettings(true)} help={() => setHelp(true)} dark={dark} toggleTheme={() => setDark(!dark)} search={value => { setSearch(value); updateListing({ q: value }) }} />}</Suspense>
    {transfersOpen && <Transfers jobs={copyJobs.data?.items || []} jobsError={copyJobs.isError} retryJobs={() => { void copyJobs.refetch() }} retryJob={async id => { await api.retryCopyJob(id); await copyJobs.refetch() }} cancelJob={async id => { await api.cancelCopyJob(id); await copyJobs.refetch() }} items={transfers.transfers} storageState={diagnostics.isError ? 'unknown' : diagnostics.data?.storage_connection?.state} close={() => setTransfersOpen(false)} pause={transfers.pause} resume={transfers.resume} cancel={transfers.cancel} clearCompleted={transfers.clearCompleted} notify={message => notify(message, true)} />}
    {toast && <div className={`toast ${toast.error ? 'toast-error' : ''}`} role={toast.error ? 'alert' : 'status'}>{toast.error ? <CircleAlert size={17} /> : <Check size={17} />}<span>{toast.message}</span><button aria-label="Dismiss notification" onClick={() => setToast(null)}><X size={14} /></button></div>}
  </div>
}

function TrashIcon() { return <CircleAlert size={15} /> }
