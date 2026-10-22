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
  const passed = [], screenshots = [], attempts = new Map(), commits = [], queueResponses = [], bodies = [], pageErrors = [], progress = []
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
  const observeResponse = response => {
    if (/\/uploads\/[^/]+\/chunks\/\d+\/commit$/.test(new URL(response.url()).pathname)) queueResponses.push(response.status())
  }
  const acceptedBytes = state => state.received_bytes + (state.pending_chunks || []).reduce((sum, chunk) => sum + chunk.bytes, 0)
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
    assert(request.postDataJSON()?.background === true, 'Browser must use durable background saving')
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
  page.on('request', observe); page.on('response', observeResponse); page.on('pageerror', error)
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
    assert(acceptedBytes(state) === chunkSize && state.max_pending_chunks === 2, 'First chunk must remain durably accepted while next chunk starts')
    assert(state.received_chunks.includes(0) || state.pending_chunks.some(chunk => chunk.index === 0 && ['queued', 'storing'].includes(chunk.state)), 'First chunk was neither queued nor stored before next browser chunk')
    assert(injectedCommitFailure && commits.join(',') === '0,0', 'Storage commit was not retried after transient 503')
    for (let part = 0; part < 25; part++) assert(attempts.get(`0:${part}`) === stagedAttempts.get(`0:${part}`), `Commit retry resent accepted part ${part}`)
    await unpublished(); secondChunk.release.resolve()
    await reached(secondCommit, 'final storage commit')
    state = await session()
    assert(acceptedBytes(state) === size && state.pending_chunks.some(chunk => chunk.index === 1 && chunk.bytes === size - chunkSize), 'Final staged part or accepted progress incorrect')
    await item().getByText('Saving to storage', { exact: false }).waitFor()
    assert(Number(await bar().getAttribute('aria-valuenow')) < 100, 'Last staged bytes falsely report complete progress')
    await unpublished(); await page.setViewportSize({ width: 390, height: 844 }); await shot('saving-mobile')
    secondCommit.release.resolve()
    await item().getByText(/^Complete(?: · \d+ retr(?:y|ies))?$/).waitFor({ timeout: 60000 })
    assert(Number(await bar().getAttribute('aria-valuenow')) === 100, 'Verified completion did not reach 100%')
    state = await session()
    assert(state.status === 'complete' && state.received_bytes === size && state.received_chunks.join(',') === '0,1', 'Final manifest geometry or state incorrect')
    assert(commits.join(',') === '0,0,1' && queueResponses.filter(status => status === 202).length === 2 && legacyRequests === 0 && completionRequests === 1, 'Unexpected queue acknowledgement or completion ordering')
    const listing = await (await page.request.get(`${origin}/api/v1/nodes?parent_id=${encodeURIComponent(folder.id)}`)).json()
    const node = listing.items.find(node => node.name === name)
    assert(node?.sha256 === expectedDigest && node.size === size, 'Published metadata checksum differs')
    const downloaded = await page.request.get(`${origin}/api/v1/nodes/${node.id}/content?download=1`, { timeout: 60000 })
    assert(downloaded.ok() && crypto.createHash('sha256').update(await downloaded.body()).digest('hex') === expectedDigest, 'Encrypted multipart download SHA-256 differs')
    await collectProgress()
    assert(progress.some(event => event.computable && event.loaded > 0 && event.total <= partSize), 'No native client upload progress events observed')
    assert(!pageErrors.length, `Uncaught browser exceptions: ${pageErrors.join(' | ')}`)
    await shot('complete-mobile')
    passed.push('durable HTTP202 background queue; bounded pending-chunk state; separate accepted/stored progress; transient HTTP503 queue retry without resending accepted parts; unpublished until verification; two storage chunks; encrypted SHA-256 equality')
    if (restartServer) passed.push('real server process stop/start with persisted encrypted staged parts; resumed without resending accepted parts')
    await page.unroute('**/api/v1/uploads/*/chunks/*/parts/*', routePart)
    await page.unroute('**/api/v1/uploads/*/chunks/*/commit', routeCommit)
    const nextSending = hold(), firstPublishing = hold(), ids = new Map()
    const fixtures = [{ name: 'Background saving A.txt', mimeType: 'text/plain', buffer: Buffer.alloc(256 * 1024, 65) }, { name: 'Background saving B.txt', mimeType: 'text/plain', buffer: Buffer.alloc(256 * 1024, 66) }]
    const createSmall = async route => {
      const request = route.request()
      if (request.method() !== 'POST' || !fixtures.some(file => file.name === request.postDataJSON()?.name)) return route.continue()
      const response = await route.fetch()
      assert(response.status() === 201, 'Small pipeline upload could not start')
      ids.set(request.postDataJSON().name, (await response.json()).id)
      return route.fulfill({ response })
    }
    const sendNext = async route => {
      if (new URL(route.request().url()).pathname.includes(`/uploads/${ids.get(fixtures[1].name)}/chunks/`)) {
        nextSending.seen.resolve(); await nextSending.release.promise
      }
      return route.continue().catch(() => {})
    }
    const publishFirst = async route => {
      if (new URL(route.request().url()).pathname === `/api/v1/uploads/${ids.get(fixtures[0].name)}/complete`) {
        firstPublishing.seen.resolve(); await firstPublishing.release.promise
      }
      return route.continue().catch(() => {})
    }
    await page.route('**/api/v1/uploads', createSmall)
    await page.route('**/api/v1/uploads/*/chunks/*/parts/*', sendNext)
    await page.route('**/api/v1/uploads/*/complete', publishFirst)
    try {
      await page.setViewportSize({ width: 1440, height: 900 })
      await page.getByLabel('Choose files to upload', { exact: true }).setInputFiles(fixtures)
      await reached(nextSending, 'next small file sending before previous publication')
      await reached(firstPublishing, 'previous file background publication')
      assert((await page.request.get(`${origin}/api/v1/nodes/${ids.get(fixtures[0].name)}`)).status() === 404, 'First file was published before delayed final verification')
      const firstItem = queue.locator('.transfer-item').filter({ has: page.locator('strong').filter({ hasText: fixtures[0].name }) })
      assert(Number(await firstItem.getByRole('progressbar').getAttribute('aria-valuenow')) < 100, 'Background work was falsely marked complete')
      assert(await button(`Pause ${fixtures[0].name}`).count() === 0, 'Fully accepted server work must not offer fake pause')
      await shot('saving-next-file')
      nextSending.release.resolve(); firstPublishing.release.resolve()
      for (const fixture of fixtures) {
        const entry = queue.locator('.transfer-item').filter({ has: page.locator('strong').filter({ hasText: fixture.name }) })
        await entry.getByText(/^Complete(?: · \d+ retr(?:y|ies))?$/).waitFor({ timeout: 60000 })
        const downloaded = await page.request.get(`${origin}/api/v1/nodes/${ids.get(fixture.name)}/content?download=1`)
        assert(downloaded.ok() && crypto.createHash('sha256').update(await downloaded.body()).digest('hex') === crypto.createHash('sha256').update(fixture.buffer).digest('hex'), 'Small background file checksum differs')
      }
      passed.push('next small file starts while previous publication is blocked; server-only finalization; no fake pause after acceptance; both encrypted downloads match SHA-256')
    } finally {
      nextSending.release.resolve(); firstPublishing.release.resolve()
      await page.unroute('**/api/v1/uploads', createSmall)
      await page.unroute('**/api/v1/uploads/*/chunks/*/parts/*', sendNext)
      await page.unroute('**/api/v1/uploads/*/complete', publishFirst)
    }
    assert(!pageErrors.length, `Uncaught browser exceptions: ${pageErrors.join(' | ')}`)
    return { result: 'PASS', adapter: 'local', live_telegram: 'NOT TESTED', restart_tested: !!restartServer, cross_file_pipeline: true, passed, size, chunk_size: chunkSize, part_size: partSize, storage_chunks: state.received_chunks.length, max_client_body: Math.max(...bodies), client_requests: bodies.length, part_attempts: Object.fromEntries(attempts), storage_commits: commits, queue_responses: queueResponses, native_progress_events: progress.length, sha256: expectedDigest, screenshots, axe_checks: axeChecks, browser_exceptions: pageErrors.length }
  } finally {
    for (const gate of [pausedPart, firstCommit, secondChunk, secondCommit]) gate.release.resolve()
    await page.unroute('**/api/v1/uploads/*/chunks/*/parts/*', routePart)
    await page.unroute('**/api/v1/uploads/*/chunks/*/commit', routeCommit)
    page.off('request', observe); page.off('response', observeResponse); page.off('pageerror', error)
  }
}
