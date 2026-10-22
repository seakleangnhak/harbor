# Milestone B — preview, memory and storage hygiene

Date: 2026-10-22. Maintained sanitized export, branch
`codex/transfer-engine-stabilization`, baseline `b6ae39e`.
Published v1.2.1, historical tags and the owner's installation remain unchanged.
**HARBOR_MILESTONE_B = PASS** for bounded local personal-use qualification.
Local candidate **1.3.0-rc.1** passes final image smoke/live checks below.
No push, release tag, registry publication or Dokploy upgrade occurs here.

## Implementation

| Change | Result and preserved boundary |
| --- | --- |
| Authenticated reader retains its current chunk until replacement/close | Removes a reproduced duplicate fetch/decrypt at chunk boundary/EOF. One plaintext chunk per reader; existing slot limit unchanged. |
| HTTP metadata preflight uses standard `http.ServeContent` with HEAD | HEAD, 304, 412 and 416 avoid remote body reads. Successful content authenticates its first served chunk before sending headers. Suffix, multipart, unsatisfiable-prefix and If-Range behavior has exact-byte coverage. Later corruption still aborts the stream. |
| Private staged-part buffer reuse | Reuses one buffer after the previous 4 MiB plaintext is consumed. Envelope/AAD/GCM/length/SHA checks remain. Independent readers and ordinary adapter Gets retain independent ownership. |
| Offline storage diagnostics and confirmed local cleanup | Existing protected state and exclusive drive lock required. No engine, key/session or Telegram startup. Full plan revalidation, guarded SQL and fsynced audit intent before local unlink. Unknown/recoverable data and all remote objects retained. |
| Qualification harnesses | Actual preview media, 10,000-entry browsing, sustained streaming mixed traffic, preview SIGKILL and actual ENOSPC recovery added. No production failure-injection hooks. |

No new production dependency, schema/envelope migration, plaintext disk cache,
preview derivative, worker pool or concurrency increase. Preserve 100 MiB
storage chunks, 4 MiB client parts, two pending chunks/upload, four globally,
one saving worker and existing two large engine-buffer slots.

## Measured performance and resources

Reader-only frozen-source comparison, five samples: boundary replay Gets
**2 → 1**, ciphertext **209,715,264 → 104,857,632 bytes**, median
**159.270 → 79.113 ms**, cumulative allocation **209,739,392 → 104,871,368 B/op**.
This removes duplicate work; ordinary cold previews still authenticate one
complete envelope. Baseline timing CV 20.92% versus 1.30% after; count/byte
evidence is stronger than a general speed claim.

Three-sample staged-reader benchmark: median cumulative allocation falls
about **95.97%**, from 105.095 MB to 4.231 MB per 100 MiB. This is local
decryption/allocation evidence, not Telegram throughput or peak-memory savings.

| Sustained local mixed fixture | Frozen baseline | Integrated changes |
| --- | ---: | ---: |
| Total test time | 89.535 s | 89.100 s |
| Continuous mixed phase | 65 s | 65 s |
| Completed mixed 100 MiB uploads | 42 | 60 |
| Full 100 MiB streaming HTTP downloads | 85 | 123 |
| Verified 256 KiB ranges | 317 | 366 |
| Complete 300 MiB folder copies | 2 | 2 |
| Lifetime process RSS high-water | 521.832 MiB | 520.047 MiB |
| Sampled anonymous cgroup maximum | 510.152 MiB | 483.727 MiB |
| Maximum last-GC live heap sample | 212.888 MiB | 208.837 MiB |
| Maximum allocated Go heap sample | 458.534 MiB | 503.471 MiB |
| Cgroup lifetime peak including file cache | 770.5 MiB | 770.0 MiB |
| Memory-limit reclaim events | 195,556 | 248,698 |
| Peak retained fixture disk | 1,576.091 MiB | 1,604.091 MiB |
| Encrypted staging / cache maximum | 200.002 / 200.000 MiB | 200.002 / 200.000 MiB |
| Queue observed / writer maximum | 2 / 1 | 2 / 1 |
| OOM events/kills | 0 / 0 | 0 / 0 |

