# Harbor acceptance report

## v1.0 / Phase 6 — October 21, 2026

**HARBOR_V1_RELEASE = PASS for local private personal use.** [Final qualification](PHASE6_FINAL_REPORT.md), [release checklist](RELEASE_CHECKLIST.md), [known limitations](KNOWN_LIMITATIONS.md), [recovery quickstart](RECOVERY_QUICKSTART.md). Exact production image passed live Telegram, multipart, SHA-256, restart/recreation and archive-only fresh-volume recovery. Default/tagged Go/race/vet, frontend, Chromium/WebKit and scoped accessibility regressions passed. This supersedes the final-image live gap in historical reports below. Live cross-DC remains **NOT TESTED**; no independent audit or public readiness implied.

## Phase 5 — October 21, 2026

**HARBOR_PHASE5 = PASS for private local personal use.** Current hardening and disaster recovery qualification: [Phase 5 final report](PHASE5_FINAL_REPORT.md), [backup/restore](PHASE5_BACKUP_RESTORE_REPORT.md), [security](PHASE5_SECURITY_REPORT.md), [storage integrity](PHASE5_STORAGE_INTEGRITY_REPORT.md), [deployment](PHASE5_DEPLOYMENT_REPORT.md), [operations](PRODUCTION_OPERATIONS.md). This phase supersedes historical plaintext-backup and non-atomic folder-copy descriptions below. Live cross-datacenter retrieval remains NOT TESTED; qualification covers private local personal use.

## Phase 4 — October 21, 2026

Premium workspace implementation and qualification: [Phase 4 final report](PHASE4_FINAL_REPORT.md), [UI audit](PHASE4_UI_AUDIT.md), [design system](PHASE4_DESIGN_SYSTEM.md), [UX evidence](PHASE4_UX_REPORT.md). Existing backend/storage format and private Telegram runtime preserved. The Phase 4 report contains scoped acceptance, current test evidence, before/after screenshots and remaining browser/copy/download limits. Earlier phase records below remain historical; live cross-datacenter retrieval is still NOT TESTED.

## Phase 3 — October 21, 2026

**HARBOR_PHASE3 = PASS.** Real Telegram Docker upload/download, session/data persistence across recreation, controlled crash/network recovery, exact hashes, measured performance and regression gates passed. Actual cross-datacenter retrieval remains **NOT TESTED**. [Phase 3 acceptance and evidence](PHASE3_FINAL_REPORT.md), [performance](PHASE3_PERFORMANCE_REPORT.md), [reliability](PHASE3_RELIABILITY_REPORT.md), [Docker](PHASE3_DOCKER_REPORT.md).

Production changes: bounded chunk/envelope allocation, safe geometry validation, seek-cache retention, 512 KiB Telegram parts and timely flood telemetry. Original host restored at http://127.0.0.1:8081; isolated test volume retained and stopped. Phase 2 report below remains historical evidence.

## Phase 2 — October 21, 2026

**HARBOR_TELEGRAM_INTEGRATION = PASS. Local MVP regression = PASS.** All nine required gates verified against the real dedicated Telegram account and intended private channel. Existing configuration values, database, keys, local container and preexisting channel messages preserved. No public deployment or remote push.

| Required acceptance | Result | Actual evidence |
| --- | --- | --- |
| MTProto authentication and channel resolution | PASS | Private native QR approval; persisted-session startup; owner/private/retention checks and link/expected-ID agreement before upload |
| Encrypted upload/retrieval/integrity | PASS | Generated TXT, PNG, PDF and multipart downloaded exactly; six tracked remote chunks independently fetched and checked for HD01 envelope, distinct sampled nonces, AES-GCM identity AAD and chunk SHA-256 |
| Multipart resume and publication safety | PASS | 4,259,861-byte fixture, two chunks; chunk 0 retained across restart; missing completion rejected; cross-chunk Range and full hash matched |
| Restart persistence | PASS | Graceful restarts retrieved all four runner fixtures; separate idle SIGKILL/session restart retrieved all five fixtures including browser file |
| Real frontend storage | PASS | Chromium uploaded 135,168-byte generated TXT, preview matched exactly, UI-managed verified download saved identical bytes/hash; completed queue and Telegram connected |
| Existing MVP regression | PASS | Go default/tagged race tests and vet, local process acceptance, Python tests, frontend tests/typecheck/build, local browser and final Docker runtime |

