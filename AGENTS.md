# Telegram Private Drive

Build a private single-owner drive. Keep changes small, inspect existing code, validate behavior, and report unverified claims honestly. Never commit secrets, data directories, dependency directories or build artifacts. Phase 4 explicitly authorizes reviewed screenshots of generated fixtures under `docs/screenshots/`; exclude credentials and personal content. Do not push or deploy publicly.

## Ownership

- Coordinator: `cmd/`, `internal/domain/`, root configuration, Go dependency manifests, deployment, `scripts/`, `docs/`, README, cross-component acceptance.
- Backend agent: `internal/api/` only; owns metadata/auth schema and HTTP handlers/tests.
- Storage agent: `internal/storage/` only except coordinator-owned `contracts.go`; owns upload/chunk schema, crypto, local and Telegram adapters, engine tests. Request Go dependencies from coordinator.
- Frontend agent: `web/` only, including npm manifests; owns UI, client types and tests.
- No concurrent edits across ownership boundaries. Propose contract changes to coordinator before changing shared interfaces. Agents do not commit; coordinator creates validated checkpoint commits.

## Contracts and security

### Milestone C final qualification ownership override

- Preserve `48aae5f`, owner state and existing releases. No push, tag, publication or deployment upgrade. Keep established encryption/chunk format, queue bounds and concurrency. Agents never commit or access private configuration, authorization, keys, databases or live logs.
- Soak agent owns `internal/api/milestone_b_memory_test.go`, new `scripts/milestone_c_soak*` and `docs/V1.3_SOAK_REPORT.md`; generated fixtures only, actual 30-minute interval before considering extension.
- Resource agent owns new resource/failure-only tests/controllers and `docs/V1.3_RESOURCE_REPORT.md`; coordinate shared runtime and compilation resources before execution. No production source edits without coordinator agreement.
- QA agent owns new qualification/browser controllers, isolated regression evidence and narrow mobile transfer overlap tests/fix after agreement. Live agent (second wave) owns new bounded-live controller/tests, using synthetic configuration for review only. Coordinator executes all real Telegram operations under source lock and removes copied authorization before unlocking.
- Coordinator owns integration, compatibility/upgrade/rollback, images/version, remaining reports/runbooks and final review. All target execution retains existing isolated containers, nonroot/read-only/no-network synthetic runtime, explicit environment and bounded resources. Reviewed host Docker controllers are allowed; never mount private home, original state or Docker socket into target containers.

### Milestone B preview, memory and hygiene ownership override

- Preserve Milestone A checkpoint `b6ae39e`, original runtime and all releases. Local reviewed commits only; no push, tags, publication or deployment. Keep 100 MiB storage chunks, 4 MiB client parts, two pending/upload, four globally and one saving worker. Coordinator alone handles serialized authorized live Telegram and private state.
- Agent A: `internal/storage/reader.go`, `read_cache.go`, cache/reader-only new tests and `docs/PREVIEW_PERFORMANCE_REPORT.md`; propose API/frontend contract changes before editing outside ownership. Preserve authenticated reads and remote completion verification. No persisted plaintext previews.
- Agent B: `internal/storage/engine.go`, `local.go`, `transport.go`, memory-only new tests/control scripts and `docs/MEMORY_QUALIFICATION.md`, `docs/MIXED_WORKLOAD_REPORT.md`. Capture baseline before production edits. Coordinate shared buffer/cache contracts with A and coordinator; do not edit pipeline/reader/cache files.
- Agent C: new `internal/storage/hygiene*.go`, new `cmd/drive/hygiene*.go` and their tests, `docs/STORAGE_HYGIENE_REPORT.md`. Coordinator wires command dispatch in main. Default read-only; cleanup requires offline exclusive drive lock, explicit scoped confirmation, complete revalidation and audit evidence. Never delete Telegram objects, recoverable jobs, original content or unknown files.
- Agent D (second wave): preview/large-directory browser harnesses and new Milestone B QA scripts/tests only. Coordinator owns frontend/API integration, Docker/build/version, architecture/operations/final docs and all real credentials. Agents never read actual env/session/key/DB/archive/live private logs and never commit.
- Use generated fixtures and established isolated offline non-root test containers with read-only source/tooling, explicit clean environment and bounded resources. Reviewed host control harnesses may orchestrate Docker. Dependency-fetching build stages receive no runtime secrets. Live tests remain coordinator-only, bounded, serialized with source authorization lock until clones stop and copied authorization is removed. Record unsupported/unverified scope honestly.

