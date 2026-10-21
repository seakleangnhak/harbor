import { lazy, Suspense, useEffect, useMemo, useRef, useState } from 'react'
import { ArrowDownToLine, ChevronLeft, ChevronRight, CircleAlert, Copy, FolderInput, Info, Loader2, Maximize2, Minimize2, Minus, Plus, RotateCcw, ShieldCheck, Star, X } from 'lucide-react'
import { contentUrl } from '../api'
import type { DriveNode } from '../types'
import { errorMessage, fileKind, formatBytes } from '../utils'
import { Button, IconButton, Modal } from './ui'
import { FileIcon } from './FileIcon'
import { clampImagePan, codeTokens, readPreviewText, textPreviewLimit } from './preview-utils'
import type { NodeAction } from './FileBrowser'

const PdfPreview = lazy(() => import('./PdfPreview'))
const isSafeImage = (node: DriveNode) => /^image\/(png|jpeg|gif|webp|avif|bmp)$/.test(node.mime_type)

function ImagePreview({ node, onError }: { node: DriveNode; onError: () => void }) {
  const [zoom, setZoom] = useState(1)
  const [pan, setPan] = useState({ x: 0, y: 0 })
  const [loading, setLoading] = useState(true)
  const image = useRef<HTMLImageElement>(null)
  const drag = useRef<{ id: number; x: number; y: number; startX: number; startY: number } | null>(null)
  const changeZoom = (value: number) => { setZoom(Math.min(4, Math.max(.5, value))); setPan({ x: 0, y: 0 }) }
  return <div className="image-viewer"><div className="image-controls" role="toolbar" aria-label="Image zoom"><IconButton label="Zoom out image" disabled={zoom <= .5} onClick={() => changeZoom(zoom - .25)}><Minus size={17} /></IconButton><button className="image-zoom-value" aria-label="Reset image to fit" onClick={() => changeZoom(1)}>{Math.round(zoom * 100)}%</button><IconButton label="Zoom in image" disabled={zoom >= 4} onClick={() => changeZoom(zoom + .25)}><Plus size={17} /></IconButton><IconButton label="Fit image to preview" onClick={() => changeZoom(1)}><RotateCcw size={16} /></IconButton><span>{zoom > 1 ? 'Drag to pan' : 'Fit to window'}</span></div>
    <div className="image-stage" tabIndex={0} role="group" aria-label="Image canvas. Zoom in and use arrow keys to pan. Home fits the image." aria-busy={loading} style={{ cursor: zoom > 1 ? 'grab' : 'default', touchAction: zoom > 1 ? 'none' : 'auto' }} onKeyDown={event => {
      if (event.key === 'Home') { event.preventDefault(); event.stopPropagation(); changeZoom(1); return }
      const movement = { ArrowLeft: [40, 0], ArrowRight: [-40, 0], ArrowUp: [0, 40], ArrowDown: [0, -40] }[event.key]
      if (zoom <= 1 || !movement || !image.current) return
      event.preventDefault(); event.stopPropagation()
      setPan(current => clampImagePan(current.x + movement[0], current.y + movement[1], image.current!.clientWidth, image.current!.clientHeight, zoom))
    }} onDoubleClick={() => changeZoom(zoom === 1 ? 2 : 1)} onPointerDown={event => {
      if (zoom <= 1 || event.button !== 0) return
      event.currentTarget.setPointerCapture(event.pointerId)
      drag.current = { id: event.pointerId, x: event.clientX, y: event.clientY, startX: pan.x, startY: pan.y }
    }} onPointerMove={event => {
      if (!drag.current || drag.current.id !== event.pointerId || !image.current) return
      setPan(clampImagePan(drag.current.startX + event.clientX - drag.current.x, drag.current.startY + event.clientY - drag.current.y, image.current.clientWidth, image.current.clientHeight, zoom))
    }} onPointerUp={() => { drag.current = null }} onPointerCancel={() => { drag.current = null }}>
      {loading && <div className="preview-message image-loading" role="status"><Loader2 className="spin" size={26} /><p>Opening image…</p></div>}
      <img ref={image} className="image-preview image-canvas" src={contentUrl(node.id)} alt={node.name} draggable={false} style={{ transform: `translate(${pan.x}px, ${pan.y}px) scale(${zoom})`, visibility: loading ? 'hidden' : 'visible' }} onLoad={() => setLoading(false)} onError={onError} />
    </div>
  </div>
}

