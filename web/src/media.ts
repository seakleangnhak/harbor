import type { DriveNode } from './types.ts'

const mediaExtensions: Record<string, string> = {
  jpg: 'image/jpeg', jpeg: 'image/jpeg', jpe: 'image/jpeg', png: 'image/png', gif: 'image/gif',
  webp: 'image/webp', avif: 'image/avif', bmp: 'image/bmp', ico: 'image/x-icon',
  heic: 'image/heic', heif: 'image/heif', pdf: 'application/pdf',
  mp4: 'video/mp4', m4v: 'video/mp4', mov: 'video/quicktime', webm: 'video/webm', ogv: 'video/ogg',
  mp3: 'audio/mpeg', m4a: 'audio/mp4', ogg: 'audio/ogg', wav: 'audio/wav', flac: 'audio/flac',
}
const mediaTypes = new Set([...Object.values(mediaExtensions), 'audio/x-wav', 'audio/webm'])

export function previewMime(node: Pick<DriveNode, 'kind' | 'mime_type' | 'name'>): string {
  if (node.kind !== 'file') return ''
  const type = node.mime_type.split(';', 1)[0].trim().toLowerCase()
  if (mediaTypes.has(type)) return type
  if (type && type !== 'application/octet-stream') return ''
  const extension = node.name.match(/\.([^.]+)$/)?.[1].toLowerCase() || ''
  return Object.hasOwn(mediaExtensions, extension) ? mediaExtensions[extension] : ''
}
