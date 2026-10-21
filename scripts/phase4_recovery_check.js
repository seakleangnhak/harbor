// playwright-cli --session NAME run-code --filename scripts/phase4_recovery_check.js
// Controlled HTTP failures on generated LOCAL fixtures. No Telegram fault injection.
async (page) => {
  const origin = new URL(page.url()).origin
  const diagnostics = await page.request.get(`${origin}/api/v1/diagnostics`)
  if (!diagnostics.ok() || (await diagnostics.json()).adapter !== 'local') throw new Error('Disposable local adapter required')
  const assert = (condition, message) => { if (!condition) throw new Error(message) }, passed = []
  await page.setViewportSize({ width: 1440, height: 900 }); await page.reload()
  const name = `Recovery ${Date.now()}.bin`, copy = name.replace('.bin', ' copy.bin'), requests = []
  let failing = true
  const chunkPattern = '**/api/v1/uploads/*/chunks/*'
  const chunkHandler = async route => {
    const index = Number(route.request().url().split('/').at(-1)); requests.push(index)
    if (index === 1 && failing) await route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ error: 'Controlled local connection failure' }) })
    else await route.continue()
  }
  await page.route(chunkPattern, chunkHandler)
  try {
    await page.getByLabel('Choose files to upload', { exact: true }).setInputFiles([{ name, mimeType: 'application/octet-stream', buffer: Buffer.alloc(8 * 1024 * 1024 + 17, 43) }])
    await page.getByRole('button', { name: `Retry ${name}`, exact: true }).waitFor({ timeout: 30000 })
    assert(requests.filter(index => index === 1).length === 3, 'Retry count not bounded at three attempts')
    const before = await page.request.get(`${origin}/api/v1/nodes?q=${encodeURIComponent(name)}`)
    assert((await before.json()).total === 0, 'Failed upload published a file')
    await page.screenshot({ path: 'docs/screenshots/after-transfer-error.png', animations: 'disabled' })
    failing = false
    await page.getByRole('button', { name: `Retry ${name}`, exact: true }).click()
    await page.getByRole('heading', { name: 'All transfers complete', exact: true }).waitFor({ timeout: 30000 })
    assert(requests.filter(index => index === 0).length === 1, 'Retry resent committed chunk')
    await page.getByRole('button', { name: 'Close transfer queue', exact: true }).click()
    await page.getByRole('button', { name, exact: true }).waitFor()
    passed.push('three bounded attempts, failed file unpublished, retry skips committed chunks')
  } finally { await page.unroute(chunkPattern, chunkHandler) }

  const listing = await page.request.get(`${origin}/api/v1/nodes?q=${encodeURIComponent(name)}`)
  const source = (await listing.json()).items.find(node => node.name === name)
  const sourcePattern = `**/api/v1/nodes/${source.id}/content`
  let release, blocked = false
  const sourceHandler = async route => {
    if (route.request().headers()['range']?.startsWith('bytes=4194304-')) {
      blocked = true
      await new Promise(resolve => { release = resolve })
    }
    await route.continue().catch(() => {})
  }
  await page.route(sourcePattern, sourceHandler)
  try {
    await page.getByRole('button', { name: `Actions for ${name}`, exact: true }).click()
    await page.getByRole('menuitem', { name: 'Copy', exact: true }).click()
    await page.getByRole('button', { name: 'Paste here', exact: true }).click()
    await page.waitForFunction(copy => JSON.parse(localStorage.getItem('harbor:transfers:v1') || '[]').some(item => item.name === copy && item.received_bytes === 4194304), copy)
    assert(blocked, 'Second source range not intercepted')
    await page.getByRole('button', { name: `Pause ${copy}`, exact: true }).click()
    release?.()
    await page.getByRole('button', { name: `Resume ${copy}`, exact: true }).waitFor()
    await page.reload()
  } finally { release?.(); await page.unroute(sourcePattern, sourceHandler) }
  await page.getByRole('button', { name: 'Transfer queue', exact: true }).click()
  await page.getByRole('button', { name: `Resume ${copy}`, exact: true }).click()
  await page.getByRole('heading', { name: 'All transfers complete', exact: true }).waitFor({ timeout: 30000 })
  const copied = await page.request.get(`${origin}/api/v1/nodes?q=${encodeURIComponent(copy)}`)
  const destination = (await copied.json()).items.find(node => node.name === copy)
  assert(destination && destination.sha256 === source.sha256 && destination.size === source.size, 'Reloaded multipart copy checksum mismatch')
  passed.push('multipart copy pauses after one committed chunk, reloads, resumes without original file, checksum matches')
  await page.getByRole('button', { name: 'Close transfer queue', exact: true }).click()

  const cancelName = name.replace('.bin', ' copy 2.bin')
  const failSource = route => route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: 'Controlled source interruption' }) })
  await page.route(sourcePattern, failSource)
  try {
    await page.getByRole('button', { name: `Actions for ${name}`, exact: true }).click()
    await page.getByRole('menuitem', { name: 'Copy', exact: true }).click()
    await page.getByRole('button', { name: 'Paste here', exact: true }).click()
    await page.getByRole('button', { name: `Cancel ${cancelName}`, exact: true }).waitFor()
    await page.waitForFunction(name => JSON.parse(localStorage.getItem('harbor:transfers:v1') || '[]').some(item => item.name === name && item.id), cancelName)
    const session = await page.evaluate(name => JSON.parse(localStorage.getItem('harbor:transfers:v1') || '[]').find(item => item.name === name)?.id, cancelName)
    await page.getByRole('button', { name: `Cancel ${cancelName}`, exact: true }).click()
    await page.getByRole('button', { name: `Cancel ${cancelName}`, exact: true }).waitFor({ state: 'hidden' })
    assert((await page.request.get(`${origin}/api/v1/uploads/${session}`)).status() === 404, 'Cancelled session not removed')
    const cancelled = await page.request.get(`${origin}/api/v1/nodes?q=${encodeURIComponent(cancelName)}`)
    assert((await cancelled.json()).total === 0, 'Cancelled copy published')
    passed.push('concurrent cancellation cleans pending session and never publishes incomplete copy')
  } finally { await page.unroute(sourcePattern, failSource) }
  return { result: 'PASS', passed, source: name, bytes: source.size, sha256: source.sha256, failed_chunk_attempts: 3 }
}
