// Run only in scripts/Dockerfile.browser with --network none --read-only and
// bounded writable /scratch and /tmp. Copy /scratch evidence before removal.
// No host source, credentials or Docker socket.
const assert = require('node:assert/strict')
const fs = require('node:fs')
const os = require('node:os')
const path = require('node:path')
const vm = require('node:vm')
const { spawn } = require('node:child_process')

const engine = process.argv[2] || 'chromium', requested = process.argv[3] || 'all'
assert(['chromium', 'webkit'].includes(engine), 'Select chromium or webkit')
const checks = { account: 'account_check.js', preview: 'preview_check.js', transport: 'transport_check.js', large: 'phase4_large_check.js', 'milestone-b': 'milestone_b_preview_check.js', 'large-b': 'phase4_large_check.js' }
assert(requested === 'all' || Object.hasOwn(checks, requested), 'Select all, account, preview, transport or large')
assert(process.platform === 'linux' && fs.existsSync('/.dockerenv'), 'Disposable Docker sandbox required')
assert(process.getuid() !== 0, 'Browser fixture must run without root')
assert(Object.values(os.networkInterfaces()).flat().every(address => address.internal), 'Run browser fixture with --network none')
assert(fs.readFileSync('/proc/mounts', 'utf8').split('\n').some(line => { const fields = line.split(' '); return fields[1] === '/' && fields[3].split(',').includes('ro') }), 'Read-only container filesystem required')
fs.accessSync('/scratch', fs.constants.W_OK)
process.umask(0o077)
const root = fs.mkdtempSync(`/scratch/browser-${engine}-`)
for (const name of ['home', 'tmp']) fs.mkdirSync(path.join(root, name), { mode: 0o700 })
const browserEnv = { PATH: '/usr/local/bin:/usr/bin:/bin', HOME: path.join(root, 'home'), TMPDIR: path.join(root, 'tmp'), LANG: 'C.UTF-8', TZ: 'UTC', PLAYWRIGHT_BROWSERS_PATH: '/ms-playwright' }
process.env.PLAYWRIGHT_BROWSERS_PATH = browserEnv.PLAYWRIGHT_BROWSERS_PATH
const { chromium, webkit } = require('/tooling/node_modules/playwright-core')
const origin = 'http://127.0.0.1:8080', token = 'synthetic-browser-bootstrap-token-32-chars', password = 'Synthetic release browser passphrase'
const results = [], failures = [], delay = milliseconds => new Promise(resolve => setTimeout(resolve, milliseconds))
let browser, page, server, suiteRoot

function startServer(env, logFile) {
  const child = spawn('/app/drive', [], { env, stdio: ['ignore', 'pipe', 'pipe'] })
  const running = { child, logBytes: 0, error: null }
  running.done = new Promise(resolve => {
    child.once('exit', (code, signal) => resolve({ code, signal }))
    child.once('error', error => { running.error = error; resolve({ code: -1 }) })
  })
  const log = bytes => {
    const remaining = 1024 * 1024 - running.logBytes
    if (remaining <= 0) { child.kill('SIGKILL'); return }
    fs.appendFileSync(logFile, bytes.subarray(0, remaining))
    running.logBytes += bytes.length
  }
  child.stdout.on('data', log); child.stderr.on('data', log)
  server = running
}

async function stopServer() {
  if (!server) return
  const stopped = server
  server = undefined
  let timer
  try {
    if (stopped.child.exitCode === null && stopped.child.signalCode === null && !stopped.error) {
      stopped.child.kill('SIGTERM')
      timer = setTimeout(() => stopped.child.kill('SIGKILL'), 10000)
    }
    const result = await stopped.done
    assert(!stopped.error && result.code === 0, 'Fixture server failed clean shutdown; inspect server.log')
  } finally { clearTimeout(timer) }
}

async function waitFor(pathname) {
  for (let attempt = 0; attempt < 200; attempt++) {
    assert(server && !server.error && server.child.exitCode === null && server.child.signalCode === null, 'Fixture server exited; inspect server.log')
    try { if ((await fetch(origin + pathname, { signal: AbortSignal.timeout(1000) })).ok) return } catch { /* Setup listener changes during activation. */ }
    await delay(100)
  }
  throw new Error('Fixture readiness timed out')
}