### Milestone A transfer stabilization ownership override

- Maintained source is this sanitized export. Preserve checkpoint `59aa6d8` and all original runtime/history. Local commits only; no push, tag, publication or deployment upgrade. User authorizes coordinator-only bounded live Telegram qualification with isolated data and serialized authorization; this supersedes earlier task-specific synthetic-only limits for that qualification.
- Upload agent: `internal/storage/{engine,transport,pipeline}.go`, upload/pipeline tests only. Cache agent: `internal/storage/{reader,read_cache,telegram}.go`, cache/Telegram tests and benchmarks only. Reliability agent: new `scripts/milestone_a*`, new `internal/storage/milestone_a*_test.go` and ignored `output/milestone-a/reliability/`; coordinate test helper contracts before writing. No overlapping edits; agents never commit or read real credentials/session/data/logs.
- Coordinator owns shared contracts/domain/API/startup/version/build/frontend/docs and live qualification. Keep two pending chunks/upload, four globally, one saving worker, 100 MiB storage chunks and 4 MiB browser parts unless comparable measurements justify change. Retain isolated offline bounded synthetic execution; production dependency builds may fetch pinned public dependencies without runtime secrets.
- Live authorization handoff must stop its original sole owner and hold its data lock before copying authorization; stop all clones before original resumes. Never overwrite original DB/key/session or delete preexisting remote messages. Report compile resource failures separately from executed race tests; live cross-DC remains NOT TESTED without real evidence.

### Bounded transfer pipeline ownership override

- Current task: faster upload/download/preview only. Work in sanitized export; preserve original repository, deployment and runtime state. No release, push or deployment. Synthetic fixtures only; existing isolated execution rules remain.
- Storage agent owns `engine.go`, `transport.go`, new durable upload-worker files and storage upload tests. Frontend agent owns upload hook, transfer helpers/types/API, Transfers and upload tests. Reader agent owns `reader.go`, new `read_cache.go` and cache tests. Coordinator owns contracts/domain, API, startup, docs and cross-component checks. No shared-file concurrent edits; agents never commit.
- Keep 100 MiB storage chunks and 4 MiB browser parts. Add opt-in durable background commit while retaining synchronous legacy commit and whole-chunk copy APIs. Raw received bytes remain remotely stored; pending bytes never imply file completion. Bound staging/worker concurrency; preserve encrypted staging, idempotency, cancellation, restart recovery and final remote integrity verification.
- Reader cache contains ciphertext only in private bounded local storage. Each hit still authenticates and verifies SHA-256. Finalization/content integrity checks bypass cache to verify actual adapter content. No plaintext cache, new dependencies or weakened HTTP guards.

### v1.2.0 release ownership override

- Owner authorizes scoped CI/CD simplification, sanitized-source push, a new annotated release and GHCR image publication. Preserve original private repository/history, deployment and all runtime state. No visibility changes or remote deployment.
- Coordinator owns version defaults, docs, review, scans, commits and remote operations. Workflow agent owns `.github/workflows/ci.yml`, `docker-publish.yml` and root `Dockerfile`. Release-check agent owns `scripts/ci_check.py` and its tests. Browser QA owns `scripts/browser_check.cjs` and `scripts/Dockerfile.browser`. Agents never commit or publish.
- Retain isolated no-network target execution with synthetic fixtures, read-only code/tooling and bounded scratch/resources. Docker Desktop is unavailable locally; authorized GitHub-hosted CI supplies disposable containers. Production dependency-fetching build stages receive no runtime secrets. Require successful CI for the exact release commit before tagging/publishing. Existing release/tag/package identity and visibility gates remain mandatory.
- Hosted Go compilation caches may use a writable, branch-scoped Actions cache on the disposable runner disk; they contain build artifacts only. Runtime fixtures remain in bounded tmpfs. Cache reuse must not expose credentials or make a failed verification successful.

### Account settings visual refinement ownership override

- Coordinator owns `OwnerAccount.tsx`, `styles.css`, visual integration and docs. Frontend agent owns `Settings.tsx` only: compact Account/Storage/Recovery navigation and existing operational content. Review agent owns `scripts/account_check.js` only and read-only accessibility review. Preserve API/security behavior, earlier preview/transport/account edits and existing design tokens. No dependencies, real account changes, commits, release or deployment.
- Settings retains its public props and title. Account panel is default; navigation uses native buttons with `aria-current`, responsive horizontal mobile navigation and persistent footer. All forms keep existing endpoints/password/session semantics. No fake metrics, avatars, controls or strength ratings. Source/test execution retains existing isolated-container requirements; Docker remains unavailable until recovered.

