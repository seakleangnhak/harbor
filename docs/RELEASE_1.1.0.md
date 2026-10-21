# Harbor v1.1.0 release qualification

2026-10-21. **RELEASE = PASS for artifact qualification and publication.** [GitHub v1.1.0](https://github.com/seakleangnhak/harbor/releases/tag/v1.1.0). Current live Telegram scenarios below remain NOT TESTED; earlier qualification is not substituted for them. Owner explicitly authorized this sanitized-source release and image publication. Original development repository, v1.0.0, running deployment, SQLite/key/session and preexisting Telegram objects are excluded and preserved.

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
| Default/tagged Go tests/race/vet | PASS | All six local commands completed; hosted main suite also passed |
| Python policy/recovery/qualification tests | PASS | 49 tests before release; post-tag compatibility change passed 28 focused sandbox tests, including numeric version boundaries |
| Chromium/WebKit and axe | PASS | 180 layouts/theme combinations, 180 axe audits, zero violations/page errors; actual 208,896-byte UI roundtrip per browser; Telegram UI simulated |
| Production Dockerfile AMD64/ARM64 | PASS | Actual source Dockerfile built both platforms locally; no runtime credentials |
| Exact-image web setup/checksum/resume/recreation | PASS | Both actual local images passed 104,923,157-byte encrypted multipart SHA-256, Range, auth/CSRF, web setup lockout, restart/resume/recreation and legacy init; native ARM64/emulated AMD64 |
| Historical image compatibility | PASS | Published v1.0.1 ARM64 child passed actual 4,259,861-byte checksum/Range/resume/recreation smoke; web setup correctly reported unavailable |
| Hosted main CI | PASS | [39580242590](https://github.com/seakleangnhak/harbor/actions/runs/39580242590), source `fd8f439`; all five jobs |
| Tagged release validation and GHCR publication | PASS | [39582004996](https://github.com/seakleangnhak/harbor/actions/runs/39582004996), immutable v1.1.0 |
| Registry index/labels/SBOM/provenance/pulled image | PASS, scoped | Hosted and independent local immutable-index verification; both platform children pulled and passed full 104,923,157-byte encrypted multipart smoke; descriptor/predicate checks, not signature verification |
| Live browser Telegram authorization/new 100 MiB remote storage | NOT TESTED | Earlier live qualification remains separately scoped |
| Live cross-datacenter Telegram retrieval | NOT TESTED | Deterministic protocol coverage retained |

Application/unit/browser tests run inside bounded no-network, read-only-root containers with synthetic fixtures. Exact-image HTTP smoke uses a dedicated loopback-published Docker bridge with outbound masquerading, DNS and peer connectivity disabled. Reviewed Docker orchestration uses the host CLI; no socket is mounted in target/test containers. Dependency-fetch build stages may access public registries without application credentials.

The initial default race run hit a 10-second test response-header deadline while awaiting authenticated 100 MiB Range output under concurrent compilation. Test-only process/client/header deadlines are now bounded at 300/90/60 seconds; production timeouts and integrity checks remain unchanged. Focused recovery race rerun passed in 18.874 seconds; full default/tagged reruns passed. Initial exact-image smoke also exposed Docker Desktop suppressing published ports on `--internal` networks; dedicated bridge controls above preserve host ingress and passed actual ARM64 smoke.

Scans: Gitleaks 8.30.1 found zero findings in release source, reachable sanitized history and actual Docker build context. Final receipt/source/history scans also passed; reports remain in ignored qualification output. Published drive/recovery binaries for both platforms exactly match locally qualified/scanned binaries. These scoped pattern checks do not constitute an independent security audit.

## Source and reproducibility

- Annotated tag `v1.1.0`: `f45dc67ee3dc33957471988d2ecb32eae2ac4a1d`.
- Source: `fd8f43987d74cbd2f99ea7b4436981345c2abff8`; clean sanitized main before tag push.
- Hosted main cold-cache builds: 124.119 and 126.301 seconds; tagged release builds: 153.008 and 153.509 seconds. Both independent builds matched: drive `a6e625305e2a8514b1a9994676da89d4f7dffce02cf434f481bb4d9a624672e9`; recovery `09673cfbe6cc5881399da704dca476fd5810f133f35bf64ec76039742589491c` (Linux AMD64).
- Onboarding remains lazy-loaded: 19.04 kB JS / 5.91 kB gzip, 12.21 kB CSS / 2.96 kB gzip. No new dependencies or speculative runtime rewrite. Existing main-bundle >500 kB build warning remains; no throughput improvement claimed.
- Original source-push run [39579598156](https://github.com/seakleangnhak/harbor/actions/runs/39579598156) was superseded/cancelled after both Docker jobs failed at internal-network port discovery. Corrected complete main run above passed; no failed run is called green.
- Fresh source and Dokploy Compose JSON checks passed version/default setup, loopback/no-host-port policies, nonroot/read-only protections and external persistent volume identity. Source setup and existing legacy configuration cases both checked.

## Published image identity

```text
ghcr.io/seakleangnhak/harbor@sha256:8d6aa16e222b3bcef948b8cf61b7799b78206879de7dcf4bc2232274e015d132
```

| Platform | Immutable child digest |
| --- | --- |
| `linux/amd64` | `sha256:2ad22f47564362ba21b5fb9700460cdd193873ac45a503e6c0e5b2ab876e83e2` |
| `linux/arm64` | `sha256:96eafe674e64597719fc42a3b777867665b109f205d40a8aa0425f74a919c684` |

`1.1.0`, `1.1` and `latest` were independently resolved to this exact index. Historical `1.0.1` and `1.0` still resolve to `sha256:d5c56ecd655e83f298e63f3e97f7df7582aecd9ebdd45b0d6aed6801b36bc855`. Both attached SPDX 2.3 SBOMs contain 54 packages; minimal SLSA provenance predicates and child-attestation descriptor bindings passed. Raw subject/signature verification and independent security auditing are outside this check.

Hosted and local pulled-image smoke both verify owner setup, packaged web assets, token/origin rejection, permanent setup closure, legacy CLI initialization, private state modes, nonroot executable/health checks, multipart reconstruction, SHA-256/Range, interrupted upload and persistent restart/recreation. Native ARM64/emulated AMD64 were tested locally. Telegram requests were not made. Disposable fixtures were removed; original healthy deployment remained running.

Post-tag changes on main contain verifier compatibility for historical 1.0.x images and this receipt/runbook cleanup only; they do not move the release tag or rebuild published artifacts.

## Distribution and upgrades

Image: `ghcr.io/seakleangnhak/harbor:1.1.0`; stable aliases `1.1` and `latest` update only after release validation. Existing `1.0.1` and `1.0` remain historical. Source repository is private; existing GHCR package is public, verified through owner API before release. No visibility changes or application deployment requested/performed.

Keep a verified encrypted backup, same volume and effective legacy credentials/configuration when upgrading. Never initialize existing data, remove keys or run two copies of one Telegram authorization. New first-run setup needs a private random runtime `DRIVE_SETUP_TOKEN`; initialized drives retain their login and binding.

Personal-use qualification is not an independent security audit, public-internet readiness, guaranteed Telegram durability or full cross-DC coverage. A small preview/Range can require one whole 100 MiB authenticated chunk; finalization still serializes metadata mutations. No new live throughput claim is made.
