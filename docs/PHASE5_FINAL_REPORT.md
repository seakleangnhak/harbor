# Harbor Phase 5 acceptance

2026-10-21, Asia/Phnom_Penh. **HARBOR_PHASE5 = PASS for private local personal use.** Baseline `63d9fd3`; all eight acceptance gates below were exercised. This does not establish independent security audit, public internet readiness, guaranteed Telegram durability, or live cross-datacenter coverage.

## Acceptance gates

| Required result | Status | Executed evidence |
| --- | --- | --- |
| Complete encrypted backup/restore in fresh isolated environment | PASS | Offline supported SQLite backup captures committed WAL; complete 104,022-byte encrypted live archive restored database/key/config/session into a nonexistent directory. No source directory overwrite. |
| Restored content integrity | PASS | Four real Telegram TXT/PNG/PDF/multipart files passed full SHA-256 after restore and after another restart; full-content audit verified four files. Nested hierarchy, favorite and Trash preserved. |
| Interrupted folder operations | PASS | Durable server jobs; actual SIGKILL after one committed chunk of 33,554,449 bytes; restart finished nine chunks, atomic visibility and exact hash, originals retained. Conflict/cancel/retry/metadata corruption tests passed. |
| Authentication and storage security | PASS, scoped | Expiration/revocation/CSRF/throttle/body bounds/private permissions/AEAD and missing-state fail-closed tests; complete default/tagged race/vet and vulnerability scans. See limitations below. |
| Docker persistence, restart and upgrade | PASS, local adapter | Final image: Phase 4→5→restart→recreate→4→5; 20 full checksum retrievals, unchanged key/folders/favorite/Trash, clean shutdown, schema 0→1. Container encrypted recovery also passed. |
| Existing real Telegram integration | PASS, host and isolated recovery | Fresh encrypted live uploads, multipart restart/resume, restored downloads and actual frontend upload/preview/download/Range passed using existing authorization serially. |
| Phase 4 UI regression | PASS, tested views | Fresh Chromium/WebKit CRUD and nested clipboard, previews, emulated mobile, themes, server-job history/reload/error states; scoped axe zero violations. Chromium fault injection and 1,000-entry virtualization passed. |
| Meaningful limits reported | PASS | Risk register and operational runbooks record provider-loss, copy/backup ceilings, permission migration, browser scope, remote garbage-collection and cross-DC limits. |

## Delivered changes

- Version-2 AES-256-GCM/scrypt encrypted archives, authenticated ordering/end marker, SHA-256 member manifests, complete recovery material and ordered chunk-manifest export. Fresh no-replace restore revokes restored web sessions. The old plaintext HTTP backup is authenticated then rejected with 410; Settings directs owners to secure offline tooling.
- Durable folder-copy API/worker with immutable plans, idempotent operation IDs, restart reconciliation, chunk progress, explicit retry/cancel, atomic visible tree publication and no original deletion. Limit increased from non-atomic browser 1,000 to server 10,000 entries; file-only browser copies retain their existing resumable flow.
- Read-only metadata and full-content integrity commands. Local orphan candidates are review-only library output. No remote garbage collector or automatic repair.
- Protected data/key/session/SQLite paths, byte-preserving 0600 SQLite permission repair, missing-bound-database rejection, safe listener policy and transactional schema-version guard. Internal copy staging cannot be manipulated through ordinary upload endpoints.
- Go 1.26.10 fixes reachable GO-2026-6809. Docker ships recovery CLI, private persistent data, nonroot/read-only/capability restrictions, resource limits, liveness/readiness and graceful shutdown. Existing 0755 volume root requires documented offline 0700 migration.
- Existing evergreen/stone UI preserved. Server copies show real byte/item progress and durable history; retry/cancel/error states work. Recovery instructions replace insecure metadata download. Keyboard-focus defect in scrolling command text was found by axe and fixed; CSP remains enforced.

Workstreams A/B/C/D owned recovery, storage jobs/integrity, security and deployment respectively. Coordinator integrated shared contracts/UI, protected credentials and serial authorization handoff, ran live recovery/browser checks and reviewed final reports. Ownership is recorded in `AGENTS.md`.

## Fresh executable evidence

All commands below completed successfully on final applicable source. `DRIVE_ENV_FILE=-` prevents accidental project-secret loading during synthetic tests. Logs retained privately under ignored `output/phase5/`; reusable harnesses are tracked.

