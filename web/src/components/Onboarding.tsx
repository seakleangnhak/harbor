import { useEffect, useRef, useState, type FormEvent } from 'react'
import { ArrowLeft, ArrowRight, Check, CheckCircle2, ExternalLink, Eye, EyeOff, HardDrive, KeyRound, LoaderCircle, LockKeyhole, Moon, QrCode, ShieldCheck, Sun } from 'lucide-react'
import { SetupError, setupErrorMessage, setupRequest, telegramDetails, validateOwnerPassword, waitForHarborReady, type TelegramSetup } from '../onboarding'
import { Brand } from './Login'
import { Button, InlineError } from './ui'
import './onboarding.css'

const steps = [
  ['Owner account', 'A private sign-in, just for you.'],
  ['Storage connection', 'Choose where your files live.'],
  ['Final review', 'Check the details. Make it yours.'],
]
const safeOrigin = () => location.protocol === 'https:' || ['localhost', '127.0.0.1', '[::1]'].includes(location.hostname)
const message = (error: unknown) => error instanceof SetupError ? error.message : 'Setup could not finish. Check your connection and retry.'

export function Onboarding({ onComplete }: { onComplete: () => void }) {
  const [step, setStep] = useState(0), [token, setToken] = useState(''), [unlocked, setUnlocked] = useState(false)
  const [password, setPassword] = useState(''), [confirmation, setConfirmation] = useState(''), [visible, setVisible] = useState(false)
  const [adapter, setAdapter] = useState<'telegram' | 'local'>('telegram')
  const [appID, setAppID] = useState(''), [appHash, setAppHash] = useState(''), [channelLink, setChannelLink] = useState(''), [channelID, setChannelID] = useState('')
  const [telegram, setTelegram] = useState<TelegramSetup>({ state: 'waiting' }), [started, setStarted] = useState(false)
  const [twoStep, setTwoStep] = useState(''), [busy, setBusy] = useState(''), [error, setError] = useState(''), [errorField, setErrorField] = useState(''), [pollError, setPollError] = useState('')
  const [saved, setSaved] = useState(false), [dark, setDark] = useState(() => document.documentElement.classList.contains('dark'))
  const title = useRef<HTMLHeadingElement>(null), errorSummary = useRef<HTMLDivElement>(null), active = useRef(true), lifecycle = useRef(new AbortController())
  const panel = step === 0 ? unlocked ? 'owner' : 'token' : step === 2 ? 'review' : adapter === 'local' ? 'local' : telegram.state === 'verified' ? 'verified' : started && telegram.state !== 'failed' ? telegram.state === 'password' ? 'password' : 'qr' : 'details'
  useEffect(() => { active.current = true; lifecycle.current = new AbortController(); return () => { active.current = false; lifecycle.current.abort() } }, [])
  useEffect(() => {
    const target = panel === 'password' ? document.getElementById('telegram-password') : title.current
    target?.focus({ preventScroll: true })
    if (target && target.getBoundingClientRect().top < 0) target.scrollIntoView({ block: 'start' })
  }, [panel])
  useEffect(() => { if (error) (document.getElementById(errorField) || errorSummary.current)?.focus({ preventScroll: true }) }, [error, errorField])

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
        if (error instanceof SetupError && error.status === 401) { setUnlocked(false); setStarted(false); setStep(0); setToken(''); setError(message(error)); setErrorField('setup-token') }
        else setPollError(message(error))
        return
      } }
      if (!controller.signal.aborted) timer = setTimeout(poll, 1500)
    }
    void poll()
    return () => { controller.abort(); clearTimeout(timer) }
  }, [started, step, adapter, telegram.state, token, pollError])

  const act = async (name: string, action: () => Promise<void>) => {
    setBusy(name); setError(''); setErrorField('')
    try { await action() } catch (error) {
      if (active.current) {
        const unauthorized = error instanceof SetupError && error.status === 401
        if (unauthorized) { setUnlocked(false); setStarted(false); setStep(0); setToken(''); setPollError('') }
        setError(message(error)); setErrorField(unauthorized || name === 'verify' ? 'setup-token' : name === 'password' ? 'telegram-password' : '')
      }
    } finally { if (active.current) setBusy('') }
  }
  const verify = (event: FormEvent) => {
    event.preventDefault()
    void act('verify', async () => {
      const status = await setupRequest<TelegramSetup>('/verify', token, 'POST', {})
      setUnlocked(true); setTelegram(status)
      if (status.account_ready) {
        setStep(1); setStarted(status.flow_active ?? (status.state !== 'waiting' && status.state !== 'failed'))
        if (status.state === 'failed' && status.error_code !== 'canceled') setError(setupErrorMessage(status.error_code))
      }
    })
  }
  const account = (event: FormEvent) => {
    event.preventDefault()
    const invalid = validateOwnerPassword(password, confirmation)
    if (invalid) { setError(invalid); setErrorField(invalid === 'Passwords do not match.' ? 'setup-confirm' : 'setup-password'); return }
    void act('account', async () => { await setupRequest('/account', token, 'POST', { password }); setPassword(''); setConfirmation(''); setVisible(false); setStep(1) })
  }
  const connect = (event: FormEvent) => {
    event.preventDefault()
    void act('telegram', async () => {
      const details = telegramDetails(appID, appHash, channelLink, channelID)
      await setupRequest('/telegram', token, 'POST', details)
      setTelegram({ state: 'waiting' }); setStarted(true); setPollError('')
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
  const toggleTheme = () => {
    const next = !dark
    setDark(next); document.documentElement.classList.toggle('dark', next)
    try { localStorage.setItem('harbor:theme', next ? 'dark' : 'light') } catch { /* Theme still works when browser storage is unavailable. */ }
  }
  const secure = safeOrigin(), pending = !!busy
  const heading = step === 0 ? unlocked ? 'Make it yours.' : 'Welcome aboard.' : step === 1 ? 'Connect your storage.' : saved ? 'Setup saved.' : 'Review your setup.'
  const note = step === 0 ? unlocked ? 'One owner. One private sign-in. Choose a password for your drive.' : 'Unlock first-run setup to create your own private drive.' : step === 1 ? 'Your files are encrypted before they leave Harbor.' : 'One last look before opening your private drive.'
  const describedBy = error ? 'setup-error' : undefined

  return <main className="setup-page"><div className="setup-shell">
    <header className="setup-header"><Brand /><div className="setup-header-actions"><span className="setup-private"><LockKeyhole size={13} />Private setup</span><button type="button" className="icon-button setup-theme" onClick={toggleTheme} aria-label={dark ? 'Switch to light mode' : 'Switch to dark mode'}>{dark ? <Sun size={18} /> : <Moon size={18} />}</button></div></header>
    <aside className="setup-guide" aria-label="Setup progress"><span className="eyebrow">A SPACE OF YOUR OWN</span><h2>Your files.<br /><em>Your own Harbor.</em></h2><p>A quiet home for everything worth keeping. Set it up on your terms.</p>
      <ol>{steps.map(([name, description], index) => <li key={name} aria-current={step === index ? 'step' : undefined} className={step === index ? 'setup-current' : step > index ? 'setup-done' : ''}><span className="setup-step-number" aria-hidden="true">{step > index ? <Check size={15} /> : `0${index + 1}`}</span><span><strong>{name}</strong><small>{description}</small></span></li>)}</ol>
      <div className="setup-assurance"><ShieldCheck size={19} /><div><strong>Private from the first file.</strong><p>Encrypted storage. Owner-only access. Keep an independent backup of what matters.</p></div></div>
    </aside>
    <section className="setup-card" aria-labelledby="setup-title" aria-busy={pending}>
      <div className="setup-card-heading"><span className="eyebrow">STEP 0{step + 1} / 03</span><h1 id="setup-title" ref={title} tabIndex={-1}>{heading}</h1><p>{note}</p></div>
      {!secure ? <InlineError>Use HTTPS before entering setup credentials. For local setup, open Harbor at localhost or 127.0.0.1.</InlineError> : <>
        {step === 0 && (!unlocked ? <form onSubmit={verify} className="setup-form" aria-describedby={describedBy}>
          <div className="setup-field"><label htmlFor="setup-token">Deployment setup token</label><SecretInput id="setup-token" label="setup token" value={token} onChange={setToken} disabled={pending} placeholder="Enter your setup token" autoComplete="off" describedBy={`setup-token-help${errorField === 'setup-token' ? ' setup-error' : ''}`} invalid={errorField === 'setup-token'} /></div>
          <p className="setup-help" id="setup-token-help">Find <code>DRIVE_SETUP_TOKEN</code> in your container environment. It keeps setup in your hands.</p>
          <Button type="submit" variant="primary" className="setup-primary" busy={busy === 'verify'} disabled={!token.trim() || pending}>Continue securely<ArrowRight size={16} /></Button>
          <p className="setup-form-footnote"><LockKeyhole size={13} />Only needed for first-run setup.</p>
        </form> : <form onSubmit={account} className="setup-form" aria-describedby={describedBy}>
          <div className="setup-status"><ShieldCheck size={16} />Setup token verified<Check size={14} /></div>
          <div className="setup-field"><label htmlFor="setup-password">Owner password</label><div className="password-input"><input id="setup-password" type={visible ? 'text' : 'password'} value={password} onChange={event => setPassword(event.target.value)} autoComplete="new-password" required disabled={pending} aria-invalid={errorField === 'setup-password' || undefined} aria-describedby={`setup-password-help${errorField === 'setup-password' ? ' setup-error' : ''}`} placeholder="Choose a strong passphrase" /><button type="button" className="icon-button" onClick={() => setVisible(!visible)} disabled={pending} aria-label={visible ? 'Hide owner password' : 'Show owner password'} aria-pressed={visible}>{visible ? <EyeOff size={17} /> : <Eye size={17} />}</button></div><p className="setup-help" id="setup-password-help">12–72 bytes. Save a unique passphrase in your password manager.</p></div>
          <div className="setup-field"><label htmlFor="setup-confirm">Confirm password</label><input id="setup-confirm" type={visible ? 'text' : 'password'} value={confirmation} onChange={event => setConfirmation(event.target.value)} autoComplete="new-password" required disabled={pending} aria-invalid={errorField === 'setup-confirm' || undefined} aria-describedby={errorField === 'setup-confirm' ? 'setup-error' : undefined} placeholder="Enter your passphrase again" /></div>
          <Button type="submit" variant="primary" className="setup-primary" busy={busy === 'account'} disabled={pending || !password || !confirmation}>Create owner account<ArrowRight size={16} /></Button>
        </form>)}
        {step === 1 && <>
          <fieldset className="setup-storage" disabled={pending || (started && telegram.state !== 'verified' && telegram.state !== 'failed')}><legend className="sr-only">Storage provider</legend>
            <label className={`setup-provider${adapter === 'telegram' ? ' setup-provider-selected' : ''}`}><input type="radio" name="storage" value="telegram" checked={adapter === 'telegram'} onChange={() => { setAdapter('telegram'); setError(''); setErrorField('') }} /><QrCode size={20} /><span><strong>Telegram</strong><small>Your private channel</small></span></label>
            <label className={`setup-provider${adapter === 'local' ? ' setup-provider-selected' : ''}`}><input type="radio" name="storage" value="local" checked={adapter === 'local'} onChange={() => { setAdapter('local'); setError(''); setErrorField('') }} /><HardDrive size={20} /><span><strong>Local storage</strong><small>This server’s volume</small></span></label>
          </fieldset>
          {adapter === 'local' ? <div className="setup-local"><span className="setup-state-icon"><HardDrive size={24} /></span><h2>A home on your server.</h2><p>Encrypted files, metadata, and your key stay in the persistent data volume. Keep it mounted across updates.</p><p className="setup-help">Storage is fixed after setup. Choose the home you want to keep.</p><Button variant="primary" className="setup-primary" onClick={() => setStep(2)} disabled={pending}>Use local storage<ArrowRight size={16} /></Button></div> : telegram.state === 'verified' ? <div className="setup-verified"><span className="setup-state-icon"><CheckCircle2 size={28} /></span><span className="eyebrow">CONNECTION CONFIRMED</span><h2>Private channel verified</h2><p>Your account and channel are connected. Harbor keeps the session and API credentials on your server.</p><Button variant="primary" className="setup-primary" onClick={() => setStep(2)} disabled={pending}>Continue<ArrowRight size={16} /></Button><Button variant="ghost" onClick={cancel} disabled={pending} busy={busy === 'cancel'}>Change account or channel</Button></div> : started && telegram.state !== 'failed' ? <div className="setup-authorization">
            {telegram.state === 'password' ? <form onSubmit={event => { event.preventDefault(); void act('password', async () => { await setupRequest('/telegram/password', token, 'POST', { password: twoStep }); setTwoStep('') }) }} className="setup-form" aria-describedby={describedBy}><span className="setup-state-icon"><KeyRound size={24} /></span><h2>Telegram two-step verification</h2><p className="setup-help">One more check from Telegram. This password is used for sign-in and is never saved.</p><div className="setup-field"><label htmlFor="telegram-password">Telegram two-step password</label><SecretInput id="telegram-password" label="Telegram two-step password" value={twoStep} onChange={setTwoStep} autoComplete="off" disabled={pending} describedBy={errorField === 'telegram-password' ? 'setup-error' : undefined} invalid={errorField === 'telegram-password'} /></div><Button type="submit" className="setup-primary" variant="primary" busy={busy === 'password'} disabled={!twoStep || pending}>Verify password<ArrowRight size={16} /></Button></form> : <><div className="setup-qr-layout"><div className="setup-qr">{telegram.qr_image?.startsWith('data:image/png;base64,') ? <img src={telegram.qr_image} width="208" height="208" alt="Temporary Telegram sign-in QR code" /> : <div role="status"><LoaderCircle size={26} className="spin" /><span>Preparing sign-in…</span></div>}</div><div className="setup-qr-guide"><h2>Link your dedicated account</h2><ol className="setup-qr-steps"><li>Open Telegram on your phone.</li><li>Go to <strong>Settings → Devices → Link Desktop Device.</strong></li><li>Scan the QR code and approve.</li></ol></div></div><p className="setup-warning"><LockKeyhole size={15} />Treat this QR as a password. Never share it or its screenshot.</p><p className="setup-waiting" role="status"><span className="setup-pulse" aria-hidden="true" />Waiting for approval and channel verification…</p></>}
            {pollError && <div className="setup-poll-error"><InlineError>{pollError}</InlineError><Button onClick={() => setPollError('')} disabled={pending}>Retry connection</Button></div>}<Button className="setup-cancel" variant="ghost" onClick={cancel} disabled={pending} busy={busy === 'cancel'}>Cancel Telegram sign-in</Button>
          </div> : <TelegramForm appID={appID} appHash={appHash} channelLink={channelLink} channelID={channelID} setAppID={setAppID} setAppHash={setAppHash} setChannelLink={setChannelLink} setChannelID={setChannelID} connect={connect} busy={busy} describedBy={describedBy} />}
        </>}
        {step === 2 && <div className="setup-review"><dl><div><dt><LockKeyhole size={17} />Account</dt><dd>Private, single owner</dd></div><div><dt>{adapter === 'telegram' ? <QrCode size={17} /> : <HardDrive size={17} />}Storage</dt><dd>{adapter === 'telegram' ? 'Verified private Telegram channel' : 'Local persistent data volume'}</dd></div><div><dt><ShieldCheck size={17} />Protection</dt><dd>Encrypted before storage</dd></div></dl><p className="setup-warning"><ShieldCheck size={17} /><span>Keep an encrypted backup of your data volume and encryption key. Telegram storage cannot replace an independent backup.</span></p><p className="setup-help">Storage is fixed after setup. Your owner password unlocks Harbor once it is ready.</p><Button variant="primary" className="setup-primary" busy={busy === 'complete'} onClick={finish} disabled={pending}>{busy === 'complete' ? 'Opening your drive…' : saved ? 'Reload Harbor' : 'Open Harbor'}<ArrowRight size={16} /></Button><Button className="setup-back" variant="ghost" onClick={() => setStep(1)} disabled={pending || saved}><ArrowLeft size={15} />Back to storage</Button></div>}
      </>}
      {error && <div id="setup-error" ref={errorSummary} tabIndex={-1} className="setup-error-summary"><InlineError>{error}</InlineError></div>}
    </section>
    <footer className="setup-footer"><span>Harbor · Your personal cloud</span><span><ShieldCheck size={13} />Credentials stay on your server.</span></footer>
  </div></main>
}

function SecretInput({ id, label, value, onChange, disabled, placeholder, autoComplete, describedBy, invalid }: { id: string; label: string; value: string; onChange: (value: string) => void; disabled?: boolean; placeholder?: string; autoComplete: string; describedBy?: string; invalid?: boolean }) {
  const [visible, setVisible] = useState(false)
  return <div className="password-input"><input id={id} value={value} onChange={event => onChange(event.target.value)} type={visible ? 'text' : 'password'} autoComplete={autoComplete} spellCheck={false} autoCapitalize="none" disabled={disabled} placeholder={placeholder} aria-describedby={describedBy} aria-invalid={invalid || undefined} required /><button type="button" className="icon-button" onClick={() => setVisible(!visible)} disabled={disabled} aria-label={`${visible ? 'Hide' : 'Show'} ${label}`} aria-pressed={visible}>{visible ? <EyeOff size={17} /> : <Eye size={17} />}</button></div>
}

function TelegramForm({ appID, appHash, channelLink, channelID, setAppID, setAppHash, setChannelLink, setChannelID, connect, busy, describedBy }: { appID: string; appHash: string; channelLink: string; channelID: string; setAppID: (value: string) => void; setAppHash: (value: string) => void; setChannelLink: (value: string) => void; setChannelID: (value: string) => void; connect: (event: FormEvent) => void; busy: string; describedBy?: string }) {
  return <form onSubmit={connect} className="setup-form setup-telegram-form" aria-describedby={describedBy}>
    <p className="setup-help setup-credentials-note">Use a dedicated account and a private channel you own. <a href="https://my.telegram.org/apps" target="_blank" rel="noreferrer">Get API credentials<ExternalLink size={12} aria-hidden="true" /><span className="sr-only"> (opens in a new tab)</span></a></p>
    <div className="setup-field-row"><div className="setup-field"><label htmlFor="telegram-app-id">Telegram API ID</label><input id="telegram-app-id" value={appID} onChange={event => setAppID(event.target.value)} inputMode="numeric" autoComplete="off" placeholder="Numeric API ID" required disabled={!!busy} /></div><div className="setup-field"><label htmlFor="telegram-app-hash">Telegram API hash</label><SecretInput id="telegram-app-hash" label="Telegram API hash" value={appHash} onChange={setAppHash} disabled={!!busy} autoComplete="off" placeholder="32-character API hash" /></div></div>
    <div className="setup-field"><label htmlFor="telegram-channel-link">Private channel link</label><SecretInput id="telegram-channel-link" label="private channel link" value={channelLink} onChange={setChannelLink} disabled={!!busy} placeholder="https://t.me/+…" autoComplete="off" describedBy="telegram-channel-help" /><p className="setup-help" id="telegram-channel-help">Use a current invite or private message link. Turn off channel auto-delete and linked discussion.</p></div>
    <details className="setup-advanced"><summary>Verify a specific channel ID <span>(optional)</span></summary><div className="setup-field"><label htmlFor="telegram-channel-id">Expected channel ID</label><input id="telegram-channel-id" value={channelID} onChange={event => setChannelID(event.target.value)} inputMode="text" autoComplete="off" placeholder="Resolve automatically from link" disabled={!!busy} /></div></details>
    <Button type="submit" variant="primary" className="setup-primary" busy={busy === 'telegram'} disabled={!!busy || !appID || !appHash || !channelLink}>Connect Telegram<ArrowRight size={16} /></Button>
  </form>
}
