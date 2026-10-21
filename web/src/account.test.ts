import assert from 'node:assert/strict'
import { test } from 'node:test'
import { ownerInitials, validateDisplayName } from './account.ts'
import { api, ApiError } from './api.ts'

test('display names use trimmed Unicode character bounds and reject controls or invalid Unicode', () => {
  assert.equal(validateDisplayName('  Harbor Owner  '), '')
  assert.equal(validateDisplayName('កំពង់ផែ'), '')
  assert.equal(validateDisplayName('🛟'.repeat(80)), '')
  for (const name of ['', ' \t ', '🛟'.repeat(81), 'Owner\nname', 'Owner\u0000', 'Owner\u007f', 'Owner\ud800']) assert.notEqual(validateDisplayName(name), '', JSON.stringify(name))
})

test('avatar initials follow current owner name without splitting Unicode characters', () => {
  assert.equal(ownerInitials('Harbor Owner'), 'HO')
  assert.equal(ownerInitials('  Harbor   Private Owner  '), 'HO')
  assert.equal(ownerInitials('Owner'), 'OW')
  assert.equal(ownerInitials('🛟 Owner'), '🛟O')
  assert.equal(ownerInitials(''), 'OW')
})

test('account requests keep passwords in JSON bodies and distinguish wrong-current from revoked sessions', async () => {
  const originalFetch = globalThis.fetch, originalWindow = Object.getOwnPropertyDescriptor(globalThis, 'window')
  const events = new EventTarget()
  let unauthorized = 0
  events.addEventListener('harbor:unauthorized', () => { unauthorized++ })
  Object.defineProperty(globalThis, 'window', { value: events, configurable: true })
  try {
    let call: { path: string; init?: RequestInit } | undefined
    globalThis.fetch = async (path, init) => { call = { path: String(path), init }; return new Response(JSON.stringify({ authenticated: true, owner: 'Harbor Owner' })) }
    assert.deepEqual(await api.updateProfile('Harbor Owner'), { authenticated: true, owner: 'Harbor Owner' })
    assert.equal(call?.path, '/api/v1/auth/profile')
    assert.equal(call?.init?.method, 'PATCH')
    assert.equal(call?.init?.body, JSON.stringify({ display_name: 'Harbor Owner' }))
    globalThis.fetch = async (path, init) => { call = { path: String(path), init }; return new Response(null, { status: 204 }) }
    await api.changePassword('synthetic-old-passphrase', 'synthetic-new-passphrase')
    assert.equal(call?.path, '/api/v1/auth/password')
    assert.equal(call?.init?.method, 'POST')
    assert.equal(call?.init?.credentials, 'same-origin')
    assert.equal(new Headers(call?.init?.headers).get('Content-Type'), 'application/json')
    assert.equal(call?.init?.body, JSON.stringify({ current_password: 'synthetic-old-passphrase', new_password: 'synthetic-new-passphrase' }))
    globalThis.fetch = async () => new Response(JSON.stringify({ error: 'Current password is incorrect.', code: 'incorrect_password' }), { status: 403 })
    await assert.rejects(api.changePassword('wrong', 'synthetic-new-passphrase'), error => error instanceof ApiError && error.status === 403 && error.code === 'incorrect_password')
    assert.equal(unauthorized, 0)
    globalThis.fetch = async () => new Response(JSON.stringify({ error: 'Session expired.' }), { status: 401 })
    await assert.rejects(api.changePassword('synthetic-old-passphrase', 'synthetic-new-passphrase'), ApiError)
    assert.equal(unauthorized, 1)
  } finally {
    globalThis.fetch = originalFetch
    if (originalWindow) Object.defineProperty(globalThis, 'window', originalWindow)
    else Reflect.deleteProperty(globalThis, 'window')
  }
})
