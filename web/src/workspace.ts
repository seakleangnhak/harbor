import type { Listing, Sort } from './types.ts'

export const preferenceKey = (listing: Pick<Listing, 'view' | 'parent_id'>) => `harbor:location:${listing.view}:${listing.parent_id}`
export function viewPreference(value: unknown, grid = false) {
  const saved = value && typeof value === 'object' ? value as Record<string, unknown> : {}
  return { grid: typeof saved.grid === 'boolean' ? saved.grid : grid, sort: (['name', 'updated_at', 'size'].includes(String(saved.sort)) ? saved.sort : 'name') as Sort, direction: (saved.direction === 'desc' ? 'desc' : 'asc') as 'asc' | 'desc' }
}

export function copyName(name: string, occupied: Set<string>, folder = false) {
  const dot = !folder && name.lastIndexOf('.') > 0 ? name.lastIndexOf('.') : name.length
  let extension = name.slice(dot)
  let candidate = name, index = 1
  while (occupied.has(candidate.toLocaleLowerCase())) {
    const suffix = index++ === 1 ? ' copy' : ` copy ${index - 1}`
    while (new TextEncoder().encode(suffix + extension).length > 254) extension = Array.from(extension).slice(0, -1).join('')
    let base = name.slice(0, dot)
    while (new TextEncoder().encode(base + suffix + extension).length > 255 && base.length) base = Array.from(base).slice(0, -1).join('')
    candidate = base + suffix + extension
  }
  occupied.add(candidate.toLocaleLowerCase())
  return candidate
}
