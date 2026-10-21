import { Archive, File, FileAudio, FileCode2, FileImage, FileSpreadsheet, FileText, FileVideo2, Folder } from 'lucide-react'
import type { DriveNode } from '../types'
import { fileKind } from '../utils'
import { cn } from './ui'

export function FileIcon({ node, large = false }: { node: Pick<DriveNode, 'kind' | 'mime_type' | 'name'>; large?: boolean }) {
  const kind = fileKind(node)
  const Icon = kind === 'folder' ? Folder : kind === 'image' ? FileImage : kind === 'video' ? FileVideo2 : kind === 'audio' ? FileAudio : kind === 'archive' ? Archive : /\.(csv|xlsx?)$/i.test(node.name) ? FileSpreadsheet : /\.(json|js|ts|tsx|py|go|html|css|sh)$/i.test(node.name) ? FileCode2 : kind === 'document' ? FileText : File
  return <span className={cn('file-icon', `file-icon-${kind}`, large && 'file-icon-large')} aria-hidden="true"><Icon size={large ? 36 : 20} strokeWidth={kind === 'folder' ? 1.5 : 1.6} fill={kind === 'folder' ? 'currentColor' : 'none'} /></span>
}
