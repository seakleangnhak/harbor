import { useEffect, useRef, useState, type FormEvent } from 'react'
import { ArrowLeft, ArrowRight, Check, CheckCircle2, Eye, EyeOff, HardDrive, KeyRound, LoaderCircle, LockKeyhole, QrCode, ShieldCheck } from 'lucide-react'
import { SetupError, setupErrorMessage, setupRequest, telegramDetails, validateOwnerPassword, waitForHarborReady, type TelegramSetup } from '../onboarding'
import { Brand } from './Login'
import { Button, InlineError } from './ui'
import './onboarding.css'

const titles = ['Make it yours.', 'Choose a home for your files.', 'Your Harbor is ready.']
const notes = ['Create the owner account for this private drive.', 'Connect a dedicated Telegram account or keep storage on this server.', 'Review your setup, then open your private drive.']
const safeOrigin = () => location.protocol === 'https:' || ['localhost', '127.0.0.1', '[::1]'].includes(location.hostname)
const message = (error: unknown) => error instanceof SetupError ? error.message : 'Setup could not finish. Check your connection and retry.'

export function Onboarding({ onComplete }: { onComplete: () => void }) {
  const [step, setStep] = useState(0), [token, setToken] = useState(''), [unlocked, setUnlocked] = useState(false)
  const [password, setPassword] = useState(''), [confirmation, setConfirmation] = useState(''), [visible, setVisible] = useState(false)
  const [adapter, setAdapter] = useState<'telegram' | 'local'>('telegram')
  const [appID, setAppID] = useState(''), [appHash, setAppHash] = useState(''), [channelLink, setChannelLink] = useState(''), [channelID, setChannelID] = useState('')
  const [telegram, setTelegram] = useState<TelegramSetup>({ state: 'waiting' }), [started, setStarted] = useState(false)
  const [twoStep, setTwoStep] = useState(''), [busy, setBusy] = useState(''), [error, setError] = useState(''), [pollError, setPollError] = useState('')
  const [saved, setSaved] = useState(false)
  const title = useRef<HTMLHeadingElement>(null), active = useRef(true), lifecycle = useRef(new AbortController())
  useEffect(() => { active.current = true; lifecycle.current = new AbortController(); return () => { active.current = false; lifecycle.current.abort() } }, [])
  useEffect(() => { title.current?.focus() }, [step])

  useEffect(() => {
    if (!started || step !== 1 || adapter !== 'telegram' || telegram.state === 'verified' || telegram.state === 'failed' || pollError) return
    const controller = new AbortController()
    let timer: ReturnType<typeof setTimeout>
    const poll = async () => {
      try {
        const next = await setupRequest<TelegramSetup>('/telegram', token, 'GET', undefined, controller.signal)
        if (controller.signal.aborted) return
        setTelegram(next)
        if (next.state === 'verified') { setAppID(''); setAppHash(''); setChannelLink(''); setChannelID(''); setTwoStep(''); setError('') }
        if (next.state === 'failed') { setTwoStep(''); setError(setupErrorMessage(next.error_code)) }
      } catch (error) { if (!controller.signal.aborted) {
        if (error instanceof SetupError && error.status === 401) { setUnlocked(false); setStarted(false); setStep(0); setToken(''); setError(message(error)) }
        else setPollError(message(error))
        return
      } }
      if (!controller.signal.aborted) timer = setTimeout(poll, 1500)
    }
    void poll()
    return () => { controller.abort(); clearTimeout(timer) }
  }, [started, step, adapter, telegram.state, token, pollError])

  const act = async (name: string, action: () => Promise<void>) => {
    setBusy(name); setError('')
    try { await action() } catch (error) {
      if (active.current) {
        if (error instanceof SetupError && error.status === 401) { setUnlocked(false); setStarted(false); setStep(0); setToken(''); setPollError('') }
        setError(message(error))
      }
    } finally { if (active.current) setBusy('') }
  }
  const verify = (event: FormEvent) => {
    event.preventDefault()
    void act('verify', async () => {
      const status = await setupRequest<TelegramSetup>('/verify', token, 'POST', {})
      setUnlocked(true); setTelegram(status)
      if (status.account_ready) { setStep(1); setStarted(status.state !== 'waiting' && status.state !== 'failed') }
    })
  }
  const account = (event: FormEvent) => {
    event.preventDefault()
    const invalid = validateOwnerPassword(password, confirmation)
    if (invalid) { setError(invalid); return }
    void act('account', async () => { await setupRequest('/account', token, 'POST', { password }); setPassword(''); setConfirmation(''); setStep(1) })
  }
  const connect = (event: FormEvent) => {
    event.preventDefault()
    void act('telegram', async () => {
      const details = telegramDetails(appID, appHash, channelLink, channelID)
      await setupRequest('/telegram', token, 'POST', details)
      setTelegram({ state: 'scanning' }); setStarted(true); setPollError('')
    })
  }
  const cancel = () => void act('cancel', async () => { await setupRequest('/telegram', token, 'DELETE'); setStarted(false); setTelegram({ state: 'waiting' }); setTwoStep(''); setPollError('') })
  const finish = () => void act('complete', async () => {
    if (saved) { window.location.reload(); return }
    await setupRequest('/complete', token, 'POST', { adapter })
    setSaved(true)
    setToken(''); setAppHash(''); setChannelLink(''); setAppID(''); setChannelID(''); setTwoStep('')
    try { await waitForHarborReady(lifecycle.current.signal) } catch {
      if (active.current) setError('Setup saved. Harbor is still starting. Reload in a moment. Check server health if this continues.')
      return
    }
    onComplete()
  })
  const secure = safeOrigin(), pending = !!busy
  return <main className="setup-page"><div className="setup-shell">
    <header className="setup-header"><Brand /><span className="eyebrow">A SPACE OF YOUR OWN</span></header>
    <aside className="setup-guide" aria-label="Setup progress"><span className="eyebrow">WELCOME ABOARD</span><h2>Your files.<br /><em>Your own Harbor.</em></h2><p>A few small steps to give your files a private home.</p>
      <ol>{['Owner account', 'Storage connection', 'Ready to go'].map((name, index) => <li key={name} aria-current={step === index ? 'step' : undefined} className={step === index ? 'setup-current' : step > index ? 'setup-done' : ''}><span className="setup-step-number" aria-hidden="true">{step > index ? <Check size={15} /> : index + 1}</span><span>{name}</span></li>)}</ol>
      <div className="setup-assurance"><ShieldCheck size={19} /><p>Files are encrypted before storage. Keep your data volume and encrypted backups safe.</p></div>
    </aside>
    <section className="setup-card" aria-labelledby="setup-title" aria-busy={pending}>
      <div className="setup-card-heading"><span className="eyebrow">STEP {step + 1} OF 3</span><h1 id="setup-title" ref={title} tabIndex={-1}>{titles[step]}</h1><p>{notes[step]}</p></div>
      {!secure ? <InlineError>Use HTTPS before entering setup credentials. For local setup, open Harbor at localhost or 127.0.0.1.</InlineError> : <>
        {step === 0 && (!unlocked ? <form onSubmit={verify} className="setup-form"><label htmlFor="setup-token">Deployment setup token</label><SecretInput id="setup-token" value={token} onChange={setToken} disabled={pending} placeholder="Enter your setup token" autoComplete="off" describedBy="setup-token-help" /><p className="setup-help" id="setup-token-help">Use <code>DRIVE_SETUP_TOKEN</code> from your container environment. This protects first-run setup from other visitors.</p><Button type="submit" variant="primary" className="setup-primary" busy={busy === 'verify'} disabled={!token.trim() || pending}>Continue securely<ArrowRight size={16} /></Button></form> : <form onSubmit={account} className="setup-form"><div className="setup-status"><ShieldCheck size={16} />Setup token verified</div><label htmlFor="setup-password">Owner password</label><div className="password-input"><input id="setup-password" type={visible ? 'text' : 'password'} value={password} onChange={event => setPassword(event.target.value)} autoComplete="new-password" required disabled={pending} aria-describedby="setup-password-help" /><button type="button" className="icon-button" onClick={() => setVisible(!visible)} aria-label={visible ? 'Hide owner password' : 'Show owner password'}>{visible ? <EyeOff size={17} /> : <Eye size={17} />}</button></div><p className="setup-help" id="setup-password-help">At least 12 bytes. A unique passphrase works well. Save it in your password manager.</p><label htmlFor="setup-confirm">Confirm password</label><input id="setup-confirm" type={visible ? 'text' : 'password'} value={confirmation} onChange={event => setConfirmation(event.target.value)} autoComplete="new-password" required disabled={pending} /><Button type="submit" variant="primary" className="setup-primary" busy={busy === 'account'} disabled={pending || !password || !confirmation}>Create owner account<ArrowRight size={16} /></Button></form>)}
        {step === 1 && <>
          <fieldset className="setup-storage" disabled={pending || (started && telegram.state !== 'verified' && telegram.state !== 'failed')}><legend className="sr-only">Storage provider</legend><label className={adapter === 'telegram' ? 'setup-provider setup-provider-selected' : 'setup-provider'}><input type="radio" name="storage" value="telegram" checked={adapter === 'telegram'} onChange={() => { setAdapter('telegram'); setError('') }} /><QrCode size={21} /><span><strong>Telegram</strong><small>Encrypted files in your private channel</small></span></label><label className={adapter === 'local' ? 'setup-provider setup-provider-selected' : 'setup-provider'}><input type="radio" name="storage" value="local" checked={adapter === 'local'} onChange={() => { setAdapter('local'); setError('') }} /><HardDrive size={21} /><span><strong>Local storage</strong><small>Keep encrypted files on this server</small></span></label></fieldset>
          {adapter === 'local' ? <div className="setup-local"><p>Files and metadata stay in your persistent data volume. Storage provider cannot be changed after setup.</p><Button variant="primary" className="setup-primary" onClick={() => setStep(2)} disabled={pending}>Use local storage<ArrowRight size={16} /></Button></div> : telegram.state === 'verified' ? <div className="setup-verified"><CheckCircle2 size={34} /><h2>Private channel verified</h2><p>Telegram account and channel are ready. Harbor keeps the session and API credentials on your server.</p><Button variant="primary" className="setup-primary" onClick={() => setStep(2)} disabled={pending}>Continue<ArrowRight size={16} /></Button></div> : started && telegram.state !== 'failed' ? <div className="setup-authorization">
            {telegram.state === 'password' ? <form onSubmit={event => { event.preventDefault(); void act('password', async () => { await setupRequest('/telegram/password', token, 'POST', { password: twoStep }); setTwoStep('') }) }} className="setup-form"><span className="setup-state-icon"><KeyRound size={24} /></span><h2>Telegram two-step verification</h2><p className="setup-help">Enter your Telegram account’s two-step password. Harbor does not save this password.</p><label htmlFor="telegram-password">Telegram two-step password</label><SecretInput id="telegram-password" value={twoStep} onChange={setTwoStep} autoComplete="off" disabled={pending} /><Button type="submit" variant="primary" busy={busy === 'password'} disabled={!twoStep || pending}>Verify password<ArrowRight size={16} /></Button></form> : <><div className="setup-qr">{telegram.qr_image?.startsWith('data:image/png;base64,') ? <img src={telegram.qr_image} width="224" height="224" alt="Temporary Telegram sign-in QR code" /> : <div role="status"><LoaderCircle size={26} className="spin" /><span>Preparing secure sign-in…</span></div>}</div><h2>Link your dedicated account</h2><ol className="setup-qr-steps"><li>Open Telegram on your phone.</li><li>Go to <strong>Settings → Devices → Link Desktop Device.</strong></li><li>Scan this QR code, then approve the sign-in.</li></ol><p className="setup-warning"><LockKeyhole size={15} />Treat this QR as a password. Never share it or its screenshot.</p><p className="setup-help" role="status">Waiting for Telegram approval and private channel verification…</p></>}
            {pollError && <div className="setup-poll-error"><InlineError>{pollError}</InlineError><Button onClick={() => setPollError('')} disabled={pending}>Retry connection</Button></div>}<Button variant="ghost" onClick={cancel} disabled={pending}>Cancel Telegram sign-in</Button>
          </div> : <TelegramForm appID={appID} appHash={appHash} channelLink={channelLink} channelID={channelID} setAppID={setAppID} setAppHash={setAppHash} setChannelLink={setChannelLink} setChannelID={setChannelID} connect={connect} busy={busy} />}
        </>}
        {step === 2 && <div className="setup-review"><span className="setup-state-icon"><CheckCircle2 size={28} /></span><dl><div><dt>Account</dt><dd>Private, single owner</dd></div><div><dt>Storage</dt><dd>{adapter === 'telegram' ? 'Verified private Telegram channel' : 'Local persistent data volume'}</dd></div><div><dt>Encryption</dt><dd>AES-256-GCM · 100 MiB chunks</dd></div></dl><p className="setup-help">Back up your data volume and encryption key. Telegram storage is not a substitute for an independent backup.</p><Button variant="primary" className="setup-primary" busy={busy === 'complete'} onClick={finish} disabled={pending}>{busy === 'complete' ? 'Opening your drive…' : saved ? 'Reload Harbor' : 'Open Harbor'}<ArrowRight size={16} /></Button><Button variant="ghost" onClick={() => setStep(1)} disabled={pending || saved}><ArrowLeft size={15} />Back to storage</Button></div>}
      </>}
      {error && <InlineError id="setup-error">{error}</InlineError>}
    </section>
    <footer className="setup-footer"><ShieldCheck size={14} /><span>Private setup. Credentials stay on your server.</span></footer>
  </div></main>
}

