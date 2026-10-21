import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ArrowDownToLine, Check, ChevronDown, CircleAlert, Database, HardDrive, Info, LockKeyhole, LogOut, RefreshCw, Server, ShieldCheck, Terminal, UserRound } from 'lucide-react'
import { api } from '../api'
import { formatBytes, storageLabel } from '../utils'
import { Button, InlineError, Modal } from './ui'
import { OwnerAccount } from './OwnerAccount'

export function Settings({ open, close, logout, owner, passwordChanged }: { open: boolean; close: () => void; logout: () => Promise<void>; owner: string; passwordChanged: () => void }) {
  const [section, setSection] = useState<'account' | 'storage' | 'recovery'>('account')
  const [accountBusy, setAccountBusy] = useState(false)
  const opener = useRef(document.activeElement instanceof HTMLElement ? document.activeElement : null)
  useEffect(() => () => {
    queueMicrotask(() => {
      if (document.querySelector('[role="dialog"]')) return
      const candidates = [opener.current, document.querySelector<HTMLElement>('[aria-label="Your profile and password"]'), document.querySelector<HTMLElement>('[aria-label="Open navigation"]')]
      candidates.find(element => element?.isConnected && element.getClientRects().length > 0 && getComputedStyle(element).visibility !== 'hidden')?.focus()
    })
  }, [])
  const diagnostics = useQuery({ queryKey: ['diagnostics'], queryFn: api.diagnostics, enabled: open, refetchInterval: open && section === 'storage' ? 5000 : false, staleTime: 4000 })
  const data = diagnostics.data
  return <Modal open={open} onOpenChange={value => { if (!value) close() }} title="Your private workspace" description="Your account, storage, and recovery essentials." className="settings-modal">
    <div className="settings-layout">
      <nav className="settings-nav" aria-label="Settings sections">{[{ id: 'account', label: 'Account', icon: UserRound }, { id: 'storage', label: 'Storage', icon: HardDrive }, { id: 'recovery', label: 'Recovery', icon: ArrowDownToLine }].map(item => <button key={item.id} id={`settings-nav-${item.id}`} type="button" aria-current={section === item.id ? 'page' : undefined} disabled={accountBusy} onClick={() => setSection(item.id as typeof section)}><item.icon size={17} aria-hidden="true" /><span>{item.label}</span></button>)}</nav>
      <div key={section} className="settings-panel" role="region" aria-labelledby={`settings-nav-${section}`}>
        {section === 'account' && <OwnerAccount owner={owner} passwordChanged={passwordChanged} onBusyChange={setAccountBusy} />}
        {section === 'storage' && <>
          <section className="settings-section">
            <div className="settings-panel-heading"><h3>Storage connection</h3>{!diagnostics.error && <Button type="button" variant="ghost" onClick={() => { void diagnostics.refetch() }} busy={diagnostics.isFetching}><RefreshCw size={14} />Refresh</Button>}</div>
            <p className="settings-panel-description">Where your encrypted files live.</p>
            {diagnostics.error ? <div><InlineError>Storage status is unavailable. Check your connection to Harbor and try again.</InlineError><Button type="button" onClick={() => { void diagnostics.refetch() }} busy={diagnostics.isFetching}><RefreshCw size={14} />Retry connection</Button></div> : !data ? <div className="settings-loading" role="status" aria-busy="true"><div aria-hidden="true" /><div aria-hidden="true" /><p>Checking storage connection…</p></div> : <>
              <div className="adapter-status"><span className="adapter-icon"><Server size={23} aria-hidden="true" /></span><div><strong>{data.adapter === 'telegram' ? 'Telegram private storage' : 'Local encrypted storage'}</strong><p>{data.adapter === 'telegram' ? 'Encrypted documents in your private channel.' : 'Encrypted files on this server.'}</p></div><span className="connected-label" data-state={data.storage_connection?.state || 'unknown'}>{data.storage_connection?.state === 'connected' || data.storage_connection?.state === 'local' ? <Check size={12} aria-hidden="true" /> : <CircleAlert size={12} aria-hidden="true" />}{storageLabel(data)}</span></div>
              <div className="diagnostic-grid"><div><span>Files</span><strong>{data.files}</strong></div><div><span>Stored</span><strong>{formatBytes(data.used_bytes)}</strong></div><div><span>In Trash</span><strong>{formatBytes(data.trash_bytes)}</strong></div><div><span>Storage chunk</span><strong>{formatBytes(data.chunk_size)}</strong></div></div>
              <div className="security-lines"><p><ShieldCheck size={15} aria-hidden="true" /><span>{data.encryption} authenticated encryption</span></p><p><Database size={15} aria-hidden="true" /><span>Metadata database {data.database === 'ok' ? 'healthy' : data.database}</span></p><p><LockKeyhole size={15} aria-hidden="true" /><span>Private owner access</span></p>{data.adapter === 'telegram' && data.storage_connection && <p><RefreshCw size={15} aria-hidden="true" /><span>{data.storage_connection.retry_count} storage retries{data.storage_connection.last_checked_at ? ` · checked ${new Date(data.storage_connection.last_checked_at).toLocaleTimeString()}` : ''}</span></p>}</div>
            </>}
          </section>
          {data && <section className="settings-section"><div className="settings-section-heading"><ShieldCheck size={17} aria-hidden="true" /><h3>{data.adapter === 'telegram' ? 'Your Telegram connection' : 'Storage setup'}</h3></div><p>{data.adapter === 'telegram' ? 'Status refreshes every five seconds here. If disconnected, check server connectivity and your dedicated Telegram account session. Resume paused transfers after reconnecting.' : 'Your drive keeps the storage provider chosen during setup. To connect a different provider, create a separate installation with a fresh data volume.'}</p><div className="settings-note"><Info size={15} aria-hidden="true" /><p>{data.adapter === 'telegram' ? 'Saved credentials and Telegram sessions stay on your server. Use the documented server recovery flow if authorization expires.' : 'New installations can connect Telegram during protected web setup, using a dedicated account and private channel.'}</p></div></section>}
        </>}
        {section === 'recovery' && <section className="settings-section">
          <div className="settings-panel-heading"><h3>Backup & recovery</h3></div><p className="settings-panel-description">Keep a way back to your files.</p>
          <div className="settings-recovery-summary"><ShieldCheck size={22} aria-hidden="true" /><div><strong>Password-encrypted archives</strong><p>Protect your metadata, encryption key, recovery settings, and any configured Telegram session.</p></div></div>
          <details className="settings-disclosure"><summary><Terminal size={16} aria-hidden="true" /><span>Server backup instructions</span><ChevronDown size={15} aria-hidden="true" /></summary><div className="settings-disclosure-body"><p>Stop Harbor before running a complete backup from your server terminal. Replace these example paths with your protected data, archive, and password-file locations.</p><code className="terminal-command" tabIndex={0}>harbor-recovery backup --data-dir /private/drive --output /private/backup.harbor --password-file /private/backup-password</code><p>Follow the production operations guide for external secret files and Telegram configuration. Store the backup password separately. Restore into a fresh directory; restored web sessions are revoked.</p></div></details>
          <div className="settings-note"><Info size={15} aria-hidden="true" /><p>Keep independent copies of important files outside Telegram to protect against account or channel loss.</p></div>
        </section>}
      </div>
    </div>
    <div className="settings-footer"><span>Harbor {data?.version ? `v${data.version}` : ''}<span> · Private workspace</span></span><Button type="button" variant="ghost" disabled={accountBusy} onClick={() => { void logout() }}><LogOut size={15} />Lock drive</Button></div>
  </Modal>
}

