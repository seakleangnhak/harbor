import { useEffect, useRef, useState } from 'react'
import { contentUrl } from '../api'
import { previewMime } from '../media'
import type { DriveNode } from '../types'
import { cn } from './ui'
import { FileIcon } from './FileIcon'

const queue: (() => void)[] = []
let active = 0
function thumbnailSlot(start: () => void) {
  let running = false, finished = false
  const begin = () => { running = true; active++; start() }
  const release = () => {
    if (finished) return
    finished = true
    if (running) { active--; queue.shift()?.() }
    else { const index = queue.indexOf(begin); if (index >= 0) queue.splice(index, 1) }
  }
  if (active < 2) begin()
  else queue.push(begin)
  return release
}

export function Thumbnail({ node }: { node: DriveNode }) {
  const [state, setState] = useState<'loading' | 'loaded' | 'failed'>('loading')
  const image = useRef<HTMLImageElement>(null)
  useEffect(() => {
    const element = image.current
    if (!element) return
    let cancelled = false, started = false, release: (() => void) | undefined
    const observer = new IntersectionObserver(([entry]) => {
      if (cancelled || started) return
      if (!entry.isIntersecting) { release?.(); release = undefined; return }
      if (!release) release = thumbnailSlot(() => { started = true; observer.disconnect(); element.src = contentUrl(node.id) })
    }, { root: element.closest('.file-scroll'), rootMargin: '100px' })
    const finish = (loaded: boolean) => { if (!cancelled) setState(loaded ? 'loaded' : 'failed'); release?.() }
    const loaded = () => finish(true), failed = () => finish(false)
    element.addEventListener('load', loaded); element.addEventListener('error', failed)
    observer.observe(element)
    return () => {
      cancelled = true; observer.disconnect()
      element.removeEventListener('load', loaded); element.removeEventListener('error', failed)
      element.removeAttribute('src'); release?.()
    }
  }, [node.id])
  const unavailable = /^image\/hei[cf]$/.test(previewMime(node)) ? 'Open to preview' : 'Preview unavailable'
  return <>{state !== 'loaded' && <span className={cn('thumbnail-fallback', state === 'loading' && 'thumbnail-loading')}><FileIcon node={node} large /><span className="thumbnail-label">{state === 'failed' ? unavailable : 'Loading preview…'}</span></span>}{state !== 'failed' && <img ref={image} className={cn('thumbnail-image', state === 'loaded' && 'thumbnail-image-loaded')} decoding="async" draggable={false} alt="" />}</>
}