### Owner account settings ownership override

- Coordinator owns recovery compatibility, docs and integration. Backend agent owns `internal/api/` account handlers/schema/tests. Frontend agent owns account Settings, Sidebar/Login/App wiring, API/types/styles and related frontend tests. Review agent owns read-only security review and new account browser checks under `scripts/`. Preserve all earlier uncommitted preview/upload work; no commits, push, release or real account changes.
- Authenticated `PATCH /auth/profile` accepts `{display_name}` and returns the existing session shape with updated `owner`. Name: trimmed, 1–80 Unicode characters, valid UTF-8, no controls. `POST /auth/password` accepts `{current_password,new_password}`, validates 12–72 UTF-8 bytes without CR/LF, verifies current password with throttling, and atomically changes bcrypt hash/revokes all sessions; 204 clears cookie. Wrong current password: 403 `incorrect_password`, not a lost-session 401. Serialize login issuance/password changes to prevent stale-password races.
- Persist display name and `password_changed_at` in one additive `owner_profile` row (id=1, default name `Owner`, empty timestamp initially). Existing owner hash remains authoritative. Never save new plaintext passwords. Recovery skips obsolete automatically discovered initialization-password files after a recorded password change; explicitly supplied recovery passwords still require verification. Existing databases/archives without the profile table retain prior behavior.
- Use only synthetic fixtures and existing no-network isolated execution rules. No reads of real environment files, sessions, keys, DBs or deployed services. Docker currently unavailable; do not run target code on host to bypass isolation.

### Client transport parts ownership override

- Coordinator owns domain/storage contracts, startup wiring, API handlers/routes/tests, docs and integration. Storage agent owns engine/staging implementation and storage tests, excluding `contracts.go`. Frontend agent owns upload transport/progress/types/UI and frontend tests. QA agent owns only new transport browser harness and ignored synthetic evidence. Preserve uncommitted preview/minimal-environment changes; no commits, push, release or deployment in this task.
- Keep storage chunks at 100 MiB and existing manifests intact. New guarded upload routes accept encrypted-at-rest staged 4 MiB client parts, then explicitly commit one assembled storage chunk. `received_bytes` retains remotely committed meaning; additive `part_size`, `staged_chunk`, `staged_parts`, `staged_bytes` describe one bounded pending storage chunk. Never treat sent or staged bytes as completed files. No plaintext staging, credentials or original runtime access; existing isolated no-network verification rules apply.

### v1.1.0 release ownership override

- Current owner request authorizes scoped cleanup, sanitized-source push, a new annotated release and GHCR publication. Earlier task-specific no-push/no-tag restrictions below are historical. Preserve original development history, v1.0.0, deployment and all private runtime state. No visibility changes or remote application deployment.
- Coordinator owns version defaults, release docs, integration, commits and remote operations. Release agent owns CI smoke/qualification helpers and their tests. Other agents review onboarding/storage read-only unless ownership is reassigned. Agents never commit or handle real credentials.
- Run application/tests in isolated no-network containers with clean environments, read-only source/tooling and bounded scratch/resources. Reviewed Docker control harnesses may manage disposable containers through the host CLI; exact-image HTTP smoke uses a dedicated loopback-published bridge with outbound masquerading, DNS and peer connectivity disabled. No external application requests or Docker socket mounts in target/test sandboxes. Production image builds may fetch pinned dependencies in isolated build stages; never supply runtime secrets or original data to builds, caches or tests.

### Web onboarding ownership override