Implemented: private dotenv loading/process precedence, configured channel guard, durable private session, native connection diagnostics, bounded retry/deadline/cancellation, fixed error redaction, supported OTP delivery notices, visible local phone input, secure QR login, acknowledged upload telemetry and verified managed downloads. Three specialized agents owned separate adapter, reliability and API/frontend/security work; coordinator integrated and qualified live operations.

Original app-code delivery remained unresolved; native QR login succeeded. First real upload exposed same-DC `DC_ID_INVALID` during verification. File stayed unpublished. Primary-API routing fixed retrieval; retained chunk was recovered without duplicate upload. Synthetic routing/bounds/pool-cleanup regressions and live recheck PASS. No security control was bypassed.

| Verification | Result | Scope |
| --- | --- | --- |
| `go test ./...`, `go test -race ./...`, `go vet ./...` | PASS | Current default CLI/API/storage |
| `GOGC=40 go test -race -p 1 -tags telegram ./...` | PASS | Full tagged suite, including QR/auth/channel/routing/bounds/retry |
| `GOGC=40 go vet -p 1 -tags telegram ./...` | PASS | No diagnostics |
| Tagged host build and final Docker build | PASS | Latest source included |
| `python3 scripts/acceptance.py --binary bin/drive` | PASS | Auth/origin rejection, interrupted restart/resume, checksums/Range, hierarchy/trash, snapshot, full recovery/session revocation |
| Python unittest discovery with ResourceWarning as error | PASS | Ten recovery/qualification tests |
| Frontend test/typecheck/build | PASS | Eight tests; production build; main-bundle size warning remains |
| Direct Go / Compose configuration | PASS | Existing values preserved, mapping compared without disclosure; Compose validates |
| Final Docker image/runtime | PASS local | Isolated QA project: healthy, UID 65532, read-only root, all capabilities dropped, no-new-privileges, loopback; four retained-fixture restart checksums after final image replacement |
| Local browser regression | PASS | TXT/PNG/PDF/multipart checksums/previews, corrupt-download rejection/retry, delayed-hash Pause/Resume, synthetic disconnected diagnostics, 320px layout |
| Live API/browser | PASS | Five generated files, six encrypted chunks; all complete checksums equal; browser preview/download and queue verified |
| Permissions / owner boundary | PASS | Environment/session/content key private regular files mode 0600; fresh anonymous live content/diagnostics returned 401 |

Measured multipart upload **0.455 MiB/s**, download **1.111 MiB/s**. Recorded upload API latency **8.929332 s**, download **3.656776 s**. Sampled maximum RSS **45.25 MiB**, CPU **14.4%**; not lifetime peaks. Concurrency one runner request, two MTProto upload workers; chunk size 4 MiB. Failed initial finalization/repair and operator idle time excluded. Detailed measured tables and limits: [performance](PERFORMANCE_REPORT.md).

Evidence retained under ignored `output/phase2/`: `telegram-qualification.json`, `live-browser-checksums.json`, `live-ciphertext-probe.txt`, `live-private-permissions.json`, `live-idle-crash.json`, `docker-qualification.json`, `final-docker-runtime.json`. Live screenshot: `output/playwright/harbor-live-telegram.png`. Fixture bytes/checksums remain reproducible; reports contain no account/channel credentials or session/key bytes. Actual configured-secret scan PASS: 77 tracked/proposed files, zero secret matches, no environment/session/key artifacts tracked.

**NOT TESTED:** live Telegram authorization inside Docker, actual cross-DC storage, induced live network loss/flood waits, crash within an in-flight request/database commit, sustained throughput, true lifetime peak RAM and Telegram disaster restore. Idle SIGKILL and synthetic retry tests do not prove those cases.

