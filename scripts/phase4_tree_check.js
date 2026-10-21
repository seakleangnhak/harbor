// playwright-cli --session NAME run-code --filename scripts/phase4_tree_check.js
// Creates generated metadata/content on disposable LOCAL adapter only.
async (page) => {
  const origin = new URL(page.url()).origin
  const diagnostics = await page.request.get(`${origin}/api/v1/diagnostics`)
  if (!diagnostics.ok() || (await diagnostics.json()).adapter !== 'local') throw new Error('Disposable local adapter required')
  const assert = (condition, message) => { if (!condition) throw new Error(message) }
  const create = async (name, parent_id = '') => { const response = await page.request.post(`${origin}/api/v1/folders`, { data: { name, parent_id } }); assert(response.ok(), 'Fixture folder failed'); return response.json() }
  const list = async parent => (await (await page.request.get(`${origin}/api/v1/nodes?parent_id=${parent}`)).json()).items
  const menu = async (name, action) => { await page.getByRole('button', { name: `Actions for ${name}`, exact: true }).click(); await page.getByRole('menuitem', { name: action, exact: true }).click() }
  const token = Date.now(), root = await create(`Tree acceptance ${token}`)
  const source = await create(`Tree source ${token}`, root.id), nested = await create(`Tree nested ${token}`, source.id)
  const target = await create('Copy destination', root.id), moveTarget = await create('Move destination', root.id)
  const bytes = Buffer.from('Nested encrypted clipboard content.\n'), sha256 = Buffer.from(await crypto.subtle.digest('SHA-256', bytes)).toString('hex')
  const upload = await (await page.request.post(`${origin}/api/v1/uploads`, { data: { name: 'leaf.txt', parent_id: nested.id, size: bytes.length, sha256, mime_type: 'text/plain' } })).json()
  assert((await page.request.put(`${origin}/api/v1/uploads/${upload.id}/chunks/0`, { data: bytes, headers: { 'Content-Type': 'application/octet-stream', 'X-Chunk-SHA256': sha256 } })).ok(), 'Fixture chunk failed')
  assert((await page.request.post(`${origin}/api/v1/uploads/${upload.id}/complete`, { data: {} })).ok(), 'Fixture publish failed')
  for (const node of [source, nested]) assert((await page.request.patch(`${origin}/api/v1/nodes/${node.id}`, { data: { favorite: true } })).ok(), 'Fixture favorite failed')
  await page.setViewportSize({ width: 1440, height: 900 }); await page.reload()
  const navigation = page.getByRole('complementary', { name: 'Main navigation' })
  const selectOverlapping = async action => {
    await navigation.getByRole('button', { name: 'Favorites', exact: true }).click()
    const searchLoaded = page.waitForResponse(response => response.url().includes('/api/v1/nodes?') && new URL(response.url()).searchParams.get('q') === String(token))
    await page.getByRole('searchbox', { name: 'Search your drive' }).fill(String(token))
    await searchLoaded
    await page.locator(`#node-${source.id}`).getByRole('checkbox', { name: `Select ${source.name}`, exact: true }).check()
    await page.locator(`#node-${nested.id}`).getByRole('checkbox', { name: `Select ${nested.name}`, exact: true }).check()
    await page.getByRole('toolbar', { name: 'Selected file actions' }).getByText('2 selected', { exact: true }).waitFor()
    await page.locator(`#node-${source.id}`).getByRole('button', { name: `Actions for ${source.name}`, exact: true }).click()
    await page.getByRole('menuitem', { name: action, exact: true }).click()
  }
  const enter = async name => {
    await navigation.getByRole('button', { name: 'My drive', exact: true }).click()
    await page.getByRole('button', { name: root.name, exact: true }).click()
    await page.getByRole('button', { name, exact: true }).click()
  }
  await selectOverlapping('Copy'); await enter(target.name)
  await page.getByRole('button', { name: 'Paste here', exact: true }).click()
  await page.getByRole('heading', { name: 'All transfers complete', exact: true }).waitFor({ timeout: 30000 })
  await page.getByRole('button', { name: 'Close transfer queue', exact: true }).click()
  const roots = await list(target.id)
  assert(roots.length === 1 && roots[0].name === source.name, 'Overlapping selection duplicated root')
  const children = await list(roots[0].id)
  assert(children.length === 1 && children[0].name === nested.name, 'Nested folder structure lost')
  assert((await list(children[0].id))[0]?.sha256 === sha256, 'Nested copied file checksum mismatch')
  assert((await list(source.id))[0]?.id === nested.id, 'Copy changed original')
  await page.getByRole('button', { name: 'Clear clipboard', exact: true }).click()
  await selectOverlapping('Cut'); await enter(moveTarget.name)
  await page.getByRole('button', { name: 'Paste here', exact: true }).click()
  await page.getByRole('status').filter({ hasText: 'Moved to new folder' }).waitFor()
  const moved = await list(moveTarget.id)
  assert(moved.length === 1 && moved[0].id === source.id, 'Cut flattened overlapping selection')
  assert((await list(source.id))[0]?.id === nested.id && (await list(nested.id))[0]?.sha256 === sha256, 'Cut lost subtree integrity')
  return { result: 'PASS', fixture: root.name, nested_sha256: sha256, checks: ['overlapping copy roots pruned', 'nested file copy checksum', 'original preserved', 'overlapping cut preserves subtree'] }
}
