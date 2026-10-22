# Harbor v1.3.0 release notes

Prepared 2026-10-22. **Local private personal-use qualification PASS. Publication
pending.** No v1.3.0 Git tag, GitHub Release or GHCR image is published by this
preparation. Published stable remains v1.2.1. See the
[publication plan](V1.3_PUBLICATION_PLAN.md) for separate approval gates.

## Changes since v1.2.1

- Durable background saving lets browser parts continue while accepted chunks
  save to Telegram. Next-file handoff keeps the queue moving. Progress separates
  browser acceptance, remote storage and verified file publication.
- Existing 100 MiB storage chunks and 4 MiB browser parts stay compatible.
  Two pending chunks per upload, four globally and one saving worker bound
  admission. Accepted queued bytes recover after restart; missing browser input
  still requires original-file reselection.
- A private ciphertext-only read cache reuses remote chunks. Every hit still
  authenticates/decrypts and checks its hash. Final remote verification bypasses
  the cache. Reader replay and conditional/range handling avoid redundant reads.
- Staging assembly reuses one part buffer without changing encryption identity,
  nonce generation or checksum verification.
- Offline storage diagnostics report metadata/jobs/staging/cache. Explicitly
  confirmed, revalidated and audited cleanup removes only proven disposable
  local objects. Unknown/recoverable data and all Telegram objects are retained.
- PDF cancellation fixes handle errored streams and cancellation before headers,
  without suppressing browser errors. Mobile navigation stays above transfers.

No encryption-format rewrite, completed-manifest migration, new runtime
dependency, queue increase or worker increase is introduced.

## Verified scope

Qualified source: `b10764ed054db1ea3031cfa581211874841ed34d`. Publication preparation
adds documentation only; the exact proposed source hash is frozen in the Gate A
approval and local candidate manifest described in the publication plan.

| Gate | Actual qualification |
| --- | --- |
| Go | Default/Telegram-tagged tests, both race modes, both vets, module verification |
| Frontend | 54 tests, TypeScript checks and production build |
| Browser | Chromium/WebKit, 14 suites, 139 scoped axe checks |
| Sustained workload | Two independent 30-minute generated local transfer runs at 768/1024 MiB, plus separate 30-minute browser media run |
| Live Telegram | Exact prepared image: 900 seconds, multipart, concurrent uploads, full SHA-256, ranges, storing-state SIGKILL recovery, restart/recreation |
| Recovery | Verified encrypted archives; archive-only fresh candidate restore and fresh v1.2.1 rollback; copied authorization removed before source unlock |
| Images | ARM64 native and AMD64 emulated runtime smokes; repeated local multiarchitecture OCI exports byte-identical |

The [final gate](V1.3_RELEASE_FINAL_GATE.md) records exact source/image identities,
commands, measurements, earlier failed attempts and limitations. These local
checks do not substitute for hosted CI or verification of a delivered GHCR image.

## Operating requirements

Use Docker/Compose v2 on Linux AMD64 or ARM64. Start with **1 GiB container memory**,
the existing **512 MiB Go soft limit**, two CPUs and one Harbor instance. The
shipped Compose files remain at 768 MiB; explicitly set `services.drive.mem_limit`
to `1g` in the deployment copy or use the documented
[Compose override](PRODUCTION_OPERATIONS.md#protected-installation). Do not change
concurrency to compensate for pressure.

768 MiB passed bounded tests but has no proven headroom. Both tested budgets
reached their cgroup ceilings through file cache; 1 GiB reduced observed reclaim
stalls without proving spare capacity or a universal minimum. Monitor RSS/live
heap separately from cgroup anonymous/file memory, events and PSI.

Plan roughly **400 MiB active encrypted staging + 256 MiB cache**, plus envelope/
filesystem overhead, SQLite/WAL, older recoverable artifacts, encrypted backups
and fresh restore space. Local storage additionally needs its encrypted file
library. This is a sizing model, not a total disk bound. No hard persistent-volume
quota exists. Monitor both container-volume and VM backing-host free space.

## Installation and upgrade

The [README](../README.md#docker-compose) currently pulls published v1.2.1.
Only after approved v1.3.0 publication and delivered-image verification, pin the
actual verified `ghcr.io/seakleangnhak/harbor@sha256:DIGEST` in the deployment.
The local no-attestation OCI digest is not a substitute for that registry digest.

Within an owner-approved maintenance window, finish or cancel upload/copy jobs, confirm queue
drain, and stop the sole Harbor process. Create and verify an encrypted offline
backup, retain its password separately, and test a fresh restore before upgrading.
Keep the same protected volume, original key, effective configuration and Telegram
session; do not initialize a bound drive. After separately approved deployment,
verify authentication, readiness, folders, old-file SHA-256/ranges, a new generated
upload/download and backup creation.

Before rollback, drain/cancel new jobs, stop the candidate, and preserve newer
references in a verified post-upgrade archive. Restore the verified pre-upgrade
archive into a fresh volume using its matching old image/configuration. Never
start v1.2.1 against pending new queue state, overwrite candidate metadata or
run original/restored copies of one mutable Telegram authorization concurrently.
Old snapshots omit newer files; retain both installations/archives until those
files have an explicit recovery/export plan. See the
[tested upgrade/rollback procedure](V1.3_UPGRADE_ROLLBACK_REPORT.md).

## Limits and durability

Telegram is an external storage dependency. Harbor guarantees no independent
content durability: loss of the account, channel or messages can make content
unavailable. Telegram recovery archives contain local metadata, keys,
configuration, session and references, **not a mirror of remote ciphertext**.
Maintain an independent protected content copy as well as encrypted metadata
backups; key loss without a usable archive has no recovery bypass.

Live cross-DC retrieval, physical Safari/iOS, multi-day operation and provider
durability remain unverified. Cold tiny ranges still authenticate a whole
100 MiB envelope; cold live-browser PDF/video latency is not qualified. Chromium
MOV is unsupported in the test distribution. No independent security audit or
public-internet security qualification is claimed. Read
[known limitations](KNOWN_LIMITATIONS.md) before relying on irreplaceable data.
