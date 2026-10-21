// playwright-cli --session NAME run-code --filename scripts/onboarding_ui_check.js
// Fresh disposable LOCAL install only. Telegram responses and credentials are synthetic.
// Run from writable /scratch with screenshots/ available; never use a configured drive.
async (page) => {
  const origin = new URL(page.url()).origin
  if (!['127.0.0.1', 'localhost', '[::1]'].includes(new URL(origin).hostname)) throw new Error('Loopback fixture required')
  const assert = (condition, message) => { if (!condition) throw new Error(message) }, passed = [], layouts = []
  const initial = await page.request.get(`${origin}/api/v1/setup/status`)
  assert(initial.ok() && (await initial.json()).required === true, 'Fresh disposable setup required')
  const token = 'synthetic-browser-bootstrap-token-32-chars', password = 'Synthetic browser owner passphrase'
  const twoStep = 'Synthetic Telegram two-step passphrase', appHash = '1'.repeat(32), link = 'https://t.me/+SyntheticBrowserFixture'
  const button = name => page.getByRole('button', { name, exact: true })
  const field = name => page.getByLabel(name, { exact: true })
  const screenshot = name => page.screenshot({ path: `screenshots/onboarding-${name}.png`, animations: 'disabled', fullPage: true })
  let axeSource, axeChecks = 0
  try { if (typeof require === 'function') axeSource = require('node:fs').readFileSync('/checks/axe.min.js', 'utf8') }
  catch (error) { if (!['ENOENT', 'EACCES'].includes(error.code)) throw error }
  if (axeSource) await page.addInitScript({ content: axeSource })
  const theme = async dark => {
    if (await page.evaluate(() => document.documentElement.classList.contains('dark')) !== dark) await button(dark ? 'Switch to dark mode' : 'Switch to light mode').click()
    assert(await page.evaluate(() => document.documentElement.classList.contains('dark')) === dark, 'Theme toggle failed')
    await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))))
  }
  const layout = async (stage, primary) => {
    for (const [width, height] of [[1440, 900], [1280, 800], [390, 844], [320, 740], [768, 900]]) {
      await page.setViewportSize({ width, height })
      for (const dark of [false, true]) {
        await theme(dark)
        assert(await page.evaluate(() => Math.max(document.documentElement.scrollWidth, document.body.scrollWidth) <= innerWidth + 1), `${stage} horizontal overflow at ${width}`)
        await page.evaluate(() => scrollTo(0, 0))
        if (stage === 'storage-telegram' && width >= 1280) {
          const natural = await button(primary).boundingBox()
          assert(natural && natural.y + natural.height <= height, `Desktop storage action fell below viewport at ${width}`)
        }
        await button(primary).scrollIntoViewIfNeeded()
        const bounds = await button(primary).boundingBox()
        assert(bounds && bounds.x >= -1 && bounds.y >= -1 && bounds.x + bounds.width <= width + 1 && bounds.y + bounds.height <= height + 1 && bounds.height >= 44, `${stage} primary action clipped or too small at ${width}`)
        const context = `${stage}-${dark ? 'dark' : 'light'}-${width}x${height}`
        if (await page.evaluate(() => !!window.axe)) {
          const violations = await page.evaluate(async () => (await window.axe.run(document, { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa'] } })).violations.map(item => ({ id: item.id, nodes: item.nodes.map(node => ({ target: node.target, summary: node.failureSummary })) })))
          assert(violations.length === 0, `${context} accessibility violations: ${JSON.stringify(violations)}`)
          axeChecks++
        }
        await page.evaluate(() => scrollTo(0, 0))
        await screenshot(context); layouts.push(context)
      }
    }
    await page.setViewportSize({ width: 1440, height: 900 }); await theme(false)
  }
  const heading = async name => {
    await page.getByRole('heading', { name, exact: true }).waitFor()
    await page.waitForFunction(() => document.activeElement?.id === 'setup-title')
  }
  const reveal = async (label, name) => {
    assert(await field(label).getAttribute('type') === 'password', `${label} must start masked`)
    await button(`Show ${name}`).click()
    assert(await field(label).getAttribute('type') === 'text', `${label} reveal failed`)
    await button(`Hide ${name}`).click()
    assert(await field(label).getAttribute('type') === 'password', `${label} hide failed`)
  }
  const invalid = async (label, text) => {
    await page.getByRole('alert').filter({ hasText: text }).waitFor()
    assert(await field(label).getAttribute('aria-invalid') === 'true', `${label} missing invalid state`)
    assert((await field(label).getAttribute('aria-describedby') || '').split(' ').includes('setup-error'), `${label} missing error description`)
    assert(await field(label).evaluate(node => document.activeElement === node), `${label} validation did not move focus`)
  }
  const pendingReveal = async (path, submit, revealButton) => {
    let release, arrive, finish
    const arrived = new Promise(resolve => { arrive = resolve })
    const handled = new Promise(resolve => { finish = resolve })
    const pattern = `**/api/v1/setup${path}`
    const handler = async route => {
      let error
      try { await new Promise(resolve => { release = resolve; arrive() }); await route.continue() }
      catch (cause) { error = cause }
      finally { finish(error) }
    }
    await page.route(pattern, handler)
    try { await submit.click(); await arrived; assert(await revealButton.isDisabled(), 'Pending setup reveal remained enabled') }
    finally {
      release?.()
      const error = release ? await handled : undefined
      await page.unroute(pattern, handler)
      if (error) throw error
    }
  }
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.setViewportSize({ width: 1440, height: 900 }); await page.reload()
  await heading('Welcome aboard.')
  assert(await button('Continue securely').isDisabled(), 'Empty setup token accepted')
  await layout('token', 'Continue securely')
  await field('Deployment setup token').focus(); await page.keyboard.press('Tab')
  assert(await button('Show setup token').evaluate(node => document.activeElement === node), 'Token reveal missing from keyboard order')
  await page.keyboard.press('Space')
  assert(await field('Deployment setup token').getAttribute('type') === 'text', 'Keyboard reveal failed')
  await page.keyboard.press('Space')
  await field('Deployment setup token').fill('wrong-synthetic-token'); await button('Continue securely').click()
  await page.getByRole('alert').filter({ hasText: 'Setup token was not accepted' }).waitFor()
  await field('Deployment setup token').fill(token)
  await pendingReveal('/verify', button('Continue securely'), button('Show setup token'))
  await heading('Make it yours.')
  assert(await button('Create owner account').isDisabled(), 'Empty owner account accepted')
  await layout('account', 'Create owner account')
  await reveal('Owner password', 'owner password')
  await field('Owner password').fill('short'); await field('Confirm password').fill('short')
  await button('Create owner account').click()
  await invalid('Owner password', 'between 12 and 72 bytes')
  await field('Owner password').fill(password); await field('Confirm password').fill(`${password} mismatch`)
  await button('Create owner account').click()
  await invalid('Confirm password', 'Passwords do not match')
  await field('Confirm password').fill(password)
  await pendingReveal('/account', button('Create owner account'), button('Show owner password'))
  await heading('Connect your storage.')
  passed.push('token gate, keyboard reveal, masked fields, pending reveals disabled, password validation with focus and error associations')

  const telegram = page.getByRole('radio', { name: /^Telegram/ }), local = page.getByRole('radio', { name: /^Local storage/ })
  assert(await telegram.isChecked(), 'Telegram should be initial storage choice')
  assert(await button('Connect Telegram').isDisabled(), 'Empty Telegram details accepted')
  assert(!(await field('Expected channel ID').isVisible()), 'Optional channel ID should be collapsed')
  await layout('storage-telegram', 'Connect Telegram')
  await reveal('Telegram API hash', 'Telegram API hash'); await reveal('Private channel link', 'private channel link')
  await page.getByText('Verify a specific channel ID', { exact: false }).click()
  assert(await field('Expected channel ID').inputValue() === '', 'Optional channel ID should start empty')

  let state = 'scanning', flowActive = false, starts = 0, cancellations = 0, passwords = 0, holdCancel = false, releaseCancel, arriveCancel, finishCancel, routeError
  const inFlight = new Set()
  const pattern = '**/api/v1/setup/telegram**'
  const respond = async route => {
    const request = route.request(), url = new URL(request.url()), method = request.method()
    assert(url.origin === origin && !url.search, 'Telegram setup credentials entered URL')
    assert(request.headers()['x-harbor-setup-token'] === token, 'Setup authorization header missing')
    if (url.pathname.endsWith('/telegram/password') && method === 'POST') {
      assert(request.postDataJSON().password === twoStep, 'Unexpected synthetic two-step submission')
      passwords++; state = 'verified'
      await route.fulfill({ status: 204 }); return
    }
    assert(url.pathname === '/api/v1/setup/telegram', 'Unexpected Telegram setup endpoint')
    if (method === 'POST') {
      const details = request.postDataJSON()
      assert(details.app_id === 12345 && details.app_hash === appHash && details.channel_link === link, 'Unexpected synthetic Telegram details')
      assert(!Object.hasOwn(details, 'channel_id'), 'Blank optional channel ID should be omitted')
      starts++; state = 'scanning'; flowActive = true
    } else if (method === 'DELETE') {
      cancellations++
      try {
        if (holdCancel) await new Promise(resolve => { releaseCancel = resolve; arriveCancel() })
        state = 'waiting'; flowActive = false; await route.fulfill({ status: 204 })
      } finally { finishCancel?.() }
      return
    }
    else assert(method === 'GET', 'Unexpected Telegram setup method')
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ state, flow_active: flowActive, account_ready: true,
      // One transparent pixel: deliberately not a Telegram sign-in credential.
      ...(state === 'scanning' ? { qr_image: 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=' } : {}) }) })
  }
  const handler = route => {
    const operation = respond(route).catch(error => { routeError ||= error }).finally(() => inFlight.delete(operation))
    inFlight.add(operation)
    return operation
  }
  await page.route(pattern, handler)
  try {
    await field('Telegram API ID').fill('not-numeric'); await field('Telegram API hash').fill(appHash); await field('Private channel link').fill(link)
    await button('Connect Telegram').click()
    await page.getByRole('alert').filter({ hasText: 'numeric API ID' }).waitFor()
    assert(await page.locator('#setup-error').evaluate(node => document.activeElement === node), 'Telegram validation summary did not receive focus')
    await field('Telegram API ID').fill('12345'); await field('Telegram API hash').fill('invalid')
    await button('Connect Telegram').click()
    await page.getByRole('alert').filter({ hasText: '32-character API hash' }).waitFor()
    await field('Telegram API hash').fill(appHash); await field('Expected channel ID').fill('not-an-id')
    await button('Connect Telegram').click()
    await page.getByRole('alert').filter({ hasText: 'Channel ID must be a valid numeric' }).waitFor()
    assert(starts === 0, 'Invalid Telegram details reached backend')
    await field('Expected channel ID').fill(''); await button('Connect Telegram').click()
    await page.getByRole('heading', { name: 'Link your dedicated account', exact: true }).waitFor()
    assert(await local.isDisabled(), 'Active Telegram authorization allowed provider change')
    await page.getByAltText('Temporary Telegram sign-in QR code', { exact: true }).waitFor()
    await layout('simulated-qr', 'Cancel Telegram sign-in')
    state = 'waiting'
    const verifyPattern = '**/api/v1/setup/verify'
    let verifyHandled = Promise.resolve(), verifyError
    const verifyHandler = route => {
      verifyHandled = route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ state: 'waiting', flow_active: true, account_ready: true }) }).catch(error => { verifyError = error })
      return verifyHandled
    }
    await page.route(verifyPattern, verifyHandler)
    try {
      await page.reload(); await heading('Welcome aboard.')
      await field('Deployment setup token').fill(token); await button('Continue securely').click()
      await heading('Connect your storage.')
      await button('Cancel Telegram sign-in').waitFor()
      assert(await local.isDisabled(), 'Reloaded waiting authorization allowed provider change')
      await layout('simulated-waiting-reload', 'Cancel Telegram sign-in')
    } finally {
      await verifyHandled; await page.unroute(verifyPattern, verifyHandler)
      if (verifyError) throw verifyError
    }
    holdCancel = true
    const cancelArrived = new Promise(resolve => { arriveCancel = resolve })
    const cancelHandled = new Promise(resolve => { finishCancel = resolve })
    try {
      await button('Cancel Telegram sign-in').click(); await cancelArrived
      assert(await local.isDisabled() && await button('Cancel Telegram sign-in').isDisabled(), 'Cancellation unlocked active flow before response')
    } finally {
      holdCancel = false; releaseCancel?.()
      if (releaseCancel) await cancelHandled
      finishCancel = undefined
    }
    await field('Telegram API ID').waitFor()
    assert(cancellations === 1 && !(await local.isDisabled()), 'Telegram cancellation did not reset provider controls')
    await field('Telegram API ID').fill('12345'); await field('Telegram API hash').fill(appHash); await field('Private channel link').fill(link)
    await button('Connect Telegram').click()
    await page.getByRole('heading', { name: 'Link your dedicated account', exact: true }).waitFor()
    state = 'password'
    await field('Telegram two-step password').waitFor()
    await page.waitForFunction(() => document.activeElement?.id === 'telegram-password')
    assert(await local.isDisabled(), 'Two-step authorization allowed provider change')
    await reveal('Telegram two-step password', 'Telegram two-step password')
    await layout('simulated-two-step', 'Verify password')
    await field('Telegram two-step password').fill(twoStep); await button('Verify password').click()
    await page.getByRole('heading', { name: 'Private channel verified', exact: true }).waitFor()
    await layout('simulated-verified', 'Continue')
    await button('Change account or channel').click()
    await field('Telegram API ID').waitFor()
    assert(cancellations === 2 && passwords === 1 && starts === 2, 'Verified Telegram reset did not cancel its flow')
    passed.push('SIMULATED Telegram validation, optional ID, QR, active waiting flow restored after reload, pending cancellation lock, two-step verification and account reset')
  } finally {
    releaseCancel?.()
    while (inFlight.size) await Promise.all([...inFlight])
    await page.unroute(pattern, handler)
    if (routeError) throw routeError
  }

  await local.check(); await layout('storage-local', 'Use local storage'); await button('Use local storage').click()
  await heading('Review your setup.')
  await page.getByText('Local persistent data volume', { exact: true }).waitFor()
  await page.getByText('Encrypted before storage', { exact: true }).waitFor()
  await layout('review', 'Open Harbor')
  await button('Back to storage').click(); await heading('Connect your storage.')
  assert(await local.isChecked(), 'Storage review lost selected provider')
  await button('Use local storage').click(); await button('Open Harbor').click()
  await page.getByRole('heading', { name: 'Welcome to Harbor', exact: true }).waitFor({ timeout: 65000 })
  await field('Owner password').fill(password); await button('Unlock drive').click()
  await page.getByRole('complementary', { name: 'Main navigation' }).waitFor()
  const final = await page.request.get(`${origin}/api/v1/setup/status`), diagnostics = await page.request.get(`${origin}/api/v1/diagnostics`)
  assert(final.ok() && (await final.json()).required === false, 'Setup remained open after completion')
  assert(diagnostics.ok(), 'Owner login failed')
  const config = await diagnostics.json()
  assert(config.adapter === 'local' && config.chunk_size === 100 * 1024 * 1024, 'Completed local storage configuration mismatch')
  assert(await page.evaluate(values => {
    const stored = [location.href, ...Object.values(localStorage), ...Object.values(sessionStorage)]
    return values.every(value => stored.every(item => !item.includes(value)))
  }, [token, password, twoStep, appHash, link]), 'Setup credentials persisted in browser storage or URL')
  await screenshot('complete')
  passed.push('real local setup completion, storage review/back, owner login, closed setup, 100 MiB default, no credentials in browser storage')
  return { result: 'PASS', passed, telegram: 'SIMULATED ONLY; no Telegram network requests', layouts, accessibility: axeChecks === layouts.length ? 'PASS: axe WCAG A/AA on every layout' : `${axeChecks ? 'PARTIAL' : 'NOT TESTED'}: axe checked ${axeChecks}/${layouts.length} layouts`, screenshots: 'screenshots/onboarding-*.png' }
}