| Command / suite | Status | Evidence |
| --- | --- | --- |
| `DRIVE_ENV_FILE=- go test -count=1 ./...` | PASS | `go-tests.log`: API 6.317s, recovery 3.933s |
| `DRIVE_ENV_FILE=- GOGC=40 go test -count=1 -p 1 -tags telegram ./...` | PASS | `go-telegram-tests.log`: API 6.799s, recovery 4.538s, storage 3.363s |
| `DRIVE_ENV_FILE=- go test -count=1 -race ./...` | PASS | `go-race.log`: API 78.076s, recovery 36.629s |
| `DRIVE_ENV_FILE=- GOGC=40 go test -count=1 -race -p 1 -tags telegram ./...` | PASS | `go-telegram-race.log`: API 71.852s, recovery 33.579s |
| `DRIVE_ENV_FILE=- go vet ./...` and tagged `GOGC=40 go vet -p 1 -tags telegram ./...` | PASS | `go-vet.log`, `go-telegram-vet.log`; no diagnostics |
| `DRIVE_ENV_FILE=- govulncheck ./...` and tagged scan | PASS | `govuln-default.log`, `govuln-telegram.log`; zero affected symbols/imported packages, unused module advisory remains |
| `python3 -m unittest discover -s scripts -p '*_test.py'` | PASS | `python-tests.log`: 16 tests |
| `python3 scripts/acceptance.py --binary bin/drive` | PASS | `local-acceptance.log`: auth/origin, interrupted upload/restart/idempotence/hash/Range, hierarchy/Trash, encrypted restore/session revocation |
| `python3 scripts/phase5_copy_check.py --binary bin/drive` | PASS | `copy-process-final.log`: actual process kill, one retained chunk, full recovered hash and cancel cleanup |
| `cd web && npm run typecheck`, `npm test`, `npm run build` | PASS | `frontend-*.log`: 23 tests, no failures; existing bundle warning remains |
| `cd web && npm audit --json` | PASS | Zero npm advisories; security agent review |
| `docker compose --env-file /dev/null -p harbor-phase5-ops config --quiet` and build | PASS | Final MTProto-capable image built; local adapter selected for isolated deployment suite |
| `python3 scripts/phase5_deployment_check.py --candidate-image harbor-phase5-ops-drive:latest --previous-image harbor-phase4-drive:latest` | PASS | `deployment/evidence.json`, `docker-qualification-final-a11y.log`; six stages and offline encrypted recovery |
| `python3 scripts/phase5_live_recovery_check.py --source-env .env` | PASS, live | `live-dr-evidence.json`, `live-dr.log`; original stopped/locked throughout cloned authorization use |
| Playwright CLI Chromium/WebKit | PASS | `browser-*.json`, `tree-*.json`, `preview-*.json`, `mobile-*.json`, `phase5-surfaces-*.json`, `visual-axe-chromium.json` |
| Chromium controlled failures / 1,000 entries | PASS | `recovery-chromium.json`, `states-chromium.json`, `large-chromium.json`; three bounded chunk attempts, resume skips committed chunks, cancellation prevents publication |
| Original migration preservation | PASS | `original-preservation.json`; existing owner/session/node/upload/chunk rows and original key preserved; `/ready` 200 |

Playwright execution uses `playwright-cli --session NAME run-code --filename SCRIPT`. Existing Phase 4 browser scripts cover actual CRUD/clipboard/previews/states/virtualization; `scripts/phase5_browser_check.js` adds Settings, server history and interrupted-status reload/retry. Axe is fulfilled through a test-only same-origin route; no application CSP bypass. Fixture and state preparation is described in the Phase 4 report and isolated controllers. Test logs can include expected negative-case errors; these cases pass by rejecting invalid operations.

Live browser result: 126,976-byte generated text, SHA-256 `9a0fe2ebc393e711382db281cbf3e459be8c44a1803d9fbab266d7610d47fd40`. Actual saved download matched; preview, HTTP 206 and completed queue passed. Restored four-file hashes are recorded in the [backup report](PHASE5_BACKUP_RESTORE_REPORT.md). No preexisting Telegram messages were deleted.

Final Docker image: `sha256:d28e2d2a9907dd076cc5278fec9b18e23c966c96903207b06aabe70dfac5011d`, 51,073,203 bytes. Four generated fixtures total 4,260,662 bytes/five chunks/eight nodes. SQLite `quick_check=ok`, graceful exit 0, no OOM. This Phase 5 Docker suite uses local storage; real Telegram inside this exact image is **NOT TESTED**. Phase 3 historical live Docker coverage is separate from Phase 5 host/recovery coverage.

Chromium 1,000-directory run: 270 ms fixture creation with four workers; 134 ms reload/load in this local run; 17 list rows and 50/80/50 grid cards at sampled scroll positions, no overlaps, last entry reachable, selected native drag correct, zero automatic content fetches. These are single-run observations, not sustained benchmarks or guarantees. Current main JS 535.85 kB / 170.40 kB gzip; PDF viewer/worker remain lazy; no new frontend dependencies.

## Visual and compatibility evidence

Reviewed generated-fixture screenshots under `docs/screenshots/`: six main desktop 1440×900/laptop 1280×800/mobile 390×844 light/dark views; Settings, copy history and unavailable-state screens; image/PDF/text/video previews; mobile navigation/actions/transfers; loading/empty/error/retry states. No critical layout/accessibility defect remained in tested views.

