// playwright-cli --session NAME run-code --filename scripts/phase4_preview_check.js
// Requires generated Phase 4 fixtures in Design library on disposable local adapter.
async (page) => {
  const diagnostics = await page.request.get(new URL('/api/v1/diagnostics', page.url()).href)
  if (!diagnostics.ok() || (await diagnostics.json()).adapter !== 'local') throw new Error('Disposable local adapter required')
  const assert = (condition, message) => { if (!condition) throw new Error(message) }, passed = []
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.reload()
  await page.getByRole('button', { name: 'Design library', exact: true }).click()
  const close = async () => { await page.getByRole('button', { name: 'Close dialog', exact: true }).click(); await page.getByRole('dialog').waitFor({ state: 'hidden' }) }
  await page.getByRole('button', { name: 'Coastline.png', exact: true }).click()
  await page.locator('img.image-canvas').evaluate(image => image.decode())
  await page.getByRole('button', { name: 'Zoom in image', exact: true }).click()
  assert(await page.getByRole('button', { name: 'Reset image to fit', exact: true }).innerText() === '125%', 'Image zoom failed')
  const stage = page.getByRole('group', { name: 'Image canvas. Zoom in and use arrow keys to pan. Home fits the image.', exact: true })
  await stage.focus(); await stage.press('ArrowRight')
  assert((await page.locator('img.image-canvas').getAttribute('style')).includes('-40px'), 'Keyboard pan failed')
  await stage.press('Home')
  await page.getByRole('button', { name: 'Expand preview', exact: true }).click()
  assert(await page.locator('.preview-fullscreen').count() === 1, 'Preview expansion failed')
  await page.screenshot({ path: 'docs/screenshots/after-image-preview.png', animations: 'disabled' })
  assert(await page.getByRole('navigation', { name: 'Nearby files', exact: true }).count() === 1, 'Nearby navigation missing')
  await close(); passed.push('image decode, zoom, expanded preview, nearby navigation')

  await page.getByRole('button', { name: 'Harbor guide.pdf', exact: true }).click()
  await page.locator('.pdf-canvas-scroll[aria-busy="false"]').waitFor()
  assert(await page.getByRole('img', { name: 'Harbor guide.pdf, page 1', exact: true }).evaluate(canvas => canvas.width > 0), 'PDF did not render')
  await page.getByRole('button', { name: 'Fit whole PDF page', exact: true }).click()
  await page.getByRole('button', { name: 'Zoom in PDF', exact: true }).click()
  assert(await page.getByRole('button', { name: 'Reset PDF zoom', exact: true }).innerText() === '125%', 'PDF zoom failed')
  await page.getByRole('textbox', { name: 'PDF page', exact: true }).fill('99')
  await page.getByRole('textbox', { name: 'PDF page', exact: true }).press('Enter')
  assert(await page.getByRole('textbox', { name: 'PDF page', exact: true }).inputValue() === '1', 'Invalid PDF page not reset')
  await page.locator('.pdf-canvas-scroll[aria-busy="false"]').waitFor()
  await page.screenshot({ path: 'docs/screenshots/after-pdf-preview.png', animations: 'disabled' })
  await close(); passed.push('PDF rendering, fit, zoom, page validation')

  await page.getByRole('button', { name: 'Harbor browser notes.txt', exact: true }).click()
  await page.getByLabel('Text preview', { exact: true }).waitFor()
  assert((await page.getByLabel('Text preview', { exact: true }).innerText()).includes('Harbor'), 'Text preview empty')
  await page.screenshot({ path: 'docs/screenshots/after-text-preview.png', animations: 'disabled' })
  await close(); passed.push('bounded text preview')

  const ranges = []
  page.on('response', response => { if (response.url().includes('/content') && response.request().headers()['range']) ranges.push(response.status()) })
  await page.getByRole('button', { name: 'Seeking proof.mp4', exact: true }).click()
  await page.waitForFunction(() => document.querySelector('video')?.readyState >= 1)
  const duration = await page.locator('video').evaluate(video => { video.currentTime = Math.min(4, video.duration / 2); return video.duration })
  await page.waitForFunction(() => { const video = document.querySelector('video'); return video && !video.seeking && video.currentTime > 0 })
  assert(duration > 0 && ranges.includes(206), 'Video seek/HTTP byte range failed')
  await page.screenshot({ path: 'docs/screenshots/after-video-preview.png', animations: 'disabled' })
  await close(); passed.push('native video metadata, seek and HTTP 206')

  await page.getByRole('button', { name: 'Actions for Coastline.png', exact: true }).click()
  const downloading = page.waitForEvent('download')
  await page.getByRole('menuitem', { name: 'Download', exact: true }).click()
  const download = await downloading
  await download.saveAs('output/phase4/browser-Coastline.png')
  await page.getByRole('heading', { name: 'All transfers complete', exact: true }).waitFor()
  await page.screenshot({ path: 'docs/screenshots/after-transfers.png', animations: 'disabled' })
  await page.getByRole('button', { name: 'Close transfer queue', exact: true }).click()
  passed.push('managed verified browser download')
  return { result: 'PASS', passed, video_duration: duration, video_range_statuses: ranges }
}