- Premium UI follow-up: coordinator owns `Onboarding.tsx`/`onboarding.css`, visual integration and evidence; QA agent owns only `scripts/onboarding_ui_check.js`. Other agents review read-only. Preserve setup API/security behavior and keep all target execution in the isolated fixture sandboxes below.
- Coordinator: `cmd/drive/`, `web/src/App.tsx`, deployment/config/docs, integration and verification. Work in sanitized public export only. Preserve original repository, running deployments, sessions and data. No push, tag or remote deployment in this task.
- Onboarding backend agent: new `internal/onboarding/` only. Guarded setup HTTP handler; injectable Telegram setup and commit callbacks; same-origin, short-lived token authorization, bounded requests and safe error responses. No main/storage/frontend edits.
- Storage agent: `internal/storage/` only. New uploads use 100 MiB chunks; old manifests retain their own geometry. Bound memory/concurrency and ciphertext limits, Telegram multipart correctness and regression tests. Do not change public Service interface without agreement.
- Frontend agent: new `web/src/components/Onboarding.tsx`, `web/src/onboarding.ts`, onboarding tests/CSS and existing transfer validation files only. Coordinator wires App. Keep tokens/Telegram credentials in memory only; no URL/localStorage/log secrets. Preserve existing UI.
- Agents do not commit or inspect real credentials. Use synthetic fixtures. Security guidance execution checks run in isolated no-network sandboxes with explicit environments, read-only source/toolchain and bounded resources; report commands needed to coordinator rather than running unsandboxed target code.
- Setup contract: `GET /api/v1/setup/status` returns only `{required:boolean}` publicly; token-guarded endpoints use `X-Harbor-Setup-Token`, never query parameters. `POST /setup/account` `{password}`; `POST /setup/telegram` `{app_id,app_hash,channel_link,channel_id?}` starts QR flow; `GET /setup/telegram` returns `{state,qr_image?,error?,error_code?}`; `POST /setup/telegram/password` `{password}`; `DELETE /setup/telegram` cancels; `POST /setup/complete` `{adapter:"telegram"|"local"}` commits only verified configuration. Setup disabled permanently when metadata/storage binding exists. Persist private config/session outside SQLite and use existing auth after completion.

### Open-source preparation ownership override

- Agent A: `README.md`, `docs/assets/` only. Original SVG branding and visually reviewed, sanitized demo screenshots; no runtime UI changes. No invented repository URL or published-image claim.
- Agent B: `.github/workflows/`, `Dockerfile`, new `scripts/release_gate.py` and gate tests only. Read-only reusable CI, stable-tag publishing gates, pinned verified actions, two-architecture image build/verification. No actual publication, credentials or original runtime access.
- Agent C: `LICENSE`, `CONTRIBUTING.md`, `SECURITY.md`, `CODE_OF_CONDUCT.md`, `THIRD_PARTY_NOTICES.md`, issue/PR templates, Dependabot, `.gitignore`, `.dockerignore` only. User approved MIT. Review tracked/history hygiene without actual private files; report paths/categories, never secret values. No history rewrite.
- Agent D (second wave): new CI-check/smoke scripts and tests outside Agent B gate files, `docs/GITHUB_ACTIONS_REPORT.md`, release validation/checklist/operations proposals; ignored `output/open-source/qa/` evidence. Agent B agrees script contracts first. No workflow/Dockerfile/README/community edits.
- Coordinator: shared version policy, integration, `scripts/release.py` compatibility if required, `docs/OPEN_SOURCE_READINESS_REPORT.md`, `docs/GHCR_PUBLISHING_GUIDE.md`, progress/runbooks, actual configured-secret/history scan, final docs/review and commits. Original `v1.0.0` stays at `f6c49fc`; do not create another release tag. No push, image publication, original data/session mutation or private deployment details in new public docs. Agents never commit. Four workstreams run in two waves with three child slots.
- Proposed first automated release is `v1.0.1`, requiring later explicit approval. Runtime version must stay honest with tested source; coordinate a build-version override rather than silently relabeling old binaries. Runtime secrets never enter build arguments/cache; PR jobs receive no publish permission. No `pull_request_target` or privileged untrusted-code execution.

### Phase 6 ownership override

- Release Engineer: `Dockerfile`, new `scripts/release.py` and release-only tests. Pin existing base images, reproducible v1.0 build/artifact manifest without secrets. No actual session/credential/host service access. Coordinator integrates Compose/shared version changes and qualifies live container.
- Storage Engineer: `internal/storage/telegram.go`, Telegram-only tests. Inspect gotd routing/references/reconnect; minimal protocol fix only with reproduced need; deterministic tests. No engine/recovery/UI edits, real credentials or live runs. Return sanitized source evidence.
- Security Engineer: read-only auth/storage/recovery/deployment review; synthetic checks and dependency scans only, results in ignored `output/phase6/security/`. Propose source fixes before editing. No actual environment, key, password, session or live logs.
- QA Engineer (next wave): regression execution and synthetic browser fixtures only; ignored `output/phase6/qa/`. No source edits without agreement, no real credentials or live sessions. Coordinator owns live browser runs and acceptance docs.
- Coordinator: shared version/config, live Docker session handoff, qualification/benchmark harness, all actual secrets, recovery run, final docs/tag/commits. Agents never commit or tag. Phase 6 overrides earlier named ownership. No original state modification; stop original and hold its lock before copied authorization use, stop clones before original resumes. No preexisting remote deletion.

