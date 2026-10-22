import assert from 'node:assert/strict'
import { readFileSync, writeFileSync } from 'node:fs'

// ponytail: PDF.js 6.4.299 drops fetch-reader cancellation promises; remove when upstream handles them.
const root = new URL('../node_modules/pdfjs-dist/', import.meta.url)
assert.equal(JSON.parse(readFileSync(new URL('package.json', root), 'utf8')).version, '6.4.299', 'Review PDF.js cancellation patch before upgrading')
const file = new URL('legacy/build/pdf.mjs', root)
const original = '  cancel(reason) {\n    this._reader?.cancel(reason);\n    this._abortController.abort();\n  }'
const source = readFileSync(file, 'utf8')
const changes = [
  ['PDFFetchStreamReader', 'BasePDFStreamReader', '_headersCapability'],
  ['PDFFetchStreamRangeReader', 'BasePDFStreamRangeReader', '_readCapability'],
].map(([name, base, capability]) => {
  const start = source.indexOf(`class ${name} extends ${base} {`)
  const end = source.indexOf('\nclass ', start + 1)
  assert.ok(start >= 0 && end > start, 'PDF.js fetch-reader source changed; review cancellation patch')
  const before = source.slice(start, end)
  const patched = original.replace('this._reader?.cancel(reason);', 'this._reader?.cancel(reason).catch(() => {});')
    .replace('this._abortController.abort();', `this.${capability}.promise.catch(() => {});\n    this._abortController.abort();`)
  const pristineCount = before.split(original).length - 1
  const patchedCount = before.split(patched).length - 1
  assert.ok(pristineCount === 1 && patchedCount === 0 || pristineCount === 0 && patchedCount === 1, 'PDF.js fetch-reader source changed; review cancellation patch')
  return { before, after: before.replace(original, patched), pristine: pristineCount === 1 }
})
assert.ok(changes.every(change => change.pristine) || changes.every(change => !change.pristine), 'PDF.js fetch-reader source changed; review cancellation patch')
if (changes[0].pristine) {
  writeFileSync(file, '// Harbor modification: handle rejected fetch-reader cancellation and pending-request promises.\n' +
    changes.reduce((text, change) => text.replace(change.before, change.after), source))
}
