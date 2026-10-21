# Progress

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