Known limits: whole-file completion re-reads all remote chunks; crash before manifest commit can leave encrypted orphan; broad orphan cleanup deferred. SQLite filenames/hashes remain host-readable. Managed browser downloads cap verification at 32 MiB; larger files use browser progress and backend authenticated chunks. Paused managed downloads restart. Vite bundle warning remains. Existing local MVP limitations below remain applicable.

Checkpoints: `0fab10c` configuration/lifecycle/diagnostics; `435cbbf` reliability runner; `57dc728` transfer UX; `03ca826` initial evidence; `f1cadc8` safe login diagnostics; `b68ae75` secure QR and same-DC retrieval fix. Final documentation checkpoint records completed qualification. Worktree clean after commit; no push. Live host service retained on loopback port 8081. Separate QA container stopped, fixture volume retained, temporary password copy removed. Automation browser session revoked after acceptance.

Detailed reports: [Telegram](TELEGRAM_INTEGRATION_REPORT.md), [reliability](RELIABILITY_REPORT.md), [security](SECURITY_REVIEW.md), [performance](PERFORMANCE_REPORT.md), [operation](../README.md).

## Phase 1 historical baseline

The records below describe the earlier local MVP and subsequent pre-Phase-2 follow-ups. Their statements about absent credentials are historical; current configuration and live status are recorded above.

Verified October 20, 2026, Asia/Phnom_Penh. Local macOS development; Linux container verification. No public deployment or remote push.

## Acceptance status

**PASS — local adapter core.** Owner can authenticate, create nested folders, upload encrypted chunks, resume after server restart, browse/preview supported files, download identical bytes, and restore trashed subtrees. A reproducible Go server, frontend source, Docker deployment and recovery tooling are included.

**PARTIAL — Telegram integration.** MTProto adapter, dedicated-account terminal setup, private-channel validation, encrypted document storage, media-DC download, reference refresh, flood-wait retry and cancellation are implemented and compile/test with `-tags telegram`. No dedicated account/session was supplied. Real Telegram authentication, uploads, downloads, deletion and service behavior are **NOT TESTED**.

Docker build/start/health, container roundtrip and browser reload/resume pass. Chromium video seeking and responsive checks also pass.

## Delivered behavior

| Area | Status | Scope |
|---|---|---|
| Owner authentication | PASS | Generated owner secret, bcrypt hash, opaque revocable HttpOnly/SameSite session, login throttle, same-origin mutations |
| Metadata and hierarchy | PASS | Folder/file rename and move, cycle rejection, case-insensitive sibling conflict checks, breadcrumbs, favorites, global search, sorting and exclusive type filters |
| Bulk operations and trash | PASS | Per-item move/favorite/trash/restore results; subtree recovery; durable permanent-deletion intent blocks unsafe restoration after partial deletion |
| Encrypted storage | PASS | AES-256-GCM per chunk, random nonce, identity-bound AAD, chunk/full-file SHA-256, ciphertext-only local blobs |
| Transfers | PASS | Multiple files, persistent queue, pause/retry/reselect, chunk status, idempotent retries/completion, restart recovery and empty files |
| Downloads | PASS | Streaming authenticated chunks, HTTP Range/HEAD, first-chunk preflight, corruption rejection and stream abort |
| File manager | PASS | Responsive sidebar/toolbar, list/grid, multi-selection, context menus, inspector, light/dark appearance, optimistic folder/favorite operations, list virtualization and explicit pagination |
| Previews | PASS | Image, Unicode text, empty text, PDF and generated H.264 video preview/seeking passed in Chromium; other audio/video codecs remain browser-dependent |
| Recovery and diagnostics | PASS | Storage/SQLite diagnostics, private consistent metadata snapshot, stopped-server encrypted-content archive, fresh-directory restore, archive hashes and session revocation |
| Docker deployment | PASS | Final image build/start/health, non-root/read-only runtime, loopback publishing, container restart/resume and checksum roundtrip |

## Executed automated verification

