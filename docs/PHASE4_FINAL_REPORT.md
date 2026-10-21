# Harbor Phase 4 acceptance

October 21, 2026, Asia/Phnom_Penh. **HARBOR_PHASE4 = PASS.** Scoped to the tested Chromium interfaces and existing private Telegram runtime. Backend/storage source, DB format and keys are unchanged; existing MTProto authorization is preserved. Existing drive and preexisting Telegram objects preserved; no public deployment or remote push.

| Acceptance gate | Result | Executed evidence |
| --- | --- | --- |
| Consistent premium file manager | PASS | Compact shell, navigation rail, sortable virtual list/grid, inline rename, clipboard, native drag and command palette; actual browser review |
| Light and dark | PASS | Six required viewport/theme captures; design tokens and login themes |
| Responsive layouts | PASS | 1440×900, 1280×800, 390×844, mobile drawer/dialog/preview/transfer checks; no page overflow |
| File interactions | PASS | Create, rename rejection/rollback, search, favorites, bulk selection, nested copy/cut, drag move, trash/restore |
| Supported previews | PASS | PNG, two-page PDF navigation, exact text, escaped highlighted code and native MP4 seek with 206; unsupported formats offer download |
| Accurate transfers | PASS | Acknowledged-byte progress, bounded retries, copy pause/reload/resume, safe cancellation, verified managed saves; native large-download distinction |
| Accessibility/layout defects | PASS | No outstanding critical/serious axe violations in exercised final views; discovered menu/PDF/mobile focus defects fixed |
| Finished screenshots | PASS | 50 reviewed before/after captures in `screenshots/` |
| Backend/Telegram regressions | PASS | Fresh default/tagged Go tests/race/vet, process acceptance, live Telegram UI checksum, final local Docker smoke |

## Verification

| Command / qualification | Result |
| --- | --- |
| `DRIVE_ENV_FILE=- go test -count=1 ./...` | PASS |
| `DRIVE_ENV_FILE=- go test -count=1 -race ./...` | PASS |
| `DRIVE_ENV_FILE=- go vet ./...` | PASS |
| `DRIVE_ENV_FILE=- GOGC=40 go test -count=1 -race -p 1 -tags telegram ./...` | PASS |
| `DRIVE_ENV_FILE=- GOGC=40 go vet -p 1 -tags telegram ./...` | PASS |
| `npm test`, `npm run typecheck`, `npm run build` in `web` | PASS; 22 tests |
| Python unittest discovery | PASS; 15 tests |
| `python3 scripts/acceptance.py --binary bin/drive` | PASS; auth/origin rejection, interrupted upload/process restart, idempotency, SHA-256/Range, hierarchy, trash, snapshot, offline backup/restore and session revocation |
| Phase 4 browser scripts | PASS; real local file operations, previews, mobile, nested trees, 1,000 items and controlled failures |
| axe 4.14.0 | PASS; tested final main, dialog/menu, preview, transfer and login states; scope below |
| Compose validation, final image build/start/health | PASS; isolated local project/volume, loopback 8087 |
| Final container integrity/recreation | PASS; generated encrypted file accessible after image/container recreation; UID 65532, read-only root, dropped capabilities and no-new-privileges |
| Real Telegram browser roundtrip | PASS; generated TXT 126,976 bytes, exact preview/full SHA-256 and 206 Range |
| Secret/artifact review | PASS; 158 tracked/proposed files scanned, zero configured credential/key matches and zero private runtime artifacts |

Live fixture SHA-256: `63ee20492686e2cae26c1d4c339cd116d0c9be6bac813b5fe5db432fd8f28b50`. Multipart copy/reload fixture: 8,388,625 bytes, SHA-256 `34039997f4abd65719aade41a4383fa03af85ea354684f3bea395b441c5f858f`. Controlled HTTP failures are local simulations, not live Telegram network/flood qualification. Phase 3 live Docker/network/restart evidence remains historical and unchanged.

## Performance and scope

1,000 generated folders: four local API workers, 300 ms creation, 120 ms reload-and-entry observation. List renders 17 rows; grid renders 50/80/50 cards at top/middle/end. No virtual-card overlap; keyboard End and selected native drag pass. Zero automatic thumbnail content requests. Single warm-local observations; no capacity guarantee.

No frontend dependencies added. PDF and command palette remain lazy. Main bundle grew from 503.16 kB/160.71 kB gzip to approximately 531.48 kB/169.37 kB gzip; existing >500 kB warning remains. CSS approximately 71.85 kB/14.31 kB gzip, lazy palette 3.34 kB/1.58 kB gzip, lazy PDF approximately 458.83 kB/136.23 kB gzip. Exact current build output retained under ignored `output/phase4/build-final.log`.

## Evidence and limits

Reports: [initial audit](PHASE4_UI_AUDIT.md), [design system](PHASE4_DESIGN_SYSTEM.md), [interaction qualification](PHASE4_UX_REPORT.md). Main captures: [desktop light](screenshots/after-desktop-light.png), [desktop dark](screenshots/after-desktop-dark.png), [laptop light](screenshots/after-laptop-light.png), [laptop dark](screenshots/after-laptop-dark.png), [mobile light](screenshots/after-mobile-light.png), [mobile dark](screenshots/after-mobile-dark.png). Additional reviewed screenshots cover previews, transfers, menus, dialogs, inspector, settings and login. [Two-page PDF](screenshots/after-pdf-pagination.png) and [highlighted code](screenshots/after-code-preview.png) captures verify real page changes and escaped literal script text without execution. Generated local fixtures are labeled local storage; live screenshot explicitly shows Telegram connected.

Detailed sanitized JSON/logs retained under ignored `output/phase4/`; executable browser harnesses committed under `scripts/`. Initial frontend test failure and extra code-preview selector mismatch were test assertion errors, corrected and rerun. Duplicate test fixture uploads were rejected safely; their retained pending records caused an aggregate-completion assertion timeout. Those records were cancelled through the UI and the recovery check reran successfully. Initial menu/PDF axe failures and an expected UID-format mismatch in a Docker checker were corrected and rerun; no failed verification is represented as a pass.

**PARTIAL by design:** folder copies capped at 1,000 entries and non-atomic; large downloads above 32 MiB rely on native browser progress; upload reload requires original file; managed download resume restarts; modest code highlighting, additive Shift ranges and native codec support. No server copy job, public sharing, OS fullscreen or new backend concurrency layer.

**NOT TESTED:** live cross-datacenter retrieval, other browser engines, physical mobile devices, screen readers and long-duration browser-memory behavior. Existing Phase 3 performance numbers were not remeasured or changed by this UI phase. Scoped axe/browser checks do not establish universal WCAG certification.

Original Telegram host remains available at http://127.0.0.1:8081. Isolated local browser/Docker fixture deployments are stopped after qualification; their generated data/volumes retained. Private temporary browser/password copies removed and automation sessions revoked. Validated commits remain local; worktree clean at handoff.

Local implementation checkpoints: `a213c3a` (audit/ownership), `b641d33` (workspace, previews, durable transfers and browser harnesses), `9a602f1` (listing error/footer, settled-menu check and transfer count spacing). Final documentation/screenshots checkpoint follows these commits.