function SecretInput({ id, value, onChange, disabled, placeholder, autoComplete, describedBy }: { id: string; value: string; onChange: (value: string) => void; disabled?: boolean; placeholder?: string; autoComplete: string; describedBy?: string }) {
  const [visible, setVisible] = useState(false)
  return <div className="password-input"><input id={id} value={value} onChange={event => onChange(event.target.value)} type={visible ? 'text' : 'password'} autoComplete={autoComplete} spellCheck={false} autoCapitalize="none" disabled={disabled} placeholder={placeholder} aria-describedby={describedBy} required /><button type="button" className="icon-button" onClick={() => setVisible(!visible)} disabled={disabled} aria-label={visible ? 'Hide value' : 'Show value'}>{visible ? <EyeOff size={17} /> : <Eye size={17} />}</button></div>
}

function TelegramForm({ appID, appHash, channelLink, channelID, setAppID, setAppHash, setChannelLink, setChannelID, connect, busy }: { appID: string; appHash: string; channelLink: string; channelID: string; setAppID: (value: string) => void; setAppHash: (value: string) => void; setChannelLink: (value: string) => void; setChannelID: (value: string) => void; connect: (event: FormEvent) => void; busy: string }) {
  return <form onSubmit={connect} className="setup-form setup-telegram-form"><p className="setup-help">Use a dedicated Telegram account and a private channel you own, with auto-delete and linked discussion disabled. Get API credentials from <a href="https://my.telegram.org/apps" target="_blank" rel="noreferrer">my.telegram.org/apps</a>.</p><label htmlFor="telegram-app-id">Telegram API ID</label><input id="telegram-app-id" value={appID} onChange={event => setAppID(event.target.value)} inputMode="numeric" autoComplete="off" placeholder="Numeric API ID" required disabled={!!busy} /><label htmlFor="telegram-app-hash">Telegram API hash</label><SecretInput id="telegram-app-hash" value={appHash} onChange={setAppHash} disabled={!!busy} autoComplete="off" /><label htmlFor="telegram-channel-link">Private channel link</label><SecretInput id="telegram-channel-link" value={channelLink} onChange={setChannelLink} disabled={!!busy} placeholder="https://t.me/+…" autoComplete="off" /><p className="setup-help">Use a current invite link or private channel message link. Access hash resolves automatically.</p><details className="setup-advanced"><summary>Verify a specific channel ID <span>(optional)</span></summary><label htmlFor="telegram-channel-id">Expected channel ID</label><input id="telegram-channel-id" value={channelID} onChange={event => setChannelID(event.target.value)} inputMode="text" autoComplete="off" placeholder="Leave empty to resolve from link" disabled={!!busy} /></details><Button type="submit" variant="primary" className="setup-primary" busy={busy === 'telegram'} disabled={!!busy || !appID || !appHash || !channelLink}>Connect Telegram<QrCode size={16} /></Button></form>
}