[Desktop light](screenshots/phase5-desktop-light.png) · [desktop dark](screenshots/phase5-desktop-dark.png) · [mobile light](screenshots/phase5-mobile-light.png) · [mobile dark](screenshots/phase5-mobile-dark.png) · [recovery settings](screenshots/phase5-settings-light.png) · [copy history](screenshots/phase5-copy-history-dark.png) · [error state](screenshots/phase5-copy-status-error.png).

Automated WebKit passed file management/nested clipboard/image/PDF/text/video 206/download/mobile and new Phase 5 surfaces. This is WebKit-engine testing on macOS, not real Safari or physical iOS certification. Scoped axe zero violations is not a complete accessibility or screen-reader audit. Real-account background screenshots remain private and uncommitted.

## Residual limits and explicit exclusions

| Area | Result / boundary |
| --- | --- |
| Live cross-datacenter retrieval | **NOT TESTED**. Protocol/source and synthetic routing/reference tests pass; no safe legitimate new cross-DC placement was reproduced. Secondary redirect from selected media pool fails safely. |
| Final-image live Telegram | **NOT TESTED**. Phase 5 isolated Docker local persistence/rollback plus current host live recovery pass; historical Phase 3 live Docker is not relabeled as this image. |
| Telegram account/channel/ciphertext loss | **PARTIAL protection**. Archive restores local recovery material and references, not independent remote ciphertext. No guaranteed provider durability; maintain independent encrypted content backup. |
| Security assurance | Focused attack-path review and tests pass. No independent penetration test, public deployment qualification, same-user/root defense or offline disk encryption supplied. SQLite names/hierarchy remain host-readable. |
| Authentication operations | Seven-day fixed sessions, logout revocation and process-local peer throttle. No all-session management UI, password/key rotation workflow or distributed throttle. |
| Backup scale/automation | Offline backup; 256 MiB DB, 64 MiB manifests, 250,000 members, 1 TiB payload ceilings. In-memory snapshot can exceed twice DB size; ceiling-scale memory NOT TESTED. Scheduler/rotation/off-provider mirror not installed. |
| Folder-copy scale | 10,000-entry/depth 512/100-root/eight unfinished limits; serial chunk worker. 1,001-entry success and 10,001 rejection verified; 10,000 successful files/live throughput not benchmarked. Sources must remain accessible until copied. |
| Orphans and cleanup | Failed jobs keep recoverable staging until explicit retry/cancel. Provider-send/manifest-commit crash can orphan ciphertext. No broad remote deletion/GC; local orphan candidates library only. |
| Migration/rollback | Only additive Phase 4↔5 qualified. Legacy Docker volume 0700 migration required offline. Quiesce transfers/jobs and verify encrypted backup before change; arbitrary future downgrade unsupported. |
| Streaming/browser limits | Managed verified downloads ≤ 32 MiB; larger transfers use browser progress/backend authenticated chunks. Paused downloads restart. Mobile is emulated; browser media codec support varies. |
| Deliberate provider restrictions | Real FLOOD_WAIT/auth revocation/channel removal not induced. Controlled doubles exercise these paths; no excessive live requests. |

## Operations, checkpoints and handoff

Runbooks: [production operations](PRODUCTION_OPERATIONS.md), [backup/restore](PHASE5_BACKUP_RESTORE_REPORT.md), [storage integrity](PHASE5_STORAGE_INTEGRITY_REPORT.md), [security](PHASE5_SECURITY_REPORT.md), [deployment](PHASE5_DEPLOYMENT_REPORT.md), [risk register](PHASE5_RISK_REGISTER.md). README, architecture, API contract, environment examples and historical acceptance pointer updated.

Validated checkpoints: `58c2112` durable jobs/state protection; `63b81f3` encrypted recovery/live DR; `1139f2f` durable copy/recovery UI and reviewed regression screenshots; `ba7d663` protected Docker upgrades and recovery operations. Final qualification documentation follows in Git history. No remote push or public infrastructure change.

Original Telegram host remains on loopback 8081 with final backend. Existing original Docker 8080 remains untouched. Temporary browser HTTP sessions are revoked and private browser-state copies removed after verification; original account authorization/preexisting owner sessions are preserved. Disposable local 8086 and qualification containers are stopped; generated fixture data/volume and sanitized ignored evidence remain. A verified encrypted pre-upgrade rollback archive and separately protected password remain locally as intentional recovery material. No secret-bearing artifacts belong to Git.

Final handoff checks passed: original `/ready` 200, fixture 8086 stopped, no isolated live-DR directories or Phase 5 test containers, temporary browser sessions revoked, private browser-state files removed, protected encrypted rollback material retained. Actual configured-secret scan covered 221 tracked/proposed files with zero matching files and zero forbidden credential/session/key/archive artifacts. `git diff --check` passed before final checkpoint.