async function initialize() {
  for (const [endpoint, body] of [['account', { password }], ['complete', { adapter: 'local' }]]) {
    const response = await fetch(`${origin}/api/v1/setup/${endpoint}`, { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-Harbor-Setup-Token': token, Origin: origin }, body: JSON.stringify(body), signal: AbortSignal.timeout(15000) })
    assert(response.ok, 'Synthetic local setup failed')
  }
  await waitFor('/ready')
}

async function runSuite(name) {
  suiteRoot = path.join(root, name)
  for (const name of ['', 'data', 'screenshots']) fs.mkdirSync(path.join(suiteRoot, name), { mode: 0o700 })
  process.chdir(suiteRoot)
  const serverEnv = { ...browserEnv, DRIVE_ENV_FILE: '-', DRIVE_DATA_DIR: path.join(suiteRoot, 'data'), DRIVE_ADDR: '127.0.0.1:8080', DRIVE_STATIC_DIR: '/app/web/dist', DRIVE_SETUP_TOKEN: token, DRIVE_STORAGE: 'local', GOMEMLIMIT: '512MiB' }
  const logFile = path.join(suiteRoot, 'server.log')
  startServer(serverEnv, logFile)
  let context
  const unexpectedOrigins = []
  const started = performance.now()
  try {
    await waitFor('/health')
    if (name !== 'account') await initialize()
    context = await browser.newContext({ viewport: { width: 1440, height: 900 }, serviceWorkers: 'block', reducedMotion: 'reduce' })
    context.setDefaultTimeout(30000); context.setDefaultNavigationTimeout(30000)
    await context.addInitScript({ path: '/checks/axe.min.js' })
    await context.route('**/*', route => {
      const target = new URL(route.request().url())
      if (target.origin === origin || target.protocol === 'data:' || target.href === 'about:blank') return route.continue()
      unexpectedOrigins.push(target.origin); return route.abort('blockedbyclient')
    })
    page = await context.newPage(); await page.goto(origin)
    if (name !== 'account') {
      await page.getByLabel('Owner password', { exact: true }).fill(password)
      await page.getByRole('button', { name: 'Unlock drive', exact: true }).click()
      await page.getByRole('heading', { name: 'My drive', exact: true }).waitFor()
    }
    const check = vm.runInNewContext(fs.readFileSync(path.join('/checks', checks[name]), 'utf8'), { require, URL, Buffer, TextEncoder, TextDecoder, setTimeout, clearTimeout, console }, { filename: checks[name] })
    const restart = async () => { await stopServer(); startServer(serverEnv, logFile); await waitFor('/ready') }
    const result = await check(page, restart, name === 'large-b' ? 10000 : 1000)
    assert(result?.result === 'PASS', `${name} did not return PASS`)
    if (name === 'account') assert(result.axe?.result === 'PASS' && result.axe.checks >= 32 && result.screenshots.length >= 32, 'Account accessibility/layout coverage missing')
    if (name === 'preview' || name === 'transport') assert(result.axe_checks > 0, `${name} accessibility checks missing`)
    if (name === 'transport') assert(result.restart_tested === true && result.storage_chunks === 2 && result.max_client_body === 4194304, 'Multipart restart qualification missing')
    fs.writeFileSync(path.join(suiteRoot, 'suite-checks.json'), JSON.stringify(result, null, 2))
    assert(!unexpectedOrigins.length, 'Browser attempted external application requests')
    await page.mouse.move(0, 0)
    await page.evaluate(async () => {
      await document.fonts.ready
      await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))
    })
    await page.screenshot({ path: path.join(suiteRoot, 'screenshots', 'final.png'), fullPage: true, animations: 'disabled' })
    assert(await page.evaluate(() => Math.max(document.documentElement.scrollWidth, document.body.scrollWidth) <= innerWidth + 1), `${name} final view horizontal overflow`)
    const accessibility = await page.evaluate(async () => (await window.axe.run(document, { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa'] } })).violations.map(item => ({ id: item.id, targets: item.nodes.map(node => node.target), summaries: item.nodes.map(node => node.failureSummary) })))
    assert(!accessibility.length, `${name} final view accessibility violations: ${JSON.stringify(accessibility)}`)
    const evidence = { ...result, suite: name, browser: engine, elapsed_seconds: Math.round((performance.now() - started) / 10) / 100, final_view_axe: 'PASS' }
    fs.writeFileSync(path.join(suiteRoot, 'result.json'), JSON.stringify(evidence, null, 2))
    results.push(evidence)
    console.log(JSON.stringify({ result: 'PASS', suite: name, browser: engine, elapsed_seconds: evidence.elapsed_seconds }))
  } catch (error) {
    if (page && !page.isClosed()) await page.screenshot({ path: path.join(suiteRoot, 'failure.png'), fullPage: true, animations: 'disabled', timeout: 5000 }).catch(() => {})
    fs.writeFileSync(path.join(suiteRoot, 'error.txt'), error.stack || String(error))
    throw error
  } finally {
    try { if (context) await context.close() } finally {
      page = undefined
      try { await stopServer() } finally {
        fs.rmSync(path.join(suiteRoot, 'data'), { recursive: true, force: true })
        fs.rmSync(path.join(suiteRoot, 'Resumable transport.bin'), { force: true })
      }
    }
  }
}

let deadline
Promise.race([
  (async () => {
    browser = await (engine === 'webkit' ? webkit : chromium).launch({ headless: true, env: browserEnv, args: engine === 'chromium' ? ['--no-sandbox', '--disable-dev-shm-usage'] : [] })
    for (const name of requested === 'all' ? ['account', 'preview', 'transport', 'large'] : [requested]) {
      try { await runSuite(name) } catch {
        failures.push({ suite: name, result: 'FAIL' })
        console.error(JSON.stringify({ result: 'FAIL', suite: name, browser: engine, message: 'Suite failed; inspect its error.txt' }))
      }
    }
    assert(!failures.length, `Browser suites failed: ${failures.map(item => item.suite).join(', ')}`)
  })(),
  new Promise((_, reject) => { deadline = setTimeout(() => reject(new Error('Browser qualification exceeded 15 minutes')), 15 * 60 * 1000) }),
]).then(() => {
  const report = { result: 'PASS', browser: engine, adapter: 'local', live_telegram: 'NOT TESTED', suites: results }
  fs.writeFileSync(path.join(root, 'result.json'), JSON.stringify(report, null, 2))
  console.log(JSON.stringify({ result: 'PASS', browser: engine, suites: results.map(item => item.suite), artifact_root: root }))
}).catch(error => {
  fs.writeFileSync(path.join(root, 'error.txt'), error.stack || String(error))
  fs.writeFileSync(path.join(root, 'result.json'), JSON.stringify({ result: 'FAIL', browser: engine, adapter: 'local', live_telegram: 'NOT TESTED', suites: results, failures }, null, 2))
  console.error(JSON.stringify({ result: 'FAIL', browser: engine, message: error.message, artifact_root: root }))
  process.exitCode = 1
}).finally(async () => {
  clearTimeout(deadline)
  if (browser) await browser.close().catch(() => {})
  await stopServer().catch(() => { process.exitCode = 1 })
})
