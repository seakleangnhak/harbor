# Telegram performance comparison — Milestone A

Date: 2026-10-22. Status: **PASS for the scoped current-source live transfer/cache qualification below**. Synthetic and current live measurements remain separately identified. Published v1.2.1 and historical release evidence are preserved. Overall release-candidate acceptance remains coordinator-owned; no current speed claim follows from older runs.

## Transfer geometry and counters

Current source keeps 100 MiB plaintext storage chunks, 4 MiB encrypted-at-rest browser staging parts, two pending chunks per upload, four globally and one saving worker. A full encrypted chunk is 104,857,632 bytes: 100 MiB plaintext plus the existing 32-byte envelope overhead. These limits were not increased for this milestone.

Browser-part acceptance, queued state and Telegram acknowledgement are separate. Background saving overlaps later client parts/next files; it does not eliminate final remote readback verification or make Telegram itself faster. Full-file publication still requires verified manifests and SHA-256.

| Counter | Meaning | Exclusions |
| --- | --- | --- |
| Storage adapter `GetCalls` | Calls made by engine/cache to obtain one complete encrypted storage chunk | Native adapter internal retries and individual MTProto RPCs |
| Live harness `GetBytes` | Ciphertext bytes returned by successful adapter calls | Wire framing, failed/partial attempts, native retry traffic |
| Live harness `PutBytes` | Ciphertext offered to adapter calls, including canceled attempts | Actual accepted wire bytes; not proof of remote publication |
| Controlled `upload.getFile` count | RPC count observed by mock invoker | Live wire measurement and Telegram account behavior |
| Go `B/op` | Cumulative allocations per benchmark operation | Process peak memory and retained heap |

## Controlled Telegram read amplification

`TestTelegram100MiBDocumentUsesBigPartsAndDownloadsIntact` exercises the actual adapter against a controlled MTProto invoker. Added assertions require:

- One `channels.getMessages` resolution for the document.
- Exactly 201 `upload.getFile` requests at 512 KiB, contiguous offsets and bounded final read.
- Exactly 104,857,632 returned ciphertext bytes.
- SHA-256 equality of downloaded ciphertext, valid big-file multipart upload and rejection above the size limit.

This is deterministic protocol coverage, not live traffic. Full Telegram-tagged race tests subsequently passed across all packages in coordinator qualification; exact commands/build resources are recorded in the final transfer report.

A single cold 256 KiB range from a full chunk requires approximately **400×** its requested payload in returned ciphertext. Four such ranges without shared cache require four adapter Gets, approximately 400 MiB returned ciphertext and, without internal retries, 804 controlled file RPCs plus four message resolutions. A shared cold cache reduces that sequence to one Get, approximately 100 MiB and 201 controlled file RPCs plus one resolution. The sequence serves 1 MiB total, so amplification remains approximately 100× even with cache. These protocol-derived counts are not a live wire count.

## Fresh synthetic before/after comparison

Linux ARM64, isolated offline container, 3 GiB memory/swap, two CPUs, `GOGC=40`, `GOMEMLIMIT=1536MiB`. Three independent one-iteration samples per mode. Exact commands, resource controls, raw samples and security checks are in [cache qualification](CACHE_QUALIFICATION.md).

| Workload | Disabled median | Enabled median | Gets, disabled → enabled | Returned ciphertext, disabled → enabled | Interpretation |
| --- | ---: | ---: | ---: | ---: | --- |
| Four same-chunk ranges, 20 ms injected delay/Get | 401.337 ms | 368.660 ms | 4 → 1 | ~400 → ~100 MiB | 75% fewer fetches, 8.14% lower synthetic median elapsed |
| Four ranges across three chunks `0,1,2,0`, no injected delay | 311.244 ms | 542.807 ms | 4 → 4 | ~400 → ~400 MiB | Cold eviction sequence adds 74.40% elapsed cost; no remote saving |

