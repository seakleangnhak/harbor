# Harbor — initial Phase 4 UI audit

Captured 2026-10-21 before UI implementation, from authenticated real Harbor at loopback 8081. Existing Phase 3 backend/session preserved. Source: React 19, TanStack Query/Virtual, Radix dialogs/menus, native CSS/Tailwind, lazy PDF.js, no animation framework. New branch starts at `5fb664d`.

## Baseline evidence

Actual file manager captured at desktop 1440×900, laptop 1280×800, mobile 390×844, in light and dark modes. All six baseline states had no page horizontal overflow. Screenshots contain generated earlier qualification objects, not personal documents. Dialog/transfer screenshots supplement this baseline. Screenshots are explicitly authorized for this phase despite prior general screenshot exclusion.

| Surface | Existing behavior / observed issue | Planned change |
| --- | --- | --- |
| Main layout | Evergreen/stone identity already coherent; 77px topbar and tall heading consume space; list rows airy relative to 10–12px labels | Compact chrome, stronger title/count/breadcrumb hierarchy, readable secondary typography, denser rows |
| Navigation | Fixed 242px sidebar; mobile drawer has focus trap; no desktop collapse | Persistent collapse, legible navigation/tooltips, intentional mobile drawer and touch actions |
| Toolbar | Search works with Cmd/Ctrl+K but looks like unbounded text; global view preference only; sort only Name header clickable | Framed search, distinct command palette, per-location view/sort preferences, real sortable columns |
| Files | List/grid, multi-select, favorites, bulk move/trash and virtualization already work; no inline rename, internal folder drag, clipboard or touch long-press | Native-feeling selection, inline rename with rollback, cut/paste/move, bounded file-copy flow, long-press and internal drag targets |
| Grid | Icons stand in for all files; folders visually indistinct from files | Refined folder cards; bounded lazy image thumbnails and clear file metadata |
| Dialogs/menus | Radix focus handling and destructive confirmation present; verbose novelty copy and small actions | Concise task copy, consistent surfaces/radii, accessible hit targets, mobile adaptation |
| Preview | Text range capped 256 KiB; PDF zoom/page controls; native video/audio with Range; image lacks zoom/pan/fullscreen | Image controls and navigation, readable code/text with escaped highlighting, resilient media/PDF loading/errors and fit controls |
| Transfers | Acknowledged upload bytes, retries, real pause/resume, bounded verified downloads and history already work | Aggregate progress, clear active/paused/failed/completed groups, compact persistent summary, accurate state-specific controls |
| Details/settings/help | Useful checksum/diagnostics and server-only Telegram setup | Consistent inspector/dialog layout, practical shortcut discoverability; retain protected setup boundary |
| Accessibility | Labels, focus outlines, skip link and reduced-motion CSS present; many desktop icons only 25–32px; small muted text | Mobile targets ≥44px, contrast checks with axe, scoped keyboard handling and verified focus return |
| Performance | Virtualization exists for large lists; PDF lazy; main bundle previously ~503kB minified | Preserve virtualization, avoid heavy libraries, lazy command/preview content where useful, measure final bundles and DOM counts |

## Design and functional boundaries

Preserve real file operations, authenticated encryption, Telegram transfer behavior and API contracts. Safe metadata updates may be optimistic with rollback. Never show unsupported pause/copy/sharing actions. Server has no copy endpoint: copying file content requires bounded verified re-upload, and folder-copy limits must be explicit. Native media controls preferred over custom player. No full large-file buffering or automatic mass Telegram thumbnail downloads.

Browser verification will exercise generated folder/upload/rename/move/favorite/trash/restore flows; previews, transfer pause/retry/error, command palette, desktop/mobile themes, keyboard and axe checks. Final captures must disable animations or wait for settled surfaces so transitional frames are not mistaken for visual defects. Backend/default/tagged regression, checksum, Docker smoke and bundle measurements remain required.

## Before screenshots

- [Desktop light](screenshots/before-desktop-light.png), [desktop dark](screenshots/before-desktop-dark.png)
- [Laptop light](screenshots/before-laptop-light.png), [laptop dark](screenshots/before-laptop-dark.png)
- [Mobile light](screenshots/before-mobile-light.png), [mobile dark](screenshots/before-mobile-dark.png)

Initial audit is an implementation baseline, not accessibility certification. Final report distinguishes observed, tested and deferred behavior.

## Resolution

Implemented and visually rechecked findings are recorded in [UX qualification](PHASE4_UX_REPORT.md) and [final acceptance](PHASE4_FINAL_REPORT.md). Final views include error-footer, menu/PDF/transfer focus and mobile rename corrections found during browser review; remaining limits are explicit in those reports.
