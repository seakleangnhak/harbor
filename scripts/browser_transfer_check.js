// playwright-cli --session NAME run-code "$(cat scripts/browser_transfer_check.js)"
// Requires an authenticated disposable local drive; leaves one generated fixture.
async (page) => {
  if (!['127.0.0.1', 'localhost', '[::1]'].includes(new URL(page.url()).hostname)) throw new Error('Use a loopback local fixture drive')
  const diagnostics = await page.request.get(new URL('/api/v1/diagnostics', page.url()).href)
  if (!diagnostics.ok() || (await diagnostics.json()).adapter !== 'local') throw new Error('Authenticate to a disposable local adapter first')
  const name = `pause-resume-${Date.now()}.bin`
  await page.evaluate(() => {
    const original = Blob.prototype.arrayBuffer
    Blob.prototype.arrayBuffer = async function () {
      Blob.prototype.arrayBuffer = original
      await new Promise(resolve => { window.harborReleaseHash = resolve })
      return original.call(this)
    }
    window.harborRestoreHash = () => { Blob.prototype.arrayBuffer = original; window.harborReleaseHash?.() }
  })
  try {
    await page.getByLabel('Choose files to upload', { exact: true }).setInputFiles([{ name, mimeType: 'application/octet-stream', buffer: Buffer.alloc(256 * 1024, 37) }])
    await page.waitForFunction(() => typeof window.harborReleaseHash === 'function')
    await page.getByRole('button', { name: `Pause ${name}`, exact: true }).click()
    await page.getByRole('button', { name: `Resume ${name}`, exact: true }).click()
    await page.evaluate(() => window.harborReleaseHash())
    await page.getByRole('region', { name: 'Transfer queue' }).getByRole('heading', { name: 'All transfers complete' }).waitFor()
    await page.getByRole('button', { name, exact: true }).waitFor()
    console.log('PASS delayed-hash pause/resume; file published after old aborted read settled')
  } finally {
    await page.evaluate(() => { window.harborRestoreHash(); delete window.harborRestoreHash; delete window.harborReleaseHash })
  }
}