Both workloads allocate approximately 419.5 MB cumulatively per sequence. No allocation improvement is demonstrated. Different injected latency means timings must not be compared across workload rows. Neither benchmark establishes real Telegram upload/download throughput or production preview latency.

## Current-source live comparison

Coordinator-only opt-in harness: `internal/storage/transfer_live_test.go`, `TestLiveTransferQualification`. Default tests skip live access. Running it requires explicit generated-fixture confirmation, a fresh private state directory and an authorized isolated Telegram session. Original authorization owner must be stopped/locked before a copied session is used; clones must stop before original resumes. No credentials or raw provider messages belong in public reports.

The harness covers a `100 MiB + 17` multipart fixture, queued next-chunk handoff, full remote verification/download, canceled worker/recreated engine, two concurrent 1 MiB staged uploads and overlapping/cross-chunk ranges. Cancellation/reconnection is not equivalent to an OS process crash; separate coordinator crash tests qualify that boundary. It records whether the canceled save actually required recovery rather than claiming recovery when acknowledgement won the race.

Current authorized execution **PASS**, **189.64 s**, using fresh supported QR authorization. Runtime: **768 MiB hard memory/no swap, 512 MiB Go soft limit, two CPUs**. Ignored coordinator evidence: `output/milestone-a/live-authorized/live-test.log` and `output/milestone-a/live-authorized/result.json`. Original authorization owner was stopped/locked before copied authorization use; source unchanged and copies removed are confirmed. Remote deletions: **0**. Observed retry count: **0**; this is telemetry, not proof that no transport-level retransmission occurred.

An earlier revoked-session attempt stopped at `telegram-connect` with sanitized classification `session_invalid`, before fixture transfer, after 0.15 s. Evidence remains in `output/milestone-a/live-current/{live-test.log,result.json}`. It supplied no throughput/integrity result. Fresh supported authentication resolved that blocker.

| Measurement | Current result |
| --- | --- |
| Authorized live connection | **PASS**, fresh supported QR authorization |
| Multipart upload, including final remote verification | **PASS**, 104,857,617 bytes (`100 MiB + 17`), 47.374262057 s, **2.110851164 MiB/s** |
| Full verified download | **PASS**, same file, 23.439372378 s, **4.266326530 MiB/s**, SHA-256 equal |
| Sequential and concurrent generated uploads | **PASS**, large file and two concurrently staged 1 MiB files verified; saving worker remains serial |
| Worker interruption/reconnection without file reselection | **PASS**, canceled 1 MiB save required recovery; engine and adapter recreated, persisted parts reused |
| Cross-chunk range | **PASS**, 43 exact bytes across 100 MiB storage boundary; direct storage reader, not HTTP |
| Four overlapping ranges, cache off | **PASS**, 90.893460474 s, four Gets, 419,430,528 returned ciphertext bytes |
| Same ranges, cold cache on | **PASS**, 23.025143760 s, one Get, 104,857,632 returned ciphertext bytes |
| Cold versus warm per-range latency | **PASS**, cache-on cold 22.773743274 s, warm 0.097472105 / 0.075899862 / 0.078015977 s |
| Retry count during live run | **PASS**, observed 0 |
| Actual network wire amplification | **NOT TESTED**; current harness counts returned ciphertext |
| Live PDF/video viewer request patterns | **NOT TESTED**; byte-range storage checks are not live browser preview tests |
| Full-size mixed preview/upload memory and sustained throughput | **NOT TESTED** |
| Live cross-datacenter retrieval | **NOT TESTED** |

The whole live workload recorded **16 adapter Gets / 740,295,219 returned ciphertext bytes** and **6 adapter Puts / 109,052,113 offered ciphertext bytes**. Offered bytes include the canceled attempt; neither counter is network wire volume. Full checksum verification deliberately reads remote content and bypasses the cache.

