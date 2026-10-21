# Browser onboarding and 100 MiB storage qualification

2026-10-21. Local source qualification **PASS**; new live Telegram web authorization and release-image qualification **NOT TESTED**. Published image 1.0.1 remains unchanged. No push, tag, image publication or remote deployment occurred.

Implementation checkpoint: `0a9277d` on `codex/web-onboarding`. Documentation/screenshots follow in a separate local commit.

## Changes

- Fresh Compose/container installations require only a random runtime `DRIVE_SETUP_TOKEN` before browser setup. The owner chooses a password, enters Telegram API/channel details, approves QR login and supplies optional two-step verification in the web UI. Local encrypted storage is also supported.
- Setup has a separate token/same-origin gate, bounded inputs and attempts, fixed safe errors, cancellation and expiry. It closes permanently after initialization. Private configuration/password/session files activate together through a synced directory rename. Existing initialized drives cannot be reclaimed through setup.
- New application chunks are 104,857,600 bytes (100 MiB), plus a 32-byte encryption envelope. Existing 4 MiB manifests retain their geometry. AES-GCM, SHA-256, chunk identifiers and remote-reference formats remain compatible. The total-file ceiling remains 1 TiB.
- Shared engine slots bound active chunk buffers; default geometry permits two operations. Encryption/decryption use owned buffers in place. Telegram retains 512 KiB multipart requests with two part workers and a bounded size-aware deadline.
- Encrypted backups automatically include browser-created configuration, owner credentials and required Telegram session material. Restore uses the existing portable archive format.

## Executed checks

Target checks used synthetic fixtures in isolated, network-disabled Linux containers, explicit environments, read-only source/toolchains, scratch-only writes and bounded CPU/RAM/time. Existing deployments, private environment files, sessions, keys and databases were untouched.

| Check | Result | Evidence |
| --- | --- | --- |
| `go test ./...`, `go test -tags telegram ./...` | PASS | All packages; process onboarding and encrypted restore included |
| `go test -race ./...`, `go test -race -tags telegram ./...` | PASS | Full default/tagged suites; focused startup/setup rerun after final Go changes |
| `go vet ./...`, `go vet -tags telegram ./...` | PASS | Both build modes |
| `npm test`, `npm run typecheck`, `npm run build` | PASS | 29 tests; final CSS included; existing Vite large-bundle warning remains |
| `python3 -m unittest discover -s scripts -p '*_test.py'` | PASS | 44 tests, 13.907 seconds; `GOFLAGS=-buildvcs=false` for sandbox Git ownership |
| `docker compose --env-file /dev/null config --quiet` | PASS | Source Compose syntax/interpolation |
| Actual process startup, owner setup and login | PASS | Public status only; unauthenticated setup/private access rejected; setup closed after activation |
| 100 MiB + 17-byte upload, restart/resume, Range and download | PASS | Process integration verifies complete SHA-256, missing-chunk rejection and persisted geometry |
| Encrypted backup/fresh restore | PASS | Key/config, hierarchy, favorites, Trash and file checksum survive; wrong password rejected; restored HTTP sessions revoked |
| Old 4 MiB manifest compatibility | PASS | Existing geometry resumes and downloads; no migration |
| Telegram 100 MiB document | PASS, deterministic double | Production adapter sends 201 big-file parts, then retrieves exact bytes; oversize/cancellation/deadlines tested. No live RPCs |
| Chromium browser setup and transfer | PASS | Actual local onboarding/login/upload/download; QR/two-step/verified screens use intercepted synthetic responses |
| Browser 100 MiB + 17-byte failure/recovery | PASS | Three bounded attempts; incomplete file hidden; retry skips chunk 0; copy pause/reload/resume and cancellation pass |
| Accessibility and responsive layout | PASS, scoped | Seven axe WCAG A/AA checks, zero violations; no horizontal overflow or page errors in tested 1440×900/390×844 light/dark views |
| Non-root runtime image | PASS, artifact smoke | Tagged Go binaries and final web assets assembled on the pinned production distroless base; health, setup, 100 MiB multipart/checksum, restart and volume-preserving recreation pass |
| Complete production Dockerfile build | PARTIAL | Offline dependency stage could not fetch uncached npm packages. Individual source builds and artifact runtime pass; this is not exact production-image qualification |
| New live Telegram web QR/two-step/channel flow | NOT TESTED | Requires dedicated-account owner interaction and separate controlled live qualification |
| Live 100 MiB Telegram upload/download and cross-DC retrieval | NOT TESTED | Offline adapter tests do not establish live behavior |
| Remote Dokploy, GHCR and new release architectures | NOT TESTED | No remote changes or publication authorized in this task |

Browser/runtime multipart fixture: 104,857,617 bytes, SHA-256 `b5754d953d4cc43200372116548cbfec1c0759a3f22d0a6c328dce06030446cf`. Fixture-only runtime image ID: `sha256:855265eae9d428d5803edd7ca0f65e53c04e00650864c92f9b513598d9c79fa5`. These identify local test artifacts, not a released image.

Tooling failures were corrected before successful reruns: executable scratch mount, Linux npm optional-binary cache, missing Go in the Python tool image and sandbox VCS stamping. The first browser audit found selected-provider text contrast 4.34:1; using the existing secondary text token fixed it. Visual review also corrected the programmatically focused heading outline. An initial container harness sent an unsupported completion field; corrected `{}` requests passed. No failed run is counted as a pass.

## Reviewed screenshots

Generated isolated fixtures only; no real account, private filename, channel selector, token, password or QR appears. Desktop storage pages are full-page captures and intentionally scroll below the 900px viewport.

- [Owner setup, light](screenshots/onboarding-owner-light.png)
- [Storage setup, light](screenshots/onboarding-storage-light.png)
- [Storage setup, dark](screenshots/onboarding-storage-dark.png)
- [Storage setup, mobile dark](screenshots/onboarding-storage-mobile.png)

## Operational limits

Build current source to use these changes; published 1.0.1 and its pinned Dokploy template retain the legacy flow. Follow [browser onboarding](WEB_ONBOARDING.md). Live authorization and the exact source-built release image still require qualification before a new release.

HTTPS is required for remote credential entry. Keep one instance per data volume and preserve `/data`; setup is not an existing-owner reset or storage-migration facility. Bootstrap tokens/QRs are temporary credentials. Content keys and saved credentials remain host-readable private files; backups must remain encrypted.

Whole-chunk authentication means a small preview/Range may fetch/decrypt 100 MiB before its first plaintext byte. Retries resend an interrupted chunk. Two slots limit active buffers, not total process/browser memory. Proxy limits must accommodate 100 MiB requests and Telegram latency. No new live throughput, lifetime memory, Safari/iOS, public-internet-security or independent-audit claim is made.
