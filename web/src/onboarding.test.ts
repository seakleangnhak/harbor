import assert from 'node:assert/strict'
import { test } from 'node:test'
import { SetupError, setupErrorMessage, setupRequest, setupStatus, telegramDetails, validateOwnerPassword, waitForHarborReady } from './onboarding.ts'

test('owner password uses bcrypt byte bounds and exact confirmation', () => {
  assert.match(validateOwnerPassword('short', 'short'), /12 and 72/)
  assert.equal(validateOwnerPassword('é'.repeat(6), 'é'.repeat(6)), '')
  assert.equal(validateOwnerPassword('x'.repeat(72), 'x'.repeat(72)), '')
  assert.match(validateOwnerPassword('é'.repeat(37), 'é'.repeat(37)), /12 and 72/)
  assert.match(validateOwnerPassword('a valid passphrase', 'another passphrase'), /do not match/)
  assert.match(validateOwnerPassword('valid password\n', 'valid password\n'), /one line/)
  assert.match(validateOwnerPassword('valid password\rmore', 'valid password\rmore'), /one line/)
})

test('Telegram setup accepts private selectors and optional safe numeric channel ID', () => {
  const hash = 'ab'.repeat(16)
  assert.deepEqual(telegramDetails('123', hash, 'https://t.me/+demo_invite', ''), { app_id: 123, app_hash: hash, channel_link: 'https://t.me/+demo_invite' })
  assert.equal(telegramDetails('123', hash, 'https://t.me/c/123/4', '-1000000000123').channel_id, -1000000000123)
  assert.equal(telegramDetails('123', hash, 'https://t.me/joinchat/demo_invite', '123').channel_id, 123)
  for (const link of ['https://t.me/+demo_invite/', 'https://t.me/joinchat/demo_invite/', 'https://t.me/c/123/4/']) assert.equal(telegramDetails('123', hash, link, '').channel_link, link.slice(0, -1))
  for (const link of ['https://t.me/publicname', 'http://t.me/+demo', 'https://example.com/+demo', 'https://user:secret@t.me/+demo', 'https://t.me/+demo?secret=1', 'https://t.me/+demo#token']) assert.throws(() => telegramDetails('123', hash, link, ''), SetupError)
  assert.throws(() => telegramDetails('1.2', hash, 'https://t.me/+demo', ''), /numeric API ID/)
  assert.throws(() => telegramDetails('2147483648', hash, 'https://t.me/+demo', ''), /numeric API ID/)
  assert.throws(() => telegramDetails('123', 'wrong', 'https://t.me/+demo', ''), /32-character API hash/)
  assert.throws(() => telegramDetails('123', hash, 'https://t.me/+demo', '9007199254740993'), /valid numeric/)
})

test('setup transport keeps bootstrap token in header and never echoes server secret errors', async () => {
  const original = globalThis.fetch
  try {
    let call: { input: string; init?: RequestInit } | undefined
    globalThis.fetch = async (input, init) => { call = { input: String(input), init }; return new Response(JSON.stringify({ account_ready: true }), { status: 200 }) }
    assert.deepEqual(await setupRequest('/account', 'synthetic-bootstrap-token', 'POST', { password: 'synthetic-owner-password' }), { account_ready: true })
    assert.equal(call?.input, '/api/v1/setup/account')
    assert.equal(new Headers(call?.init?.headers).get('X-Harbor-Setup-Token'), 'synthetic-bootstrap-token')
    assert.equal(call?.init?.cache, 'no-store')
    assert.equal(call?.init?.credentials, 'same-origin')
    assert.equal(call?.init?.body, JSON.stringify({ password: 'synthetic-owner-password' }))
    globalThis.fetch = async () => new Response(JSON.stringify({ error: 'private-provider-secret' }), { status: 401 })
    await assert.rejects(setupRequest('/verify', 'invalid', 'POST', {}), error => error instanceof SetupError && error.status === 401 && !error.message.includes('private-provider-secret') && error.message.includes('Setup token'))
    globalThis.fetch = async () => new Response(JSON.stringify({ code: 'setup_expired', error: 'private-provider-secret' }), { status: 401 })
    await assert.rejects(setupRequest('/verify', 'expired', 'POST', {}), error => error instanceof SetupError && error.status === 401 && error.message.includes('Restart Harbor') && !error.message.includes('private-provider-secret'))
    globalThis.fetch = async () => new Response(JSON.stringify({ code: 'setup_busy', error: 'private-provider-secret' }), { status: 409 })
    await assert.rejects(setupRequest('/telegram', 'token', 'POST', {}), error => error instanceof SetupError && error.status === 409 && error.message.includes('Wait a moment') && !error.message.includes('private-provider-secret'))
    globalThis.fetch = async () => new Response(null, { status: 429, headers: { 'Retry-After': '900' } })
    await assert.rejects(setupRequest('/telegram', 'token', 'POST', {}), /Wait 15 minutes/)
    globalThis.fetch = async () => { throw new TypeError('private-server-path') }
    await assert.rejects(setupRequest('/telegram', 'token'), error => error instanceof SetupError && !error.message.includes('private-server-path'))
  } finally { globalThis.fetch = original }
})

test('status preserves initialized older deployments and rejects malformed responses', async () => {
  const original = globalThis.fetch
  try {
    globalThis.fetch = async () => new Response(null, { status: 404 })
    assert.deepEqual(await setupStatus(), { required: false })
    globalThis.fetch = async () => new Response(JSON.stringify({ required: true, unexpected: 'not returned' }))
    assert.deepEqual(await setupStatus(), { required: true })
    globalThis.fetch = async () => new Response(JSON.stringify({ required: 'true' }))
    await assert.rejects(setupStatus(), /invalid setup status/)
    globalThis.fetch = async () => new Response(null, { status: 503 })
    await assert.rejects(setupStatus(), /could not be checked/)
  } finally { globalThis.fetch = original }
  assert.match(setupErrorMessage('channel_mismatch'), /do not match/)
  assert.match(setupErrorMessage('untrusted-secret'), /did not finish/)
})

test('saved setup waits for actual runtime readiness and supports cancellation', async () => {
  const original = globalThis.fetch
  try {
    globalThis.fetch = async (input, init) => {
      assert.equal(input, '/ready')
      assert.equal(new Headers(init?.headers).has('X-Harbor-Setup-Token'), false)
      return new Response(JSON.stringify({ status: 'ready' }), { status: 200 })
    }
    await waitForHarborReady(new AbortController().signal)
    const abort = new AbortController()
    globalThis.fetch = async () => { abort.abort(); return new Response(null, { status: 503 }) }
    await assert.rejects(waitForHarborReady(abort.signal), /aborted/i)
  } finally { globalThis.fetch = original }
})
