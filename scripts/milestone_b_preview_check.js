// Generated fixtures only. Local-adapter browser timings do not establish cold
// Telegram latency; upload verification has already warmed the OS file cache.
async (page, restartServer) => {
  const fs = require('node:fs'), crypto = require('node:crypto')
  const origin = new URL(page.url()).origin
  if (!['127.0.0.1', 'localhost', '[::1]'].includes(new URL(origin).hostname)) throw new Error('Loopback fixture required')
  const diagnostics = await page.request.get(`${origin}/api/v1/diagnostics`)
  if (!diagnostics.ok() || (await diagnostics.json()).adapter !== 'local') throw new Error('Disposable local adapter required')
  const assert = (value, message) => { if (!value) throw new Error(message) }
  const button = name => page.getByRole('button', { name, exact: true })
  const samples = {}, screenshots = [], errors = [], ranges = [], requestFailures = [], actions = []
  const mark = action => actions.push({ milliseconds: Date.now(), action })
  let axeChecks = 0
  const hash = bytes => crypto.createHash('sha256').update(bytes).digest('hex')
  const measure = async (name, action) => { mark(name); const start = Date.now(); await action(); (samples[name] ||= []).push(Date.now() - start) }
  const watch = response => { if (/\/nodes\/[^/]+\/content$/.test(new URL(response.url()).pathname)) ranges.push({ id: new URL(response.url()).pathname.split('/')[4], status: response.status(), range: response.headers()['content-range'] || null }) }
  const onError = error => { errors.push({ stack: error.stack || error.message, recent_actions: actions.slice(-12) }); fs.writeFileSync('suite-checks.json', JSON.stringify({ result: 'FAIL', browser_errors: errors })) }
  const onFailed = request => { if (/\/nodes\/[^/]+\/content$/.test(new URL(request.url()).pathname)) requestFailures.push({ id: new URL(request.url()).pathname.split('/')[4], error: request.failure()?.errorText || 'unknown' }) }
  const folderResponse = await page.request.post(`${origin}/api/v1/folders`, { data: { name: 'Milestone B previews', parent_id: '' } })
  assert(folderResponse.status() === 201, 'Preview folder creation failed')
  const folder = await folderResponse.json()
  const upload = async (name, bytes) => {
    const digest = hash(bytes)
    const response = await page.request.post(`${origin}/api/v1/uploads`, { data: { name, parent_id: folder.id, size: bytes.length, mime_type: 'application/octet-stream', sha256: digest } })
    assert(response.status() === 201, `Fixture ${name} begin failed`)
    const upload = await response.json()
    for (let offset = 0, index = 0; offset < bytes.length; offset += upload.chunk_size, index++) {
      const chunk = bytes.subarray(offset, offset + upload.chunk_size)
      assert((await page.request.put(`${origin}/api/v1/uploads/${upload.id}/chunks/${index}`, { data: chunk, headers: { 'Content-Type': 'application/octet-stream', 'X-Chunk-SHA256': hash(chunk) } })).ok(), `Fixture ${name} chunk failed`)
    }
    const complete = await page.request.post(`${origin}/api/v1/uploads/${upload.id}/complete`, { data: {} })
    assert(complete.ok(), `Fixture ${name} verification failed`)
    const node = await complete.json()
    assert(node.sha256 === digest, `Fixture ${name} manifest checksum failed`)
    return node
  }
  const picture = async (mime, width, height) => Buffer.from(await page.evaluate(async ({ mime, width, height }) => {
    const canvas = document.createElement('canvas'); canvas.width = width; canvas.height = height
    const context = canvas.getContext('2d'), gradient = context.createLinearGradient(0, 0, width, height)
    gradient.addColorStop(0, '#f4ebce'); gradient.addColorStop(1, '#1f5657'); context.fillStyle = gradient; context.fillRect(0, 0, width, height)
    for (let i = 0; i < 120; i++) { context.fillStyle = `hsl(${(i * 13) % 360} 35% 50%)`; context.fillRect((i * 67) % width, (i * 43) % height, 70, 65) }
    context.fillStyle = '#f8f3dc'; context.font = `${Math.floor(width / 18)}px sans-serif`; context.fillText('Harbor generated preview', width / 20, height / 2)
    const blob = await new Promise(resolve => canvas.toBlob(resolve, mime, .9))
    return Array.from(new Uint8Array(await blob.arrayBuffer()))
  }, { mime, width, height }))
  const png = await upload('01 Coastline.png', await picture('image/png', 1280, 800))
  const jpeg = await upload('02 Large harbor.jpeg', await picture('image/jpeg', 4096, 3072))
  const objects = ['<< /Type /Catalog /Pages 2 0 R >>', '<< /Type /Pages /Kids [4 0 R 6 0 R 8 0 R 10 0 R] /Count 4 >>', '<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>']
  for (let i = 0; i < 4; i++) {
    const stream = `BT /F1 24 Tf 40 700 Td (Harbor generated PDF page ${i + 1}) Tj ET\n`
    objects.push(`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 3 0 R >> >> /Contents ${5 + i * 2} 0 R >>`, `<< /Length ${stream.length} >>\nstream\n${stream}endstream`)
  }
  const padding = '% Generated harmless PDF padding for byte-range qualification\n'.repeat(18000)
  objects.push(`<< /Length ${padding.length} >>\nstream\n${padding}endstream`)
  let source = '%PDF-1.4\n', offsets = []
  for (const [index, object] of objects.entries()) { offsets.push(source.length); source += `${index + 1} 0 obj\n${object}\nendobj\n` }
  const xref = source.length
  source += `xref\n0 ${objects.length + 1}\n0000000000 65535 f \n${offsets.map(offset => `${String(offset).padStart(10, '0')} 00000 n \n`).join('')}trailer\n<< /Size ${objects.length + 1} /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF\n`
  const pdf = await upload('03 Harbor guide.pdf', Buffer.from(source))
  const videos = []
  for (const [name, path] of [['04 Seeking proof.webm', '/checks/seek.webm'], ['05 Native codec.mov', '/checks/seek.mov']]) {
    assert(fs.existsSync(path), `Required generated video missing: ${path}`)
    videos.push(await upload(name, fs.readFileSync(path)))
  }
  const text = await upload('06 Cancel preview.txt', Buffer.alloc(1024 * 1024, 'A'))
  await page.evaluate(() => localStorage.setItem('harbor:thumbnails', 'false'))
  await page.reload(); await button(folder.name).click(); await page.getByRole('heading', { name: folder.name, exact: true }).waitFor()
  await button('List view').click()
  page.on('response', watch); page.on('pageerror', onError); page.on('requestfailed', onFailed)
  const close = async () => { mark('Close preview'); await button('Close dialog').click(); await page.getByRole('dialog').waitFor({ state: 'hidden' }) }
  const open = async node => { mark(`Open ${node.name}`); await button(node.name).click(); await page.getByRole('dialog', { name: node.name, exact: true }).waitFor() }
  const imageReady = async () => { await page.waitForFunction(() => { const image = document.querySelector('img.image-canvas'); return image?.complete && image.naturalWidth > 0 && getComputedStyle(image).visibility !== 'hidden' }) }
  const pdfReady = async pageNumber => {
    await page.locator('.pdf-canvas-scroll[aria-busy="false"]').waitFor()
    await page.getByRole('img', { name: `${pdf.name}, page ${pageNumber}`, exact: true }).waitFor()
    await page.locator('.pdf-canvas-scroll .sr-only').filter({ hasText: `Harbor generated PDF page ${pageNumber}` }).waitFor()
    assert(!await page.locator('.pdf-viewer [role="alert"]').count(), 'PDF renderer reported error')
  }
  const shot = async label => {
    const path = `screenshots/milestone-b-${label}.png`
    await page.screenshot({ path, fullPage: true, animations: 'disabled' }); screenshots.push(path)
    assert(await page.evaluate(() => Math.max(document.documentElement.scrollWidth, document.body.scrollWidth) <= innerWidth + 1), `${label} overflow`)
    const violations = await page.evaluate(async () => (await window.axe.run(document, { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa'] } })).violations.map(item => ({ id: item.id, targets: item.nodes.map(node => node.target) })))
    assert(!violations.length, `${label} axe violations: ${JSON.stringify(violations)}`); axeChecks++
  }
  const videoResults = []
  try {
    for (const [label, node] of [['png', png], ['jpeg-12mp', jpeg]]) {
      for (let attempt = 0; attempt < 5; attempt++) {
        await measure(`${label}-${attempt ? 'repeat' : 'first'}-open`, async () => { await open(node); await imageReady() })
        if (node.id === jpeg.id) assert(await page.locator('img.image-canvas').evaluate(image => image.naturalWidth === 4096 && image.naturalHeight === 3072), 'Large JPEG dimensions changed')
        if (attempt === 0) await shot(label)
        await close()
      }
    }
    for (let attempt = 0; attempt < 5; attempt++) {
      await measure(`pdf-${attempt ? 'repeat' : 'first'}-page1`, async () => { await open(pdf); await pdfReady(1) })
      await measure('pdf-next-page', async () => { await button('Next PDF page').click(); await pdfReady(2) })
      await measure('pdf-page4', async () => { await page.getByLabel('PDF page', { exact: true }).fill('4'); await page.getByLabel('PDF page', { exact: true }).press('Enter'); await pdfReady(4) })
      if (attempt === 0) await shot('pdf-page4')
      await close()
    }
    assert(ranges.some(item => item.id === pdf.id && item.status === 206), 'Multipage PDF did not exercise HTTP206')
    for (const node of videos) {
      const result = { name: node.name, playback: 'NOT TESTED', seek: 'NOT TESTED' }
      await measure(`${node.name.endsWith('webm') ? 'webm' : 'mov'}-metadata`, async () => { await open(node); await page.waitForFunction(() => document.querySelector('video')?.readyState >= 1 || !!document.querySelector('.preview-stage [role="alert"]')) })
      if (!await page.locator('video').count()) {
        result.playback = 'UNSUPPORTED'; result.seek = 'UNSUPPORTED'
        assert((await page.getByRole('alert').innerText()).includes('MOV playback depends') || node.name.endsWith('webm'), 'Unsupported codec lacks guidance')
      } else {
        await measure(`${node.name.endsWith('webm') ? 'webm' : 'mov'}-play`, async () => {
          await page.locator('video').evaluate(async element => { element.muted = true; await element.play() })
          await page.waitForFunction(() => { const video = document.querySelector('video'); return video && !video.paused && video.currentTime > .2 })
        })
        result.playback = 'PASS'
        await measure(`${node.name.endsWith('webm') ? 'webm' : 'mov'}-seek`, async () => {
          await page.locator('video').evaluate(element => { element.pause(); element.currentTime = 4 })
          await page.waitForFunction(() => { const video = document.querySelector('video'); return video && !video.seeking && video.readyState >= 2 && Math.abs(video.currentTime - 4) < .1 })
        })
        result.seek = 'PASS'; result.duration = await page.locator('video').evaluate(element => element.duration)
        assert(ranges.some(item => item.id === node.id && item.status === 206), 'Video did not use HTTP206')
        await shot(node.name.endsWith('webm') ? 'webm-seek' : 'mov-seek')
      }
      videoResults.push(result); await close()
    }
    assert(videoResults.some(result => result.playback === 'PASS' && result.seek === 'PASS'), 'No supported real video was qualified')
    for (let attempt = 0; attempt < 4; attempt++) { await open(pdf); await close(); await open(png); await imageReady(); await close() }
    const delayed = `**/api/v1/nodes/${text.id}/content`
    let textRequestSeen
    const textRequest = new Promise(resolve => { textRequestSeen = resolve })
    await page.route(delayed, async route => { textRequestSeen(); await new Promise(resolve => setTimeout(resolve, 300)); await route.continue().catch(() => {}) })
    try { await open(text); await textRequest; await close(); await open(jpeg); await imageReady(); await close() } finally { await page.unroute(delayed) }
    const bytes = Buffer.alloc(16 * 1024 * 1024, 91), uploadName = 'Mixed preview upload.bin'
    await page.route('**/api/v1/uploads/*/chunks/*/parts/*', async route => { await new Promise(resolve => setTimeout(resolve, 150)); await route.continue().catch(() => {}) })
    try {
      await page.locator('input[type="file"]').first().setInputFiles({ name: uploadName, mimeType: 'application/octet-stream', buffer: bytes })
      await open(pdf); await pdfReady(1); await button('Next PDF page').click(); await pdfReady(2); await close()
      let node
      for (let attempt = 0; attempt < 600; attempt++) {
        const listing = await (await page.request.get(`${origin}/api/v1/nodes?parent_id=${folder.id}&limit=50000`)).json()
        node = listing.items.find(item => item.name === uploadName)
        if (node) break
        await new Promise(resolve => setTimeout(resolve, 100))
      }
      assert(node, `Mixed browser upload did not publish within60seconds: ${(await page.locator('.transfer-item').allTextContents()).join('; ')}`)
      assert(node.sha256 === hash(bytes), 'Mixed upload published wrong digest')
      const download = await page.request.get(`${origin}/api/v1/nodes/${node.id}/content`)
      assert(download.ok() && hash(await download.body()) === node.sha256, 'Mixed upload download digest mismatch')
    } finally { await page.unroute('**/api/v1/uploads/*/chunks/*/parts/*') }
    await button('Close transfer queue').click()
    await button('Switch to dark mode').click()
    await open(pdf); await pdfReady(1); await shot('pdf-dark'); await close()
    await page.setViewportSize({ width: 390, height: 844 }); await open(jpeg); await imageReady(); await shot('image-mobile-dark'); await close()
    await button('Open navigation').click(); await button('Switch to light mode').click(); await button('Close navigation').click()
    await open(pdf); await pdfReady(1); await button('Next PDF page').click(); await pdfReady(2); await shot('pdf-mobile-light'); await close()
    await restartServer(); await page.reload(); await page.getByRole('heading', { name: 'My drive', exact: true }).waitFor(); await button(folder.name).click()
    await open(pdf); await pdfReady(1); await close()
    assert(!errors.length, `Uncaught browser errors: ${JSON.stringify(errors)}`)
    const percentiles = Object.fromEntries(Object.entries(samples).map(([name, values]) => { const sorted = [...values].sort((a, b) => a - b); return [name, { samples_ms: values, p50_ms: sorted[Math.ceil(sorted.length * .5) - 1], p95_ms: sorted[Math.ceil(sorted.length * .95) - 1], count: values.length }] }))
    return { result: 'PASS', adapter: 'local', timing_scope: 'UI first/repeat open after verified upload; OS cache may be warm; not cold Telegram', previews: { png: 'PASS', jpeg_pixels: 4096 * 3072, pdf_pages: 4, pdf_navigation: 'PASS', repeated_pdf_cycles: 5, rapid_navigation_cycles: 4, text_cancellation: 'PASS', mixed_upload_preview_sha256: 'PASS', restart_preview: 'PASS' }, video: videoResults, latency: percentiles, content_responses: ranges.length, range206: ranges.filter(item => item.status === 206).length, failed_or_cancelled_content_requests: requestFailures, browser_exceptions: errors.length, screenshots, axe_checks: axeChecks }
  } finally { page.off('response', watch); page.off('pageerror', onError); page.off('requestfailed', onFailed) }
}
