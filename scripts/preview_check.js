// playwright-cli --session NAME run-code --filename scripts/preview_check.js
// Authenticated disposable LOCAL adapter only; generated fixtures are retained.
// Run in writable /scratch with screenshots/ available. Optional /checks/seek.webm
// verifies seeking; /checks/seek.mov qualifies browser-specific MOV decoding.
async (page) => {
  const origin = new URL(page.url()).origin
  if (!['127.0.0.1', 'localhost', '[::1]'].includes(new URL(origin).hostname)) throw new Error('Loopback fixture required')
  const diagnostics = await page.request.get(`${origin}/api/v1/diagnostics`)
  if (!diagnostics.ok() || (await diagnostics.json()).adapter !== 'local') throw new Error('Authenticated disposable local adapter required')
  const assert = (condition, message) => { if (!condition) throw new Error(message) }
  const passed = [], screenshots = [], pageErrors = [], consoleErrors = [], ranges = [], requests = new Map()
  const button = name => page.getByRole('button', { name, exact: true })
  const close = async () => { await button('Close dialog').click(); await page.getByRole('dialog').waitFor({ state: 'hidden' }) }
  const watch = request => { const match = new URL(request.url()).pathname.match(/^\/api\/v1\/nodes\/([^/]+)\/content$/); if (match && request.method() === 'GET') requests.set(match[1], (requests.get(match[1]) || 0) + 1) }
  const error = cause => pageErrors.push(cause.message)
  const consoleError = message => { if (message.type() === 'error') consoleErrors.push(message.text()) }
  const ranged = response => { if (new URL(response.url()).pathname.endsWith('/content') && response.request().headers()['range']) ranges.push({ id: new URL(response.url()).pathname.split('/')[4], status: response.status() }) }
  let axeChecks = 0, videoResult = 'NOT TESTED', videoDuration = 0, movResult = 'NOT TESTED', thumbnailPeakRequests = 0
  const shot = async name => {
    await page.mouse.move(0, 0)
    await page.evaluate(() => { const element = document.activeElement; if (element instanceof HTMLElement && element.getAttribute('aria-label') === 'Open navigation') element.blur() })
    await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))))
    const path = `screenshots/preview-${name}.png`
    await page.screenshot({ path, animations: 'disabled', fullPage: true })
    assert(await page.evaluate(() => Math.max(document.documentElement.scrollWidth, document.body.scrollWidth) <= innerWidth + 1), `${name} horizontal overflow`)
    if (await page.evaluate(() => !!window.axe)) {
      const violations = await page.evaluate(async () => (await window.axe.run(document, { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa'] } })).violations.map(item => ({ id: item.id, targets: item.nodes.map(node => node.target), summaries: item.nodes.map(node => node.failureSummary) })))
      assert(!violations.length, `${name} accessibility violations: ${JSON.stringify(violations)}`); axeChecks++
    }
    screenshots.push(path)
  }
  const theme = async dark => {
    if (await page.evaluate(() => document.documentElement.classList.contains('dark')) === dark) return
    const toggle = button(dark ? 'Switch to dark mode' : 'Switch to light mode')
    const mobile = !(await toggle.isVisible())
    if (mobile) await button('Open navigation').click()
    await toggle.click()
    if (mobile) await button('Close navigation').click()
    assert(await page.evaluate(() => document.documentElement.classList.contains('dark')) === dark, 'Theme switch failed')
  }
  const rootListing = await (await page.request.get(`${origin}/api/v1/nodes?parent_id=&limit=50000`)).json()
  let folderName = 'Preview fixtures', suffix = 1
  while (rootListing.items.some(node => node.name === folderName)) folderName = `Preview fixtures ${++suffix}`
  const created = await page.request.post(`${origin}/api/v1/folders`, { data: { name: folderName, parent_id: '' } })
  assert(created.status() === 201, 'Preview fixture folder creation failed')
  const folder = await created.json()
  const upload = (name, bytes) => page.evaluate(async ({ parent_id, name, bytes }) => {
    const data = new Uint8Array(bytes)
    const hash = async value => Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256', value))).map(byte => byte.toString(16).padStart(2, '0')).join('')
    const json = async (path, body) => {
      const response = await fetch(`/api/v1${path}`, { method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
      if (!response.ok) throw new Error(`Fixture API ${path} failed (${response.status})`)
      return response.json()
    }
    const sha256 = await hash(data), session = await json('/uploads', { name, parent_id, size: data.length, mime_type: 'application/octet-stream', sha256 })
    if (!Number.isSafeInteger(session.chunk_size) || session.chunk_size <= 0) throw new Error('Invalid fixture chunk size')
    for (let offset = 0, index = 0; offset < data.length; offset += session.chunk_size, index++) {
      const chunk = data.slice(offset, offset + session.chunk_size)
      const response = await fetch(`/api/v1/uploads/${encodeURIComponent(session.id)}/chunks/${index}`, { method: 'PUT', credentials: 'same-origin', headers: { 'Content-Type': 'application/octet-stream', 'X-Chunk-SHA256': await hash(chunk) }, body: chunk })
      if (!response.ok) throw new Error(`Fixture chunk ${index} failed (${response.status})`)
    }
    const node = await json(`/uploads/${encodeURIComponent(session.id)}/complete`, {})
    const response = await fetch(`/api/v1/nodes/${encodeURIComponent(node.id)}/content`, { credentials: 'same-origin' })
    if (!response.ok || await hash(await response.arrayBuffer()) !== sha256 || node.sha256 !== sha256) throw new Error('Fixture encrypted roundtrip checksum failed')
    return node
  }, { parent_id: folder.id, name, bytes })
  const image = mime => page.evaluate(async mime => {
    const canvas = document.createElement('canvas'); canvas.width = 960; canvas.height = 600
    const context = canvas.getContext('2d')
    context.fillStyle = mime === 'image/png' ? '#d8e8e7' : '#eadfc9'; context.fillRect(0, 0, 960, 600)
    context.fillStyle = '#426c6b'; context.beginPath(); context.moveTo(0, 390); context.lineTo(210, 285); context.lineTo(420, 350); context.lineTo(660, 225); context.lineTo(960, 315); context.lineTo(960, 600); context.lineTo(0, 600); context.fill()
    context.fillStyle = '#213e41'; context.beginPath(); context.moveTo(0, 485); context.lineTo(290, 370); context.lineTo(590, 445); context.lineTo(800, 350); context.lineTo(960, 400); context.lineTo(960, 600); context.lineTo(0, 600); context.fill()
    context.fillStyle = '#f7f1db'; context.beginPath(); context.arc(745, 125, 51, 0, Math.PI * 2); context.fill()
    context.fillStyle = '#faf7ec'; context.fillRect(170, 190, 35, 180); context.fillRect(160, 175, 55, 22)
    context.fillStyle = '#213e41'; context.beginPath(); context.moveTo(155, 173); context.lineTo(188, 144); context.lineTo(220, 173); context.fill()
    const blob = await new Promise(resolve => canvas.toBlob(resolve, mime, .88))
    return Array.from(new Uint8Array(await blob.arrayBuffer()))
  }, mime)
  const png = await upload('Coastline.png', await image('image/png'))
  const jpeg = await upload('Harbor light.jpeg', await image('image/jpeg'))
  const pdfText = 'BT /F1 18 Tf 35 150 Td (Harbor preview verification) Tj ET'
  const pdfObjects = [
    '<< /Type /Catalog /Pages 2 0 R >>',
    '<< /Type /Pages /Kids [3 0 R] /Count 1 >>',
    '<< /Type /Page /Parent 2 0 R /MediaBox [0 0 320 220] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>',
    '<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>',
    `<< /Length ${pdfText.length} >>\nstream\n${pdfText}\nendstream`,
  ]
  let pdfSource = '%PDF-1.4\n'
  const offsets = []
  for (const [index, object] of pdfObjects.entries()) { offsets.push(pdfSource.length); pdfSource += `${index + 1} 0 obj\n${object}\nendobj\n` }
  const xref = pdfSource.length
  pdfSource += `xref\n0 ${pdfObjects.length + 1}\n0000000000 65535 f \n${offsets.map(offset => `${String(offset).padStart(10, '0')} 00000 n \n`).join('')}trailer\n<< /Size ${pdfObjects.length + 1} /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF\n`
  const pdf = await upload('Harbor guide.pdf', Array.from(new TextEncoder().encode(pdfSource)))
  const text = await upload('Preview notes.txt', Array.from(new TextEncoder().encode('Harbor preview verification\n\nAll content in this folder is generated for isolated browser testing.\n')))
  const heic = await upload('Camera format.HEIC', Array.from(new TextEncoder().encode('Synthetic unsupported HEIC fixture; no personal photograph.')))
  const mov = await upload('Unsupported clip.mov', Array.from(new TextEncoder().encode('Synthetic unsupported MOV fixture; no personal video.')))
  const wav = Buffer.alloc(44 + 16000)
  wav.write('RIFF'); wav.writeUInt32LE(wav.length - 8, 4); wav.write('WAVEfmt ', 8)
  wav.writeUInt32LE(16, 16); wav.writeUInt16LE(1, 20); wav.writeUInt16LE(1, 22)
  wav.writeUInt32LE(8000, 24); wav.writeUInt32LE(16000, 28); wav.writeUInt16LE(2, 32); wav.writeUInt16LE(16, 34)
  wav.write('data', 36); wav.writeUInt32LE(16000, 40)
  for (let sample = 0; sample < 8000; sample++) wav.writeInt16LE(Math.round(Math.sin(sample * Math.PI * 2 * 440 / 8000) * 4000), 44 + sample * 2)
  const audio = await upload('Harbor tone.wav', Array.from(wav))
  let video, nativeMov
  try { if (typeof require === 'function') video = await upload('Seeking proof.webm', Array.from(require('node:fs').readFileSync('/checks/seek.webm'))) }
  catch (cause) { if (!['ENOENT', 'EACCES'].includes(cause.code)) throw cause }
  try { if (typeof require === 'function') nativeMov = await upload('Native codec proof.mov', Array.from(require('node:fs').readFileSync('/checks/seek.mov'))) }
  catch (cause) { if (!['ENOENT', 'EACCES'].includes(cause.code)) throw cause }
  passed.push('generated PNG/JPEG, PDF, text and unsupported media uploaded with generic MIME; encrypted SHA-256 roundtrips')
  page.on('request', watch); page.on('response', ranged); page.on('pageerror', error); page.on('console', consoleError)
  const enter = async () => { await button(folder.name).click(); await page.getByRole('heading', { name: folder.name, exact: true }).waitFor(); await button('Grid view').click() }
  const goRoot = () => page.getByRole('navigation', { name: 'Folder breadcrumb', exact: true }).getByRole('button', { name: 'My drive', exact: true }).click()
  const open = async node => { await button(node.name).click(); await page.getByRole('dialog', { name: node.name, exact: true }).waitFor() }
  const decoded = () => page.waitForFunction(() => { const image = document.querySelector('img.image-canvas'); return image?.complete && image.naturalWidth > 0 && getComputedStyle(image).visibility !== 'hidden' })
  try {
    await page.setViewportSize({ width: 1440, height: 900 }); await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.evaluate(() => localStorage.removeItem('harbor:thumbnails')); await page.reload()
    const imageIds = new Set([png.id, jpeg.id, heic.id]), thumbnailPattern = '**/api/v1/nodes/*/content'
    let thumbnailRequests = 0
    const delayedThumbnail = async route => {
      if (!imageIds.has(new URL(route.request().url()).pathname.split('/')[4])) return route.continue()
      thumbnailRequests++; thumbnailPeakRequests = Math.max(thumbnailPeakRequests, thumbnailRequests)
      let response
      try { response = await route.fetch({ maxRedirects: 0 }); await new Promise(resolve => setTimeout(resolve, 160)) }
      finally { thumbnailRequests-- }
      await route.fulfill({ response })
    }
    await page.route(thumbnailPattern, delayedThumbnail)
    try {
      await enter(); await theme(false)
      assert(await button('Hide thumbnails').getAttribute('aria-pressed') === 'true', 'Grid thumbnails should default on')
      for (const node of [png, jpeg]) await page.locator(`#node-${node.id} img.thumbnail-image-loaded`).waitFor()
      await page.locator(`#node-${heic.id} .thumbnail-label`).filter({ hasText: 'Open to preview' }).waitFor()
      assert(thumbnailPeakRequests === 2, `Thumbnail concurrency expected 2; observed ${thumbnailPeakRequests}`)
      passed.push('three delayed image originals exercise thumbnail queue; measured peak concurrent requests is 2')
    } finally { await page.unroute(thumbnailPattern, delayedThumbnail) }
    assert(!requests.get(mov.id) && (!video || !requests.get(video.id)) && (!nativeMov || !requests.get(nativeMov.id)), 'Grid eagerly downloaded video content')
    await shot('grid-desktop-light')
    await button('Hide thumbnails').click(); assert(await page.locator('img.thumbnail-image').count() === 0, 'Hide thumbnails retained image fetch elements')
    await goRoot(); await enter(); assert(await button('Show thumbnails').getAttribute('aria-pressed') === 'false', 'Thumbnail preference lost on navigation')
    await page.reload(); await enter(); assert(await button('Show thumbnails').getAttribute('aria-pressed') === 'false', 'Thumbnail preference lost on reload')
    await button('Show thumbnails').click(); await page.reload(); await enter(); await page.locator(`#node-${png.id} img.thumbnail-image-loaded`).waitFor()
    passed.push('default-on lazy raster thumbnails, no eager videos, hide/show persistence after navigation and reload')

    await open(png); await decoded(); await button('Zoom in image').click()
    assert(await button('Reset image to fit').innerText() === '125%', 'Image zoom failed')
    const stage = page.getByRole('group', { name: 'Image canvas. Zoom in and use arrow keys to pan. Home fits the image.', exact: true })
    await stage.focus(); await stage.press('ArrowRight')
    assert((await page.locator('img.image-canvas').getAttribute('style')).includes('-40px'), 'Image keyboard pan failed')
    await stage.press('Home'); await button('Expand preview').click(); await shot('image-desktop-light'); await close()
    await button('Hide thumbnails').click()
    const retryImage = await upload('Retry network.png', await image('image/png'))
    await page.reload(); await enter()
    const pattern = `**/api/v1/nodes/${retryImage.id}/content`
    const abort = route => route.abort('failed')
    await page.route(pattern, abort, { times: 1 })
    try {
      await open(retryImage); await page.getByRole('alert').filter({ hasText: 'Preview could not open' }).waitFor()
      await button('Retry preview').click(); await decoded()
    } finally { await page.unroute(pattern, abort) }
    await close(); await open(jpeg); await decoded(); await close()
    passed.push('generic-MIME PNG/JPEG previews, zoom, keyboard pan, fullscreen, injected image failure and retry')

    await open(text); await page.getByLabel('Text preview', { exact: true }).waitFor()
    assert((await page.getByLabel('Text preview', { exact: true }).innerText()).includes('Harbor preview verification'), 'Generic-MIME text preview failed')
    await shot('text-desktop-light'); await close(); passed.push('bounded text preview using filename fallback')
    await open(pdf); await page.locator('.pdf-canvas-scroll[aria-busy="false"]').waitFor()
    const pdfError = page.locator('.pdf-viewer [role="alert"]')
    assert(!await pdfError.count(), `PDF rendering failed: ${await pdfError.allTextContents()}`)
    assert(await page.getByRole('img', { name: `${pdf.name}, page 1`, exact: true }).evaluate(canvas => canvas.width > 0 && canvas.height > 0), 'Generic-MIME PDF canvas did not render')
    await page.locator('.pdf-canvas-scroll .sr-only').filter({ hasText: 'Harbor preview verification' }).waitFor()
    const pdfWidth = await page.locator('.pdf-canvas-scroll canvas').evaluate(canvas => canvas.width)
    await button('Zoom in PDF').click()
    await page.waitForFunction(width => document.querySelector('.pdf-canvas-scroll canvas')?.width > width, pdfWidth)
    await page.locator('.pdf-canvas-scroll[aria-busy="false"]').waitFor()
    assert(await button('Reset PDF zoom').innerText() === '125%', 'PDF zoom failed')
    await button('Fit whole PDF page').click(); await button('Fit PDF to width').waitFor()
    await page.locator('.pdf-canvas-scroll[aria-busy="false"]').waitFor()
    await page.locator('.pdf-canvas-scroll .sr-only').filter({ hasText: 'Harbor preview verification' }).waitFor()
    await shot('pdf-desktop-light'); await close(); passed.push('generated generic-MIME PDF canvas, accessible text, zoom and fit-page rerender')
    for (const [node, guidance] of [[heic, 'HEIC/HEIF support varies'], [mov, 'MOV playback depends']]) {
      await open(node); await page.getByRole('alert').filter({ hasText: guidance }).waitFor()
      assert(await button('Download original').isVisible(), 'Unsupported format missing download fallback')
      const previousRetry = await button('Retry preview').elementHandle()
      await button('Retry preview').click()
      await page.waitForFunction(element => !element.isConnected, previousRetry)
      await page.getByRole('alert').filter({ hasText: guidance }).waitFor(); await shot(node === heic ? 'heic-fallback' : 'mov-fallback'); await close()
    }
    passed.push('HEIC and unsupported MOV expose codec guidance, retry remount and original-download action; native media may reuse cached bytes')
    await open(audio); await page.waitForFunction(() => document.querySelector('audio')?.readyState >= 1)
    assert(await page.locator('audio').evaluate(element => element.duration > 0 && element.hasAttribute('src')), 'Native audio metadata or source missing')
    await page.locator('audio').focus(); await page.keyboard.press('ArrowRight')
    assert(await page.getByRole('dialog', { name: audio.name, exact: true }).isVisible(), 'Native audio controls navigated to another file')
    await close(); passed.push('generic-MIME WAV native metadata, persistent media source and keyboard isolation')
    if (nativeMov) {
      await open(nativeMov)
      await page.waitForFunction(() => document.querySelector('video')?.readyState >= 1 || !!document.querySelector('.preview-stage [role="alert"]'))
      if (await page.locator('video').count()) {
        assert(await page.locator('video').evaluate(element => { element.currentTime = Math.min(1, element.duration / 2); return element.duration > 0 }), 'MOV metadata missing')
        await page.waitForFunction(() => !!document.querySelector('.preview-stage [role="alert"]') || (() => { const element = document.querySelector('video'); return element && element.readyState >= 2 && !element.seeking && element.currentTime > 0 })())
      }
      if (await page.locator('video').count()) {
        movResult = 'PASS'
      } else {
        assert((await page.getByRole('alert').innerText()).includes('MOV playback depends'), 'MOV failure lacked codec guidance')
        movResult = 'UNSUPPORTED'
      }
      await close()
    }
    if (video) {
      await open(video); await page.waitForFunction(() => document.querySelector('video')?.readyState >= 1)
      videoDuration = await page.locator('video').evaluate(element => { element.currentTime = Math.min(1, element.duration / 2); return element.duration })
      await page.waitForFunction(() => { const element = document.querySelector('video'); return element && element.readyState >= 2 && !element.seeking && element.currentTime > 0 })
      assert(videoDuration > 0 && ranges.some(range => range.id === video.id && range.status === 206), 'WebM filename fallback video seek or native HTTP 206 failed')
      await shot('video-seeking'); await close(); videoResult = 'PASS'; passed.push('generated generic-MIME WebM video metadata, seeking and browser HTTP 206')
    }
    await button('Show thumbnails').click(); await page.locator(`#node-${png.id} img.thumbnail-image-loaded`).waitFor()
    await theme(true); await shot('grid-desktop-dark'); await open(jpeg); await decoded(); await shot('image-desktop-dark'); await close()
    await page.setViewportSize({ width: 390, height: 844 }); await shot('grid-mobile-dark'); await open(png); await decoded(); await shot('image-mobile-dark'); await close()
    await theme(false); await shot('grid-mobile-light'); await open(png); await decoded(); await shot('image-mobile-light'); await close()
    for (const [width, height, name] of [[320, 740, 'grid-phone-narrow-light'], [768, 1024, 'grid-tablet-light'], [1024, 768, 'grid-laptop-light']]) {
      await page.setViewportSize({ width, height }); await shot(name)
    }
    assert(!pageErrors.length, `Unexpected browser exception: ${pageErrors.join(' | ')}`)
    passed.push('desktop and mobile light/dark screenshots, no horizontal overflow or uncaught browser exceptions')
    return { result: 'PASS', passed, fixture_folder: folder.name, screenshots, axe_checks: axeChecks, browser_exceptions: pageErrors.length, expected_resource_console_errors: consoleErrors.length, thumbnail_peak_requests: thumbnailPeakRequests, video_seek: videoResult, video_duration: videoDuration, mov_playback: movResult, video_range_statuses: video ? ranges.filter(range => range.id === video.id).map(range => range.status) : [], sha256: { png: png.sha256, jpeg: jpeg.sha256, pdf: pdf.sha256, text: text.sha256 } }
  } finally {
    page.off('request', watch); page.off('response', ranged); page.off('pageerror', error); page.off('console', consoleError)
  }
}
