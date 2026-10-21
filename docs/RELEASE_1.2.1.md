# Harbor v1.2.1 patch qualification

2026-10-22. **Status: PENDING.** Historical v1.2.0 tag/image remain immutable. The owner-authorized CI/CD and release task includes this narrowly scoped corrective release. No deployment, visibility change or original runtime state access.

## Defect and fix

The v1.2.0 release passed exact-commit CI and both published-image checks. Later documentation-only [CI 39657728596](https://github.com/seakleangnhak/harbor/actions/runs/39657728596) passed Go/frontend/Docker and seven browser suites, but WebKit preview reported two uncaught native stream-controller errors after all 15 screenshot contexts completed. This run is **FAIL**, not a successful rerun.

Installed PDF.js 6.4.299 requires `reason instanceof Error` in its stream cancellation callback before setting its internal `isClosed` flag. Harbor passed no reason. Native cancellation could close the underlying stream while the PDF.js callback rejected before updating that flag, allowing late worker `ENQUEUE`/`CLOSE` messages to hit an already closed native stream.

Both bounded PDF-text cancellation sites now supply an `Error`. No broader lifecycle refactor, dependency change or suppression of browser errors. Preview regression adds four open/zoom/close cycles and preserves stack traces/current preview identity for any uncaught exception. Existing 256 Ki-character text limit and preview layout remain unchanged.

Go caches use a toolchain/platform/dependency prefix, a per-commit key and compatible prefix restoration. Each successful new commit saves refreshed build artifacts; an old immutable dependency-only entry can no longer prevent updates. Go still validates source, dependencies and compiler flags internally. Migration may start cold; timing evidence distinguishes cold, partial-cache and warm runs. Test commands, isolation and release gates remain unchanged.

## Acceptance

Required before tagging: successful hosted main CI for the exact patch commit, all eight browser suites with zero uncaught errors, frontend types/build/tests, Go/race/vet and both architecture image smokes. Required after tagging: GHCR immutable-index/attestation validation and actual published-image checksum/restart/recreation checks on AMD64 and ARM64. Results and digest will be recorded after execution.

**PASS, pre-cache-change candidate:** [CI 39659167222](https://github.com/seakleangnhak/harbor/actions/runs/39659167222), commit `572b0cb`, all four jobs. Backend took 18m with the older dependency-only cache. All eight browser suites passed, including four repeated PDF open/zoom/close cycles in each engine; 110 screenshots and 106 axe checkpoints. Account/preview/transport suites captured no uncaught exceptions; the large-directory suite has no separate exception listener. Reviewed 12 desktop/mobile/light/dark screenshots without critical visual defects. Rolling-cache workflow qualification remains pending for the final source commit.

Existing [v1.2.0 qualification](RELEASE_1.2.0.md) remains scoped to its actual passing runs. New live Telegram throughput/web authorization, live cross-DC retrieval and optional video/MOV playback remain **NOT TESTED** here. Synthetic local-adapter checks are not remote Telegram qualification, an independent audit or public-internet readiness.

## Upgrade

Create and verify an encrypted backup; preserve the existing persistent volume and effective storage configuration. Use the final qualified `ghcr.io/seakleangnhak/harbor:1.2.1` image when published. Never reinitialize an existing drive or replace its content key. This task performs no deployment.