export function Help({ open, close }: { open: boolean; close: () => void }) {
  return <Modal open={open} onOpenChange={value => { if (!value) close() }} title="A few quiet shortcuts" description="Less clicking. More getting things done." className="help-modal"><div className="shortcut-list">{[['Quick actions', '⌘ / Ctrl', 'K'], ['Search your drive', '', '/'], ['Copy selected items', '⌘ / Ctrl', 'C'], ['Cut selected items', '⌘ / Ctrl', 'X'], ['Paste into current folder', '⌘ / Ctrl', 'V'], ['Upload files', '⌘ / Ctrl', 'U'], ['Select all visible items', '⌘ / Ctrl', 'A'], ['Open selected item', '', 'Enter'], ['Preview selected file', '', 'Space'], ['Rename selected item', '', 'F2'], ['Move selection to trash', '', 'Delete'], ['Clear selection or close dialog', '', 'Esc'], ['Choose a range', 'Shift', 'Click'], ['Add to selection', '⌘ / Ctrl', 'Click']].map(([label, modifier, key]) => <div key={label}><span>{label}</span><span>{modifier && <kbd>{modifier}</kbd>}<kbd>{key}</kbd></span></div>)}</div><p className="help-note">Double-click a file to preview, or a folder to open. Right-click for more actions. Files are encrypted before storage and verified before download.</p></Modal>
}