Each run uses 768 MiB hard/no swap, 512 MiB Go soft and two CPUs. Small
kernel-accounting overshoot does not change the configured limit. Page-cache
reclaim pressure is substantial; **no spare cgroup-memory claim**. Sampler and
streaming clients share the process. One runtime sample/mode, fixed order and
different completed work counts limit causal throughput/RSS conclusions.
Workload includes active transfers after 60 seconds, queued-work restart,
post-restart eviction and original/copied-file full checksums. All finish;
pending/part rows drain. Persistent volumes have no hard application disk quota.

[Preview performance](PREVIEW_PERFORMANCE_REPORT.md),
[memory](MEMORY_QUALIFICATION.md) and [mixed workload](MIXED_WORKLOAD_REPORT.md)
contain percentile methods, CPU/goroutine/disk measurements and evidence scope.

## Regression evidence

All target execution uses isolated containers with generated fixtures, read-only
source/root, nonroot users, explicit environments and bounded resources.
Synthetic runtime has no network. Dependency-fetching build stages receive no
runtime secrets. Coordinator-only live checks serialize the separate authorized
test session; copied authorization is removed before releasing its source lock.

| Actual command/check | Result |
| --- | --- |
| `go mod verify` and Go formatting | PASS |
| `go test -count=1 -p 1 -timeout 5m ./...` | PASS |
| Same full suite with `-race` | PASS |
| Same full suite with `-tags telegram` | PASS |
| Same full suite with `-race -tags telegram` | PASS |
| `go vet -p 1 ./...`, also `-tags telegram` | PASS |
| Frontend `npm test`, `npm run typecheck`, fresh production build | PASS: 48 tests; build 8.51 s |
| Final Chromium/WebKit existing and new suites | PASS: 12 individual suites, 129 scoped axe checks, zero uncaught exceptions |
| Exact-image executable process acceptance | PASS: auth/origin guards, encrypted multipart/ranges, fresh encrypted backup/restore/checksum, session revocation; 3.054 s on final RC |
| Exact-image executable copy/crash acceptance | PASS: bounded copy/retry/cancel/restart and three-current-chunk SIGKILL recovery; 2.511 s on final RC |
| Real child SIGKILL during controlled preview fetch | PASS: disposable torn cache removed on restart, completed ciphertext/SHA unchanged |
| Existing nine transfer crash boundaries | PASS in full Go tests/race; queue recovery and no premature publication retained |
| Hygiene protection/failure-injection tests | PASS in all full Go modes; actual cleanup SIGKILL remains NOT TESTED |

Full Go commands plus vet/build take 635.966 s. Go 1.26.10 linux/arm64;
dedicated 8 GiB/three-CPU Colima VM; test/compile container 6.5 GiB hard/no swap,
4.5 GiB Go soft, two CPUs, `GOGC=30`, `-p 1`, `CGO_ENABLED=1`.
Compiler capacity is separate from the 768 MiB runtime qualification. Race
detection was retained. Final focused checks cover the later range-prefix fix
and test-only resource metric addition; ledger follows below.

Process acceptance controllers use 2 GiB including their Python full-response
buffers and file cache. Those whole-container peaks, 883.57 and 993.67 MiB,
are not application RSS or a claim of execution under 768 MiB. Separate sustained
streaming and exact-image runtime checks qualify that limit.

## Browser/media qualification

Four-page 1,117,527-byte PDF, 12 MP JPEG/PNG, six-second VP8 WebM and H.264 MOV
are generated fixtures. PDF page navigation/reopen/cancellation, image loading,
real playback/time advancement/seek to four seconds, HTTP 206, rapid navigation,
16 MiB upload while rendering PDF, final SHA-256, restart and mobile themes pass.
Chromium and WebKit play/seek WebM. WebKit plays/seeks MOV; Chromium MOV is
**UNSUPPORTED** in this tooling distribution and shows fallback guidance.