| Command/check | Result | Evidence |
|---|---|---|
| `go test ./...` | PASS | CLI, API and local engine tests |
| `go test -race ./...` | PASS | Final default build; no races reported |
| `go vet ./...` | PASS | No diagnostics |
| `GOGC=40 go test -race -p 1 -tags telegram ./...` | PASS | Full tagged CLI/API/storage suite, including retention/flood-wait/session/reference validation |
| `GOGC=40 go build -p 1 -tags telegram -trimpath -ldflags='-s -w' -o bin/drive-telegram ./cmd/drive` | PASS | Executable built; this does not verify Telegram network behavior |
| `go build -trimpath -o bin/drive ./cmd/drive` and `python3 scripts/acceptance.py --binary bin/drive` | PASS | Real child processes: rejected auth/origin, interrupted transfer/restart, exact SHA-256/bytes, cross-chunk Range, hierarchy, rename/move/favorites, subtree recovery, zero-byte content, snapshot, full backup/restore and logout/session revocation |
| `python3 -W error::ResourceWarning -m unittest discover -s scripts -p '*_test.py'` | PASS | Seven tests: archive roundtrip, secret exclusion/session revocation, wrong key/live backup, traversal, malformed manifest, missing blob, output race and shared limits |
| Large-list API regression | PASS | 10,080 active children; hidden nested trash, totals/offsets, default/max pages, sorting, search escaping and category precedence |
| `npm test` | PASS | Four tests: resumable missing indices, incremental SHA-256/empty content, filename validation and metadata categories |
| `npm run typecheck` | PASS | TypeScript check |
| `npm run build` | PASS | Vite production bundle; PDF viewer/worker load lazily |
| `npm audit --omit=dev` | PASS | Zero reported vulnerabilities |
| `govulncheck` default and `-tags telegram` | PASS | Zero reachable findings. Module-only `GO-2026-5932` remains for unused unmaintained `golang.org/x/crypto/openpgp`; application does not import it |
| `docker compose config --quiet` | PASS | Compose validation |
| Docker build/start/health/roundtrip | PASS | Go 1.26.9 tagged image; non-root startup, interrupted 8 MiB+ transfer across container restart, exact bytes/SHA-256, cross-chunk Range, rename/favorite, subtree restore and permanent deletion; final frontend assets included |

Go was upgraded to 1.26.9, Chi to 5.3.0 and x/net to 0.60.0 after vulnerability scanning. No claim of a complete penetration test or security guarantee is made.

## Browser acceptance

Playwright CLI drove a real Chromium browser against Go-served production assets and disposable local fixtures. Passed: login, create/open/rename folder and breadcrumbs, multiple text/PNG/PDF/empty-file uploads, completed transfer queue, Unicode and empty text previews, image preview, PDF rendering/download equality, F2 rename, dialog focus trap, search, favorites, move, subtree trash/restore, list/grid and dark appearance. At 320px, document/body width matched viewport without horizontal overflow.

Reload/resume passed with a real 8,388,649-byte file: chunk 0 persisted at 4,194,304 bytes, network interruption stopped later chunks, reload required original-file selection, only chunks 1 and 2 uploaded, and browser-downloaded SHA-256 matched `aca33fe0c83c2ee614ab2a1e7cceb0656eb905b3a777a42f54ed859dd2773ed4`.

A 1,000-folder fixture created through API rendered only 18 list rows; scrolling reached folder 1,000. Grid rendered 60 cards and reached last folder. Axe WCAG A/AA/2.1AA scans: desktop zero violations/23 passing checks; 320px mobile zero violations/28 passing checks. Scans cover tested states, not every application state or complete accessibility compliance.

Generated five-second H.264 fixture uploaded through UI and previewed successfully. Seeking to 3.0 seconds reached `readyState=4` without media error; authenticated media request returned HTTP 206 with Range/Content-Range. Tablet viewport 768px also matched document/body width without overflow.

Final Docker-served PDF assets were rechecked separately: uploaded fixture rendered visible 1936×2581 canvas, extracted expected text and zoomed to 125% without font/CMap warnings or errors.

Settings browser gate also passed against final Docker: diagnostics loaded; metadata backup downloaded as 65,536-byte SQLite file with `PRAGMA integrity_check` returning `ok`. Fixture file was permanently removed and session revoked afterward. Temporary credential and downloaded database copies were deleted.

