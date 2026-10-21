// playwright-cli --session NAME run-code --filename scripts/account_check.js
// Fresh disposable LOCAL install only, inside an isolated no-network sandbox.
// Set DRIVE_SETUP_TOKEN=synthetic-browser-bootstrap-token-32-chars in that fixture.
// Run from writable /scratch with screenshots/ available; optional /checks/axe.min.js.
async (page) => {
  const origin = new URL(page.url()).origin
  if (!['127.0.0.1', 'localhost', '[::1]'].includes(new URL(origin).hostname)) throw new Error('Loopback fixture required')
  const assert = (condition, message) => { if (!condition) throw new Error(message) }
  const initial = await page.request.get(`${origin}/api/v1/setup/status`)
  assert(initial.ok() && (await initial.json()).required === true, 'Fresh disposable installation required; no existing account may be changed')
  const token = 'synthetic-browser-bootstrap-token-32-chars'
  const originalPassword = 'Synthetic account fixture password', newPassword = 'Synthetic changed account password'
  const wrongPassword = 'Synthetic wrong current password', displayName = 'Harbor Test 船'
  const passed = [], screenshots = [], pageErrors = [], unsafeURLs = []
  let axeChecks = 0, axeSource
  try { if (typeof require === 'function') axeSource = require('node:fs').readFileSync('/checks/axe.min.js', 'utf8') }
  catch (error) { if (!['ENOENT', 'EACCES'].includes(error.code)) throw error }
  if (axeSource) await page.addInitScript({ content: axeSource })
  const button = name => page.getByRole('button', { name, exact: true })
  const field = name => page.getByLabel(name, { exact: true })
  const settings = page.getByRole('dialog', { name: 'Your private workspace', exact: true })
  const navigation = settings.getByRole('navigation', { name: 'Settings sections', exact: true })
  const sectionButton = name => navigation.getByRole('button', { name, exact: true })
  const openSettings = async () => {
    await page.getByRole('button', { name: 'Settings', exact: true, includeHidden: true }).waitFor({ state: 'attached' })
    if (!(await button('Settings').isVisible())) await button('Open navigation').click()
    await button('Settings').click(); await settings.waitFor()
    assert(await sectionButton('Account').getAttribute('aria-current') === 'page', 'Account must be default settings section')
  }
  const closeSettings = async () => { await button('Close dialog').click(); await settings.waitFor({ state: 'hidden' }) }
  const section = async name => {
    await sectionButton(name).click()
    assert(await sectionButton(name).getAttribute('aria-current') === 'page', `${name} navigation missing current state`)
    const panel = settings.getByRole('region', { name, exact: true })
    const heading = panel.getByRole('heading', { name: { Account: 'Profile', Storage: 'Storage connection', Recovery: 'Backup & recovery' }[name], exact: true })
    await heading.waitFor()
    assert(await panel.evaluate(node => node.scrollTop === 0), `${name} retained previous section scroll position`)
    const bounds = await panel.boundingBox(), title = await heading.boundingBox()
    assert(bounds && title && title.y >= bounds.y && title.y + title.height <= bounds.y + bounds.height + 1, `${name} heading clipped after section navigation`)
    if (name === 'Storage') {
      const action = await panel.getByRole('button', { name: 'Refresh', exact: true }).boundingBox()
      assert(action && action.y >= bounds.y && action.y + action.height <= bounds.y + bounds.height + 1, 'Storage refresh clipped after section navigation')
    }
  }
  const currentCookie = async () => {
    const cookie = (await page.context().cookies(origin)).find(item => item.name === 'drive_session')
    assert(cookie && cookie.httpOnly && cookie.sameSite === 'Strict', 'Owner cookie protection missing')
    return `drive_session=${cookie.value}`
  }
  const currentOwner = async () => {
    const response = await page.request.get(`${origin}/api/v1/auth/session`)
    assert(response.ok(), 'Owner session unexpectedly lost')
    return (await response.json()).owner
  }
  const login = async password => {
    await field('Owner password').fill(password); await button('Unlock drive').click()
  }
  const shot = async name => {
    await page.mouse.move(0, 0)
    await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))))
    assert(await page.evaluate(() => Math.max(document.documentElement.scrollWidth, document.body.scrollWidth) <= innerWidth + 1), `${name} horizontal overflow`)
    const viewport = page.viewportSize(), modal = await settings.boundingBox(), footer = await settings.getByRole('button', { name: 'Lock drive', exact: true }).boundingBox()
    assert(viewport && modal && modal.x >= -1 && modal.y >= -1 && modal.x + modal.width <= viewport.width + 1 && modal.y + modal.height <= viewport.height + 1, `${name} dialog clipped`)
    assert(footer && footer.y >= modal.y && footer.y + footer.height <= Math.min(modal.y + modal.height, viewport.height) + 1, `${name} persistent footer clipped`)
    assert(await settings.locator('.settings-panel').evaluate(node => node.scrollWidth <= node.clientWidth + 1), `${name} inner panel overflow`)
    if (viewport.width < 768) {
      for (const name of ['Account', 'Storage', 'Recovery']) {
        const bounds = await sectionButton(name).boundingBox()
        assert(bounds && bounds.width >= 44 && bounds.height >= 44, `${name} mobile navigation target too small`)
      }
    }
    const axeReady = await page.evaluate(() => !!window.axe)
    assert(!axeSource || axeReady, `${name} supplied axe failed to initialize`)
    if (axeReady) {
      const violations = await page.evaluate(async () => (await window.axe.run(document, { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa'] } })).violations.map(item => ({ id: item.id, targets: item.nodes.map(node => node.target), summaries: item.nodes.map(node => node.failureSummary) })))
      assert(!violations.length, `${name} accessibility violations: ${JSON.stringify(violations)}`); axeChecks++
    }
    const path = `screenshots/account-${name}.png`
    await page.screenshot({ path, animations: 'disabled', fullPage: true }); screenshots.push(path)
  }
  const observeError = error => pageErrors.push(error.message)
  const observeRequest = request => {
    if ([token, originalPassword, newPassword, wrongPassword].some(value => request.url().includes(value) || request.url().includes(encodeURIComponent(value)))) unsafeURLs.push(new URL(request.url()).pathname)
  }
  page.on('pageerror', observeError); page.on('request', observeRequest)
  try {
    const headers = { 'X-Harbor-Setup-Token': token, Origin: origin }
    const account = await page.request.post(`${origin}/api/v1/setup/account`, { headers, data: { password: originalPassword } })
    assert(account.ok(), 'Synthetic account setup failed')
    const completion = await page.request.post(`${origin}/api/v1/setup/complete`, { headers, data: { adapter: 'local' } })
    assert(completion.ok(), 'Synthetic local installation failed')
    let ready = false
    for (let attempt = 0; attempt < 150; attempt++) {
      try { ready = (await page.request.get(`${origin}/ready`, { timeout: 1000 })).ok() } catch { /* Setup listener transitions to normal API. */ }
      if (ready) break
      await page.waitForTimeout(200)
    }
    assert(ready, 'Fixture API failed to become ready')
    await page.setViewportSize({ width: 1440, height: 900 }); await page.emulateMedia({ reducedMotion: 'reduce' }); await page.reload()
    await login(originalPassword); await button('Settings').waitFor()
    const diagnostics = await page.request.get(`${origin}/api/v1/diagnostics`)
    assert(diagnostics.ok() && (await diagnostics.json()).adapter === 'local', 'Disposable local adapter required')
    const oldCookies = [await currentCookie()]
    const secondLogin = await page.request.post(`${origin}/api/v1/auth/login`, { headers: { Origin: origin }, data: { password: originalPassword } })
    assert(secondLogin.ok(), 'Second synthetic session failed'); oldCookies.push(await currentCookie())
    assert(oldCookies[0] !== oldCookies[1], 'Independent session tokens required')
    await openSettings()
    await field('Display name').fill(`  ${displayName}  `); await button('Save profile').click()
    await page.waitForFunction(name => document.querySelector('.owner-card')?.textContent?.includes(name), displayName)
    assert(await currentOwner() === displayName, 'Profile name was not trimmed and persisted')
    await closeSettings(); await page.reload(); await openSettings()
    assert(await field('Display name').inputValue() === displayName, 'Profile lost after reload')
    passed.push('fresh guarded local onboarding; profile Unicode/trim/save/sidebar update/reload; protected independent sessions')

    await sectionButton('Account').focus(); await page.keyboard.press('Tab')
    assert(await sectionButton('Storage').evaluate(node => document.activeElement === node), 'Settings native-button Tab order incorrect')
    await page.keyboard.press('Space')
    assert(await sectionButton('Storage').getAttribute('aria-current') === 'page', 'Space failed to activate Storage')
    await settings.getByRole('heading', { name: 'Storage connection', exact: true }).waitFor()
    await page.keyboard.press('Tab')
    assert(await sectionButton('Recovery').evaluate(node => document.activeElement === node), 'Recovery missing from native-button Tab order')
    await page.keyboard.press('Enter')
    assert(await sectionButton('Recovery').getAttribute('aria-current') === 'page', 'Enter failed to activate Recovery')
    await settings.getByRole('heading', { name: 'Backup & recovery', exact: true }).waitFor()
    const disclosure = settings.locator('.settings-disclosure > summary')
    await disclosure.focus(); await page.keyboard.press('Space')
    assert(await settings.locator('.settings-disclosure').getAttribute('open') !== null, 'Keyboard failed to open server backup instructions')
    await settings.locator('.terminal-command').waitFor({ state: 'visible' })
    await page.keyboard.press('Space')
    assert(await settings.locator('.settings-disclosure').getAttribute('open') === null, 'Keyboard failed to close server backup instructions')
    await section('Account')
    await settings.locator('label[for="owner-display-name"]').click()
    assert(await field('Display name').evaluate(node => document.activeElement === node), 'Display name label failed to focus input')
    await button('Change password').click()
    await page.waitForFunction(() => document.activeElement?.id === 'account-current-password')
    for (const name of ['Current password', 'New password', 'Confirm new password']) await field(name).fill('Synthetic cancel-only password')
    await button('Cancel').click(); await button('Change password').click()
    for (const name of ['Current password', 'New password', 'Confirm new password']) assert(await field(name).inputValue() === '', 'Canceled password form retained credentials')
    await field('Current password').fill('Synthetic discard-on-navigation password'); await section('Storage'); await section('Account'); await button('Change password').click()
    assert(await field('Current password').inputValue() === '', 'Navigation retained password input')
    for (let index = 0; index < 16; index++) {
      await page.keyboard.press('Tab')
      assert(await settings.evaluate(node => node.contains(document.activeElement)), 'Tab escaped modal focus trap')
    }
    await field('Current password').focus(); await page.getByRole('tooltip').waitFor({ state: 'hidden' })
    await page.keyboard.press('Escape'); await settings.waitFor({ state: 'hidden' })
    await page.waitForFunction(() => ['Settings', 'Your profile and password', 'Open navigation'].includes(document.activeElement?.getAttribute('aria-label') || ''))
    await openSettings()
    passed.push('native-button Tab/Space/Enter section navigation; keyboard recovery disclosure; labeled input focus; cancel/navigation discard passwords; modal focus trap, Escape and launcher focus restoration')

    for (const [width, height] of [[320, 740], [390, 844], [768, 1024], [1024, 768], [1280, 800], [1440, 900]]) {
      for (const theme of ['light', 'dark']) {
        await closeSettings(); await page.setViewportSize({ width, height })
        await page.evaluate(theme => localStorage.setItem('harbor:theme', theme), theme); await page.reload(); await openSettings()
        assert(await page.evaluate(theme => document.documentElement.classList.contains('dark') === (theme === 'dark'), theme), 'Theme did not apply')
        await shot(`profile-${theme}-${width}`)
        await button('Change password').click(); await field('Current password').waitFor()
        for (const name of ['Current password', 'New password', 'Confirm new password']) assert(await field(name).getAttribute('type') === 'password', `${name} must start masked`)
        await field('Current password').scrollIntoViewIfNeeded(); await shot(`password-${theme}-${width}`)
        if (width === 390 || width === 1440) {
          await section('Storage'); await settings.getByRole('heading', { name: 'Storage connection', exact: true }).waitFor(); await shot(`storage-${theme}-${width}`)
          await section('Recovery'); await settings.getByRole('heading', { name: 'Backup & recovery', exact: true }).waitFor(); await shot(`recovery-${theme}-${width}`)
        }
      }
    }
    await closeSettings(); await page.setViewportSize({ width: 1440, height: 900 }); await openSettings(); await button('Change password').click()
    await field('Current password').fill(originalPassword); await field('New password').fill('short'); await field('Confirm new password').fill('short')
    await button('Update password').click()
    await page.getByRole('alert').filter({ hasText: /12.*72/ }).waitFor()
    assert(await field('New password').getAttribute('aria-invalid') === 'true' && await field('New password').evaluate(node => document.activeElement === node), 'New-password validation missing field state/focus')
    assert(await currentOwner() === displayName, 'Invalid new password ended session')
    await field('New password').fill(newPassword); await field('Confirm new password').fill(`${newPassword} mismatch`); await button('Update password').click()
    await page.getByRole('alert').filter({ hasText: /match/i }).waitFor()
    assert(await field('Confirm new password').getAttribute('aria-invalid') === 'true' && await field('Confirm new password').evaluate(node => document.activeElement === node), 'Confirmation validation missing field state/focus')
    await field('Current password').fill(wrongPassword); await field('Confirm new password').fill(newPassword); await button('Update password').click()
    await page.getByRole('alert').filter({ hasText: 'Current password is incorrect' }).waitFor()
    await page.waitForFunction(() => document.activeElement?.id === 'account-current-password')
    assert(await currentOwner() === displayName && await settings.isVisible(), 'Wrong current password lost authenticated workspace')
    passed.push('password length/matching validation; wrong-current error keeps account authenticated')

    await field('Current password').fill(originalPassword); await button('Update password').click()
    await page.getByText('Password changed. Sign in with your new password.', { exact: true }).waitFor()
    for (const cookie of oldCookies) {
      const response = await page.request.get(`${origin}/api/v1/auth/session`, { headers: { Cookie: cookie } })
      assert(response.status() === 401, 'Password change failed to revoke an old session')
    }
    assert(!(await page.context().cookies(origin)).some(cookie => cookie.name === 'drive_session'), 'Password-change cookie not cleared')
    await login(originalPassword); await page.getByRole('alert').filter({ hasText: 'Incorrect password' }).waitFor()
    await login(newPassword); await button('Settings').waitFor()
    assert(await currentOwner() === displayName, 'Changed password/profile login failed')
    await page.reload(); assert(await currentOwner() === displayName, 'New session lost after reload')
    assert(await page.evaluate(secrets => [...Object.values(localStorage), ...Object.values(sessionStorage)].every(value => secrets.every(secret => !value.includes(secret))), [token, originalPassword, newPassword, wrongPassword]), 'Credentials persisted in browser storage')
    assert(!unsafeURLs.length, 'Credentials entered request URL')
    assert(!pageErrors.length, `Browser exceptions: ${JSON.stringify(pageErrors)}`)
    passed.push('all old sessions revoked; cookie cleared; old password rejected; new password/reload works; no credential URL/browser storage leaks')
    return { result: 'PASS', passed, screenshots, axe: axeSource ? { result: 'PASS', checks: axeChecks } : { result: 'NOT TESTED', reason: '/checks/axe.min.js unavailable' }, pageErrors }
  } finally {
    page.off('pageerror', observeError); page.off('request', observeRequest)
  }
}
