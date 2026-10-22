import assert from 'node:assert/strict'
import { copyFileSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { spawnSync } from 'node:child_process'
import test, { type TestContext } from 'node:test'

const original = '  cancel(reason) {\n    this._reader?.cancel(reason);\n    this._abortController.abort();\n  }'
const patched = ['_headersCapability', '_readCapability'].map(capability =>
  original.replace('this._reader?.cancel(reason);', 'this._reader?.cancel(reason).catch(() => {});')
    .replace('this._abortController.abort();', `this.${capability}.promise.catch(() => {});\n    this._abortController.abort();`))
const installed = readFileSync(new URL('../node_modules/pdfjs-dist/legacy/build/pdf.mjs', import.meta.url), 'utf8')
const pristine = patched.reduce((source, method) => source.replaceAll(method, original), installed)
assert.equal(pristine.split(original).length - 1, 2)

function fixture(t: TestContext, source = pristine, version = '6.4.299') {
  const root = mkdtempSync(join(tmpdir(), 'harbor-pdf-patch-'))
  t.after(() => rmSync(root, { recursive: true, force: true }))
  const dependency = join(root, 'node_modules/pdfjs-dist')
  mkdirSync(join(dependency, 'legacy/build'), { recursive: true })
  mkdirSync(join(root, 'scripts'))
  copyFileSync(new URL('../scripts/patch-pdfjs.mjs', import.meta.url), join(root, 'scripts/patch-pdfjs.mjs'))
  writeFileSync(join(dependency, 'package.json'), JSON.stringify({ version }))
  const bundle = join(dependency, 'legacy/build/pdf.mjs')
  writeFileSync(bundle, source)
  return {
    bundle,
    run: () => spawnSync(process.execPath, [join(root, 'scripts/patch-pdfjs.mjs')], {
      encoding: 'utf8', timeout: 10_000, env: { HOME: root, TMPDIR: root },
    }),
  }
}

test('PDF.js installer patches both installed readers and repeated installation changes nothing', t => {
  const check = fixture(t)
  const first = check.run()
  assert.equal(first.status, 0, first.stderr)
  const result = readFileSync(check.bundle, 'utf8')
  assert.equal(result.split(original).length - 1, 0)
  for (const method of patched) assert.equal(result.split(method).length - 1, 1)
  assert.ok(patched.reduce((source, method) => source.replaceAll(method, original), result).endsWith(pristine), 'Unrelated dependency source must remain unchanged')
  const second = check.run()
  assert.equal(second.status, 0, second.stderr)
  assert.equal(readFileSync(check.bundle, 'utf8'), result)
})

test('PDF.js installer refuses an unsupported version without changing its source', t => {
  const check = fixture(t, pristine, '6.4.300')
  const result = check.run()
  assert.notEqual(result.status, 0)
  assert.match(result.stderr, /Review PDF.js cancellation patch before upgrading/)
  assert.equal(readFileSync(check.bundle, 'utf8'), pristine)
})

test('PDF.js installer refuses a partially patched dependency without changing its source', t => {
  const partial = pristine.replace(original, patched[0])
  const check = fixture(t, partial)
  const result = check.run()
  assert.notEqual(result.status, 0)
  assert.match(result.stderr, /PDF.js fetch-reader source changed/)
  assert.equal(readFileSync(check.bundle, 'utf8'), partial)
})
