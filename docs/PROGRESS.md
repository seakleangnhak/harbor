# Progress

## Milestone C — local v1.3.0 release readiness PASS

Two independent 30-minute mixed-transfer intervals pass at 768/1024 MiB, with
checksums, queue drain and zero OOM events; cgroup page-cache pressure remains
explicit. Full default/tagged/race/vet checks, 54 frontend tests, 14 browser
suites/139 axe checks, RC live Telegram, encrypted recovery and isolated
v1.2.1 upgrade/rollback pass within their recorded scope. Mobile navigation
stacking and two reproduced PDF cancellation rejection paths are fixed.

Fresh final-patch browser media now passes 1800.006 seconds/30 cycles, one
restart, 480 MiB upload/download SHA, zero browser errors/OOM and clean late
metadata. Earlier host-full/Desktop interruptions remain documented; original
Harbor is healthy after authorized recovery and owner cleanup recovered about
29 GiB. Source defaults are locally prepared as 1.3.0; published v1.2.1 and
Compose pull default remain unchanged.
Exact prepared ARM64/native and AMD64/emulated image smokes, repeated identical
multiarch OCI exports and the prepared image's five-stage local upgrade/rollback
cycle pass. Exact prepared 1.3.0 live repeat passes 900.011 seconds/five paced
cycles: multipart/full SHA/ranges, three concurrent uploads, storing-state
SIGKILL recovery, restart/recreation, encrypted archive-only candidate restore
and fresh v1.2.1 rollback. Both restored starts exclude source volume. Original
authorization is unchanged; copies removed and source lock released. Final
documentation-only packaging maps back to the same qualified runtime; final
source/policy/scanner receipts remain in ignored qualification output.
**HARBOR_V1.3_RELEASE_READY = PASS** within documented local/private scope.
No tag, push, publication or deployment upgrade occurred.
See [final gate](V1.3_RELEASE_FINAL_GATE.md) and its four qualification reports.

## Milestone B — PASS, local 1.3.0-rc.1 only

Starting checkpoint `b6ae39e` preserved. Reader boundary replay avoids a duplicate
100 MiB fetch/decrypt; HTTP HEAD/304/412/416 avoids body reads; staged assembly
reuses one private 4 MiB buffer. Offline diagnostics and explicitly confirmed,
revalidated/audited local cleanup preserve unknown data, recoverable jobs and all
Telegram objects. No format/schema/queue/worker increase or plaintext derivative.

Full default/tagged Go tests, both race modes/vets, 48 frontend tests/typecheck/build,
12 Chromium/WebKit suites and 129 scoped axe checks pass. Latest range fix has
four-mode focused recheck. Real PDF/image/WebM play/seek, mobile/themes and mixed
upload/SHA pass; MOV works in tested WebKit and is unsupported in Chromium.
10,000-folder listing loads in 318/509 ms in one local fixture observation.

Identical-harness 65 s sustained mixed pair plus restart/eviction passes at
768 MiB hard/no swap, 512 MiB Go soft: current 60 uploads, 123 full downloads,
366 ranges, two 300 MiB copies. Lifetime process RSS 520.05 MiB; cgroup reaches
its limit with substantial reclaim, so spare headroom is not claimed. Allocation
benchmark falls about 96%; single local pair is not a universal speed claim.
Actual staging ENOSPC/retry, preview SIGKILL, backup/fresh restore and copy crash
checks pass. Remote orphan count stays unknown; actual cleanup SIGKILL untested.

Current-source live 100 MiB + 17 upload including verification: 38.078 s /
2.626 MiB/s; full download 19.119 s / 5.230 MiB/s. Initially cold cached range
21.764 s, warm 77–87 ms; four Gets → one for four overlapping reads. Cold whole-
chunk latency persists. Exact local RC image additionally passes 5 MiB HTTP
multipart, SIGKILL recovery without reselection, SHA/206, recreation/session
persistence and clean shutdown. Source authorization unchanged/copies removed.

Local image `harbor:1.3.0-rc.1`, ARM64 ID
`sha256:344373078e8bac44a5a6732d8b1fb6a23a164d589a39ff484fbd0ff0273bba2f`.
Published v1.2.1/Compose pull default retained. No push, tag, image publication or
Dokploy upgrade. Cross-DC, live browser media cold latency, physical Safari/iOS,
power loss and long live soaks remain NOT TESTED. Local qualification is not an
independent audit or public-internet approval.

[Final report](MILESTONE_B_FINAL_REPORT.md), [preview](PREVIEW_PERFORMANCE_REPORT.md),
[memory](MEMORY_QUALIFICATION.md), [mixed load](MIXED_WORKLOAD_REPORT.md),
[hygiene](STORAGE_HYGIENE_REPORT.md), [candidate/rollback](V1.3_RELEASE_CANDIDATE.md).

## Milestone A transfer stabilization — PASS, local candidate only

Maintained export branch `codex/transfer-engine-stabilization`; build `1.3.0-dev.1`.
Checkpoint `59aa6d8` preserves all unreleased source/audit work; `c99caef` fixes
remote success/failure SQL updates blocked behind a slow next browser body.
No worker/queue/chunk-size increase. Cold-cache deadlock fix retained.

Fresh default/tagged Go tests and both race variants, both vets, 48 frontend
tests/typecheck/build, eight Chromium/WebKit suites and 108 scoped axe checks
pass. Adequate-memory 8 GiB isolated VM closes tagged-race compiler blocker.
Nine real child-process SIGKILL boundaries recover with SHA-256/no premature
publication. Final-image executables pass encrypted backup/fresh restore and
three-chunk copy SIGKILL recovery. Mixed production-size upload/copy/ranges pass
at 768 MiB hard memory, but reach that limit; no headroom claim.

