import { useEffect, useRef, useState } from 'react'
import { getDocument, GlobalWorkerOptions, type PDFDocumentProxy, type RenderTask } from 'pdfjs-dist/legacy/build/pdf.mjs'
import type { TextContent } from 'pdfjs-dist/types/src/display/api'
import workerUrl from 'pdfjs-dist/legacy/build/pdf.worker.min.mjs?url'
import { ChevronLeft, ChevronRight, CircleAlert, Loader2, Minus, Plus } from 'lucide-react'
import { contentUrl } from '../api'
import { errorMessage } from '../utils'
import { IconButton } from './ui'
import { textPreviewLimit } from './preview-utils'

GlobalWorkerOptions.workerSrc = workerUrl
const fontAssets = import.meta.glob<string>('../../node_modules/pdfjs-dist/standard_fonts/*.{pfb,ttf}', { eager: true, query: '?url&no-inline', import: 'default' })
const cmapAssets = import.meta.glob<string>('../../node_modules/pdfjs-dist/cmaps/*.bcmap', { eager: true, query: '?url&no-inline', import: 'default' })

class BundledPdfData {
  async fetch({ kind, filename }: { kind: string; filename: string }) {
    const assets = kind === 'standardFontDataUrl' ? fontAssets : kind === 'cMapUrl' ? cmapAssets : {}
    const url = Object.entries(assets).find(([path]) => path.endsWith(`/${filename}`))?.[1]
    if (!url) throw new Error('PDF resource unavailable')
    const response = await fetch(url)
    if (!response.ok) throw new Error('PDF resource could not load')
    return new Uint8Array(await response.arrayBuffer())
  }
}