Screenshots: `output/playwright/harbor-mobile-320-light.png`, `harbor-mobile-320-dark.png`, `harbor-tablet-768.png`, `harbor-1000-folders-list.png`, `harbor-pdf-preview.png`, `harbor-docker-pdf-preview.png`, `harbor-docker-settings.png`, `harbor-resume-after-reload.png`, `harbor-video-seeking.png`. Resume/video/PDF/backup JSON and axe output are alongside screenshots. Generated evidence and scan/build logs live under ignored `output/`; dependency/runtime/data artifacts are excluded from Git.

## Security and recovery review

Every file/transfer/diagnostic/backup API route requires owner session. No signup, JWT mint endpoint, public file URL, browser Telegram credential exchange or implicit adapter fallback exists. Filename/path validation, bound bodies, parameterized SQL, safe static-root access, CSP and safe inline MIME allowlist constrain untrusted inputs. Local secret files require private permissions and regular files. Process lock prevents concurrent servers on one data directory.

Review fixes include private temporary metadata snapshots, chunk preflight and corrupt-stream termination, static symlink containment, Telegram channel binding and upload message matching, durable permanent deletion, cancellation of unpublished completed transfers, archive output race handling, malformed/missing-blob rejection, matching backup/restore ceilings, missing binding fail-closed startup and Telegram auto-delete rejection.

## Remaining limits

- Real Telegram account operations remain NOT TESTED; no credentials/session were available. Keep dedicated private channel auto-delete disabled, including after startup.
- A crash between blob write and SQLite insertion can leave unreferenced encrypted blobs/messages. Safe upload resume works; automatic orphan enumeration/garbage collection is deferred.
- Browser reload requires selecting original file again. Its size/name/full SHA-256 must match before resumed publication.
- Large completion verifies all chunks while holding metadata mutation lock. Reads remain available; folder edits and new upload creation can wait during verification or remote permanent deletion. Real Telegram latency and large-scale throughput were not measured.
- Content is encrypted; SQLite filenames, sizes and hierarchy are not. Key loss means unrecoverable content. Preserve original key and account/session separately from content archives.
- Full recovery script caps archives at 250,000 files and 1 TiB uncompressed including metadata. It rejects larger backups before creating an archive. Online UI backup contains metadata only.
- Text preview caps display at 256 KiB. PDF raster rendering has bounded canvas and extracted text; complex PDFs and every font/codec/browser combination are not exhaustively tested.
- Chromium interaction checks do not replace Safari/Firefox, assistive-technology or real-device acceptance; those are NOT TESTED. No exhaustive WCAG certification or external security audit was performed.
- Owner-password rotation UI/CLI is not implemented. Configuration changes do not reset initialized owner password; preserve original generated secret separately.
- No sharing, multi-user access, content overwrite/version history or public deployment; MVP remains a private modular monolith.

No unresolved FAIL is recorded for completed acceptance checks. PARTIAL and NOT TESTED items above remain explicit scope/verification limits. Final image serves byte-identical built JS/CSS and passed a fresh 8 MiB+ checksum roundtrip. Image runs as UID 65532 with read-only root, all capabilities dropped and no-new-privileges. Image size observed at approximately 40.8 MB; idle memory around 17 MiB was a single observation, not a performance benchmark.

## Operation

See [README](../README.md) for local/Docker startup, configuration, terminal Telegram setup and offline recovery. [Architecture](ARCHITECTURE.md) and [API contract](API_CONTRACT.md) describe boundaries. Checkpoints: architecture `b104c39`, working core `cab1e2a`, security/recovery `585b0b2`, responsive frontend `7f1db68`; final deployment/report checkpoint records this acceptance. No remote push was made.

## October 21 follow-up: existing channel discovery

**PASS — implementation and offline verification.** New `telegram-channels` command authenticates through server terminal, searches normal/archived dialogs, and lists eligible owned private channels with titles, positive MTProto IDs and signed access hashes. It creates no channel and does not bind drive storage. Both Telegram commands require interactive input/output and acquire server's data-directory lock. Documented `data-*` directories are now ignored by Git and Docker builds.

