export type TelegramSetupState = 'waiting' | 'scanning' | 'password' | 'verified' | 'failed'
export type TelegramSetup = { state: TelegramSetupState; flow_active?: boolean; qr_image?: string; error_code?: string; account_ready?: boolean }
export type TelegramDetails = { app_id: number; app_hash: string; channel_link: string; channel_id?: number }

export class SetupError extends Error {
  status: number
  constructor(message: string, status = 0) { super(message); this.status = status }
}

export function setupErrorMessage(code?: string): string {
  switch (code) {
    case 'channel_access': return 'Channel could not be verified. Use a private channel owned by this account and its current invite link. Disable auto-delete and linked discussion.'
    case 'channel_mismatch': return 'Channel ID and link do not match. Check the intended storage channel.'
    case 'unauthorized': case 'session_unauthorized': case 'session_invalid': case 'auth_restart': return 'Telegram authorization expired. Start a new QR sign-in.'
    case 'password_invalid': case 'password_hash_invalid': case 'password_required': return 'Telegram two-step password was not accepted. Try again.'
    case 'channel_retention': return 'Disable auto-delete in the storage channel before trying again.'
    case 'timeout': case 'deadline': return 'Telegram sign-in timed out. Start again to get a fresh QR code.'
    case 'flood_wait': case 'rate_limited': return 'Telegram requested a pause. Wait before trying again.'
    case 'invalid_config': case 'configuration': case 'api_credentials': return 'Check your Telegram API ID, API hash, and private channel link.'
    case 'unavailable': case 'network': return 'Telegram could not be reached. Check server connectivity and try again.'
    case 'build_disabled': return 'This build does not support Telegram. Use a Telegram-enabled Harbor image.'
    case 'canceled': return 'Telegram sign-in was canceled. Start again when ready.'
    default: return 'Telegram setup did not finish. Check your account and channel settings, then try again.'
  }
}

export async function setupRequest<T>(path: string, token: string, method = 'GET', body?: unknown, signal?: AbortSignal): Promise<T> {
  let response: Response
  try {
    response = await fetch(`/api/v1/setup${path}`, {
      method, credentials: 'same-origin', cache: 'no-store', redirect: 'error', signal,
      headers: { 'X-Harbor-Setup-Token': token, ...(body === undefined ? {} : { 'Content-Type': 'application/json' }) },
      ...(body === undefined ? {} : { body: JSON.stringify(body) }),
    })
  } catch (error) {
    if (signal?.aborted) throw error
    throw new SetupError('Harbor could not be reached. Check your connection and retry.')
  }
  if (!response.ok) {
    const details = await response.json().catch(() => ({}))
    if (response.status === 401 && details?.code === 'setup_expired') throw new SetupError('Setup expired. Restart Harbor, then reload this page to begin again.', 401)
    if (response.status === 409 && details?.code === 'setup_busy') throw new SetupError('Telegram is still finishing the previous step. Wait a moment, then retry.', 409)
    const messages: Record<number, string> = {
      400: 'Check the entered details and try again.', 401: 'Setup token was not accepted. Use DRIVE_SETUP_TOKEN from your deployment environment.',
      403: 'Setup requires a same-origin connection. Open Harbor directly using its configured address.',
      404: 'Setup is closed. Reload Harbor to sign in.', 409: 'Setup state changed. Reload Harbor to continue.',
      413: 'Setup request is too large.', 429: 'Too many setup attempts. Wait 15 minutes before retrying.',
    }
    throw new SetupError(messages[response.status] || 'Setup could not finish. Retry or check the server diagnostics.', response.status)
  }
  return response.status === 204 ? undefined as T : response.json()
}

export async function setupStatus(signal?: AbortSignal): Promise<{ required: boolean }> {
  const response = await fetch('/api/v1/setup/status', { credentials: 'same-origin', cache: 'no-store', redirect: 'error', signal })
  if (response.status === 404) return { required: false }
  if (!response.ok) throw new SetupError('Harbor setup status could not be checked. Retry in a moment.', response.status)
  const value: unknown = await response.json()
  if (!value || typeof value !== 'object' || !('required' in value) || typeof value.required !== 'boolean') throw new SetupError('Harbor returned an invalid setup status. Reload to try again.')
  return { required: value.required }
}

export async function waitForHarborReady(signal: AbortSignal): Promise<void> {
  signal.throwIfAborted()
  const controller = new AbortController(), abort = () => controller.abort()
  signal.addEventListener('abort', abort, { once: true })
  const deadline = setTimeout(abort, 60_000)
  try {
    for (;;) {
      controller.signal.throwIfAborted()
      try {
        const response = await fetch('/ready', { credentials: 'same-origin', cache: 'no-store', redirect: 'error', signal: controller.signal })
        if (response.status === 200 && (await response.json()).status === 'ready') return
        await response.body?.cancel().catch(() => {})
      } catch { controller.signal.throwIfAborted() }
      controller.signal.throwIfAborted()
      await new Promise<void>((resolve, reject) => {
        const stop = () => { clearTimeout(timer); reject(controller.signal.reason) }
        const timer = setTimeout(() => { controller.signal.removeEventListener('abort', stop); resolve() }, 1000)
        controller.signal.addEventListener('abort', stop, { once: true })
      })
    }
  } finally { clearTimeout(deadline); signal.removeEventListener('abort', abort) }
}

export function validateOwnerPassword(password: string, confirmation: string): string {
  if (/[\r\n]/.test(password)) return 'Owner password must stay on one line.'
  const length = new TextEncoder().encode(password).length
  if (length < 12 || length > 72) return 'Choose a password between 12 and 72 bytes. Unicode characters can use more than one byte.'
  return password === confirmation ? '' : 'Passwords do not match.'
}

export function telegramDetails(appID: string, appHash: string, channelLink: string, channelID: string): TelegramDetails {
  const id = Number(appID), channel = channelID.trim(), link = channelLink.trim().replace(/\/$/, '')
  if (!/^\d+$/.test(appID.trim()) || !Number.isSafeInteger(id) || id <= 0 || id > 2147483647) throw new SetupError('Enter the numeric API ID from my.telegram.org/apps.')
  if (!/^[a-f\d]{32}$/i.test(appHash.trim())) throw new SetupError('Enter the 32-character API hash from my.telegram.org/apps.')
  let url: URL
  try { url = new URL(link) } catch { throw new SetupError('Enter a current private channel link, such as https://t.me/+…') }
  if (url.protocol !== 'https:' || url.hostname !== 't.me' || url.username || url.password || url.port || url.search || url.hash || !/^\/(?:\+[A-Za-z0-9_-]+|joinchat\/[A-Za-z0-9_-]+|c\/\d+\/\d+)\/?$/.test(url.pathname)) throw new SetupError('Use a private https://t.me/+… invite link or private channel message link.')
  const channelNumber = Number(channel)
  if (channel && (!/^-?\d+$/.test(channel) || !Number.isSafeInteger(channelNumber) || channelNumber === 0 || channelNumber < 0 && (channelNumber >= -1000000000000 || channelNumber <= -2000000000000))) throw new SetupError('Channel ID must be a valid numeric Telegram channel ID, or leave it empty.')
  return { app_id: id, app_hash: appHash.trim(), channel_link: link, ...(channel ? { channel_id: channelNumber } : {}) }
}
