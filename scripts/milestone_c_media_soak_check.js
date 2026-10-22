// Opt-in 30-minute Chromium local-media workload. This does not measure cold Telegram media.
async (page, restartServer) => {
  const fs = require('node:fs'), vm = require('node:vm'), crypto = require('node:crypto'), process = require('node:process')
  const { setInterval, clearInterval } = require('node:timers')
  const assert = (value, message) => { if (!value) throw new Error(message) }
  const wait = milliseconds => new Promise(resolve => setTimeout(resolve, milliseconds))
  const initialCheck = vm.runInNewContext(fs.readFileSync('/checks/milestone_b_preview_check.js', 'utf8'), { require, URL, Buffer, TextEncoder, TextDecoder, setTimeout, clearTimeout, console })
  const initial = await initialCheck(page, restartServer)
  assert(initial.result === 'PASS' && restartServer, 'Initial real-media qualification required')
  const origin = new URL(page.url()).origin, button = name => page.getByRole('button', { name, exact: true })
  const listing = async parent => {
    const response = await page.request.get(`${origin}/api/v1/nodes?parent_id=${encodeURIComponent(parent)}`)
    try { assert(response.ok(), 'Fixture listing failed'); return (await response.json()).items } finally { await response.dispose() }
  }
  const folder = (await listing('')).find(node => node.name === 'Milestone B previews')
  assert(folder?.kind === 'folder', 'Generated preview fixture missing')
  const nodes = await listing(folder.id), pdf = nodes.find(node => node.name.endsWith('.pdf')), image = nodes.find(node => node.name.endsWith('.jpeg')), video = nodes.find(node => node.name.endsWith('.webm'))
  assert(pdf && image && video, 'Real PDF/JPEG/WebM fixtures required')
  await page.setViewportSize({ width: 1440, height: 900 })
  const open = async node => { await button(node.name).click(); await page.getByRole('dialog', { name: node.name, exact: true }).waitFor() }
  const close = async () => { await button('Close dialog').click(); await page.getByRole('dialog').waitFor({ state: 'hidden' }) }
  const pdfReady = async number => {
    await page.locator('.pdf-canvas-scroll[aria-busy="false"]').waitFor()
    await page.getByRole('img', { name: `${pdf.name}, page ${number}`, exact: true }).waitFor()
    await page.locator('.pdf-canvas-scroll .sr-only').filter({ hasText: `Harbor generated PDF page ${number}` }).waitFor()
  }
  const data = Buffer.alloc(16 * 1024 * 1024, 91), digest = crypto.createHash('sha256').update(data).digest('hex')
  const samples = [], cycles = [], errors = [], failedRanges = [], automationSamples = []
  const diagnostic = await page.context().newCDPSession(page)
  const sampleAutomation = async () => {
    // Pinned tooling internals: retained payload totals describe automation, not application RSS.
    const objects = [...page._connection._objects.values()]
    const requests = objects.filter(object => object._type === 'Request')
    const heap = await diagnostic.send('Runtime.getHeapUsage'), dom = await diagnostic.send('Memory.getDOMCounters')
    automationSamples.push({ seconds: (Date.now() - began) / 1000, request_objects: requests.length, retained_request_body_bytes: requests.reduce((sum, request) => sum + (request._initializer.postData?.length || 0), 0), channel_objects: objects.length, response_listeners: page.listenerCount('response'), requestfailed_listeners: page.listenerCount('requestfailed'), browser_heap: heap, browser_dom: dom })
  }
  const counters = path => Object.fromEntries(fs.readFileSync(path, 'utf8').trim().split('\n').map(line => { const [name, value] = line.trim().split(/\s+/); return [name, Number(value)] }))
  const diskBytes = directory => fs.readdirSync(directory, { withFileTypes: true }).reduce((sum, entry) => {
    const path = `${directory}/${entry.name}`
    try { return sum + (entry.isDirectory() ? diskBytes(path) : entry.isFile() ? fs.statSync(path).size : 0) } catch (error) { if (error.code === 'ENOENT') return sum; throw error }
  }, 0)
  let pid, restarted = false, range206 = 0, stopped = false
  const began = Date.now(), duration = 30 * 60 * 1000
  const sample = () => {
    if (stopped) return
    try {
      if (!pid || !fs.existsSync(`/proc/${pid}/status`)) pid = fs.readdirSync('/proc').filter(name => /^\d+$/.test(name)).find(name => { try { return fs.readFileSync(`/proc/${name}/comm`, 'utf8').trim() === 'drive' } catch { return false } })
      let status = ''
      try { if (pid) status = fs.readFileSync(`/proc/${pid}/status`, 'utf8') } catch (error) { if (error.code !== 'ENOENT') throw error; pid = undefined }
      const stat = counters('/sys/fs/cgroup/memory.stat')
      samples.push({ seconds: (Date.now() - began) / 1000, app_rss_bytes: Number(status.match(/^VmRSS:\s+(\d+)/m)?.[1] || 0) * 1024, app_hwm_bytes: Number(status.match(/^VmHWM:\s+(\d+)/m)?.[1] || 0) * 1024, cgroup_bytes: Number(fs.readFileSync('/sys/fs/cgroup/memory.current', 'utf8')), cgroup_peak_bytes: Number(fs.readFileSync('/sys/fs/cgroup/memory.peak', 'utf8')), anon_bytes: stat.anon, file_cache_bytes: stat.file, cpu_usage_usec: counters('/sys/fs/cgroup/cpu.stat').usage_usec, node: process.memoryUsage(), scratch_bytes: diskBytes('/scratch') })
    } catch (error) { errors.push(`Resource sampler: ${error.code || error.message}`) }
  }
  const exception = error => errors.push(error.stack || error.message)
  const response = response => { if (/\/nodes\/[^/]+\/content$/.test(new URL(response.url()).pathname) && response.status() === 206) range206++ }
  const failed = request => { if (/\/nodes\/[^/]+\/content$/.test(new URL(request.url()).pathname)) failedRanges.push(request.failure()?.errorText || 'unknown') }
  page.on('pageerror', exception); page.on('response', response); page.on('requestfailed', failed)
  sample()
  const timer = setInterval(sample, 1000)
  try {
    while (Date.now() - began < duration) {
      const start = Date.now(), cycle = { index: cycles.length, seconds: (start - began) / 1000 }
      let release, seen
      const held = new Promise(resolve => { release = resolve }), reached = new Promise(resolve => { seen = resolve })
      let first = true
      const part = async route => { if (first) { first = false; seen(); await held } await route.continue().catch(() => {}) }
      await page.route('**/api/v1/uploads/*/chunks/*/parts/*', part)
      const name = `Media soak ${cycles.length}.bin`
      try {
        // Keep selected bytes on disk; repeated base64 fixture injection inflates automation memory.
        fs.writeFileSync(name, data, { mode: 0o600 })
        await page.getByLabel('Choose files to upload', { exact: true }).setInputFiles(name)
        let deadline
        try { await Promise.race([reached, new Promise((_, reject) => { deadline = setTimeout(() => reject(new Error('Media upload part not observed')), 30000) })]) } finally { clearTimeout(deadline) }
        const previewStart = Date.now()
        await open(pdf); await pdfReady(1); cycle.pdf_first_page_ms = Date.now() - previewStart
        release()
        const pageStart = Date.now()
        await button('Next PDF page').click(); await pdfReady(2); cycle.pdf_next_page_ms = Date.now() - pageStart
        await close()
      } finally { release(); await page.unroute('**/api/v1/uploads/*/chunks/*/parts/*', part) }
      const imageStart = Date.now()
      await open(image); await page.waitForFunction(() => { const image = document.querySelector('img.image-canvas'); return image?.complete && image.naturalWidth === 4096 }); cycle.jpeg_ms = Date.now() - imageStart; await close()
      const videoStart = Date.now()
      await open(video)
      await page.locator('video').evaluate(async video => { video.muted = true; await video.play() })
      await page.waitForFunction(() => { const video = document.querySelector('video'); return video && !video.paused && video.currentTime > .2 })
      cycle.video_play_ms = Date.now() - videoStart
      const seekStart = Date.now()
      await page.locator('video').evaluate(video => { video.pause(); video.currentTime = 4 })
      await page.waitForFunction(() => { const video = document.querySelector('video'); return video && !video.seeking && video.readyState >= 2 && Math.abs(video.currentTime - 4) < .1 })
      cycle.video_seek_ms = Date.now() - seekStart; await close()
      await open(pdf); await close()
      const queue = page.getByRole('region', { name: 'Transfer queue', exact: true })
      await queue.locator('.transfer-item').filter({ hasText: name }).getByText('Complete', { exact: true }).waitFor({ timeout: 60000 })
      const node = (await listing(folder.id)).find(node => node.name === name)
      assert(node?.sha256 === digest, 'Media soak upload published invalid checksum')
      const downloaded = await page.request.get(`${origin}/api/v1/nodes/${node.id}/content`)
      try { assert(downloaded.ok() && crypto.createHash('sha256').update(await downloaded.body()).digest('hex') === digest, 'Media soak download checksum differs') } finally { await downloaded.dispose() }
      for (const suffix of ['', '/permanent']) {
        const removed = await page.request.delete(`${origin}/api/v1/nodes/${node.id}${suffix}`)
        try { assert(removed.status() === 204, 'Generated media upload recycling failed') } finally { await removed.dispose() }
      }
      await button('Clear history').click(); await button('Close transfer queue').click()
      fs.unlinkSync(name)
      assert((await listing(folder.id)).length === nodes.length, 'Generated media fixture count grew')
      if (!restarted && Date.now() - began >= duration / 2) {
        await restartServer(); await page.reload(); await page.getByRole('heading', { name: 'My drive', exact: true }).waitFor(); await button(folder.name).click(); restarted = true
      }
      cycle.active_ms = Date.now() - start; cycles.push(cycle)
      await sampleAutomation()
      fs.writeFileSync('suite-checks.json', JSON.stringify({ result: 'RUNNING', duration_seconds: (Date.now() - began) / 1000, cycles, samples, automation_samples: automationSamples }))
      assert(!errors.length, `Media soak exception: ${errors.join('; ')}`)
      await wait(Math.max(0, Math.min(60000 - cycle.active_ms, duration - (Date.now() - began))))
    }
    sample()
    assert(cycles.length >= 30 && restarted && range206 > 0 && samples.length >= 1500 && samples.some(sample => sample.app_rss_bytes > 0), 'Media duration/resource/range coverage incomplete')
    assert(!errors.length, `Media soak exception: ${errors.join('; ')}`)
    const events = counters('/sys/fs/cgroup/memory.events')
    assert(events.oom === 0 && events.oom_kill === 0, 'Media workload encountered cgroup OOM')
    return { result: 'PASS', duration_seconds: (Date.now() - began) / 1000, timing_scope: 'Local warmed media; same Chromium context, one server restart; 60-second cycle cadence, active duty recorded; not live Telegram media', initial_media: initial, cycles, samples, automation_samples: automationSamples, automation_scope: 'Read-only installed Playwright request-body/channel counts and CDP heap/DOM; no forced collection or context replacement', cgroup_memory_events: events, range206, canceled_content_requests: failedRanges, browser_exceptions: errors, fixture_files_remaining: nodes.length, uploaded_bytes: cycles.length * data.length, verified_download_bytes: cycles.length * data.length, screenshots: initial.screenshots, axe_checks: initial.axe_checks }
  } finally { stopped = true; clearInterval(timer); page.off('pageerror', exception); page.off('response', response); page.off('requestfailed', failed); await diagnostic.detach().catch(() => {}) }
}
