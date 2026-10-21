import { useEffect, useRef, useState, type FormEvent, type Ref } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { Check, Circle, Eye, EyeOff, LockKeyhole, ShieldCheck, UserRound } from 'lucide-react'
import { api, ApiError } from '../api'
import { ownerInitials, validateDisplayName } from '../account'
import { validateOwnerPassword } from '../onboarding'
import type { Session } from '../types'
import { errorMessage } from '../utils'
import { Button, IconButton, InlineError } from './ui'

export function OwnerAccount({ owner, passwordChanged, onBusyChange }: { owner: string; passwordChanged: () => void; onBusyChange?: (busy: boolean) => void }) {
  const queryClient = useQueryClient()
  const [name, setName] = useState(owner)
  const [saving, setSaving] = useState(false)
  const [profileError, setProfileError] = useState('')
  const [saved, setSaved] = useState(false)
  const [expanded, setExpanded] = useState(false)
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirmation, setConfirmation] = useState('')
  const [changing, setChanging] = useState(false)
  const [passwordError, setPasswordError] = useState('')
  const [invalidField, setInvalidField] = useState<'current' | 'new' | 'confirmation' | null>(null)
  const profileInput = useRef<HTMLInputElement>(null)
  const currentInput = useRef<HTMLInputElement>(null)
  const newInput = useRef<HTMLInputElement>(null)
  const confirmationInput = useRef<HTMLInputElement>(null)
  const busy = saving || changing
  const dirty = name !== owner
  const nameLength = [...name.trim()].length
  const passwordBytes = new TextEncoder().encode(newPassword).byteLength
  const validLength = passwordBytes >= 12 && passwordBytes <= 72 && !/[\r\n]/.test(newPassword)
  const matches = !!confirmation && newPassword === confirmation
  useEffect(() => { if (expanded) currentInput.current?.focus() }, [expanded])
  useEffect(() => { if (!changing && invalidField === 'current') currentInput.current?.focus() }, [changing, invalidField])
  useEffect(() => { onBusyChange?.(busy); return () => onBusyChange?.(false) }, [busy, onBusyChange])

  const saveProfile = async (event: FormEvent) => {
    event.preventDefault()
    if (saving || changing) return
    const validation = validateDisplayName(name)
    setSaved(false); setProfileError(validation)
    if (validation) { profileInput.current?.focus(); return }
    setSaving(true)
    try {
      const session = await api.updateProfile(name.trim())
      await queryClient.cancelQueries({ queryKey: ['session'] })
      queryClient.setQueryData<Session | null>(['session'], previous => previous ? session : previous)
      setName(session.owner); setSaved(true)
    } catch (error) { setProfileError(errorMessage(error)) } finally { setSaving(false) }
  }

  const changePassword = async (event: FormEvent) => {
    event.preventDefault()
    if (changing || saving) return
    const validation = currentPassword ? validateOwnerPassword(newPassword, confirmation) : 'Enter your current password.'
    setPasswordError(validation); setInvalidField(null)
    if (validation) {
      const field = !currentPassword ? 'current' : validateOwnerPassword(newPassword, newPassword) ? 'new' : 'confirmation'
      setInvalidField(field)
      const input = field === 'current' ? currentInput : field === 'new' ? newInput : confirmationInput
      input.current?.focus()
      return
    }
    setChanging(true)
    try {
      await api.changePassword(currentPassword, newPassword)
      setCurrentPassword(''); setNewPassword(''); setConfirmation('')
      passwordChanged()
    } catch (error) {
      if (error instanceof ApiError && error.code === 'incorrect_password') {
        setPasswordError('Current password is incorrect. Try again.')
        setCurrentPassword(''); setInvalidField('current')
      } else if (error instanceof ApiError && error.status === 429) setPasswordError('Too many password attempts. Wait before trying again.')
      else setPasswordError(errorMessage(error))
    } finally { setChanging(false) }
  }

  return <>
    <div className="account-identity"><span className="owner-avatar" aria-hidden="true">{ownerInitials(owner)}</span><div><span className="account-eyebrow">YOUR ACCOUNT</span><strong>{owner}</strong><span>Personal workspace</span></div><span className="account-owner-label"><LockKeyhole size={12} aria-hidden="true" />Owner</span></div>
    <section className="settings-section account-section" aria-labelledby="profile-title">
      <div className="settings-section-heading"><UserRound size={17} aria-hidden="true" /><div><h3 id="profile-title">Profile</h3><p>Your name, across your workspace.</p></div></div>
      <form className="account-form" onSubmit={saveProfile} aria-busy={saving} noValidate>
        <div><div className="account-field-label"><label htmlFor="owner-display-name">Display name</label><span aria-hidden="true" data-invalid={nameLength > 80}>{nameLength} / 80</span></div><input ref={profileInput} id="owner-display-name" className="form-input" value={name} onChange={event => { setName(event.target.value); setSaved(false); setProfileError('') }} autoComplete="nickname" maxLength={160} required disabled={busy} aria-invalid={!!profileError} aria-describedby={`profile-hint${profileError ? ' profile-error' : ''}`} /><p id="profile-hint" className="account-hint">Shown in your workspace. Up to 80 characters.</p></div>
        {profileError && <InlineError id="profile-error">{profileError}</InlineError>}
        <div className="account-form-footer"><span className={saved ? 'account-success' : 'account-draft'} role="status">{saved ? <><Check size={14} aria-hidden="true" />Profile saved.</> : dirty ? 'Unsaved changes' : 'Only visible in your drive'}</span><div>{dirty && <Button type="button" variant="ghost" disabled={busy} onClick={() => { setName(owner); setSaved(false); setProfileError(''); profileInput.current?.focus() }}>Cancel changes</Button>}<Button type="submit" variant="primary" busy={saving} disabled={changing || !dirty}>{saving ? 'Saving…' : 'Save profile'}</Button></div></div>
      </form>
    </section>
    <section className="settings-section account-section" aria-labelledby="password-title" data-expanded={expanded}>
      <div className="settings-section-heading"><LockKeyhole size={17} aria-hidden="true" /><div><h3 id="password-title">Password</h3><p>A unique passphrase keeps your drive private.</p></div><Button type="button" disabled={busy} aria-expanded={expanded} aria-controls={expanded ? 'account-password-form' : undefined} onClick={() => { setExpanded(!expanded); setCurrentPassword(''); setNewPassword(''); setConfirmation(''); setPasswordError(''); setInvalidField(null) }}>{expanded ? 'Cancel' : 'Change password'}</Button></div>
      {expanded && <form id="account-password-form" className="account-form" onSubmit={changePassword} aria-busy={changing} aria-describedby={passwordError ? 'account-password-error' : undefined} noValidate>
        <div className="account-security-notice"><ShieldCheck size={17} aria-hidden="true" /><p>Changing your password signs out every browser, including this one. Finish or pause active transfers first.</p></div>
        <PasswordField id="account-current-password" label="Current password" value={currentPassword} change={value => { setCurrentPassword(value); setPasswordError(''); setInvalidField(null) }} disabled={busy} current inputRef={currentInput} error={invalidField === 'current'} />
        <PasswordField id="account-new-password" label="New password" value={newPassword} change={value => { setNewPassword(value); setPasswordError(''); setInvalidField(null) }} disabled={busy} inputRef={newInput} description="password-guidance" error={invalidField === 'new'} />
        <div className="account-password-guidance" id="password-guidance"><p>12–72 UTF-8 bytes. Spaces count; no line breaks.</p><span data-invalid={passwordBytes > 72}>{passwordBytes} / 72 bytes</span></div>
        <PasswordField id="account-confirm-password" label="Confirm new password" value={confirmation} change={value => { setConfirmation(value); setPasswordError(''); setInvalidField(null) }} disabled={busy} inputRef={confirmationInput} error={invalidField === 'confirmation'} />
        <div className="account-password-checks"><span data-met={validLength}>{validLength ? <Check size={13} aria-hidden="true" /> : <Circle size={11} aria-hidden="true" />}{validLength ? 'Length requirement met' : '12–72 bytes required'}</span><span data-met={matches}>{matches ? <Check size={13} aria-hidden="true" /> : <Circle size={11} aria-hidden="true" />}{matches ? 'Passwords match' : 'Confirm your new password'}</span></div>
        {passwordError && <InlineError id="account-password-error">{passwordError}</InlineError>}
        <div className="account-form-footer"><span className="account-draft"><LockKeyhole size={12} aria-hidden="true" />All sessions will end</span><Button type="submit" variant="primary" busy={changing} disabled={saving}>{changing ? 'Updating…' : 'Update password'}</Button></div>
      </form>}
    </section>
  </>
}

function PasswordField({ id, label, value, change, disabled, current, description, inputRef, error }: { id: string; label: string; value: string; change: (value: string) => void; disabled: boolean; current?: boolean; description?: string; inputRef?: Ref<HTMLInputElement>; error?: boolean }) {
  const [visible, setVisible] = useState(false)
  return <div><label htmlFor={id}>{label}</label><div className="password-input"><input ref={inputRef} id={id} type={visible ? 'text' : 'password'} value={value} onChange={event => change(event.target.value)} autoComplete={current ? 'current-password' : 'new-password'} maxLength={72} required disabled={disabled} aria-invalid={!!error} aria-describedby={error ? 'account-password-error' : description} autoCapitalize="none" spellCheck={false} /><IconButton label={`${visible ? 'Hide' : 'Show'} ${label.toLowerCase()}`} onClick={() => setVisible(!visible)} aria-pressed={visible} disabled={disabled}>{visible ? <EyeOff size={17} /> : <Eye size={17} />}</IconButton></div></div>
}
