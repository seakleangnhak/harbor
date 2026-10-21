// playwright-cli --session NAME run-code --filename scripts/phase4_large_check.js
// Authenticated disposable LOCAL adapter only. Generated fixtures stay retained.
async (page) => {
  const origin = new URL(page.url()).origin
  if (!['127.0.0.1', 'localhost', '[::1]'].includes(new URL(origin).hostname)) throw new Error('Loopback fixture required')
  const diagnostics = await page.request.get(`${origin}/api/v1/diagnostics`)
  if (!diagnostics.ok() || (await diagnostics.json()).adapter !== 'local') throw new Error('Disposable local adapter required')
  const assert = (condition, message) => { if (!condition) throw new Error(message) }
  const create = async (name, parent_id = '') => {
    const response = await page.request.post(`${origin}/api/v1/folders`, { data: { name, parent_id } })
    assert(response.status() === 201, 'Generated folder creation failed'); return response.json()
  }
  const list = async parent => {
    const response = await page.request.get(`${origin}/api/v1/nodes?parent_id=${encodeURIComponent(parent)}&limit=50000`)
    assert(response.ok(), 'Generated folder listing failed'); return response.json()
  }
  const stamp = Date.now(), root = await create(`Large directory ${stamp}`), tiny = await create(`Drag acceptance ${stamp}`)
  const created = new Array(1000), creationStart = Date.now()
  let cursor = 0
  await Promise.all(Array.from({ length: 4 }, async () => {
    for (;;) { const index = cursor++; if (index >= 1000) return; created[index] = await create(`Folder ${String(index).padStart(4, '0')}`, root.id) }
  }))
  const creationMs = Date.now() - creationStart
  assert((await list(root.id)).total === 1000, 'Expected 1000 persisted folders')
  const alpha = await create('Drag alpha', tiny.id), beta = await create('Drag beta', tiny.id), target = await create('Drop target', tiny.id)
  const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aL1sAAAAASUVORK5CYII=', 'base64')
  const upload = await page.request.post(`${origin}/api/v1/uploads`, { data: { name: 'Thumbnail probe.png', parent_id: tiny.id, size: png.length, mime_type: 'image/png', sha256: '' } })
  assert(upload.status() === 201, 'Thumbnail fixture upload failed')
  const session = await upload.json()
  assert((await page.request.put(`${origin}/api/v1/uploads/${session.id}/chunks/0`, { headers: { 'Content-Type': 'application/octet-stream' }, data: png })).ok(), 'Thumbnail fixture chunk failed')
  assert((await page.request.post(`${origin}/api/v1/uploads/${session.id}/complete`, { data: {} })).ok(), 'Thumbnail fixture publication failed')
  let contentRequests = 0
  const watch = request => { if (request.method() === 'GET' && /\/api\/v1\/nodes\/[^/]+\/content(?:\?|$)/.test(request.url())) contentRequests++ }
  const settle = () => page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))))
  const enter = async folder => {
    await page.reload(); await page.getByRole('button', { name: folder.name, exact: true }).click()
    await page.getByRole('heading', { name: folder.name, exact: true }).waitFor()
  }
  await page.setViewportSize({ width: 1440, height: 900 }); page.on('request', watch)
  try {
    const loadStart = Date.now(); await enter(root)
    await page.getByRole('button', { name: 'List view', exact: true }).click()
    await page.waitForFunction(() => document.querySelector('.files-footer > span')?.textContent?.startsWith('1000 items'))
    const loadMs = Date.now() - loadStart, listRows = await page.locator('.file-row').count()
    assert(listRows > 0 && listRows < 100, `List virtualization rendered ${listRows} rows`)
    await page.locator('.file-row').first().focus(); await page.keyboard.press('End')
    await page.waitForFunction(id => document.activeElement?.id === `node-${id}`, created[999].id)
    assert(await page.locator('.file-row').count() < 100, 'End navigation expanded all rows')
    await page.getByRole('button', { name: 'Grid view', exact: true }).click(); await settle()
    const gridCounts = [], scroll = page.locator('.file-scroll')
    for (const fraction of [0, 0.5, 1]) {
      await scroll.evaluate((element, part) => { element.scrollTop = (element.scrollHeight - element.clientHeight) * part }, fraction)
      await settle()
      const boxes = await page.locator('.file-card').evaluateAll(cards => cards.map(card => { const r = card.getBoundingClientRect(); return { x: r.x, y: r.y, width: r.width, height: r.height } }))
      gridCounts.push(boxes.length); assert(boxes.length > 0 && boxes.length < 150, 'Grid DOM exceeded its virtual window')
      for (let i = 0; i < boxes.length; i++) for (let j = i + 1; j < boxes.length; j++) {
        const a = boxes[i], b = boxes[j]
        assert(Math.min(a.x + a.width, b.x + b.width) - Math.max(a.x, b.x) <= 1 || Math.min(a.y + a.height, b.y + b.height) - Math.max(a.y, b.y) <= 1, 'Virtual grid cards overlap')
      }
    }
    const last = await page.locator(`#node-${created[999].id}`).boundingBox(), viewport = await scroll.boundingBox()
    assert(last && viewport && last.y >= viewport.y - 1 && last.y + last.height <= viewport.y + viewport.height + 1, 'Last grid item unreachable')
    await enter(tiny); await page.getByRole('button', { name: 'List view', exact: true }).click()
    await page.getByRole('checkbox', { name: 'Select Drag alpha', exact: true }).check()
    await page.getByRole('button', { name: 'Drag beta', exact: true }).click({ modifiers: ['Meta'] })
    await page.getByRole('toolbar', { name: 'Selected file actions' }).getByText('2 selected', { exact: true }).waitFor()
    await page.locator(`#node-${alpha.id}`).dragTo(page.locator(`#node-${target.id}`))
    await page.getByRole('status').filter({ hasText: 'Moved to new folder' }).waitFor()
    const moved = (await list(target.id)).items
    assert(moved.length === 2 && moved.some(node => node.id === alpha.id) && moved.some(node => node.id === beta.id), 'Native drag failed selected-sibling move')
    await page.getByRole('button', { name: 'Grid view', exact: true }).click(); await settle()
    assert(await page.getByRole('button', { name: 'Show thumbnails', exact: true }).getAttribute('aria-pressed') === 'false', 'Thumbnails unexpectedly enabled')
    assert(contentRequests === 0, 'Grid automatically fetched file content')
    return { result: 'PASS', folders: 1000, creation_workers: 4, creation_ms: creationMs, reload_and_load_ms: loadMs, rendered_list_rows: listRows, rendered_grid_cards: gridCounts, end_keyboard: 'PASS', grid_overlap: 'NONE', native_selected_drag: 'PASS', automatic_content_requests: contentRequests, fixture_ids: [root.id, tiny.id] }
  } finally { page.off('request', watch) }
}