Initial integrated Chromium PDF first page 856 ms, repeat p50/p95 236/258 ms,
next-page 108/119 ms. Quiet paired WebKit first page is 1,198 → 1,500 ms,
repeat p50/p95 1,086/1,411 → 1,020/1,289 ms and next-page
211/493 → 98/603 ms. Earlier next-page slowdown does not reproduce, but
results/tails remain mixed; no blanket browser acceleration is established.
Full distributions belong in the preview report. Browser/local OS caches
may be warm. These are real UI timings, **not cold Telegram media timings**.

10,000 generated folders load in 318/509 ms (Chromium/WebKit), sort in
195/285 ms and search in 316/362 ms in one observed run. List renders 17 virtual
rows; grid 50/80/50 cards during navigation; zero unsolicited content downloads.
1,000-entry existing regressions also pass. Tmpfs-backed metadata/fixtures,
small samples and browser scheduling limit performance inference.

Visual review covers desktop PDF/image, actual seeked video and mobile themes.
Existing mobile transfer-panel overlap with sidebar controls remains documented.
Physical Safari/iOS, complex PDF corpus and long-duration browser heap remain
NOT TESTED. Scoped axe success is not universal accessibility certification.

## Storage safety and security

AES-256-GCM fresh nonce/chunk AAD, plaintext chunk SHA-256 and full-file final
verification remain unchanged. Cache holds ciphertext only; every hit decrypts
and verifies, and finalization bypasses cache. Owned restart cache is disposable;
unknown files are refused. Cold 100 MiB small-range amplification remains.
Three-chunk cache thrash saves no Gets and adds 52.67% synthetic median cost;
do not enlarge cache/concurrency without measurements.

Maintenance refuses an active drive, missing/unsafe metadata, stale confirmation,
symlink/hard-link candidates and integrity failures. Recoverable jobs, canonical
blobs and unknown restoration artifacts are retained. No remote enumeration,
age-based abandonment inference or Telegram deletion exists. Interrupted local
cleanup is reconciled by a fresh diagnostic plan; filesystem/SQL/audit writes
are not one atomic transaction. See [hygiene report](STORAGE_HYGIENE_REPORT.md).

Reviewed source and maintained reachable Git history scans report zero Gitleaks
findings. This is not an independent security audit or public-internet
qualification. No secrets, private fixture state or runtime archives are tracked.

## Final qualification ledger and release decision

| Final check | Actual outcome |
| --- | --- |
| Latest API range-prefix fix, all four default/tagged/race modes and both API vets | PASS, 17.516 s; subsequent changes are version defaults/documentation only |
| Actual 12 MiB staging tmpfs ENOSPC | PASS, 0.07 s at 768 MiB hard/512 MiB Go soft; failed part unacknowledged, accepted part retained, restart/retry/full SHA pass |
| Current-source live Telegram | PASS, 173.859 s controller; 100 MiB + 17 multipart upload including final verification 38.078 s / 2.626 MiB/s; full download 19.119 s / 5.230 MiB/s; small concurrency, interrupted worker/session recreation and exact ranges pass |
| Current-source live cache | Four uncached ranges 89.278 s / 4 Gets / 419,430,528 ciphertext bytes; initially empty cache 22.008 s / 1 Get / 104,857,632 bytes. Cold 21.764 s, warm 87.417 / 77.087 / 79.248 ms |
| Final RC production Dockerfile build | PASS, local ARM64 image `harbor:1.3.0-rc.1`; version defaults/OCI/backend/frontend agree |
| Final RC exact-image local smoke | PASS: nonroot/executables/health, auth/CSRF, guarded onboarding/lockout, private modes, 104,923,157-byte multipart SHA, Range, resume/restart/recreation and clean shutdown |
| Final RC exact-image live Telegram HTTP/SIGKILL | PASS: fully accepted 5 MiB via two parts, 202 background saving, premature completion rejected; actual exit-137 SIGKILL during observed storing, automatic recovery without reselection, SHA/206 after restart and recreation, clean shutdown |
| Final RC image executables, backup and copy regression rerun | PASS, fresh local encrypted archive/restore/full SHA and three-chunk copy SIGKILL; timings above |
| Final RC UI equivalence | All 195 packaged UI files byte-identical to browser-qualified production build; no UI logic change after browser suite |
| Private authorization handling | Both live harnesses confirm source unchanged and copies removed after target shutdown; separate final-volume check confirms no copied authorization |

