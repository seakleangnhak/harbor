// Explicit qualification: creates ONE generated fixture on verified loopback Telegram drive.
// playwright-cli --session NAME run-code --filename scripts/phase4_live_check.js
async (page) => {
  if (new URL(page.url()).origin !== 'http://127.0.0.1:8081') throw new Error('Verified Harbor loopback runtime required')
  const response = await page.request.get('http://127.0.0.1:8081/api/v1/diagnostics')
  const diagnostics = await response.json()
  if (!response.ok() || diagnostics.adapter !== 'telegram' || diagnostics.storage_connection?.state !== 'connected') throw new Error('Verified connected Telegram adapter required')
  const name = `phase4-live-${Date.now()}.txt`, text = 'Harbor Phase 4 live encrypted Telegram browser qualification.\n'.repeat(2048)
  const buffer = Buffer.from(text)
  const expected = Buffer.from(await crypto.subtle.digest('SHA-256', buffer)).toString('hex')
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.getByLabel('Choose files to upload', { exact: true }).setInputFiles([{ name, mimeType: 'text/plain', buffer }])
  await page.getByRole('heading', { name: 'All transfers complete', exact: true }).waitFor({ timeout: 60000 })
  await page.getByRole('button', { name, exact: true }).waitFor()
  const listing = await page.request.get(`http://127.0.0.1:8081/api/v1/nodes?q=${encodeURIComponent(name)}`)
  const nodes = (await listing.json()).items
  if (nodes.length !== 1 || nodes[0].sha256 !== expected || nodes[0].size !== buffer.length) throw new Error('Published Telegram metadata checksum mismatch')
  await page.screenshot({ path: 'docs/screenshots/after-live-telegram.png', animations: 'disabled' })
  await page.getByRole('button', { name: 'Close transfer queue', exact: true }).click()
  await page.getByRole('button', { name, exact: true }).click()
  await page.getByLabel('Text preview', { exact: true }).waitFor()
  if (await page.getByLabel('Text preview', { exact: true }).textContent() !== text) throw new Error('Telegram text preview mismatch')
  await page.getByRole('button', { name: 'Close dialog', exact: true }).click()
  await page.getByRole('dialog').waitFor({ state: 'hidden' })
  await page.getByRole('button', { name: `Actions for ${name}`, exact: true }).click()
  const pending = page.waitForEvent('download')
  await page.getByRole('menuitem', { name: 'Download', exact: true }).click()
  const download = await pending
  await download.saveAs('output/phase4/live-download.txt')
  await page.getByRole('heading', { name: 'All transfers complete', exact: true }).waitFor()
  const ranged = await page.request.get(`http://127.0.0.1:8081/api/v1/nodes/${nodes[0].id}/content`, { headers: { Range: 'bytes=1-32' } })
  if (ranged.status() !== 206 || !(await ranged.body()).equals(buffer.subarray(1, 33))) throw new Error('Telegram HTTP Range mismatch')
  return { result: 'PASS', adapter: 'telegram', fixture: name, node_id: nodes[0].id, bytes: buffer.length, sha256: expected, preview: 'exact', range: 206, download: 'output/phase4/live-download.txt' }
}