For the four-range cache pair, fetches/returned ciphertext fall **75%** and total elapsed falls **74.67%**. Cache-off per-range seconds: `22.829963577`, `20.556803989`, `23.083309539`, `24.423365411`; each makes one Get of 104,857,632 bytes. Cache-on per-range seconds: `22.773743274`, `0.097472105`, `0.075899862`, `0.078015977`; Gets are `1,0,0,0`. This is one cache-off-then-cache-on pair, without randomized order or repeated network trials. It verifies repeated-range reuse; it does not establish universal throughput improvement or faster cold reads. Cache-on's cold read remains approximately 22.8 s.

The direct-storage run's 95 resource samples reached maximum **311.1 MiB working set / 64.50% CPU**, with no OOM. Sampling does not establish lifetime RSS peak, CPU time or full-size combined-load ceilings.

## Exact production-image HTTP cross-check

Coordinator also completed a separate **PASS** against the current production image using a 5 MiB generated file. Browser-sized parts were 4 MiB + 1 MiB; HTTP acceptance was followed by background queue acceptance. After the API reported `storing`, a controlled SIGKILL produced exit 137 with no OOM. Restart automatically saved persisted parts without file reselection; completion/full SHA-256 and actual HTTP 206 passed. Container recreation retained data/session access and repeated download SHA-256/206 passed; shutdown was clean. Source authorization remained unchanged and copied authorization was removed.

| Metric | Measured result |
| --- | --- |
| Start-to-ready, three starts | 0.605 / 0.373 / 0.603 s |
| Idle sampled working set | 8.941 MiB |
| 5 MiB client acceptance | 0.0492 s |
| Recovery plus final verification | 3.9498 s |
| Download after SIGKILL recovery | 1.60346 s, 3.11825 MiB/s |
| Cold download after container recreation | 3.54419 s, 1.41076 MiB/s |
| Maximum sampled resources | 36.88 MiB working set / 9.99% CPU |

This image check covers HTTP/state persistence with one 5 MiB storage chunk. It does not qualify 100 MiB image memory, a remote RPC byte offset at termination, or a full-size mixed workload. Observed `storing` is the established crash boundary; it is not proof of a specific in-flight MTProto byte. Image identity, commands and detailed crash assertions belong to coordinator's final report. These small-file timings must not be compared directly with the 100 MiB storage-harness throughput.

## Historical live context

These reported measurements predate the current 100 MiB storage geometry/background pipeline/cache. They used 4 MiB application chunks, different source and sequential network conditions. Preserve them as context, not a current before/after experiment.

| Historical 100 MiB run | Upload (MiB/s) | Download (MiB/s) | Source |
| --- | ---: | ---: | --- |
| Phase 3 baseline | 1.121734 | 3.952927 | `docs/PHASE3_PERFORMANCE_REPORT.md` |
| Phase 3 optimized | 1.354059 | 4.040140 | Same report; 25 storage chunks |
| Phase 6 exact historical container | 1.626430 | 4.212658 | `docs/PHASE6_FINAL_REPORT.md` |

Phase 6 recorded 61.484365 s total upload, including 24.592902 s final verification, and 23.737983 s download. Phase 3 baseline/optimized single-run comparisons already disclose network variability. A live 512 MiB Telegram workload was not verified in those reports. No percentage relative to those runs should be attributed to this milestone without comparable current measurements.

## Routing, references and reconnection review

Installed gotd/td: **v0.162.0**. Reviewed source paths: `telegram/downloader/{builder,stream}.go`, `telegram/invoke.go`, `telegram/sub_conns.go`, `telegram/pool.go` and Harbor `internal/storage/telegram.go`.