| Fresh check | Result | Evidence |
|---|---|---|
| `go test ./...` and `go vet ./...` | PASS | Default CLI/API/storage suite; no vet diagnostics |
| `go test -race ./cmd/drive` | PASS | CLI registration, nonterminal rejection and exclusive-lock regressions |
| `GOGC=40 go test -race -p 1 -tags telegram ./cmd/drive` and tagged CLI vet | PASS | Both Telegram commands reject unsafe terminal/concurrent execution |
| Tagged storage tests, race tests and vet | PASS | Fake gotd RPC: paginated normal/archive dialogs, cursor preservation, deduplication, ordering, channel/retention filters, cancellation and empty results; unexpected/mutating RPC rejected |
| Tagged host CLI build | PASS | Updated executable built successfully |
| `docker compose build` | PASS | Updated tagged production image built |
| Disposable Docker `telegram-channels` invocation | PASS | Fake API credentials, private tmpfs data directory: recognized command, exit 1 with interactive-terminal requirement; no real login attempted |
| Live Telegram login and channel discovery | NOT TESTED | No dedicated account credentials/session supplied; user enters login privately in terminal |

Frontend and API behavior did not change in this follow-up; browser acceptance above remains October 20 evidence. Existing local container/data were preserved. Telegram integration remains PARTIAL until live-account acceptance.

## October 21 follow-up: private links instead of manual access hashes

**PASS — implementation and offline verification.** `USERNAME_OR_CHANNEL_LINK` accepts existing owned private channel invite links (`t.me/+...`, `t.me/joinchat/...`) and private message links (`t.me/c/ID/MESSAGE`). Setup/server resolve the access hash internally. Public usernames remain rejected. Invite resolution uses `messages.checkChatInvite` and requires existing owner membership; no automatic join/import occurs. Supplied links cannot fall through to channel creation. Explicit ID/hash configuration remains supported but cannot be combined with a link; malformed/partial numeric values and whitespace-only selectors are rejected.

Startup resolves within its authenticated MTProto connection, then checks the numeric channel ID against storage binding before opening SQLite/engine. Changing a link cannot redirect an existing drive. Invite tokens are omitted from validation/RPC failure messages. No credentials were collected and no dependency/cache was added.

| Fresh check | Result | Evidence |
|---|---|---|
| `go test ./...`, default CLI race check and `go vet ./...` | PASS | Configuration conflicts, signed hashes, malformed values, exclusive lock/terminal requirements and rejected binding changes preserving metadata/binding bytes |
| Tagged storage tests/race/vet | PASS | Real parser plus fake gotd RPC tests: Already-only invites, private/owner/retention enforcement, archived message links, legacy lookup, invalid-selector preflight, cancellation, redaction and rejection of unexpected/mutating RPCs |
| `GOGC=40 go test -race -p 1 -tags telegram ./cmd/drive` and tagged CLI vet/build | PASS | Updated tagged executable built, CLI checks passed |
| Tagged executable smoke | PASS | Malformed/blank selectors rejected; syntactically valid invite with no session rejected; no token echoed and no SQLite/storage binding created; temporary data and fake API credentials only |
| `python3 scripts/acceptance.py --binary bin/drive` | PASS | Actual local server: auth/origin rejection, interrupted/restarted upload, exact bytes/SHA-256, Range, hierarchy/trash, snapshot and full recovery |
| Compose validation and `docker compose build` | PASS | Updated production image includes link configuration |
| Isolated Docker selector checks | PASS | Invalid/public selectors and missing session fail as expected; redacted errors, fake API credentials, read-only root and disposable tmpfs data |
| Live Telegram private-link login/lookup/storage | NOT TESTED | Requires dedicated account session and human terminal login |

Telegram integration remains **PARTIAL**. Message-link resolution currently scans eligible normal/archived dialogs; an unrelated channel retention RPC failure can block target resolution. Prefer an active private invite link to avoid that scan. Revoked/expired invites need a valid replacement for the same channel. Existing local container/data were preserved; browser acceptance was not repeated because frontend/API code did not change. No new unresolved FAIL or remote push.
