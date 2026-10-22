import assert from 'node:assert/strict'
import { readFileSync, writeFileSync } from 'node:fs'

// ponytail: PDF.js 6.4.299 drops fetch-reader cancellation promises; remove when upstream handles them.
const root = new URL('../node_modules/pdfjs-dist/', import.meta.url)
assert.equal(JSON.parse(readFileSync(new URL('package.json', root), 'utf8')).version, '6.4.299', 'Review PDF.js cancellation patch before upgrading')
const file = new URL('legacy/build/pdf.mjs', root)
const original = '  cancel(reason) {\n    this._reader?.cancel(reason);\n    this._abortController.abort();\n  }'
const patched = original.replace('this._reader?.cancel(reason);', 'this._reader?.cancel(reason).catch(() => {});')
const source = readFileSync(file, 'utf8')
const pristineCount = source.split(original).length - 1
const patchedCount = source.split(patched).length - 1
assert.ok(pristineCount === 2 && patchedCount === 0 || pristineCount === 0 && patchedCount === 2, 'PDF.js fetch-reader source changed; review cancellation patch')
if (pristineCount) {
  writeFileSync(file, '// Harbor modification: handle rejected fetch-reader cancellation promises.\n' + source.replaceAll(original, patched))
}