Fresh isolated QR authorization enables bounded current-source live PASS:
100 MiB + 17 upload including verification 47.374 s / 2.111 MiB/s; full download
23.439 s / 4.266 MiB/s. Four overlapping ranges: 90.893 s/four Gets uncached,
23.025 s/one Get cold cache; warm ranges 76–97 ms. Exact production image also
passes live 5 MiB HTTP multipart, SIGKILL/restart without reselection, SHA/206,
container recreation/session persistence and clean shutdown. Cloned sessions
removed after stopping targets; original deployment stays healthy.

[Final report](TRANSFER_ENGINE_FINAL_REPORT.md), [crash matrix](TRANSFER_CRASH_MATRIX.md),
[performance](TELEGRAM_PERFORMANCE_COMPARISON.md), [cache](CACHE_QUALIFICATION.md),
[candidate gate](V1.3_RELEASE_CANDIDATE.md) supersede historical PARTIAL entry below.
Live cross-DC/PDF/video, physical iOS, power loss and sustained full-capacity
stress remain NOT TESTED. Orphan/staging cleanup, cold 100 MiB fetch granularity
and native large-download progress remain limits. No push, tag, publication or
deployment upgrade.

## Background saving and read reuse — PARTIAL

Current scope: overlap browser parts with durable backend saves while keeping 100 MiB Telegram chunks, 4 MiB client parts, encrypted staging and remote readback verification. Separate storage, frontend and reader-cache agents own disjoint files; coordinator owns contracts/API/startup/tests/docs. Source adds a two-chunk per-upload/four-chunk global staging window and a single joined saving worker; a sender/monitor handoff also lets the next small file start while prior storage work finishes. Cache only encrypted reader chunks in bounded private storage, authenticating every read. Final verification no longer holds the global metadata-mutation lock; publication repeats conflict/storage checks.

Fresh isolated audit execution now passes focused/full default Go tests and race, 48 frontend tests/typecheck/build, eight Chromium/WebKit suites with 112 screenshots/108 scoped axe checkpoints, exact ARM64 image smoke, encrypted fresh restore and folder-copy SIGKILL recovery. Audit reproduced and fixed a cold-cache mutex deadlock and repaired a stale copy-crash fixture for 100 MiB geometry. Production-flag Telegram-tagged tests and both vet variants pass; fresh tagged race remains NOT TESTED after three compiler memory failures. Synthetic cache reads reduce remote Gets 4 → 1 with no demonstrated allocation reduction or live speed claim. Full results and failed-attempt explanations are recorded in the [current-state handoff](CURRENT_PROJECT_STATE.md). New upload-worker mid-save process termination, mixed resource loads and current live Telegram remain NOT TESTED. Existing private runtime, original repository/history and releases remain untouched; no commit, push or deployment. [Evidence and limits](TRANSFER_PIPELINE.md).

## v1.2.1 — corrective release and registry qualification PASS

