import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createServer } from 'node:http'
import { setTimeout } from 'node:timers/promises'
import test from 'node:test'

const source = readFileSync(new URL('../node_modules/pdfjs-dist/legacy/build/pdf.mjs', import.meta.url), 'utf8')
const readers = [
  ['PDFFetchStreamReader', 'BasePDFStreamReader', '_headersCapability'],
  ['PDFFetchStreamRangeReader', 'BasePDFStreamRangeReader', '_readCapability'],
]

function classSource(name: string) {
  const start = source.indexOf(`class ${name} `)
  const end = source.indexOf('\nclass ', start + 1)
  assert.ok(start >= 0 && end > start, `Installed ${name} must be exercised`)
  return source.slice(start, end)
}

const fetchStart = source.indexOf('function fetchUrl(')
const fetchUrl = new Function(`return ${source.slice(fetchStart, source.indexOf('\nfunction ensureResponseStatus', fetchStart))}`)()

test('installed PDF.js fetch readers handle cancellation after a stream errors', async () => {
  for (const [name, , capability] of readers) {
    const method = classSource(name).match(/  cancel\(reason\) \{[\s\S]*?\n  \}/)?.[0]
    assert.ok(method, `${name}.cancel must be exercised`)
    const controller = new AbortController()
    controller.abort()
    const reader = new ReadableStream({ start(stream) { stream.error(controller.signal.reason) } }).getReader()
    const cancel = new Function('reason', method.slice(method.indexOf('{') + 1, method.lastIndexOf('}')))
    assert.equal(cancel.call({ _reader: reader, _abortController: controller, [capability]: { promise: Promise.resolve() } }, new Error('Preview closed')), undefined)
    await setTimeout(0) // Node's test runner fails on detached unhandled rejections.
    reader.releaseLock()
  }
})

for (const [name, baseName, capability] of readers) {
  test(`installed ${name} safely cancels a real fetch before response headers`, { timeout: 10_000 }, async t => {
    let received!: () => void
    const requestArrived = new Promise<void>(resolve => { received = resolve })
    const server = createServer(() => received()) // Hold headers pending until cancellation.
    t.after(async () => { server.closeAllConnections(); await new Promise<void>(resolve => server.close(() => resolve())) })
    await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve))
    const address = server.address()
    assert.ok(address && typeof address !== 'string')
    const base = new Function(`return ${classSource(baseName)}`)()
    const Reader = new Function(baseName, 'fetchUrl', `return ${classSource(name)}`)(base, fetchUrl)
    const reader = new Reader({ _source: { url: new URL(`http://127.0.0.1:${address.port}/pending`), withCredentials: false }, headers: new Headers() }, 0, 262144)
    await requestArrived
    assert.equal(reader._reader, null, 'Cancellation must precede response headers')
    assert.equal(reader.cancel(new Error('Preview closed')), undefined)
    await setTimeout(0) // Attach consumer later so an unhandled capability cannot be hidden.
    await assert.rejects(reader[capability].promise, { name: 'AbortError' })
  })
}
