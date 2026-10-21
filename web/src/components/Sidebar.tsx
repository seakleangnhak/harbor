import { ArrowUpRight, ChevronDown, CircleHelp, Clock3, HardDrive, LockKeyhole, Menu, Moon, PanelLeftClose, PanelLeftOpen, Settings2, ShieldCheck, Star, Sun, Trash2, Upload } from 'lucide-react'
import { useEffect, useRef } from 'react'
import type { Diagnostics, View } from '../types'
import { formatBytes, storageLabel } from '../utils'
import { Brand } from './Login'
import { Button, IconButton } from './ui'

export function Sidebar({ view, navigate, upload, settings, help, diagnostics, connectionError, dark, toggleTheme, mobileOpen, closeMobile, collapsed = false, toggleCollapsed }: { view: View; navigate: (view: View) => void; upload: () => void; settings: () => void; help: () => void; diagnostics?: Diagnostics; connectionError?: boolean; dark: boolean; toggleTheme: () => void; mobileOpen: boolean; closeMobile: () => void; collapsed?: boolean; toggleCollapsed?: () => void }) {
  const sidebar = useRef<HTMLElement>(null)
  useEffect(() => {
    if (!mobileOpen) return
    const previous = document.activeElement as HTMLElement | null
    const buttons = () => Array.from(sidebar.current?.querySelectorAll<HTMLButtonElement>('button:not(:disabled)') || []).filter(button => button.getClientRects().length > 0 && getComputedStyle(button).visibility !== 'hidden')
    buttons()[0]?.focus()
    const keyboard = (event: KeyboardEvent) => {
      if (event.key === 'Escape') { event.preventDefault(); closeMobile() }
      if (event.key === 'Tab') {
        const elements = buttons(), first = elements[0], last = elements.at(-1)
        if (!sidebar.current?.contains(document.activeElement)) { event.preventDefault(); (event.shiftKey ? last : first)?.focus() }
        else if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus() }
        else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus() }
      }
    }
    document.addEventListener('keydown', keyboard)
    return () => { document.removeEventListener('keydown', keyboard); if (!document.querySelector('[role="dialog"]')) previous?.focus() }
  }, [mobileOpen])
  const openSettings = () => { closeMobile(); settings() }
  const openHelp = () => { closeMobile(); help() }
  const items = [{ view: 'all' as const, icon: HardDrive, label: 'My drive' }, { view: 'recent' as const, icon: Clock3, label: 'Recent' }, { view: 'favorites' as const, icon: Star, label: 'Favorites' }, { view: 'trash' as const, icon: Trash2, label: 'Trash' }]
  return <><div className={`sidebar-scrim ${mobileOpen ? 'visible' : ''}`} onClick={closeMobile} aria-hidden="true" /><aside ref={sidebar} className={`sidebar ${mobileOpen ? 'sidebar-open' : ''} ${collapsed ? 'sidebar-collapsed' : ''}`} aria-label="Main navigation" role={mobileOpen ? 'dialog' : undefined} aria-modal={mobileOpen || undefined}>
    <div className="sidebar-brand"><Brand />{toggleCollapsed && <IconButton className="sidebar-collapse-toggle" label={collapsed ? 'Expand navigation' : 'Collapse navigation'} onClick={toggleCollapsed}>{collapsed ? <PanelLeftOpen size={16} /> : <PanelLeftClose size={16} />}</IconButton>}<IconButton className="mobile-only" label="Close navigation" onClick={closeMobile}><PanelLeftClose size={18} /></IconButton></div>
    <button className="workspace-switch" onClick={openSettings} aria-label="Personal workspace settings"><span className="workspace-avatar">P</span><span><strong>Personal workspace</strong><small>Only you have access</small></span><ChevronDown size={14} /></button>
    <Button variant="primary" className="sidebar-upload" aria-label="Upload files" title={collapsed ? 'Upload files' : undefined} onClick={() => { upload(); closeMobile() }}><Upload size={17} /><span>Upload files</span><span className="shortcut-key">⌘ U</span></Button>
    <div className="nav-label">WORKSPACE</div><nav>{items.map(item => <button key={item.view} className={`nav-item ${view === item.view ? 'active' : ''}`} onClick={() => { navigate(item.view); closeMobile() }} aria-label={item.label} title={collapsed ? item.label : undefined} aria-current={view === item.view ? 'page' : undefined}><item.icon size={18} strokeWidth={1.8} /><span>{item.label}</span>{item.view === 'all' && diagnostics !== undefined && <span className="nav-count">{diagnostics.files + diagnostics.folders}</span>}</button>)}</nav>
    <div className="sidebar-bottom"><div className="storage-card"><div className="storage-card-label"><ShieldCheck size={17} /><span>Private storage</span><span className="status-dot" data-state={connectionError ? 'unknown' : diagnostics?.storage_connection?.state || 'unknown'} /></div><strong>{diagnostics ? formatBytes(diagnostics.used_bytes) : '—'}<small> securely stored</small></strong><div className="storage-divider" /><p role="status">{connectionError ? 'Connection status unavailable' : storageLabel(diagnostics)}</p><button onClick={openSettings}>Storage details<ArrowUpRight size={14} /></button></div>
      <button className="nav-item" onClick={openSettings} aria-label="Settings" title={collapsed ? 'Settings' : undefined}><Settings2 size={18} /><span>Settings</span></button><div className="sidebar-footer"><button onClick={openHelp} aria-label="Help and keyboard shortcuts"><CircleHelp size={15} /><span>Help & shortcuts</span></button><IconButton label={dark ? 'Switch to light mode' : 'Switch to dark mode'} onClick={toggleTheme}>{dark ? <Sun size={16} /> : <Moon size={16} />}</IconButton></div>
      <div className="owner-card"><span className="owner-avatar">YO</span><span><strong>Your space</strong><small>Private owner</small></span><LockKeyhole size={14} /></div>
    </div>
  </aside></>
}

export function MobileMenuButton({ onClick }: { onClick: () => void }) { return <IconButton className="mobile-only" label="Open navigation" onClick={onClick}><Menu size={20} /></IconButton> }
