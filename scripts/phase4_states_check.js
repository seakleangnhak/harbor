// playwright-cli --session NAME run-code --filename scripts/phase4_states_check.js
// Controlled loading/error states on disposable LOCAL fixtures only.
async (page) => {
  const response = await page.request.get(new URL('/api/v1/diagnostics', page.url()).href)
  if (!response.ok() || (await response.json()).adapter !== 'local') throw new Error('Disposable local adapter required')
  const assert = (condition, message) => { if (!condition) throw new Error(message) }, passed = []
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.evaluate(() => localStorage.setItem('harbor:theme', 'light')); await page.reload()
  await page.getByRole('button', { name: 'Design library', exact: true }).click()
  await page.getByRole('button', { name: 'Archive', exact: true }).click()
  await page.getByRole('heading', { name: 'A place for your files', exact: true }).waitFor()
  await page.screenshot({ path: 'docs/screenshots/after-empty.png', animations: 'disabled' })
  const pattern = '**/api/v1/nodes?**', query = `controlled-unavailable-${Date.now()}`
  let release, first = true
  const handler = async route => {
    if (new URL(route.request().url()).searchParams.get('q') !== query) { await route.continue(); return }
    if (first) { first = false; await new Promise(resolve => { release = resolve }) }
    await route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: 'Controlled local service interruption' }) })
  }
  await page.route(pattern, handler)
  try {
    await page.getByRole('searchbox', { name: 'Search your drive', exact: true }).fill(query)
    await page.getByLabel('Loading files', { exact: true }).waitFor()
    await page.screenshot({ path: 'docs/screenshots/after-loading.png', animations: 'disabled' })
    release?.()
    await page.getByRole('heading', { name: "Files couldn't load", exact: true }).waitFor()
    assert(await page.locator('.files-footer').innerText().then(text => text.includes('Files unavailable') && !text.includes('Loading…')), 'Error footer still reports loading')
    await page.screenshot({ path: 'docs/screenshots/after-list-error.png', animations: 'disabled' })
  } finally { release?.(); await page.unroute(pattern, handler) }
  await page.getByRole('button', { name: 'Try again', exact: true }).click()
  await page.getByRole('heading', { name: 'No matching files', exact: true }).waitFor()
  await page.screenshot({ path: 'docs/screenshots/after-no-results.png', animations: 'disabled' })
  passed.push('empty, controlled skeleton/error, retry to no-results')
  await page.getByRole('complementary', { name: 'Main navigation' }).getByRole('button', { name: 'My drive', exact: true }).click()
  await page.getByRole('button', { name: 'Design library', exact: true }).click()
  await page.getByRole('button', { name: 'Filter files: All types', exact: true }).click()
  await page.getByRole('menuitem', { name: 'Images', exact: true }).click()
  await page.getByRole('button', { name: 'Coastline.png', exact: true }).waitFor()
  assert(await page.getByRole('button', { name: 'small.txt', exact: true }).count() === 0, 'Image filter retained text')
  await page.getByRole('button', { name: 'Filter files: Images', exact: true }).click()
  await page.getByRole('menuitem', { name: 'All types', exact: true }).click()
  await page.getByRole('button', { name: 'small.txt', exact: true }).waitFor()
  const trigger = page.getByRole('button', { name: 'Actions for Coastline.png', exact: true })
  await trigger.click(); await page.getByRole('menu').waitFor()
  await page.keyboard.press('ArrowDown'); await page.keyboard.press('Escape')
  await page.getByRole('menu').waitFor({ state: 'hidden' })
  await page.waitForFunction(() => document.activeElement?.getAttribute('aria-label') === 'Actions for Coastline.png')
  assert(await trigger.evaluate(element => document.activeElement === element), 'Menu focus did not return')
  await trigger.click(); await page.getByRole('menuitem', { name: 'Move to…', exact: true }).click()
  await page.getByRole('dialog').waitFor()
  await page.screenshot({ path: 'docs/screenshots/after-move-dialog.png', animations: 'disabled' })
  await page.getByRole('button', { name: 'Cancel', exact: true }).click()
  await page.getByRole('dialog').waitFor({ state: 'hidden' })
  passed.push('type filter, menu Escape focus return, move dialog')
  await page.getByRole('button', { name: 'Collapse navigation', exact: true }).click(); await page.reload()
  await page.getByRole('button', { name: 'Expand navigation', exact: true }).waitFor()
  await page.screenshot({ path: 'docs/screenshots/after-navigation-rail.png', animations: 'disabled' })
  await page.getByRole('button', { name: 'Expand navigation', exact: true }).click()
  await page.setViewportSize({ width: 320, height: 740 })
  assert(!await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), '320px page overflow')
  assert(await page.getByRole('button', { name: 'Open command palette (⌘ K / Ctrl K)', exact: true }).isVisible(), 'Mobile quick actions hidden')
  passed.push('navigation collapse persists and 320px controls fit')
  return { result: 'PASS', passed }
}
