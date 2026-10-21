export const textPreviewLimit = 256 * 1024

export function clampImagePan(x: number, y: number, width: number, height: number, zoom: number) {
  if (zoom <= 1) return { x: 0, y: 0 }
  const limitX = width * (zoom - 1) / 2, limitY = height * (zoom - 1) / 2
  return { x: Math.max(-limitX, Math.min(limitX, x)), y: Math.max(-limitY, Math.min(limitY, y)) }
}

export type CodeToken = { text: string; kind?: 'string' | 'comment' | 'keyword' | 'number' }
const codePattern = /"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|`(?:[^`\\]|\\.)*`|\/\/[^\n]*|#[^\n]*|\/\*[\s\S]*?(?:\*\/|$)|\b(?:async|await|break|case|class|const|continue|def|else|enum|export|extends|false|for|from|func|function|if|import|in|interface|let|new|nil|null|package|pass|return|struct|switch|throw|true|try|type|undefined|var|while)\b|\b\d+(?:\.\d+)?\b/g

export function codeTokens(text: string): CodeToken[] {
  const tokens: CodeToken[] = []
  let offset = 0, count = 0
  for (const match of text.matchAll(codePattern)) {
    if (++count > 2000) break
    if (match.index > offset) tokens.push({ text: text.slice(offset, match.index) })
    const value = match[0], kind = /^["'`]/.test(value) ? 'string' : /^(\/\/|\/\*|#)/.test(value) ? 'comment' : /^\d/.test(value) ? 'number' : 'keyword'
    tokens.push({ text: value, kind }); offset = match.index + value.length
  }
  if (offset < text.length) tokens.push({ text: text.slice(offset) })
  return tokens
}

export async function readPreviewText(response: Response, size: number, signal?: AbortSignal): Promise<string> {
  const reader = response.body?.getReader()
  try {
    const length = Math.min(textPreviewLimit, size)
    if (!response.ok) throw new Error(`Preview unavailable (${response.status}). Download the original or retry.`)
    if (!Number.isSafeInteger(size) || size < 0) throw new Error('File preview metadata is invalid.')
    if (size > textPreviewLimit && (response.status !== 206 || response.headers.get('Content-Range') !== `bytes 0-${textPreviewLimit - 1}/${size}`)) throw new Error('Bounded text preview is unavailable. Download the original instead.')
    if (!reader) { if (!length) return ''; throw new Error('Preview stream is unavailable.') }
    const bytes = new Uint8Array(length)
    let received = 0
    for (;;) {
      signal?.throwIfAborted()
      const { done, value } = await reader.read()
      if (done) break
      if (received + value.byteLength > length) throw new Error('Text preview exceeded its limit. Download the original instead.')
      bytes.set(value, received); received += value.byteLength
    }
    signal?.throwIfAborted()
    if (received !== length) throw new Error('Preview interrupted. Close and reopen this file to retry.')
    return new TextDecoder().decode(bytes)
  } finally {
    if (reader) { await reader.cancel().catch(() => {}); reader.releaseLock() }
  }
}
