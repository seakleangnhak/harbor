# Harbor v1.1.0 release qualification

2026-10-21. **RELEASE = PENDING** until hosted validation and published-image checks finish. Owner explicitly authorized this sanitized-source release and image publication. Original development repository, v1.0.0, running deployment, SQLite/key/session and preexisting Telegram objects are excluded and preserved.

## Scope

- Browser first-run owner password, Telegram QR/optional two-step authorization and local storage selection, guarded by runtime setup token and same-origin checks.
- Premium light/dark responsive onboarding; active Telegram flow survives browser reload.
- New 100 MiB application chunks, bounded two-slot buffers, in-place authenticated encryption and persisted legacy 4 MiB geometry compatibility.
- Source/runtime/image version alignment, pull-based Compose quickstart, browser-based Dokploy setup, preserved legacy configuration on upgrades.
- Qualification helper chunk ceiling corrected from 8 MiB to 100 MiB; exact-image tests cover real web onboarding plus legacy initialization.
- Release package guard compares owner-approved visibility and immutable ID, retaining owner/repository linkage checks. Existing public package is not made private or vice versa.

## Verification ledger

| Check | Result | Evidence |
| --- | --- | --- |
| Independent onboarding/storage review | PASS, scoped | No concrete release blocker found; no speculative refactor |
| Frontend tests/typecheck/production build | PASS | 29 tests; Node 24; isolated no-network container |
| Default/tagged Go tests/race/vet | PENDING | Fresh isolated checks; no personal state |
| Python policy/recovery/qualification tests | PASS | 49 tests; fresh no-network sandbox, compiled recovery CLI |
| Chromium/WebKit and axe | PASS | 180 layouts/theme combinations, 180 axe audits, zero violations/page errors; actual 208,896-byte UI roundtrip per browser; Telegram UI simulated |
| Production Dockerfile AMD64/ARM64 | PASS | Actual source Dockerfile built both platforms locally; no runtime credentials |
| Exact-image web setup/checksum/resume/recreation | PARTIAL | ARM64 passed 104,923,157-byte encrypted multipart roundtrip, Range, auth/CSRF, web setup lockout, restart/resume/recreation and legacy init; AMD64 running |
| Hosted main/release CI and GHCR publication | NOT TESTED | Must record actual run IDs/results |
| Registry index/labels/SBOM/provenance/pulled image | NOT TESTED | Must record immutable digest |
| Live browser Telegram authorization/new 100 MiB remote storage | NOT TESTED | Earlier live qualification remains separately scoped |
| Live cross-datacenter Telegram retrieval | NOT TESTED | Deterministic protocol coverage retained |

Application/unit/browser tests run inside bounded no-network, read-only-root containers with synthetic fixtures. Exact-image HTTP smoke uses a dedicated loopback-published Docker bridge with outbound masquerading, DNS and peer connectivity disabled. Reviewed Docker orchestration uses the host CLI; no socket is mounted in target/test containers. Dependency-fetch build stages may access public registries without application credentials.

The initial default race run hit a 10-second test response-header deadline while awaiting authenticated 100 MiB Range output under concurrent compilation. Test-only process/client/header deadlines are now bounded at 300/90/60 seconds; production timeouts and integrity checks remain unchanged. Focused recovery race rerun passed in 18.874 seconds; full rerun continues. Initial exact-image smoke also exposed Docker Desktop suppressing published ports on `--internal` networks; dedicated bridge controls above preserve host ingress and passed actual ARM64 smoke.

Scans: Gitleaks 8.30.1 found zero findings in staged source, all 18 reachable sanitized commits and actual Docker build context. These scoped pattern checks do not constitute an independent security audit.

## Distribution and upgrades

Image: `ghcr.io/seakleangnhak/harbor:1.1.0`; stable aliases `1.1` and `latest` update only after release validation. Existing `1.0.1` and `1.0` remain historical. Source repository is private; existing GHCR package is public, verified through owner API before release. No visibility changes or application deployment requested/performed.

Keep a verified encrypted backup, same volume and effective legacy credentials/configuration when upgrading. Never initialize existing data, remove keys or run two copies of one Telegram authorization. New first-run setup needs a private random runtime `DRIVE_SETUP_TOKEN`; initialized drives retain their login and binding.

Personal-use qualification is not an independent security audit, public-internet readiness, guaranteed Telegram durability or full cross-DC coverage. A small preview/Range can require one whole 100 MiB authenticated chunk; finalization still serializes metadata mutations. No new live throughput claim is made.