export function Preview({ node, close, next, previous, onDownload, files, onNavigate }: { node: DriveNode | null; close: () => void; next?: () => void; previous?: () => void; onDownload: (node: DriveNode) => void; files?: DriveNode[]; onNavigate?: (node: DriveNode) => void }) {
  const [text, setText] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const [expanded, setExpanded] = useState(false)
  const kind = node ? fileKind(node) : 'other'
  const safeImage = !!node && isSafeImage(node)
  const textPreview = !!node && (node.mime_type.startsWith('text/') || /\.(txt|md|json|csv|log|go|py|tsx?|jsx?|css|ya?ml|toml|ini|sh|sql|rs|java|c|cpp|h)$/i.test(node.name))
  const codePreview = !!node && /\.(json|go|py|tsx?|jsx?|css|ya?ml|toml|ini|sh|sql|rs|java|c|cpp|h)$/i.test(node.name)
  const tokens = useMemo(() => codePreview ? codeTokens(text) : [], [text, codePreview])
  const index = files?.findIndex(file => file.id === node?.id) ?? -1
  const nearby = files && index >= 0 ? files.slice(Math.max(0, index - 2), index + 3) : []
  useEffect(() => {
    setError(''); setText(''); setLoading(false)
    if (!node) { setExpanded(false); return }
    if (!textPreview || node.size === 0) return
    const controller = new AbortController()
    setLoading(true)
    fetch(contentUrl(node.id), { credentials: 'same-origin', headers: { Range: `bytes=0-${textPreviewLimit - 1}` }, signal: controller.signal })
      .then(async response => {
        if (response.status === 401) window.dispatchEvent(new Event('harbor:unauthorized'))
        return readPreviewText(response, node.size, controller.signal)
      })
      .then(setText).catch(error => { if (!controller.signal.aborted) setError(errorMessage(error)) }).finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [node?.id, textPreview])
  useEffect(() => {
    if (!node) return
    const handler = (event: KeyboardEvent) => {
      const target = event.target
      if (target instanceof HTMLElement && (target.matches('input,textarea,select,video,audio') || target.isContentEditable || target.closest('.pdf-viewer'))) return
      if (event.key === 'ArrowRight' && next) { event.preventDefault(); next() }
      if (event.key === 'ArrowLeft' && previous) { event.preventDefault(); previous() }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [node, next, previous])
  return <Modal open={!!node} onOpenChange={open => { if (!open) close() }} title={node?.name || 'File preview'} description={node ? `${formatBytes(node.size)} · ${node.mime_type || 'File'}` : undefined} className={`preview-modal${expanded ? ' preview-fullscreen' : ''}`}>
    {node && <><div className="preview-toolbar"><span className="preview-navigation">{index >= 0 && files ? `${index + 1} of ${files.length}` : 'File preview'}</span><div>{(next || previous) && <><IconButton label="Previous file" onClick={previous} disabled={!previous}><ChevronLeft size={17} /></IconButton><IconButton label="Next file" onClick={next} disabled={!next}><ChevronRight size={17} /></IconButton></>}<IconButton label={expanded ? 'Restore preview size' : 'Expand preview'} onClick={() => setExpanded(!expanded)}>{expanded ? <Minimize2 size={17} /> : <Maximize2 size={17} />}</IconButton></div></div>
      <div className="preview-stage">{loading ? <div className="preview-message" role="status"><Loader2 className="spin" size={26} /><p>Opening preview…</p></div> : error ? <div className="preview-message" role="alert"><CircleAlert size={30} /><p>{error}</p><Button onClick={() => onDownload(node)}>Download file</Button></div> : safeImage ? <ImagePreview key={node.id} node={node} onError={() => setError('Image preview could not load. Download the original or reopen to retry.')} /> : node.mime_type === 'application/pdf' ? <Suspense fallback={<div className="preview-message" role="status"><Loader2 className="spin" size={26} /><p>Opening PDF…</p></div>}><PdfPreview key={node.id} id={node.id} name={node.name} /></Suspense> : textPreview ? <div className="text-preview-wrap">{node.size > textPreviewLimit && <p className="preview-limit">First 256 KiB shown. Download for the full file.</p>}<pre className={`text-preview${codePreview ? ' text-code' : ''}`} tabIndex={0} aria-label={codePreview ? 'Code preview' : 'Text preview'}><code>{codePreview ? tokens.map((token, index) => token.kind ? <span key={index} className={`token-${token.kind}`}>{token.text}</span> : token.text) : text || (node.size === 0 ? 'This file is empty.' : '')}</code></pre></div> : kind === 'video' ? <video key={node.id} controls playsInline preload="metadata" src={contentUrl(node.id)} aria-label={node.name} onError={() => setError('This browser cannot play the video. Download to open it in your preferred player.')} /> : kind === 'audio' ? <div className="audio-preview"><FileIcon node={node} large /><strong>{node.name}</strong><audio key={node.id} controls preload="metadata" src={contentUrl(node.id)} aria-label={node.name} onError={() => setError('This browser cannot play the audio format. Download the original to listen.')} /></div> : <div className="preview-message"><FileIcon node={node} large /><h3>Preview unavailable</h3><p>Download this format to open it in your preferred app.</p><Button variant="primary" onClick={() => onDownload(node)}><ArrowDownToLine size={16} />Download file</Button></div>}</div>
      {safeImage && onNavigate && nearby.length > 1 && <nav className="preview-thumbnails" aria-label="Nearby files">{nearby.map(file => <button key={file.id} className={`preview-thumbnail${file.id === node.id ? ' active' : ''}`} aria-label={`Preview ${file.name}`} aria-current={file.id === node.id ? 'true' : undefined} title={file.name} onClick={() => onNavigate(file)}>{isSafeImage(file) && file.size <= 2 * 1024 * 1024 ? <img loading="lazy" src={contentUrl(file.id)} alt="" /> : <FileIcon node={file} />}</button>)}</nav>}
      <div className="preview-footer"><span><ShieldCheck size={14} />Private, authenticated preview</span><Button onClick={() => onDownload(node)}><ArrowDownToLine size={15} />Download</Button></div>
    </>}
  </Modal>
}
export function Inspector({ node, close, onAction, notify }: { node: DriveNode; close: () => void; onAction: (action: NodeAction, node: DriveNode) => void; notify: (message: string) => void }) {
  const fullDate = (date: string) => new Date(date).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' })
  return <aside className="inspector" aria-label="File details"><div className="inspector-heading"><h2>File details</h2><IconButton label="Close file details" onClick={close}><X size={17} /></IconButton></div><div className="inspector-visual"><FileIcon node={node} large /></div><h3>{node.name}</h3><span className="inspector-type">{node.kind === 'folder' ? 'Folder' : node.mime_type || 'File'}</span><div className="inspector-actions"><Button onClick={() => onAction('favorite', node)}><Star size={15} fill={node.favorite ? 'currentColor' : 'none'} />{node.favorite ? 'Favorited' : 'Favorite'}</Button><IconButton label="Move item" onClick={() => onAction('move', node)}><FolderInput size={17} /></IconButton></div><dl><div><dt>Size</dt><dd>{node.kind === 'folder' ? '—' : formatBytes(node.size)}</dd></div><div><dt>Created</dt><dd>{fullDate(node.created_at)}</dd></div><div><dt>Modified</dt><dd>{fullDate(node.updated_at)}</dd></div><div><dt>Access</dt><dd><ShieldCheck size={13} />Only you</dd></div>{node.kind === 'file' && <div><dt>Encryption</dt><dd>AES-256-GCM</dd></div>}</dl>{node.sha256 && <div className="checksum-box"><div><span>SHA-256 CHECKSUM</span><IconButton label="Copy checksum" onClick={() => { void navigator.clipboard.writeText(node.sha256).then(() => notify('Checksum copied')).catch(() => notify('Clipboard unavailable. Select the checksum to copy.')) }}><Copy size={13} /></IconButton></div><code>{node.sha256}</code></div>}<div className="inspector-bottom"><Info size={14} /><span>{node.kind === 'folder' ? 'Folders organize files without changing encrypted content.' : 'Every chunk is authenticated before your file is delivered.'}</span></div></aside>
}