export default function PdfPreview({ id, name }: { id: string; name: string }) {
  const [document, setDocument] = useState<PDFDocumentProxy | null>(null)
  const [pageNumber, setPageNumber] = useState(1)
  const [pageDraft, setPageDraft] = useState('1')
  const [zoom, setZoom] = useState(1)
  const [width, setWidth] = useState(600)
  const [height, setHeight] = useState(600)
  const [fit, setFit] = useState<'width' | 'page'>('width')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [pageText, setPageText] = useState('')
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const containerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const task = getDocument({ url: contentUrl(id), withCredentials: true, disableAutoFetch: true, disableStream: true, rangeChunkSize: 256 * 1024, disableFontFace: true, enableXfa: false, useWasm: false, useWorkerFetch: false, BinaryDataFactory: BundledPdfData, maxImageSize: 16_777_216, canvasMaxAreaInBytes: 16_777_216 })
    let cancelled = false
    task.promise.then(pdf => { if (!cancelled) setDocument(pdf) }).catch(error => { if (!cancelled) { setError(errorMessage(error)); setLoading(false) } })
    return () => { cancelled = true; void task.destroy() }
  }, [id])

  useEffect(() => {
    const element = containerRef.current
    if (!element) return
    const observer = new ResizeObserver(([entry]) => { setWidth(entry.contentRect.width); setHeight(entry.contentRect.height) })
    observer.observe(element)
    return () => observer.disconnect()
  }, [])

  useEffect(() => {
    if (!document) return
    let cancelled = false
    let render: RenderTask | undefined
    let textReader: ReadableStreamDefaultReader<TextContent> | undefined
    setLoading(true); setError(''); setPageText('')
    document.getPage(pageNumber).then(async page => {
      if (cancelled || !canvasRef.current) return
      const normal = page.getViewport({ scale: 1 })
      const fitted = fit === 'page' ? Math.min((width - 40) / normal.width, (height - 40) / normal.height) : (width - 40) / normal.width
      const scale = Math.min(Math.max(0.1, fitted) * zoom, 4096 / Math.max(normal.width, normal.height), Math.sqrt(4_194_304 / (normal.width * normal.height)))
      const viewport = page.getViewport({ scale })
      const ratio = Math.min(window.devicePixelRatio || 1, 2, Math.sqrt(8_388_608 / (viewport.width * viewport.height)))
      const canvas = canvasRef.current
      canvas.width = Math.floor(viewport.width * ratio); canvas.height = Math.floor(viewport.height * ratio)
      canvas.style.width = `${viewport.width}px`; canvas.style.height = `${viewport.height}px`
      render = page.render({ canvas, viewport, transform: [ratio, 0, 0, ratio, 0, 0] })
      await render.promise
      if (cancelled) return
      setLoading(false)
      // ponytail: cap accessible page text at 256 Ki characters; original PDF retains full text.
      const reader: ReadableStreamDefaultReader<TextContent> = page.streamTextContent().getReader()
      textReader = reader
      try {
        let text = ''
        while (!cancelled && text.length < textPreviewLimit) {
          const { done, value } = await reader.read()
          if (done || cancelled) break
          for (const item of value.items) {
            if ('str' in item) {
              if (text) text += ' '
              text += item.str.slice(0, textPreviewLimit - text.length)
            }
            if (text.length >= textPreviewLimit) break
          }
        }
        if (!cancelled) setPageText(text)
      } catch {
        if (!cancelled) setPageText('Page text unavailable. Download the original PDF to read its text.')
      } finally {
        await reader.cancel(new Error('PDF text extraction cancelled')).catch(() => {})
        reader.releaseLock()
        textReader = undefined
      }
    }).catch(error => { if (!cancelled) { setError(errorMessage(error)); setLoading(false) } })
    return () => { cancelled = true; render?.cancel(); void textReader?.cancel(new Error('PDF preview changed')).catch(() => {}) }
  }, [document, pageNumber, width, height, zoom, fit])

  useEffect(() => setPageDraft(String(pageNumber)), [pageNumber])
  const goToDraft = () => {
    const value = Number(pageDraft)
    if (document && Number.isInteger(value) && value >= 1 && value <= document.numPages) setPageNumber(value)
    else setPageDraft(String(pageNumber))
  }

  return <div className="pdf-viewer"><div className="pdf-viewer-toolbar"><div><IconButton label="Previous PDF page" disabled={!document || pageNumber === 1} onClick={() => setPageNumber(value => value - 1)}><ChevronLeft size={16} /></IconButton><label className="pdf-page-label">Page <input className="pdf-page-input" aria-label="PDF page" inputMode="numeric" value={pageDraft} disabled={!document} onChange={event => setPageDraft(event.target.value)} onBlur={goToDraft} onKeyDown={event => { if (event.key === 'Enter') { event.preventDefault(); goToDraft(); event.currentTarget.blur() } }} /> of {document?.numPages || '…'}</label><IconButton label="Next PDF page" disabled={!document || pageNumber >= document.numPages} onClick={() => setPageNumber(value => value + 1)}><ChevronRight size={16} /></IconButton></div><div><button className="pdf-fit-button" aria-label={fit === 'width' ? 'Fit whole PDF page' : 'Fit PDF to width'} onClick={() => { setFit(value => value === 'width' ? 'page' : 'width'); setZoom(1) }}>{fit === 'width' ? 'Fit page' : 'Fit width'}</button><IconButton label="Zoom out PDF" disabled={zoom <= .5} onClick={() => setZoom(value => Math.max(.5, value - .25))}><Minus size={15} /></IconButton><button className="pdf-zoom-value" aria-label="Reset PDF zoom" onClick={() => setZoom(1)}>{Math.round(zoom * 100)}%</button><IconButton label="Zoom in PDF" disabled={zoom >= 2} onClick={() => setZoom(value => Math.min(2, value + .25))}><Plus size={15} /></IconButton></div></div><div className="pdf-canvas-scroll" ref={containerRef} tabIndex={0} role="region" aria-label="PDF document viewport" aria-busy={loading}>{loading && <div className="pdf-loading" role="status"><Loader2 size={23} className="spin" /><span>Rendering page…</span></div>}{error ? <div className="preview-message" role="alert"><CircleAlert size={25} /><p>PDF preview unavailable: {error}</p><a className="button button-secondary" href={contentUrl(id, true)}>Download original PDF</a></div> : <canvas ref={canvasRef} role="img" aria-label={`${name}, page ${pageNumber}`} />}<p className="sr-only">{pageText}</p></div></div>
}
