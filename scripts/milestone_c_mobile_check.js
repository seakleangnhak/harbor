// Generated local fixture; hold one browser commit while exercising mobile navigation.
async page => {
  const crypto = require('node:crypto')
  const origin = new URL(page.url()).origin
  const diagnostics = await page.request.get(`${origin}/api/v1/diagnostics`)
  if (!['127.0.0.1', 'localhost', '[::1]'].includes(new URL(origin).hostname) || !diagnostics.ok() || (await diagnostics.json()).adapter !== 'local') throw new Error('Disposable authenticated local adapter required')
  const assert = (value, message) => { if (!value) throw new Error(message) }
  const button = name => page.getByRole('button', { name, exact: true })
  const name = 'Mobile navigation fixture.txt', bytes = Buffer.alloc(256 * 1024, 65)
  let release, seen
  const held = new Promise(resolve => { release = resolve })
  const reached = new Promise(resolve => { seen = resolve })
  const route = async request => { seen(); await held; await request.continue().catch(() => {}) }
  await page.route('**/api/v1/uploads/*/chunks/*/commit', route)
  const queue = page.getByRole('region', { name: 'Transfer queue', exact: true })
  const screenshots = [], checks = []
  let axeChecks = 0
  const shot = async label => {
    const path = `screenshots/mobile-c-${label}.png`
    await page.screenshot({ path, fullPage: true, animations: 'disabled' }); screenshots.push(path)
    assert(await page.evaluate(() => Math.max(document.documentElement.scrollWidth, document.body.scrollWidth) <= innerWidth + 1), `${label} horizontal overflow`)
  }
  try {
    await page.setViewportSize({ width: 390, height: 844 })
    await page.getByLabel('Choose files to upload', { exact: true }).setInputFiles({ name, mimeType: 'text/plain', buffer: bytes })
    let timer
    try { await Promise.race([reached, new Promise((_, reject) => { timer = setTimeout(() => reject(new Error('Browser commit was not observed')), 30000) })]) } finally { clearTimeout(timer) }
    await queue.waitFor()
    const item = queue.locator('.transfer-item').filter({ hasText: name })
    await item.getByText('Saving to storage', { exact: false }).waitFor()
    for (const [width, height] of [[390, 844], [320, 740]]) {
      await page.setViewportSize({ width, height })
      for (const theme of ['dark', 'light']) {
        await button('Open navigation').click()
        const navigation = page.getByRole('dialog', { name: 'Main navigation', exact: true })
        await navigation.waitFor()
        const themeButton = button(`Switch to ${theme} mode`)
        await themeButton.scrollIntoViewIfNeeded()
        const hit = await themeButton.evaluate(element => {
          const box = element.getBoundingClientRect(), top = document.elementFromPoint(box.x + box.width / 2, box.y + box.height / 2)
          return { receives_pointer: element === top || element.contains(top), covered_by: top?.closest('.transfers-panel') ? 'transfers-panel' : top?.tagName || 'none' }
        })
        console.log(JSON.stringify({ mobile_navigation_hit: hit, theme, width }))
        assert(hit.receives_pointer, `Mobile theme button blocked by ${hit.covered_by} while transfer queue is open`)
        await themeButton.click()
        assert(await page.locator('html').evaluate(element => element.classList.contains('dark')) === (theme === 'dark'), 'Mobile theme button did not switch theme')
        await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))))
        await shot(`navigation-${width}-${theme}`)
        const violations = await page.evaluate(async () => (await window.axe.run(document.querySelector('.sidebar[role="dialog"]'), { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa'] } })).violations.map(value => ({ id: value.id, nodes: value.nodes.map(node => ({ target: node.target, summary: node.failureSummary })) })))
        assert(!violations.length, `Mobile navigation accessibility violations: ${JSON.stringify(violations)}`); axeChecks++
        await button('Settings').click()
        await page.getByRole('dialog', { name: 'Your private workspace', exact: true }).waitFor()
        await shot(`settings-${width}-${theme}`)
        await button('Close dialog').click()
        await queue.waitFor()
        checks.push(`${width}px ${theme} theme and Settings remain operable with saving transfer`)
      }
    }
    await button('Open navigation').click()
    const scrimPoint = await button('Close transfer queue').boundingBox()
    assert(scrimPoint, 'Transfer close button geometry missing')
    await page.mouse.click(scrimPoint.x + scrimPoint.width / 2, scrimPoint.y + scrimPoint.height / 2)
    await page.getByRole('dialog', { name: 'Main navigation', exact: true }).waitFor({ state: 'hidden' })
    await queue.waitFor()
    await button('Open navigation').click(); await page.keyboard.press('Escape')
    await button('Open navigation').waitFor()
    assert(await button('Open navigation').evaluate(element => document.activeElement === element), 'Escape did not restore mobile menu focus')
    checks.push('Scrim receives pointer above transfer queue; Escape restores menu focus')
    release()
    await item.getByText('Complete', { exact: true }).waitFor({ timeout: 30000 })
    const listing = await (await page.request.get(`${origin}/api/v1/nodes`)).json(), node = listing.items.find(value => value.name === name)
    assert(node?.sha256 === crypto.createHash('sha256').update(bytes).digest('hex'), 'Fixture did not publish verified checksum')
    await shot('verified-transfer')
    await button('Close transfer queue').click()
    return { result: 'PASS', passed: checks, axe_checks: axeChecks, screenshots }
  } finally { release(); await page.unroute('**/api/v1/uploads/*/chunks/*/commit', route) }
}