- Downloader `Stream` is sequential with a bounded handoff channel. Existing 512 KiB MTProto part size remains unchanged; no speculative parallel downloader was added.
- Harbor resolves the stored channel/message into a fresh document before reading. Each outer retry rebuilds the document/file reference, allowing expired-reference recovery without persisting stale provider references as the sole locator.
- Same-DC documents use the existing primary invoker. Other-DC documents select a media invoker using the fresh `Document.DCID`; Harbor closes that media invoker after use.
- gotd native primary invocation handles `FILE_MIGRATE`/`STATS_MIGRATE` through authorized sub-connections. Harbor's selected media-invoker migration fallback retries once through that native path while preserving file location, offset and context.
- Native authorized sub-connections are reused by DC. `MediaOnly` does not establish an equivalent Harbor-owned per-DC media-pool cache. No new pool lifetime/concurrency policy was justified without measured legitimate cross-DC workload.
- Controlled tests cover same/other-DC selection, migrated partial reads, missing/invalid documents, canceled calls, terminal errors and refreshed references. Mock migration coverage is not live cross-DC qualification.

**Live cross-datacenter result: NOT TESTED.** No account stress, deliberate flood trigger or unsupported routing manipulation is warranted to obtain it.

## Interpretation and next measurements

Existing bounded concurrency remains unchanged. Current live evidence supports retaining the cache for repeated same-chunk reads; synthetic eviction results expose a local cost when the working set exceeds its two entries. Neither supports raising cache/worker limits or advertising universal speedup. Historical Phase 3/6 numbers remain context, not a comparable control group.

Next measurements: representative PDF/video HTTP patterns, full-size combined read/write memory, sustained throughput and legitimate live cross-DC retrieval when safely available. Full Telegram-tagged race coverage passed; earlier compiler memory exhaustion remains a build-resource failure, not an application race. This scoped PASS supplies current live performance evidence to the coordinator's overall release decision; it does not replace full milestone acceptance.

## Milestone B current-source live requalification

Date: 2026-10-22. Direct current-source Telegram test **PASS**, controller
173.859 s; compiled test SHA-256
`80b0d17cd23c8ebd5f3714c1832326048be3a4a63b1b335a1160e81ff0fa40d8`.
100 MiB + 17 bytes, 4 MiB client parts, one saving worker; 768 MiB hard/no swap,
512 MiB Go soft, two CPUs. Fresh isolated metadata/key and copied authorized
session. Source lock held until target stopped and copied authorization removed;
source authorization unchanged. Zero retries and zero remote deletions.

| Measurement | Milestone A | Milestone B |
| --- | ---: | ---: |
| Upload including final remote verification | 47.374 s / 2.111 MiB/s | 38.078 s / 2.626 MiB/s |
| Full verified download | 23.439 s / 4.266 MiB/s | 19.119 s / 5.230 MiB/s |
| Four uncached overlapping ranges | 90.893 s / 4 Gets | 89.278 s / 4 Gets |
| Four ranges, cache initially empty | 23.025 s / 1 Get | 22.008 s / 1 Get |
| First cache-enabled range | 22.774 s | 21.764 s |
| Subsequent cache-enabled ranges | 97.472 / 75.900 / 78.016 ms | 87.417 / 77.087 / 79.248 ms |

This is one run per milestone, same fixture/account/hardware class but different
source and uncontrolled network/provider scheduling. Observed throughput is not
a causal or universal improvement claim. Fixed-order cache-off then cache-on
comparison reduces actual returned ciphertext 419,430,528 → 104,857,632 bytes
and adapter Gets 4 → 1 (three reused hits out of four logical reads). Each warm
hit still authenticates/decrypts/hashes the complete chunk. A cold 256 KiB read
therefore still has roughly 400× returned-ciphertext amplification.

Full-file SHA-256, exact 43-byte cross-chunk range, two concurrent small uploads,
interrupted-worker recovery and engine/session recreation without source
reselection pass. Total accounting: 16 Gets / 740,295,219 returned ciphertext
bytes; six Puts / 109,052,113 offered ciphertext bytes including canceled
attempts. These are adapter counters, **not native RPC counts or wire traffic**.
Live cross-DC and live browser PDF/video cold/seek latency remain **NOT TESTED**.
Local browser media results belong in [preview qualification](PREVIEW_PERFORMANCE_REPORT.md).
Ignored logs/result/metrics: `output/milestone-b/live-current/`.
