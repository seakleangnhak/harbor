import type { DriveNode } from './types.ts'

export const nodeDragType = 'application/x-harbor-nodes'

export function topLevelSelection(nodes: DriveNode[], ancestors: ReadonlyMap<string, readonly string[]>): DriveNode[] {
  const folders = new Set(nodes.filter(node => node.kind === 'folder').map(node => node.id)), seen = new Set<string>()
  return nodes.filter(node => {
    if (seen.has(node.id) || folders.has(node.parent_id) || ancestors.get(node.parent_id)?.some(id => folders.has(id))) return false
    seen.add(node.id)
    return true
  })
}

export function draggedNodeIds(value: string): string[] {
  try {
    const ids: unknown = JSON.parse(value)
    if (!Array.isArray(ids) || !ids.length || ids.length > 10000 || ids.some(id => typeof id !== 'string' || !id || id.length > 128 || /[\x00-\x1f\x7f]/.test(id))) return []
    return [...new Set(ids)]
  } catch { return [] }
}

export function nextFileIndex(index: number, key: string, columns: number, count: number): number | undefined {
  const next = key === 'Home' ? 0 : key === 'End' ? count - 1 : key === 'ArrowDown' ? index + columns : key === 'ArrowUp' ? index - columns : key === 'ArrowRight' ? index + 1 : key === 'ArrowLeft' ? index - 1 : index
  const bounded = Math.max(0, Math.min(count - 1, next))
  return !count || bounded === index ? undefined : bounded
}

export function canShowThumbnail(node: Pick<DriveNode, 'mime_type' | 'size' | 'trashed_at'>): boolean {
  return !node.trashed_at && node.size > 0 && node.size <= 8 * 1024 * 1024 && /^image\/(png|jpeg|gif|webp|avif|bmp)$/.test(node.mime_type)
}
