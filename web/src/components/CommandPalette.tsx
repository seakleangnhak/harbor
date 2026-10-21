import { useEffect, useRef, useState } from 'react'
import { ArrowUpRight, Clock3, FolderPlus, HardDrive, Keyboard, Moon, Search, Settings2, Star, Sun, Trash2, Upload } from 'lucide-react'
import type { DriveNode, View } from '../types'
import { FileIcon } from './FileIcon'
import { Modal } from './ui'

export default function CommandPalette({ close, items, navigate, open, upload, createFolder, settings, help, dark, toggleTheme, search }: { close: () => void; items: DriveNode[]; navigate: (view: View) => void; open: (node: DriveNode) => void; upload: () => void; createFolder: () => void; settings: () => void; help: () => void; dark: boolean; toggleTheme: () => void; search: (query: string) => void }) {
  const [query, setQuery] = useState('')
  const [active, setActive] = useState(0)
  const list = useRef<HTMLDivElement>(null)
  const actions = [
    { label: 'Upload files', icon: Upload, key: '⌘ U', run: upload },
    { label: 'New folder', icon: FolderPlus, key: '', run: createFolder },
    { label: 'My drive', icon: HardDrive, key: '', run: () => navigate('all') },
    { label: 'Recent files', icon: Clock3, key: '', run: () => navigate('recent') },
    { label: 'Favorites', icon: Star, key: '', run: () => navigate('favorites') },
    { label: 'Trash', icon: Trash2, key: '', run: () => navigate('trash') },
    { label: dark ? 'Use light theme' : 'Use dark theme', icon: dark ? Sun : Moon, key: '', run: toggleTheme },
    { label: 'Settings', icon: Settings2, key: '', run: settings },
    { label: 'Keyboard shortcuts', icon: Keyboard, key: '', run: help },
  ].filter(item => item.label.toLowerCase().includes(query.toLowerCase()))
  const files = query ? items.filter(item => item.name.toLowerCase().includes(query.toLowerCase())).slice(0, 8) : []
  const options = [...actions.map(item => ({ ...item, node: undefined as DriveNode | undefined })), ...files.map(node => ({ label: node.name, node, key: '', icon: HardDrive, run: () => open(node) })), ...(query ? [{ label: `Search entire drive for “${query}”`, icon: Search, key: '↵', node: undefined, run: () => search(query) }] : [])]
  useEffect(() => { setActive(0) }, [query])
  useEffect(() => { list.current?.querySelector('[data-active="true"]')?.scrollIntoView({ block: 'nearest' }) }, [active])
  const choose = (index: number) => { const item = options[index]; if (item) { close(); item.run() } }
  return <Modal open onOpenChange={value => { if (!value) close() }} title="Quick actions" description="Jump to a file, a view, or your next action." className="command-modal">
    <div className="command-search"><Search size={18} /><input autoFocus aria-label="Search actions or files" role="combobox" aria-expanded="true" aria-controls="harbor-commands" aria-autocomplete="list" aria-activedescendant={options.length ? `command-${active}` : undefined} placeholder="What would you like to do?" value={query} onChange={event => setQuery(event.target.value)} onKeyDown={event => { if (event.key === 'ArrowDown' || event.key === 'ArrowUp') { event.preventDefault(); setActive(value => (value + (event.key === 'ArrowDown' ? 1 : -1) + options.length) % Math.max(1, options.length)) } if (event.key === 'Enter') { event.preventDefault(); choose(active) } }} /></div>
    <div ref={list} className="command-list" id="harbor-commands" role="listbox" aria-label="Actions and matching files">{options.map((item, index) => <div id={`command-${index}`} key={`${item.node?.id || item.label}`} role="option" aria-selected={active === index} data-active={active === index} className="command-item" onPointerMove={() => setActive(index)} onMouseDown={event => event.preventDefault()} onClick={() => choose(index)}><span className="command-icon">{item.node ? <FileIcon node={item.node} /> : <item.icon size={17} />}</span><span className="command-copy">{item.label}{item.node && <small>{item.node.kind === 'folder' ? 'Open folder' : 'Preview file'}</small>}</span>{item.key ? <kbd className="command-key">{item.key}</kbd> : <ArrowUpRight size={13} />}</div>)}</div>
    <div className="command-footer"><span>↑ ↓ to navigate</span><span>↵ to open</span><span>esc to close</span></div>
  </Modal>
}
