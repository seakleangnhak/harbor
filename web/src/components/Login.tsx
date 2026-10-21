import { useState, type FormEvent } from 'react'
import { ArrowRight, Check, Eye, EyeOff, HardDrive, LockKeyhole, ShieldCheck } from 'lucide-react'
import { api } from '../api'
import { errorMessage } from '../utils'
import { Button, IconButton, InlineError } from './ui'

export function Brand({ compact = false }: { compact?: boolean }) {
  return <div className="brand"><span className="brand-mark" aria-hidden="true"><svg width="27" height="27" viewBox="0 0 32 32" fill="none"><path d="M8 5v22M24 5v22M8 16h16" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" /><path d="M13 7v5m6 8v5" stroke="currentColor" opacity=".6" strokeWidth="2" strokeLinecap="round" /></svg></span>{!compact && <span>Harbor<span className="brand-dot">.</span></span>}</div>
}

export function Login({ onLogin, notice }: { onLogin: () => void; notice?: string }) {
  const [password, setPassword] = useState('')
  const [visible, setVisible] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true); setError('')
    try { await api.login(password); setPassword(''); onLogin() } catch (error) { setError(errorMessage(error)) } finally { setBusy(false) }
  }
  return <main className="login-page"><div className="login-shell">
    <div className="login-brand"><Brand /><span className="eyebrow">YOUR PERSONAL CLOUD</span></div>
    <section className="login-story" aria-label="About Harbor"><div className="login-illustration" aria-hidden="true"><div className="illustration-orbit orbit-one" /><div className="illustration-orbit orbit-two" /><div className="illustration-folder"><span /><FolderShape /><ShieldCheck size={28} /></div><div className="floating-note note-one"><HardDrive size={18} /><span>A home for your files</span></div><div className="floating-note note-two"><Check size={16} /><span>Encrypted by default</span></div></div>
      <span className="eyebrow">KEEP WHAT MATTERS</span><h2>Your files.<br /><em>Your own space.</em></h2><p>Organize, preview, and keep your files close. Protected with encryption and private owner access.</p>
    </section>
    <section className="login-form-wrap"><div className="login-form-heading"><span className="login-lock"><LockKeyhole size={21} /></span><h1>Welcome to Harbor</h1><p>Unlock your drive to pick up where you left off.</p></div>
      {notice && <p className="login-notice" role="status"><ShieldCheck size={17} /><span>{notice}</span></p>}
      <form onSubmit={submit} aria-busy={busy}><label htmlFor="owner-password">Owner password</label><div className="password-input"><input id="owner-password" type={visible ? 'text' : 'password'} value={password} onChange={event => setPassword(event.target.value)} autoComplete="current-password" autoFocus required placeholder="Enter your password" disabled={busy} aria-invalid={!!error} aria-describedby={error ? 'login-error' : undefined} /><IconButton label={visible ? 'Hide password' : 'Show password'} onClick={() => setVisible(!visible)} disabled={busy}>{visible ? <EyeOff size={17} /> : <Eye size={17} />}</IconButton></div>
        {error && <InlineError id="login-error">{error}</InlineError>}<Button type="submit" variant="primary" className="login-submit" busy={busy} disabled={!password}>{busy ? 'Unlocking…' : 'Unlock drive'}{!busy && <ArrowRight size={17} />}</Button>
      </form><div className="login-security"><ShieldCheck size={15} /><span>Encrypted storage. Only you have access.</span></div><p className="login-help">Use your owner password to access this private drive.</p>
    </section>
    <footer className="login-footer"><span>Harbor · Private by design</span><span>Your personal cloud, on your terms.</span></footer>
  </div></main>
}

function FolderShape() { return <svg viewBox="0 0 120 90" width="120" height="90" fill="none"><path d="M5 22a10 10 0 0 1 10-10h29l12 12h49a10 10 0 0 1 10 10v42a10 10 0 0 1-10 10H15A10 10 0 0 1 5 76Z" fill="currentColor" /><path d="M5 37h110" stroke="white" strokeOpacity=".3" strokeWidth="2" /></svg> }