### Phase 5 ownership override

- Agent A (backup): new `internal/recovery/`, new `cmd/harbor-recovery/`, `scripts/recovery.py` and recovery-only tests, `docs/PHASE5_BACKUP_RESTORE_REPORT.md`. Complete encrypted offline archives with version/integrity, SQLite supported snapshot API, content key, allowlisted recovery config, Telegram session; no plaintext archive or secrets on stdout. Coordinator alone supplies actual secret-bearing inputs/live runs.
- Agent B (integrity): new `internal/api/copy_jobs.go` and copy-job tests, new `scripts/phase5_copy_check.py` and tests, new `internal/storage/integrity*.go`, `docs/PHASE5_STORAGE_INTEGRITY_REPORT.md`. Durable bounded server jobs, staged/atomic metadata publication, restart/idempotency/cancellation, no original deletion. Do not edit shared server/routes/schema files; coordinator integrates hooks. No storage/crypto/Telegram rewrite.
- Agent C (security): `internal/api/auth.go`, new API security tests; new `cmd/drive/security*.go` tests; new Telegram-only protection tests if needed, `docs/PHASE5_SECURITY_REPORT.md`. Source-first attack paths and minimal fixes. Propose shared server/main changes to coordinator. No secrets/live sessions.
- Agent D (deployment, next wave): `Dockerfile`, `compose.yaml`, new deployment-only scripts/tests and `docs/PHASE5_DEPLOYMENT_REPORT.md`. No actual Telegram session operations; coordinator qualifies private volumes/live recovery.
- Coordinator: shared `server.go`, `main.go`, contracts/domain, frontend integration, environment examples, risk register, operations/final reports, all real credentials/session handoff and live qualification. Agents never read `.env`, passwords, keys, session files, sensitive archives or live logs; synthetic fixtures only. No commits by agents.
- Phase 5 supersedes Phase 4 for named files. Other files require ownership agreement. Server copy contract: authenticated `/api/v1/copy-jobs` POST/GET, `/{id}` GET, `/{id}/retry` POST, `/{id}` DELETE(cancel). Source IDs, destination parent and client operation ID; immutable plan, bounded serial chunk processing, persisted progress, atomic visible metadata where practical. Existing file copy/transfer APIs remain compatible. Coordinator approves final wire types before UI integration.
- Recovery is offline into a fresh nonexistent directory, never overwrites existing data. Default restored HTTP sessions revoked; existing original sessions untouched. Preserve original Telegram authorization: stop original before any isolated instance uses its copied authorization, stop isolated instances before original resumes. Never copy restored metadata over original. No remote deletion of preexisting objects.

### Phase 4 ownership override

- Agent A: `web/src/styles.css`, `components/ui.tsx`, `components/Login.tsx`, `docs/PHASE4_DESIGN_SYSTEM.md`. Own all CSS; coordinate new class names with peers. Refine existing evergreen/stone identity, compact chrome, accessible contrast/touch targets and reduced motion. No dependency additions.
- Agent B: `components/Sidebar.tsx`, `Toolbar.tsx`, `FileBrowser.tsx`, `ActionDialogs.tsx`, new file-manager-only helpers/tests. Add optional component callbacks for inline rename, internal move, clipboard and navigation collapse; coordinator integrates `App.tsx`. No CSS edits.
- Agent C: `components/Preview.tsx`, `PdfPreview.tsx`, `Transfers.tsx`, `use-transfers.ts`, new preview/transfer-only helpers/tests. Preserve bounded streaming, real pause/resume semantics and digest verification; coordinate additive hook APIs with coordinator. No CSS edits.
- Coordinator: `App.tsx`, `api.ts`, shared `types.ts`/`utils.ts`, package scripts/config, command palette, shared integration and tests/docs/screenshots. Backend/storage source stays unchanged unless a demonstrated regression requires a narrow fix.
- Phase 4 overrides prior ownership. Agents never read actual `.env`, passwords, sessions, keys or live logs; coordinator alone handles private authentication and live operations. Do not touch preexisting remote objects. Use generated fixture data, preserve current storage/session, no public deployment/push. Agents do not commit.
- Keep public component props compatible until coordinator agrees changes. Current API has no server-side copy; any file copy must re-upload through bounded encrypted chunk flow, with original untouched and accurate transfer state. Do not invent sharing features or decorative transfer controls.

