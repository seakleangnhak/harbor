import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { setTimeout } from 'node:timers/promises'
import test from 'node:test'

test('installed PDF.js fetch readers handle cancellation after a stream errors', async () => {
  const source = readFileSync(new URL('../node_modules/pdfjs-dist/legacy/build/pdf.mjs', import.meta.url), 'utf8')
  const methods = [...source.matchAll(/  cancel\(reason\) \{\n    this\._reader\?\.cancel\(reason\)[^\n]*\n    this\._abortController\.abort\(\);\n  \}/g)]
  assert.equal(methods.length, 2, 'Both installed fetch readers must be exercised')
  for (const [method] of methods) {
    const controller = new AbortController()
    controller.abort()
    const reader = new ReadableStream({ start(stream) { stream.error(controller.signal.reason) } }).getReader()
    const cancel = new Function('reason', method.slice(method.indexOf('{') + 1, method.lastIndexOf('}')))
    assert.equal(cancel.call({ _reader: reader, _abortController: controller }, new Error('Preview closed')), undefined)
    await setTimeout(0) // Node's test runner fails on any detached unhandled rejection.
    reader.releaseLock()
  }
})