Immutable **local ARM64 image/index ID** (not a registry digest):
`sha256:344373078e8bac44a5a6732d8b1fb6a23a164d589a39ff484fbd0ff0273bba2f`.
Browser suites ran application image
`sha256:e17b7eb22baba5c3c5dfdb004e5b18e0c00f47d904dc9d6eab4b9d0b41b861b4`;
only version labeling/defaults changed for RC and its packaged UI is identical.
Full Go/race/frontend unit checks precede that metadata-only promotion;
RC frontend production build and exact-image checks are fresh.

Exact RC live startup-to-ready: 0.564 / 0.329 / 0.564 s. Browser acceptance
of 5 MiB takes 47.345 ms while remotely stored bytes remain zero; recovery and
final verification 3.889 s. Download after kill/recreation takes 1.058 / 1.024 s,
with full SHA-256 and HTTP 206. Observed storing proves process interruption,
not a specific MTProto wire/acknowledgement offset. Current-source direct test
samples reach 306.5 MiB working set / 59.90% CPU (87 samples); exact RC small-file
run reaches 30.39 MiB / 7.73% CPU (four samples), ready idle sample 8.918 MiB.
Docker working-set samples are not lifetime RSS or sustained capacity peaks.
Both live runtimes use 768 MiB hard/no swap, 512 MiB Go soft and two CPUs.

Reproduce builds with the command in [candidate guide](V1.3_RELEASE_CANDIDATE.md).
Coordinator live commands use the existing reviewed `transfer_live_check.py`
and `milestone_a_docker_live.py`, private source configuration outside Git,
immutable image IDs, the isolated compiled-binary volume and fresh ignored
output directories. Never run copied authorizations concurrently. Public
[performance comparison](TELEGRAM_PERFORMANCE_COMPARISON.md) records raw timing
and one-run variance limits; no universal Telegram speedup is attributed.

All ten Milestone B acceptance areas pass within these explicit limits. Real
PDF/video render/play/seek is qualified using local generated browser fixtures;
live Telegram browser media latency and live cross-DC remain NOT TESTED.
Sustained local fixture pressure is qualified, not multi-hour/live mixed-load
headroom. Actual cleanup SIGKILL/power loss, hard persistent-disk quotas and
remote orphan ownership/enumeration remain unverified or unsupported. Unknown
remote/local artifacts are retained, not silently collected. No critical
corruption, authentication regression, incomplete publication or deadlock was
found in the tested scope. This is not an independent security audit.

Local evidence stays under ignored `output/milestone-b/`: qualification source
manifests/logs, paired reader/staging/memory data, final/quiet browser artifacts,
current-source live and final RC live results. Four reviewed generated screenshots
are committed; private authorization, keys, fixture metadata and archives are not.
Live generated fixture objects remain tracked in protected new volumes without
copied authorization; no preexisting Telegram messages were deleted. Original
owner container remains healthy; default Docker context remains Desktop; the
dedicated Colima VM is stopped. No owner deployment/volume was used for destructive
checks, and no remote publication or upgrade occurred.

Checkpoints: `e441165` transfer/read fixes and tests; `2a2568b` conservative
maintenance; `f07d30a` real media/large-directory harnesses. Final candidate/version
and documentation checkpoint follows; branch HEAD records its hash. Work began
from clean `b6ae39e`; all modifications were preserved in local commits. Existing
published tags remain unchanged. **1.3.0-rc.1 is ready for owner review**, with
explicit approval required for any push, stable tag, GHCR publication or deployment.
Next work should prioritize a longer live mixed-load soak and a separately
reviewed secure preview-derivative lifecycle; no speculative implementation starts
without owner review.