### Phase 3 ownership override

- Agent A: `internal/storage/telegram.go`, `telegram_test.go` and new Telegram-only benchmark/test files. Inspect MTProto allocation/routing/retry; propose changes after coordinator baseline. No live sessions or `.env` access.
- Agent B: `internal/storage/engine.go`, `reader.go`, `engine_test.go` and new engine/reader test/benchmark files; reliability failure injection. Do not edit Telegram adapter or its tests. No new production concurrency without measured approval.
- Agent C: `Dockerfile`, `compose.yaml`, `scripts/phase3_docker_check.py` and its tests, `docs/PHASE3_DOCKER_REPORT.md`; isolated Docker qualification design and operations/readiness review. Coordinator alone executes secret-bearing setup/live Docker operations.
- Coordinator: `cmd/`, `internal/api/`, `internal/domain/`, `contracts.go`, `telegram_config.go`, root dependencies, frontend, benchmark harness and remaining Phase 3 docs; all live credentials, session handoff, benchmarks and integration.
- Phase 3 overrides Phase 2 ownership below. No agent reads actual `.env`, session, keys, owner password, or live logs. Use synthetic fixtures only; request coordinator-sanitized results. No commits by agents.
- Host and Docker must never concurrently use copies of the same MTProto authorization. Stop host before isolated volume import; stop Docker before resuming host. Never write a cloned metadata database back over original drive. No preexisting remote deletions; generated qualification objects stay tracked.
- Preserve API and blob interfaces unless coordinator approves specific changes. Baseline chunk size is 4 MiB, engine slots 4, uploader part workers 2. Optimizations require comparable before/after evidence and integrity/cancellation tests.

### Phase 2 ownership override

- Agent A: `internal/storage/telegram.go`, `telegram_disabled.go`, `telegram_test.go` only; MTProto resolution, lifecycle, status and live-adapter fixes.
- Agent B: non-Telegram `internal/storage/` files except `contracts.go`; `scripts/telegram_qualification.py` and its tests; `docs/RELIABILITY_REPORT.md`, `docs/PERFORMANCE_REPORT.md`. No live writes until coordinator verifies target.
- Agent C: `internal/api/`, `web/`, and `docs/SECURITY_REVIEW.md`; authenticated connection diagnostics, focused security review, transfer telemetry UI and QA.
- Coordinator: `cmd/`, `internal/domain/`, `internal/storage/contracts.go`, `telegram_config.go`, root config, remaining scripts/docs, local interactive authentication and all live test authorization.
- Never read or relay `.env` values to agents. Agents use synthetic credentials only. Coordinator alone handles live environment/session. Existing drive data and preexisting channel messages are preserved.
- Shared status: `domain.StorageStatus{State, LastCheckedAt, RetryCount}`; Telegram `Status() domain.StorageStatus`; API `Config.StorageStatus func() domain.StorageStatus`; diagnostics adds `storage_connection`. No raw Telegram errors, account/channel identity or credentials in HTTP status.
- With a channel link, `TelegramConfig.ChannelID` is an optional expected positive MTProto ID. Resolve link and compare before any upload. Manual access hash remains legacy-only. Root normalizes configured Bot API channel IDs.

Read `docs/API_CONTRACT.md` and `docs/ARCHITECTURE.md` before implementation. Normal private API resources require owner session; first-run setup uses the separate bootstrap-token gate described above. Same-origin mutation checks, HttpOnly SameSite cookie, server-side session revocation, bounded request bodies, parameterized SQL, safe filenames, and folder-cycle checks are required. Never return saved Telegram credentials/session to the browser. Owner-authorized web setup accepts credentials over HTTPS or loopback only. No public signup or sharing.

Use AES-256-GCM with fresh nonce and chunk identity as authenticated data. Verify plaintext SHA-256 and reject corrupted content. Keep keys outside SQLite and version control. Never silently switch adapters on a configured drive. Local adapter must work without Telegram credentials. Persistent upload state must tolerate retries and restart.

## Verification

Run focused checks during implementation, then `go test ./...`, `go test -race ./...`, frontend typecheck/build and browser acceptance. Use real temporary fixtures, never personal data. Test auth rejection, checksum roundtrip, interrupted upload, restart, range download, rename/move, trash recovery, corruption, and backup recovery. Keep `docs/PROGRESS.md` current. Distinguish PASS, PARTIAL, FAIL, NOT TESTED in final report. Respect reference licenses: architecture research only unless license is verified.
