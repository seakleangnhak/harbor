// playwright-cli --session NAME run-code --filename scripts/transport_check.js
// Authenticated disposable LOCAL adapter only. Run in writable /scratch.
async (page, restartServer) => {
  const fs = require('node:fs'), crypto = require('node:crypto')
  const origin = new URL(page.url()).origin
  if (!['127.0.0.1', 'localhost', '[::1]'].includes(new URL(origin).hostname)) throw new Error('Loopback fixture required')
  const diagnostics = await page.request.get(`${origin}/api/v1/diagnostics`)
  if (!diagnostics.ok() || (await diagnostics.json()).adapter !== 'local') throw new Error('Authenticated disposable local adapter required')
  const assert = (condition, message) => { if (!condition) throw new Error(message) }
  const deferred = () => { let resolve; const promise = new Promise(done => { resolve = done }); return { promise, resolve } }
  const hold = () => ({ seen: deferred(), release: deferred() })
  const reached = (gate, label) => new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error(`Timed out waiting for ${label}`)), 120000)
    gate.seen.promise.then(() => { clearTimeout(timer); resolve() })
  })
  const pausedPart = hold(), firstCommit = hold(), secondChunk = hold(), secondCommit = hold()
  const partSize = 4 * 1024 * 1024, chunkSize = 100 * 1024 * 1024, size = chunkSize + 1024 * 1024 + 123
  const name = 'Resumable transport.bin', fixture = `${require('node:process').cwd()}/${name}`
  const block = Buffer.alloc(partSize)
  for (let index = 0; index < block.length; index++) block[index] = (index * 31 + (index >>> 10) + 7) & 255
  const hash = crypto.createHash('sha256'), file = fs.openSync(fixture, 'wx', 0o600)
  try {
    for (let offset = 0; offset < size; offset += partSize) {
      block.writeUInt32LE(offset / partSize, 0)
      const bytes = block.subarray(0, Math.min(partSize, size - offset))
      fs.writeSync(file, bytes); hash.update(bytes)
    }
  } finally { fs.closeSync(file) }
  const expectedDigest = hash.digest('hex')
  const folderResponse = await page.request.post(`${origin}/api/v1/folders`, { data: { name: `Transport fixtures ${Date.now()}`, parent_id: '' } })
  assert(folderResponse.status() === 201, 'Fixture folder creation failed')
  const folder = await folderResponse.json(), button = name => page.getByRole('button', { name, exact: true })
  const passed = [], screenshots = [], attempts = new Map(), commits = [], bodies = [], pageErrors = [], progress = []
  let uploadId, injectedFailure = false, injectedCommitFailure = false, completionRequests = 0, legacyRequests = 0, axeChecks = 0
  const queue = page.getByRole('region', { name: 'Transfer queue', exact: true })
  const item = () => queue.locator('.transfer-item').filter({ has: page.locator('strong').filter({ hasText: name }) })
  const bar = () => item().getByRole('progressbar')
  const session = async () => {
    assert(uploadId, 'Browser upload session not observed')
    const response = await page.request.get(`${origin}/api/v1/uploads/${uploadId}`)
    assert(response.ok(), `Upload state unavailable (${response.status()})`)
    return response.json()
  }
  const unpublished = async () => {
    const response = await page.request.get(`${origin}/api/v1/nodes?parent_id=${encodeURIComponent(folder.id)}`)
    assert(response.ok() && !(await response.json()).items.some(node => node.name === name), 'Incomplete upload became a visible file')
    assert(completionRequests === 0, 'Client finalized before all storage chunks committed')
  }
  const shot = async label => {
    await page.mouse.move(0, 0)
    await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))))
    const path = `screenshots/transport-${label}.png`
    await page.screenshot({ path, fullPage: true, animations: 'disabled' })
    assert(await page.evaluate(() => Math.max(document.documentElement.scrollWidth, document.body.scrollWidth) <= innerWidth + 1), `${label} horizontal overflow`)
    if (label.startsWith('saving-') && await page.evaluate(() => !!window.axe)) {
      const violations = await page.evaluate(async () => (await window.axe.run(document, { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa'] } })).violations.map(item => ({ id: item.id, targets: item.nodes.map(node => node.target), summaries: item.nodes.map(node => node.failureSummary) })))
      assert(!violations.length, `${label} accessibility violations: ${JSON.stringify(violations)}`)
      axeChecks++
    }
    screenshots.push(path)
  }
  const collectProgress = async () => progress.push(...await page.evaluate(() => window.__harborTransportQA || []))
  const observe = request => {
    const path = new URL(request.url()).pathname
    if (/\/uploads\/[^/]+\/complete$/.test(path) && request.method() === 'POST') completionRequests++
    if (/\/uploads\/[^/]+\/chunks\/\d+$/.test(path) && request.method() === 'PUT') legacyRequests++
  }
  const routePart = async route => {
    const request = route.request(), match = new URL(request.url()).pathname.match(/\/uploads\/([^/]+)\/chunks\/(\d+)\/parts\/(\d+)$/)
    if (request.method() !== 'PUT' || !match) return route.continue()
    uploadId ||= match[1]
    assert(uploadId === match[1], 'Unexpected second upload session')
    const key = `${match[2]}:${match[3]}`, attempt = (attempts.get(key) || 0) + 1
    attempts.set(key, attempt)
    const body = request.postDataBuffer()
    // WebKit omits Blob request bytes from Playwright; inspect native send instead.
    const sent = body ? { size: body.length, digest: crypto.createHash('sha256').update(body).digest('hex') } : await page.evaluate(async path => {
      const records = window.__harborTransportBodies || []
      const record = records.findLast(item => new URL(item.url, location.origin).pathname === path)
      return record ? { size: record.size, digest: await record.digest } : null
    }, new URL(request.url()).pathname)
    assert(sent && sent.size > 0 && sent.size <= partSize, `Oversized client part ${key}: ${sent?.size}`)
    assert(request.headers()['x-part-sha256'] === sent.digest, `Part ${key} digest differs from transmitted bytes`)
    bodies.push(sent.size)
    if (key === '0:2' && !injectedFailure) { injectedFailure = true; return route.abort('failed') }
    if (key === '0:4' && attempt === 1) { pausedPart.seen.resolve(); await pausedPart.release.promise; return route.abort('failed').catch(() => {}) }
    if (key === '1:0') { secondChunk.seen.resolve(); await secondChunk.release.promise }
    return route.continue()
  }
  const routeCommit = async route => {
    const request = route.request(), match = new URL(request.url()).pathname.match(/\/uploads\/([^/]+)\/chunks\/(\d+)\/commit$/)
    if (request.method() !== 'POST' || !match) return route.continue()
    commits.push(Number(match[2]))
    assert((request.postDataBuffer()?.length || 0) < 1024, 'Commit request contains file data')
    const gate = match[2] === '0' ? firstCommit : secondCommit
    gate.seen.resolve(); await gate.release.promise
    if (match[2] === '0' && !injectedCommitFailure) {
      injectedCommitFailure = true
      return route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: 'Synthetic temporary storage failure', code: 'storage_unavailable' }) })
    }
    return route.continue()
  }
  await page.addInitScript(() => {
    window.__harborTransportQA = []
    window.__harborTransportBodies = []
    const open = XMLHttpRequest.prototype.open, send = XMLHttpRequest.prototype.send
    XMLHttpRequest.prototype.open = function(method, url, ...options) { this.__transportURL = String(url); return open.call(this, method, url, ...options) }
    XMLHttpRequest.prototype.send = function(body) {
      if (/\/chunks\/\d+\/parts\/\d+$/.test(this.__transportURL || '')) {
        const url = this.__transportURL
        const digest = body.arrayBuffer().then(bytes => crypto.subtle.digest('SHA-256', bytes)).then(value => Array.from(new Uint8Array(value)).map(byte => byte.toString(16).padStart(2, '0')).join(''))
        window.__harborTransportBodies.push({ url, size: body.size, digest })
        this.upload.addEventListener('progress', event => window.__harborTransportQA.push({ url, loaded: event.loaded, total: event.total, computable: event.lengthComputable }))
      }
      return send.call(this, body)
    }
  })
  const error = cause => pageErrors.push(cause.message)
  page.on('request', observe); page.on('pageerror', error)
  await page.route('**/api/v1/uploads/*/chunks/*/parts/*', routePart)
  await page.route('**/api/v1/uploads/*/chunks/*/commit', routeCommit)
  try {
    await page.setViewportSize({ width: 1440, height: 900 }); await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.reload(); await button(folder.name).click()
    await page.getByRole('heading', { name: folder.name, exact: true }).waitFor()
    await page.getByLabel('Choose files to upload', { exact: true }).setInputFiles(fixture)
    await reached(pausedPart, 'pause boundary')
    let state = await session()
    assert(state.chunk_size === chunkSize && state.part_size === partSize && state.total_chunks === 2, 'Storage/client geometry differs from contract')
    assert(state.received_bytes === 0 && state.received_chunks.length === 0, 'Staged parts counted as stored')
    assert(state.staged_chunk === 0 && state.staged_bytes === 4 * partSize && [0, 1, 2, 3].every(index => state.staged_parts.includes(index)), 'First four parts not durably staged')
    assert(injectedFailure && attempts.get('0:2') === 2, 'Failed 4 MiB part was not automatically retried once')
    await unpublished(); await shot('uploading-desktop')
    await button(`Pause ${name}`).click(); pausedPart.release.resolve()
    await item().getByText('Paused', { exact: false }).waitFor()
    await collectProgress()
    const acceptedAttempts = new Map(attempts)
    if (restartServer) await restartServer()
    await page.reload(); await button('Transfer queue').click()
    await button(`Select original ${name}`).waitFor()
    state = await session()
    assert(state.staged_bytes === 4 * partSize && state.received_bytes === 0, 'Reload lost accepted parts')
    const chooseOriginal = page.waitForEvent('filechooser')
    await button(`Select original ${name}`).click()
    await (await chooseOriginal).setFiles(fixture)
    await reached(firstCommit, 'first storage commit')
    for (const index of [0, 1, 2, 3]) assert(attempts.get(`0:${index}`) === acceptedAttempts.get(`0:${index}`), `Resume resent accepted part ${index}`)
    state = await session()
    assert(state.received_bytes === 0 && state.staged_bytes === chunkSize && state.staged_parts.length === 25, 'First commit did not assemble 25 staged parts')
    await item().getByText('Saving to storage', { exact: false }).waitFor()
    assert(Number(await bar().getAttribute('aria-valuenow')) < 100, 'Staged upload falsely reports complete progress')
    assert((await item().innerText()).includes('0 B stored'), 'Transfer failed to distinguish accepted and stored bytes')
    await unpublished(); await shot('saving-first-chunk')
    const stagedAttempts = new Map(attempts)
    passed.push('4 MiB client parts, native XHR progress, one injected part retry, pause/reload/reselect retaining accepted parts')
    firstCommit.release.resolve()
    await reached(secondChunk, 'second storage chunk')
    state = await session()
    assert(state.received_bytes === chunkSize && state.received_chunks.length === 1 && state.received_chunks[0] === 0, 'First storage chunk not committed before second chunk')
    assert(injectedCommitFailure && commits.join(',') === '0,0', 'Storage commit was not retried after transient 503')
    for (let part = 0; part < 25; part++) assert(attempts.get(`0:${part}`) === stagedAttempts.get(`0:${part}`), `Commit retry resent accepted part ${part}`)
    await unpublished(); secondChunk.release.resolve()
    await reached(secondCommit, 'final storage commit')
    state = await session()
    assert(state.received_bytes === chunkSize && state.staged_chunk === 1 && state.staged_bytes === size - chunkSize, 'Final staged part or committed progress incorrect')
    await item().getByText('Saving to storage', { exact: false }).waitFor()
    assert(Number(await bar().getAttribute('aria-valuenow')) < 100, 'Last staged bytes falsely report complete progress')
    await unpublished(); await page.setViewportSize({ width: 390, height: 844 }); await shot('saving-mobile')
    secondCommit.release.resolve()
    await item().getByText(/^Complete(?: · \d+ retr(?:y|ies))?$/).waitFor({ timeout: 60000 })
    assert(Number(await bar().getAttribute('aria-valuenow')) === 100, 'Verified completion did not reach 100%')
    state = await session()
    assert(state.status === 'complete' && state.received_bytes === size && state.received_chunks.join(',') === '0,1', 'Final manifest geometry or state incorrect')
    assert(commits.join(',') === '0,0,1' && legacyRequests === 0 && completionRequests === 1, 'Unexpected whole-chunk upload or completion ordering')
    const listing = await (await page.request.get(`${origin}/api/v1/nodes?parent_id=${encodeURIComponent(folder.id)}`)).json()
    const node = listing.items.find(node => node.name === name)
    assert(node?.sha256 === expectedDigest && node.size === size, 'Published metadata checksum differs')
    const downloaded = await page.request.get(`${origin}/api/v1/nodes/${node.id}/content?download=1`, { timeout: 60000 })
    assert(downloaded.ok() && crypto.createHash('sha256').update(await downloaded.body()).digest('hex') === expectedDigest, 'Encrypted multipart download SHA-256 differs')
    await collectProgress()
    assert(progress.some(event => event.computable && event.loaded > 0 && event.total <= partSize), 'No native client upload progress events observed')
    assert(!pageErrors.length, `Uncaught browser exceptions: ${pageErrors.join(' | ')}`)
    await shot('complete-mobile')
    passed.push('separate accepted/stored progress during delayed commits; transient HTTP503 commit retry without resending accepted parts; unpublished until verification; two storage chunks; encrypted SHA-256 equality')
    if (restartServer) passed.push('real server process stop/start with persisted encrypted staged parts; resumed without resending accepted parts')
    return { result: 'PASS', adapter: 'local', live_telegram: 'NOT TESTED', restart_tested: !!restartServer, passed, size, chunk_size: chunkSize, part_size: partSize, storage_chunks: state.received_chunks.length, max_client_body: Math.max(...bodies), client_requests: bodies.length, part_attempts: Object.fromEntries(attempts), storage_commits: commits, native_progress_events: progress.length, sha256: expectedDigest, screenshots, axe_checks: axeChecks, browser_exceptions: pageErrors.length }
  } finally {
    for (const gate of [pausedPart, firstCommit, secondChunk, secondCommit]) gate.release.resolve()
    await page.unroute('**/api/v1/uploads/*/chunks/*/parts/*', routePart)
    await page.unroute('**/api/v1/uploads/*/chunks/*/commit', routeCommit)
    page.off('request', observe); page.off('pageerror', error)
  }
}
