// Disposable authenticated LOCAL adapter; requires axe source at output/phase5/axe.min.js.
async (page) => {
  const origin = new URL(page.url()).origin
  const diagnostics = await page.request.get(`${origin}/api/v1/diagnostics`)
  if (!diagnostics.ok() || (await diagnostics.json()).adapter !== 'local') throw new Error('Disposable local adapter required')
  const assert = (value, message) => { if (!value) throw new Error(message) }
  const evidence = []
  const axeURL = `${origin}/__phase5_axe.js`
  await page.route(axeURL, route => route.fulfill({ status: 200, contentType: 'application/javascript', path: 'output/phase5/axe.min.js' }))
  const audit = async name => {
    await page.addScriptTag({ url: axeURL })
    const result = await page.evaluate(async () => {
      const result = await window.axe.run(document, { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa'] } })
      return { violations: result.violations.map(item => ({ id: item.id, impact: item.impact, nodes: item.nodes.map(node => node.target) })), passes: result.passes.length, overflow: document.documentElement.scrollWidth > innerWidth }
    })
    assert(!result.violations.length && !result.overflow, JSON.stringify({ name, ...result }))
    evidence.push({ name, ...result })
  }
  for (const theme of ['light', 'dark']) {
    await page.setViewportSize({ width: 1440, height: 900 })
    await page.evaluate(theme => localStorage.setItem('harbor:theme', theme), theme)
    await page.reload()
    await page.getByRole('complementary', { name: 'Main navigation' }).getByRole('button', { name: 'Settings', exact: true }).click()
    const settings = page.getByRole('dialog', { name: 'Your private workspace', exact: true })
    await settings.getByText('Backup & recovery', { exact: true }).waitFor()
    assert((await settings.innerText()).includes('password-encrypted backup'), 'Encrypted backup guidance absent')
    assert(!await settings.getByRole('button', { name: /Download.*backup/ }).count(), 'Plaintext backup control remains')
    const blocked = await page.request.get(`${origin}/api/v1/backup`)
    assert(blocked.status() === 410, 'Plaintext backup endpoint still available')
    await page.screenshot({ path: `docs/screenshots/phase5-settings-${theme}.png`, animations: 'disabled' })
    await audit(`settings-${theme}`)
    await page.getByRole('button', { name: 'Close dialog', exact: true }).click()
    await page.getByRole('button', { name: 'Transfer queue', exact: true }).click()
    await page.getByRole('region', { name: 'Transfer queue', exact: true }).getByText('Folder copies', { exact: false }).waitFor()
    await page.screenshot({ path: `docs/screenshots/phase5-copy-history-${theme}.png`, animations: 'disabled' })
    await audit(`copy-history-${theme}`)
    await page.getByRole('button', { name: 'Close transfer queue', exact: true }).click()
  }
  const pattern = '**/api/v1/copy-jobs'
  await page.route(pattern, route => route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: 'Controlled local interruption' }) }))
  try {
    await page.getByRole('button', { name: 'Transfer queue', exact: true }).click()
    await page.getByRole('alert').filter({ hasText: 'Folder copy status unavailable' }).waitFor({ timeout: 20000 })
    await page.getByRole('heading', { name: 'Transfer status unavailable', exact: true }).waitFor()
    await audit('copy-status-unavailable')
    await page.screenshot({ path: 'docs/screenshots/phase5-copy-status-error.png', animations: 'disabled' })
  } finally { await page.unroute(pattern) }
  await page.getByRole('button', { name: 'Refresh copy status', exact: true }).click()
  await page.getByRole('alert').filter({ hasText: 'Folder copy status unavailable' }).waitFor({ state: 'hidden' })
  await page.getByRole('button', { name: 'Close transfer queue', exact: true }).click()
  await page.reload()
  await page.getByRole('button', { name: 'Transfer queue', exact: true }).click()
  await page.getByRole('region', { name: 'Transfer queue', exact: true }).getByText('Folder copies', { exact: false }).waitFor()
  await page.getByRole('button', { name: 'Close transfer queue', exact: true }).click()
  await page.unroute(axeURL)
  return { result: 'PASS', checks: ['encrypted recovery guidance', 'plaintext export rejected', 'persistent job history survives reload', 'status interruption truthful and accessible', 'refresh recovery'], evidence }
}