Released [v1.2.1](https://github.com/seakleangnhak/harbor/releases/tag/v1.2.1) from `dbbe3d0`. Corrects PDF.js text-stream cancellation found by post-release WebKit checks; strict errors and four repeated PDF lifecycle cycles remain in both engines. Rolling Go cache keys restore compatible artifacts and save refreshed results per successful commit, fixing immutable dependency-only snapshots. Existing test commands, bounded offline execution and owner/tag/package gates remain.

[Exact-source CI 39661996927](https://github.com/seakleangnhak/harbor/actions/runs/39661996927) passed all four jobs: cold backend 20m59s, 58 Python tests, 123/160 default/tagged race tests, both vet variants, deterministic binaries, 41 frontend tests, both native image smokes and eight browser suites with 110 screenshots/106 axe checkpoints. Cold migration saved cache ID `9299923521`; warm timing is recorded separately in release notes.

[Publisher 39665307955](https://github.com/seakleangnhak/harbor/actions/runs/39665307955) passed all gates and both actual pulled-image checksum/restart/recreation checks in 5m12s. Tags `1.2.1`, `1.2`, `latest` share `sha256:87553d2564da6f990e6be18a7b8946833b7600a22b0287d72ad6042077253a74`; independent raw registry/config/attestation checks passed. Original history, runtime state and versioned tags/images remain intact. New live Telegram/cross-DC/video qualification and signature verification remain NOT TESTED. [Complete receipts and limits](RELEASE_1.2.1.md).

## v1.2.0 — CI/CD, release and registry qualification PASS

Owner authorized sanitized-source push, a new version and GHCR image publication. Version 1.2.0 includes preview, staged client transport, account/settings and minimal-environment changes. Original private repository/runtime data and historical tags remain preserved. Existing source repository is private; owner API confirms the existing public package belongs to this repository and matches the pinned identity. No visibility change or remote application deployment.

Accepted [main CI 39655816126](https://github.com/seakleangnhak/harbor/actions/runs/39655816126) passed all four jobs for `335b1a4` in 4m12s. Cold backend execution passed Go/race/vet; warm backend finished in 1m45s with cached Go results. Python 58 tests, frontend 41 tests/typecheck/build, native AMD64/ARM64 Docker smokes, all eight Chromium/WebKit suites, 110 screenshots and 106 axe checkpoints passed. Browser transport verified real process restart/resume and 105,906,299-byte checksum equality. Twelve key screenshots reviewed with no critical visual defects. Previous partial account/UI sections below are historical and superseded by this qualification.

Pipeline removes duplicate work and costly tooling image export, reuses compilation cache and proves successful exact-commit main CI before stable publication. Owner/tag/package/security gates remain; ordinary superseded CI cancels while release publication stays serialized. Annotated `v1.2.0` was pushed only after source qualification. [Registry run 39656653561](https://github.com/seakleangnhak/harbor/actions/runs/39656653561) passed in 5m39s, including both published-image checksum/auth/onboarding/restart/recreation smokes. At v1.2.0 qualification, `1.2.0`, `1.2` and `latest` shared index `sha256:13ea2d9146ce0151684cc232d44b79bc3edd68e026dfb7fba2a6f75c04d27378`; platform manifests, OCI labels and SPDX/SLSA attestations independently checked. [GitHub release](https://github.com/seakleangnhak/harbor/releases/tag/v1.2.0) published. Gitleaks scanned all 28 sanitized reachable commits before tagging with zero findings. Local Docker remains unavailable; tests used isolated hosted containers. New live Telegram/cross-DC and optional video seeking/MOV checks remain NOT TESTED. [Release ledger and measured timing caveats](RELEASE_1.2.0.md).

## Account settings visual refinement — source complete, qualification PARTIAL

Source audit found a crowded settings flow, weak profile-save feedback and password errors without reliable field focus. Settings now separates Account, Storage and Recovery using native button navigation and named content regions. Desktop uses compact side navigation; mobile uses horizontal navigation, 44px minimum form controls and a bounded scrolling panel with a persistent footer. Existing evergreen/stone theme tokens supply light/dark colors.

Account includes owner identity, display-name count, explicit unsaved/saved states and cancel. Password editing starts collapsed, clears secrets when cancelled or leaving Account, reports actual UTF-8 length/matching requirements and focuses invalid fields. Busy forms guard navigation and sign-out. Storage includes real loading/error/retry states; recovery uses a keyboard-operable disclosure for existing server instructions. Modal focus restoration includes the mobile navigation launcher. Existing account API and session-revocation behavior remain intact.

**PASS (static only):** fresh `git diff --check` and `node --check scripts/account_check.js`; independent final source review found no further actionable issue. Browser harness now covers six widths, both themes, 32 planned screenshots/axe contexts, navigation/disclosure keyboard behavior, focus trapping/restoration, validation focus, secret clearing and account regressions. These checks are prepared, not executed.

**NOT TESTED:** rendered visual quality, screenshots, browser/axe checks, frontend tests/typecheck/build and account backend/recovery execution. Docker Desktop remains unavailable with a missing daemon socket. Project isolation rules prohibit moving target execution onto the host. Earlier preview/transport evidence does not qualify these changes. Original repository remains clean; earlier export changes are preserved. No dependencies, real-account changes, commits, publication or deployment.

## Owner account settings — implementation complete, qualification PARTIAL

Added Profile and Password sections to Settings. Saved Unicode display names update the sidebar/topbar initials through the session cache. Password forms verify current/new/confirmation inputs, use masked fields with explicit reveal buttons, show UTF-8 byte guidance, and clear secrets after success. A successful change returns to sign-in with a confirmation notice. Wrong-current errors preserve the authenticated workspace; expired/revoked sessions clear private query caches, including background session-check failures.

Authenticated same-origin profile/password routes use bounded input validation. An additive singleton profile preserves legacy owner data. Password changes serialize against login issuance, save only a bcrypt hash, and atomically revoke every web session with the recovery timestamp. Existing initialization secrets cannot reset the changed hash on restart. Complete recovery omits obsolete automatically discovered initialization passwords after a change; explicit recovery password sources remain verified. Encrypted content keys, manifests and Telegram authorizations are unchanged by these handlers.

Backend tests were added for migration, name/password bounds, authentication/origin rejection, throttle, session revocation, restart, transaction rollback and concurrent login/change requests. Recovery tests cover actual account changes, encrypted backup/verify/fresh restore, both legacy and browser setup layouts, current/old-password login, profile preservation, content equality, two restarts, explicit stale-password rejection and older archives without profile metadata. Frontend tests cover names/initials and account request/error contracts. `scripts/account_check.js` guards against initialized installations and prepares fresh-fixture browser, theme/mobile/screenshot and optional axe checks.

**PASS (static only):** `git diff --check`, `gofmt -l` for the changed Go files, and `node --check scripts/account_check.js`. Independent source review found no blocking account/recovery defect. Original development worktree remains clean; earlier preview/transport changes are preserved.

**NOT TESTED for these account changes:** Go tests/race/vet, frontend tests/typecheck/build, browser interactions/screenshots/axe, Docker image and live Telegram regressions. Docker Desktop remains unavailable (missing daemon socket) after the prior disk-space incident. Existing project rules require isolated no-network containers; target execution was not moved onto the host. Earlier successful preview/transport runs do not qualify this newer source. No account credentials or private data were accessed; nothing was committed, published or deployed.

Next qualification: restore Docker Desktop, then run isolated `go test -p 1 ./...`, `go test -p 1 -race ./...`, `go vet -p 1 ./...`, frontend `npm test`, `npm run typecheck`, `npm run build`, and `scripts/account_check.js` in Chromium/WebKit with a new disposable local-adapter setup. Build/test the resulting image before releasing account settings.

## Client transport parts — local qualification PASS

Verified 2026-10-22. Existing 100 MiB storage geometry, ciphertext format, keys and manifests remain intact. Browser file uploads now send serial 4 MiB parts with individual SHA-256 acknowledgements, encrypted private staging and an explicit storage-chunk commit. Per-upload locking permits one pending storage chunk, up to 25 parts. A remote commit failure retains local parts; missing/corrupt parts can be repaired using the original file. Stored chunks remain idempotent across a stage-cleanup failure/restart. Existing whole-chunk clients and copy jobs remain supported.

Transfer UI separates transient sent bytes, durable server acceptance and adapter-committed bytes. Native XHR progress is throttled for rendering; speed/ETA describe sending to Harbor, while saving and final integrity verification have separate states. Part and commit retries are independently bounded to two retries. Browser pause/reload/reselection resumes accepted parts; progress remains below 100% until verified publication. Transient sent bytes never enter durable browser state.

| Verification | Result |
| --- | --- |
| `go test -p 1 ./...`, `go test -p 1 -race ./...`, `go vet -p 1 ./...` | PASS in isolated no-network containers; Telegram-tagged API/storage tests also PASS |
| `npm test`, `npm run typecheck`, `npm run build` | PASS; 38 frontend tests; no new dependencies |
| New storage/API transport tests | PASS; actual 100 MiB aggregation plus tail, exact bounds/digests, authentication/origin/copy-stage guards, duplicate/concurrent retries, cancel, missing/corrupt stages, malformed metadata and remote-commit/cleanup crash window |
| Encrypted `Backup`/`Verify`/fresh `Restore` with pending parts | PASS, including focused race test; archive-only key/config/metadata recovery, committed checksums, absent staged files resent from original and restored DB restart |
| `scripts/transport_check.js`, Chromium and WebKit | PASS; 105,906,299-byte fixture, two storage chunks, 28 client requests each at most 4,194,304 bytes; one part-network failure, pause/reload/reselection and one HTTP 503 commit retry; acknowledged parts retained; final SHA-256 `6029196e8fead8bd9005dd9f756b85228a075503dada2f5de6dbc7ab6e9f6994` |
| Transfer visual/accessibility checks | PASS; reviewed desktop/mobile saving views, four axe audits with zero violations and zero uncaught browser exceptions; real native upload progress events in both engines |
| Fresh preview and 1,000-folder regressions | PASS in both production engines; another 32 axe audits, zero exceptions/overflow; PNG/JPEG/PDF/text/audio/WebM checksums, seeking/HTTP 206, thumbnail limits and virtualized listing preserved; MOV decoding remains browser dependent |
| Final ARM64 production Dockerfile and `scripts/ci_check.py docker-smoke --image harbor-transport:local --platform linux/arm64 --version 1.1.0` | PASS; encrypted legacy multipart checksum, onboarding/auth/CSRF, Range, resume/restart/container recreation, non-root permissions/health and clean shutdown |

Local image index: `sha256:eb32542c8c6f3718f2ae33e2833dfb01cdbb2fab7dfee97d104f8921a1a3c88a`. Evidence remains ignored under `output/transport-check/`; successful transfer runs initially recorded `chromium/transport-4ntVzn` and `webkit/transport-jFAoxg`, preview runs `preview-chromium/preview-zMHJAp` and `preview-webkit/preview-yGVa6p`. Browser body inspection uses actual intercepted request bytes in Chromium; WebKit requires inspecting the actual Blob passed to native XHR because Playwright omits its POST body. The server and complete download independently verify digests in both engines.

Limits: pending local disk usage is up to one 100 MiB chunk per upload, plus encryption overhead. Recovery archives deliberately omit uncommitted part files; the original file is required to resend them. Individual browser file copies still use whole storage chunks to avoid repeated remote-range reads. Crash-created unreferenced encrypted blobs remain a conservative/manual cleanup ceiling. This task used generated local-adapter fixtures; new live Telegram performance, physical Safari/iOS and remote deployment are NOT TESTED. No private runtime files, original repository, release, push or deployment configuration changed; source/local image remain unpublished.

Extra process-level staged-part restart qualification is **NOT TESTED**: host ENOSPC was followed by Docker Desktop becoming unavailable. Earlier engine/DB restarts, encrypted recovery, browser reload and Docker legacy restart tests passed before this outage. Only generated test fixtures/dependency copies were cleaned, freeing about 2 GiB; results/screenshots retained. Docker CLI/native startup recovery did not succeed; Docker Desktop needs owner attention before further container checks. No Docker data reset, volume deletion, global prune or private-state cleanup was attempted.

## Preview repair — local qualification PASS

Image thumbnails now default on, retain their hide/show preference and load near the viewport with at most two original-image requests. Preview routing normalizes MIME metadata and recognizes safe filename extensions for missing/generic MIME; existing files and metadata stay unchanged. Image/text/native media provide retry states, loading feedback and original-download fallbacks. HEIC/HEIF and MOV decoding remains browser/codec dependent; no transcoder or dependency added. HTML/XML/SVG content remains attachment-only.

PDF preview now uses PDF.js's bundled compatibility build and matching worker. This fixes missing `Map.getOrInsertComputed` in Chromium. Cancellable, bounded text-stream reading fixes WebKit's missing stream async iterator; optional text-extraction failure preserves the rendered page. Native audio/video survive React StrictMode effect replay and retain their own keyboard controls. The development API proxy preserves Host so supported login passes existing same-origin checks.

| Verification | Result |
| --- | --- |
| `go test -p 1 ./...`, `go test -p 1 -race ./...`, `go vet -p 1 ./...` | PASS |
| Telegram-tagged API/storage tests with production `CGO_ENABLED=0 GOFLAGS="-buildvcs=false -trimpath"` | PASS; initial uncached compilation exceeded the 2 GiB sandbox limit; retry with production flags passed |
| `npm test`, `npm run typecheck`, production Dockerfile build | PASS; 32 frontend tests, linux/arm64 image |
| `scripts/preview_check.js`: Chromium production, WebKit production, Chromium development StrictMode | PASS; 16 settled-view axe audits per run, 48 total with zero violations and zero uncaught exceptions |
| Generated PNG/JPEG/PDF/text/audio and WebM through encrypted local storage | PASS; SHA-256 roundtrips, PDF canvas/text/zoom/fit, image zoom/pan/fullscreen, injected transport failure/retry, WebM decoded frame/seek and HTTP 206 |
| Actual generated H.264 MOV frame/seek | PASS in WebKit; UNSUPPORTED in Linux Chromium, actionable fallback verified |
| Thumbnail queue and responsive views | PASS; peak two original-image requests, persistent opt-out, no eager video GETs; 320/390/768/1024/1440px samples, light/dark desktop/mobile, no measured horizontal overflow |
| 1,000-folder browser checks | PASS in both production engines; 17 list rows, 50/80/50 grid cards, no overlaps, keyboard End and selected drag; explicit thumbnail opt-out makes zero content GETs |
| `python3 scripts/ci_check.py docker-smoke --image harbor-preview:local --platform linux/arm64 --version 1.1.0` | PASS; 104,923,157-byte encrypted multipart checksum, auth/CSRF, Range, onboarding/lockout, legacy init, resume/restart/recreation, non-root permissions/health/shutdown |

Browser artifacts remain ignored under `output/preview-check/`: `qa-webkit-production/preview-NwfPMK`, `qa-chromium-production/preview-XED201`, `qa-chromium-development/preview-SNbd7q`. Screenshots precede axe with finite transitions disabled, so audits inspect settled rendered states; early WebKit scans during transitions produced transient contrast failures. No theme CSS workaround or audit waiver retained.

Reviewed generated-fixture screenshots: [desktop light](screenshots/preview-grid-desktop-light.png), [desktop dark](screenshots/preview-grid-desktop-dark.png), [image](screenshots/preview-image-desktop-light.png), [PDF](screenshots/preview-pdf-desktop-light.png), [mobile grid](screenshots/preview-grid-mobile-dark.png), [mobile image](screenshots/preview-image-mobile-dark.png). PNG text/EXIF metadata absent.

Limits: HEIC failure guidance uses an intentionally invalid synthetic fixture, not successful HEIC decoding. Native codecs remain browser/device dependent; physical Safari/iOS and live Telegram operations are NOT TESTED in this task. Application and test execution used isolated synthetic local adapters with no external network access. Dependency-fetching image builds received no runtime secrets. No private runtime data, original installation, release, remote repository or deployment changed. Current source changes and local image remain unpublished.

## Minimal first-run environment — PASS

`.env.example` now requires only a generated setup token, with commented HTTPS and Compose-port overrides. Owner password and Telegram details stay in browser onboarding; README retains the full configuration reference. Clean-environment Compose validation passed for localhost defaults, synthetic token/HTTPS/port overrides and the Dokploy template. No runtime code, private configuration, volumes or deployment changed.

## v1.1.0 release — artifact qualification and publication PASS

Owner authorized sanitized-source cleanup, push, new release and GHCR image. Release agent updated qualification geometry and exact-image web-onboarding smoke; independent onboarding/storage review found no release blocker or justified runtime rewrite. Coordinator aligned version defaults and installation docs. Existing public GHCR visibility is pinned explicitly; repository visibility stays private. No original installation/session/data changes.

Fresh frontend/default/tagged Go/race/vet, Python, Chromium/WebKit/axe and both production image builds/smokes passed; hosted main CI also passed all five jobs. [Tagged release validation](https://github.com/seakleangnhak/harbor/actions/runs/39582004996), GHCR publication and independent pulled-image verification passed. AMD64/ARM64 index `sha256:8d6aa16e222b3bcef948b8cf61b7799b78206879de7dcf4bc2232274e015d132` is shared by `1.1.0`, `1.1` and `latest`; [release report](RELEASE_1.1.0.md) records exact scope and [GitHub release](https://github.com/seakleangnhak/harbor/releases/tag/v1.1.0). Test-only bounded deadlines and isolated Docker bridge ingress fixed actual qualification failures; full reruns passed. Post-tag verifier compatibility also passed 28 focused tests and actual v1.0.1 ARM64 smoke. Current live Telegram web setup/100 MiB throughput and live cross-DC remain NOT TESTED. Original installation remains healthy; no remote application deployment or visibility change. Historical sections below retain their original scope.

## Premium onboarding UI — local qualification PASS

First-run setup now uses a compact responsive shell, clear progress, native provider cards, light/dark control, accessible secret fields and distinct Telegram/review states. Protected active-flow status fixes reload recovery without exposing account/channel identity. Retry guidance and trailing-slash channel normalization align with backend behavior. Existing workspace, encrypted state and deployment remain intact.

Default Go tests/race/vet and 29 frontend tests/typecheck/build pass. Final Chromium and WebKit runs pass 180 layout/theme checks and 180 axe audits with zero violations/page errors. Reviewed desktop/mobile light/dark screenshots are saved. Telegram states are simulated; both browsers verify actual local completion/login and a 200 KiB upload/download with SHA-256 equality. [Audit and evidence](ONBOARDING_UI_REVIEW.md). No push, image publication or remote deployment.

## Browser onboarding and 100 MiB chunks — local qualification PASS

Fresh source installations now start from Compose/container runtime environment and a random `DRIVE_SETUP_TOKEN`, then create their owner password and choose local/Telegram storage in the web UI. Telegram uses guarded QR/optional two-step input, private-channel verification and an atomically activated private configuration/session bundle. Existing initialized data, legacy CLI/environment setup, keys, sessions and 4 MiB manifests remain supported. Current source features are not present in published image 1.0.1; its pinned Dokploy template remains on the legacy flow. No new image release, push or remote deployment is implied.

Coordinator owns startup/config persistence, deployment, recovery integration and final acceptance. Backend agent completed first-run authorization/state handlers and synthetic security tests. Storage agent completed the 100 MiB default, compatible bounds, in-place buffers and shared reader/upload/finalization limits. Frontend agent completed owner/storage/review onboarding, QR/two-step states, readiness handoff, 100 MiB client validation and user-facing documentation.

**PASS:** default/Telegram-tagged Go tests, race and vet; 29 frontend tests, typecheck/build; 44 Python tests; actual process onboarding, 100 MiB resume/restart/Range/checksum and encrypted fresh restore. Chromium setup/local transfer plus 100 MiB bounded retry, pause/reload/resume and cancellation pass. Seven scoped axe checks have zero violations; desktop/mobile light/dark screenshots reviewed. Non-root artifact runtime passes health, multipart checksums, restart and volume-preserving recreation. Source Compose validates.

**PARTIAL:** complete production Dockerfile build was blocked by unavailable offline npm dependencies; separately built final binaries/web assets and a matching distroless runtime were tested. **NOT TESTED:** new live Telegram web authorization/100 MiB live Telegram, live cross-DC, new published image and remote Dokploy. Current source is committed locally only; existing deployment and release remain unchanged. [Qualification report](WEB_ONBOARDING_REPORT.md) · [Setup guide](WEB_ONBOARDING.md) · [API contract](API_CONTRACT.md).

## Private v1.0.1 release — artifact qualification PASS

Owner-authorized sanitized source/tag, [GitHub Release v1.0.1](https://github.com/seakleangnhak/harbor/releases/tag/v1.0.1) and private GHCR publication completed. Full tagged Go/default/Telegram/race/vet/reproducibility, frontend and both Docker jobs passed. Actual package index `sha256:d5c56ecd655e83f298e63f3e97f7df7582aecd9ebdd45b0d6aed6801b36bc855` passed local and hosted immutable-image checks, both architecture encrypted multipart/checksum/restart/recreation smokes, labels/SBOM/provenance and scoped image-content scans. [Exact receipt](GHCR_RELEASE_REPORT.md).

Original publisher remains FAIL after successful upload because GITHUB_TOKEN omitted package repository metadata; retry stopped before login/build/push. Owner API confirmed correct private association, then pinned nonsecret immutable package ID. Follow-up helper/workflow changes on main passed 44 local Python tests/actionlint, all five hosted main CI jobs and read-only hosted verification without moving tag or image. Publishing automation remains PARTIAL until a future approved release exercises the amendment. Original deployment/history/v1.0.0 remain preserved; public visibility not authorized; live cross-DC remains NOT TESTED.

## Final publication gate — local preparation

Historical privacy review covers all refs/reflogs and 652 stored objects. One machine-path blob survives in four original revisions and two old source archives; historical identity metadata is retained privately. No unresolved credential/private-endpoint finding. Separate reviewed source export uses approved public identity, with no original commits/tags/runtime/artifacts. New publication docs specify private-first remote validation and proposed v1.0.1; original v1.0.0 stays intact. Local workflow/README/release checks and export verification recorded under ignored `output/publication-gate/`. Publication remains BLOCKED pending separate approvals and actual remote results. [Decision](PUBLICATION_GO_NO_GO.md).

Updated 2026-10-21, Asia/Phnom_Penh.

## Open-source preparation — local PASS

User-approved MIT; existing v1.0.0 remains immutable. Agent A completed README/original branding/six reviewed screenshot assets and desktop/mobile theme rendering. Agent B completed SHA-pinned read-only CI, protected stable-tag GHCR workflow, native cross-compilation and release gates. Agent C completed community/security templates, dependency/license notices/font source and redacted history hygiene. Agent D completed fresh Go/default/tagged/race/vet, Node 24/23 frontend tests, 38 Python checks, actionlint and independent cold-cache binary comparison. Coordinator qualified both production image architectures, encrypted multipart/restart/recreation smoke, local SPDX/SLSA export, source/context/image secret checks and release runbooks. All local preparation gates passed. Checkpoints `61db843` (MIT/community), `e3f34c0` (README/assets), `a619068` (CI/gates/images/docs); committed-source candidate packaging passed exact ARM64 image parity and artifact hashes. Final evidence checkpoint follows.

No push, package publication or new release tag. v1.0.1 is proposed only. Remote Actions/GHCR NOT TESTED. History privacy review (old developer path and Git identity) remains before publication; no credential discovery or history rewrite. Current results in [readiness](OPEN_SOURCE_READINESS_REPORT.md), [Actions evidence](GITHUB_ACTIONS_REPORT.md) and [publishing guide](GHCR_PUBLISHING_GUIDE.md).

## Phase 6 — final release qualification

**HARBOR_V1_RELEASE = PASS for local private personal use.** [Final evidence](PHASE6_FINAL_REPORT.md). Runtime checkpoint `6fe39cf`, baseline `2e2a5df`. Release Engineer pinned images and proved repeatable exports/archived-source payload parity; Storage Engineer fixed bounded media FILE_MIGRATE delegation and added deterministic recovery tests; Security Engineer reviewed authentication/recovery/private state and scans; QA independently ran all default/tagged Go/race/vet, 23 frontend tests/build/typecheck, 24 Python tests and 18 Chromium/WebKit suites with 22 scoped axe audits. Coordinator qualified exact final image with serial live authorization, browser downloads, benchmarks and archive-only restoration.

Image `sha256:30f3c57ce9e1b5cb41b7de0e1678fa010bdb403fc5b8a091bb9647a9b833c924`: real encrypted TXT/PNG/PDF/multipart, restart/resume, container recreation and SHA-256 passed. Fresh third volume received only 117,334-byte encrypted archive/password; recovered key/config/DB/session supplied successful downloads, hierarchy/favorite/Trash and restart. Browser actual 126,976-byte save matched SHA-256, preview and HTTP206. 100 MiB measured 1.626430 upload / 4.212658 download MiB/s. Startup 0.760 s; sampled transfer working set 38.270 MiB (not lifetime RSS).

Original 8081 resumed only after clone/private-volume removal; original 8080 untouched. Release artifacts/docs remain local, annotated tag follows clean accepted checkpoint and final artifact verification; no push/public deployment/preexisting remote deletion. Live cross-DC stays NOT TESTED. Provider loss, key/password loss, copy/backup ceilings, host trust and browser scope stay explicit.

## Phase 5 — complete qualification

**HARBOR_PHASE5 = PASS for private local personal use.** [Final evidence and limitations](PHASE5_FINAL_REPORT.md). Baseline `63d9fd3`; risk register preceded implementation and now records final controls/residual risks. A delivered complete encrypted WAL-safe recovery; B durable atomic-visible folder jobs and read-only integrity; C source-first security/permissions/auth tests; D isolated Docker persistence/upgrade/rollback; coordinator integrated shared contracts/UI and executed private live recovery/browser checks.

Fresh isolated real Telegram DR restored 104,022-byte encrypted archive containing original clone key/config/session/database; four TXT/PNG/PDF/multipart files passed hashes after restore and another restart; hierarchy/favorite/Trash preserved. Wrong password/tamper/missing key/interruption rejected. Actual copy-process SIGKILL after one 4 MiB chunk resumed 33,554,449 bytes with exact SHA-256 and no premature visibility.100/1,000/1,001 entries/depth 512 pass; 10,001 rejected safely.

Final default/tagged Go tests/race/vet/vulnerability scans, 16 Python tests, 23 frontend tests/typecheck/build, local process acceptance and current real Telegram browser 126,976-byte checksum/preview/Range pass. Chromium/WebKit CRUD/nested clipboard/previews/mobile and Phase 5 job/settings/error screens pass; scoped axe zero violations.1,000-entry Chromium view renders 17 rows, 50–80 cards with no overlap. Final 51,073,203-byte Docker image passes six-stage local upgrade/restart/recreate/rollback suite and offline encrypted recovery; 20 checksum retrievals preserve metadata/key/state.

Checkpoints: `58c2112` jobs/state guards; `63b81f3` encrypted recovery/DR; `1139f2f` UI/regression screenshots; `ba7d663` Docker/operations; final evidence checkpoint follows. Missing-bound-metadata fail-closed, private SQLite, internal staging isolation, listener policy, schema fencing, Go 1.26.10 and accessible command scrolling fixes qualified. Reports and operations runbooks updated. No public deployment, remote push or preexisting Telegram deletion.

Live cross-DC and live Telegram inside exact final Phase 5 Docker image remain **NOT TESTED**. Provider account/channel loss needs independent encrypted content copy; backup does not mirror Telegram ciphertext. Copy/backup ceilings, orphan handling, browser/security scope and legacy volume permission migration remain explicit. Original 8081 service/key/auth/data preserved; original Docker 8080 untouched. Temporary test services/browser sessions/state cleaned after qualification; encrypted pre-upgrade rollback material retained privately.

## Phase 4 — complete qualification

**HARBOR_PHASE4 = PASS.** [Final evidence and limits](PHASE4_FINAL_REPORT.md). Initial browser/source audit preceded implementation; six baseline viewport/theme captures retained. Agent A delivered design tokens/CSS/shared primitives; Agent B file manager/navigation; Agent C previews/transfers; coordinator integrated App/clipboard/palette, protected live authentication, visual review and regression qualification. Backend/data format and keys unchanged; existing Telegram authorization preserved.

Real local browser CRUD/rollback, nested copy/cut, favorites/trash, image/two-page PDF/text/code/video, mobile selection/focus/rename, 1,000-item virtualization/native drag, bounded retry and copy reload/cancel pass. Real Telegram generated 126,976-byte UI upload/preview/download/full SHA-256 and Range pass. Fresh default/tagged Go tests/race/vet, 22 frontend tests/typecheck/build, 15 Python tests, process recovery and final isolated local Docker image/health/recreation checksum pass. Six required main viewport/theme captures plus nine surfaces and three login states have zero scoped axe violations. Menu, PDF/transfer keyboard focus, mobile rename/focus and stale listing-error footer findings fixed and rechecked.

Reports: `PHASE4_UI_AUDIT.md`, `PHASE4_DESIGN_SYSTEM.md`, `PHASE4_UX_REPORT.md`, `PHASE4_FINAL_REPORT.md`; 50 reviewed screenshots in `docs/screenshots/`. Checkpoints: `a213c3a` audit/ownership, `b641d33` integrated UI/checks, `9a602f1` final state/focus/cosmetic corrections; final evidence checkpoint follows. Remaining limits include 1,000-entry non-atomic browser folder copy, additive Shift ranges, native progress above 32 MiB, Chromium/emulated mobile scope and live cross-DC NOT TESTED. No push or preexisting remote deletion.

Original Telegram host remains on http://127.0.0.1:8081. Isolated local fixture/Docker deployments stopped after acceptance; generated data/volume retained. Temporary browser/password copies removed and automation HTTP sessions revoked. Worktree clean at handoff.

## Phase 3 — complete qualification

**HARBOR_PHASE3 = PASS.** Six acceptance gates passed; live cross-datacenter retrieval **NOT TESTED**. [Final evidence and limits](PHASE3_FINAL_REPORT.md).

| Owner | Completed scope | Result |
| --- | --- | --- |
| Agent A | MTProto part/RPC benchmarks, 512 KiB parts, flood telemetry, routing/retry tests, performance report | PASS; no speculative concurrency/pool layer |
| Agent B | Bounded allocation, seek cache, corrupt-geometry guard, fault injection, reliability report, independent source review | PASS; deterministic missing/corrupt/retry/cancellation tests |
| Agent C | Sanitized Docker/security checker, own-schema benchmark recheck fix, Docker report | PASS; no Dockerfile/Compose rewrite needed |
| Coordinator | Baseline/integration, isolated session handoff, real Docker crash/network/browser tests, regressions, security review, commits | PASS; original service restored, no preexisting deletion/push/public deployment |

Real 1/10/100 MiB baseline and optimized hashes passed; 100 MiB observed upload 1.121734 → 1.354059 MiB/s. Sampled host RSS 51.6875 → 61.5 MiB; reduced allocation is not reduced measured RSS. Local 512 MiB exact roundtrip passed with sampled 35.734375 MiB maximum RSS. Docker fixture resume, clean restart, recreation, older-file retrieval, interrupted HTTP download/network recovery, browser preview/verified save and 320px layout passed. Fifteen Python tests, eight frontend tests/typecheck/build, Go default/tagged race/vet and process/recovery acceptance passed.

Two checkpoints: `2214c6f` production/tests, `e894b6d` qualification harnesses; final docs checkpoint follows. Original host Telegram process at http://127.0.0.1:8081; isolated `harbor-phase3-tg` container stopped, fixture volume retained. Host/clone session never ran concurrently. Temporary Docker password/browser state removed; temporary HTTP sessions revoked. Extra post-network checker hit existing login throttle; no bypass. Live rate restrictions were never deliberately triggered.

Remaining: live cross-DC/secondary media redirect, crash inside RPC/SQLite commit, sustained load and lifetime RAM peak NOT TESTED. Whole-file completion cost/metadata lock, orphan ciphertext window and 32 MiB managed-download cap remain documented. Phase 2 sections below are historical.

## Phase 2 — complete qualification

**HARBOR_TELEGRAM_INTEGRATION = PASS. Local MVP regression = PASS.** Nine required gates verified: real MTProto QR authorization, configured target agreement, encrypted upload, retrieval, AES-GCM/SHA-256 equality, multipart resume, session/application restart, real browser storage and local regression. Existing data, configuration values, keys and preexisting messages preserved. No public deployment or remote push.

| Owner | Scope | Final status |
| --- | --- | --- |
| Agent A | Telegram adapter, QR authorization, channel/session/status/retry, same-DC routing | PASS; full tagged race suite/vet, independent routing review and live retrieval |
| Agent B | Integrity/resume/recovery regressions, controlled runner, reliability/performance reports | PASS; runner small/multipart/resume/restart plus measured throughput and sampled CPU/RSS |
| Agent C | Auth/redaction, diagnostics, transfer telemetry/verified download UI, security report | PASS; eight frontend tests/typecheck/build, API tests/race and source review |
| Coordinator | Private environment/auth flow, contracts/integration, live/browser/Docker evidence, commits/reports | PASS; all required live gates, remote ciphertext probe, idle-crash recovery and local regression |

## Final evidence

- Real small TXT/PNG/PDF and 4,259,861-byte multipart roundtrips passed. Exactly 4 MiB chunk 0 persisted across restart; incomplete publication rejected, resume and cross-chunk Range matched.
- Browser uploaded 135,168-byte generated text through UI; exact preview and managed verified download passed. Completed queue and actual Telegram-connected state observed.
- Six tracked remote chunks independently fetched while server stopped: `tg:` references, exact HD01 sizes, distinct sampled nonces, AES-GCM upload/index AAD and plaintext chunk SHA-256 passed.
- Graceful restart retrieved all runner files. Separate owned idle-server SIGKILL/restart reused session and retrieved all five files unchanged. Fresh anonymous content/diagnostics returned 401.
- Default Go tests/race/vet, full tagged race/vet, tagged build, local process acceptance, ten Python tests, eight frontend tests/typecheck/build and Compose validation passed.
- Latest Docker image passed isolated local health/security and four-fixture restart retrieval. Original port-8080 local container/data preserved. QA container stopped without deleting its fixture volume; temporary password copy removed.
- Multipart measured 0.455 MiB/s upload and 1.111 MiB/s download; max sampled RSS 45.25 MiB and CPU 14.4%. Single controlled run, not sustained capacity or lifetime peak.

## Defects resolved during qualification

Initial OTP login failed; original cause unknown. Later app-delivered code did not arrive. Phone now echoes only in private Terminal; OTP/password remain hidden. Delivery notices identify supported methods and reject unsupported flows before OTP; no auto-resend. Native QR approval succeeded using already signed-in dedicated account; buffered update avoids lost approval during rendering. Three-minute QR network context does not forcibly interrupt blocking local password entry.

First real TXT verification returned `DC_ID_INVALID` because media authorization was exported to current datacenter. Failed file stayed unpublished. Same-DC download now uses primary API; cross-DC pool lifecycle retained. Retained chunk recovered without duplicate upload. Routing/length/truncation/pool-cleanup regressions and live recheck passed.

Other resolved Phase 2 issues: cancellation cannot return empty success; raw provider errors do not reach logs/HTTP; fast Pause/Resume preserves newer request after aborted hash worker settles; managed downloads reject corrupt/truncated bytes before browser save.

## Boundaries

NOT TESTED: live Telegram inside Docker, actual cross-DC storage, induced live connection loss/flood waits, in-flight request/DB crash, sustained load, lifetime peak RAM and Telegram disaster restore. Synthetic failures and idle SIGKILL do not substitute for these cases. No broad remote cleanup; encrypted orphan window remains. Full completion re-reads all chunks. Browser verified-download cap 32 MiB; larger files use browser progress. Metadata remains host-readable.

## Phase 1 historical baseline

Local MVP PASS: folder CRUD/move/rename integrity, favorites/search/sort, trash recovery, encrypted resumable uploads, Range downloads, previews including PDF/video seeking, responsive list/grid and virtualization. Browser checks included 320/768px layouts, 1,000-item views and axe zero violations in exercised desktop/mobile states. Offline backup/restore and Docker multipart/restart checks passed. Cross-browser/assistive-device certification remains NOT TESTED.

Original ownership remains in AGENTS.md. Phase 1 checkpoints: `b104c39`, `cab1e2a`, `585b0b2`, `7f1db68`. Phase 2 checkpoints: `0fab10c`, `435cbbf`, `57dc728`, `03ca826`, `f1cadc8`, `b68ae75`; final docs checkpoint records qualification. No remote push. Live host service retained at http://127.0.0.1:8081; automation session revoked after acceptance. Reports: FINAL_REPORT, TELEGRAM_INTEGRATION_REPORT, RELIABILITY_REPORT, SECURITY_REVIEW, PERFORMANCE_REPORT.
