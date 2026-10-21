# Phase 4 UX qualification

October 21, 2026, Asia/Phnom_Penh. Generated fixtures only. Chromium against the actual production SPA and authenticated Go API; disposable encrypted local drive on port 8086 for destructive/failure tests, existing verified Telegram drive on port 8081 for live storage. No mock file manager or backend replacement.

## Implemented

- Compact evergreen/stone shell, persistent collapsible desktop navigation, readable secondary typography, framed search, consistent light/dark tokens and mobile drawer.
- Per-location list/grid and sort preferences; sortable name/date/size columns; bounded lazy image thumbnails enabled explicitly to avoid mass Telegram reads.
- Inline rename with validation, collision rollback and focus return. Optimistic create/rename/favorite metadata; server remains authoritative.
- Cmd/Ctrl and Shift selection, arrow/Home/End navigation, F2 rename, touch long-press selection, context menus and bulk actions. Native internal drag moves selected siblings into folders.
- Internal copy/cut/paste clipboard. Folder plans retain hierarchy and prune overlapping ancestor/descendant selections. File copies use exact authenticated Range reads and verified encrypted uploads; originals remain intact.
- Lazy command palette with combobox/listbox semantics, arrow/Enter navigation, matching loaded files, global search and real actions. Cmd/Ctrl+K opens it; `/` focuses search.
- Image fit/zoom/pointer and keyboard pan, expanded preview and nearby navigation; bounded text/code preview; PDF fit/page/zoom and keyboard-scrollable viewport; native ranged video/audio.
- Transfer summary, committed byte progress, speed/remaining bytes/ETA, attention/paused/history groups, retry/cancel and supported pause/resume. Final verification cannot be cancelled ambiguously. Queue restoration retains up to 1,000 pending records and 100 history records.

## Actual interaction results

| Check | Result | Evidence |
| --- | --- | --- |
| Create/navigate, inline rename, duplicate-name rollback | PASS | `phase4_browser_check.js`; rejected name leaves original metadata intact |
| Per-folder preferences, sortable columns, palette/text/search | PASS | Browser assertions; exact text preview, command focus on combobox |
| Browser upload/copy/cut, favorites, trash/restore | PASS | Real local API; copied SHA-256 matches source |
| Nested folder copy/cut with overlapping selections | PASS | `phase4_tree_check.js`; one copied root, nested leaf hash retained, cut preserves subtree |
| 1,000-item list/grid and native multi-selection drag | PASS | `phase4_large_check.js`; 17 list rows, 50/80/50 grid cards at top/middle/end, no overlap, End reaches final item |
| Thumbnail network efficiency | PASS | Zero automatic content GETs in large/drag fixture views; explicit thumbnail toggle required |
| Image/PDF/text/video preview | PASS | Real PNG decode, zoom/pan/fit, PDF canvas/page validation and two-page next/input navigation, exact text, highlighted JSON with escaped script text and no execution, 5-second MP4 seek with HTTP 206 |
| Managed browser download | PASS | Saved PNG SHA-256 equals original; bounded verification before browser save |
| Controlled transfer failure/retry | PASS | HTTP 500 on chunk 1 produces exactly three attempts; no completed node; Retry skips committed chunk 0 |
| Copy pause/reload/resume/cancel | PASS | 8,388,625-byte copy resumes after first 4 MiB, full SHA-256 matches; cancelled pending session returns 404 and no file appears |
| Mobile drawer/selection/rename/preview/transfers | PASS | 390×844; focus trap and settings handoff, synthetic touch hold, 190px rename input and 44×44px actions, no overflow |
| Real Telegram UI upload/preview/download | PASS | Generated 126,976-byte TXT; exact preview, saved SHA-256 equality and bytes 1–32 HTTP 206 |

Scripts under `scripts/phase4_*_check.js` run through `playwright-cli --session NAME run-code --filename SCRIPT`. Authenticate securely to a disposable loopback local adapter first. Local scripts refuse Telegram; the explicitly named live script guards the existing verified Telegram runtime and creates one generated fixture. Preview/mobile scripts expect the generated `Design library` fixture set. All scripts retain their fixtures for inspection.

## Visual and accessibility review

Before/after captures cover desktop 1440×900, laptop 1280×800 and mobile 390×844, light and dark. Each final main view had no page horizontal overflow and zero axe WCAG 2 A/AA/2.1 AA violations. Additional captures cover command palette, menus, folder dialog, inspector, settings/help, previews, transfers and mobile editing/navigation. Login light/dark/390px views also passed axe; invalid password rejected visibly.

The review found and fixed mobile focus-trap inclusion of a hidden button, narrow mobile rename controls, modal-menu `aria-hidden-focus`, a PDF viewport and transfer history lacking keyboard focus, and a stale loading footer after listing failure. Nonmodal menus keep Escape/focus return; dialogs retain Radix modal focus handling. Reduced-motion support remains native CSS. This is scoped browser evidence, not assistive-device certification.

## Measured frontend cost

One local 1,000-folder observation: fixture creation **300 ms** with four API workers; full page reload plus directory entry/render **120 ms**. These are warm local measurements, not Telegram throughput or a broad benchmark. DOM remains virtualized. No new dependencies or animation framework.

Baseline main bundle: **503.16 kB / 160.71 kB gzip**. Final main bundle and lazy PDF/palette/CSS sizes are recorded in the final report. Existing Vite >500 kB warning remains; correctness and bounded transfers took precedence over speculative bundler changes. PDF and command palette load separately.

## Limits

Folder copy is a browser-driven, non-atomic plan capped at 1,000 total entries; larger trees need a durable server job. Partial queued copies remain visible and originals stay intact. Browser storage quota can prevent best-effort queue persistence; server upload sessions remain recoverable. Upload reload requires original file; copies resume verified chunks; paused managed downloads restart. Downloads above 32 MiB use native browser progress. Image expansion fills the browser viewport, not OS fullscreen. Syntax highlighting is intentionally modest and capped at 2,000 tokens/256 KiB. Native media codec support varies.

Shift selection adds ranges from the most recent anchor; reversing direction does not shrink an existing selection. Bulk failures show completed/failed counts plus the first error. Clipboard/view preferences stay in the current browser; no public sharing or cloud-synced clipboard. Real mobile hardware, other browsers and screen readers remain NOT TESTED. Live cross-datacenter Telegram retrieval remains NOT TESTED; no new cross-DC claim in this UI phase.
