# Ciphertext cache qualification — Milestone A

Milestone B retains this cache policy and adds the authenticated boundary-replay
fix, staged-buffer reuse, sustained eviction/mixed tests and offline maintenance.
See [current preview results](PREVIEW_PERFORMANCE_REPORT.md) and
[Milestone B decision](MILESTONE_B_FINAL_REPORT.md). Numbers below retain their
Milestone A source, timing and workload scope; they are not silently relabeled.

Date: 2026-10-22. Scope: maintained public-export source, preserved checkpoint `59aa6d8` plus Milestone A changes. Status: **PASS for cache correctness and the bounded current-source live comparison below**. Focused cache races and the full Telegram-tagged race suite pass. Release-candidate acceptance remains coordinator-owned. Historical Telegram runs do not qualify this cache.

## Behavior and security boundaries

The cache is an optional Telegram read optimization. Local-adapter reads bypass it. It stores encrypted chunk envelopes in private `<data>/read-cache`; it never stores plaintext. Existing 100 MiB storage chunks and 4 MiB browser parts remain unchanged.

| Property | Implementation and evidence |
| --- | --- |
| Disk bound | Two entries and 256 MiB, including reservations for in-flight writes. `TestReadCacheEvictionReservationsAndInvalidation` asserts both bounds. These limits describe cache disk state, not total process RAM. |
| Authentication | Every hit authenticates AES-256-GCM, chunk identity, plaintext size and SHA-256 through the existing decrypt path. Wrong key, ciphertext corruption and substituted manifest fields fail closed. |
| Publication | A new cache entry becomes visible only after authenticated decryption. Failed or invalidated fills cannot publish a usable entry. |
| Buffer ownership | Each reader receives its own decrypted buffer; a reader cannot mutate another reader's plaintext through the cache. |
| Simultaneous misses | Same-chunk requests share an in-flight fetch. A canceled leader does not permanently cancel another reader; the remaining reader can retry. |
| Corruption | A bad cache entry is removed and fetched again from remote. Remote corruption/missing content is returned as failure, never accepted as a valid hit. |
| Eviction | Least-recently-used unpinned entry; pinned entries and reserved writes count toward admission. No room means uncached read, not unbounded disk growth. |
| Cache I/O failure | Falls back to direct adapter reads. Failed cache cleanup disables further admissions. No plaintext staging fallback. |
| Restart | Recognized disposable cache files are removed on initialization. Cache is deliberately cold after restart and omitted from recovery archives. Unrecognized files/symlinks cause rejection instead of deletion outside the cache contract. |
| Deletion | Remote-reference invalidation prevents a late fill from resurrecting the deleted entry. |
| Final verification | File completion and integrity auditing bypass the cache, so a warm entry cannot conceal missing or corrupted remote content during verification. |

HTTP owner-session checks remain outside this internal cache. Cache tests establish cryptographic verification; they do not replace API authentication/authorization qualification.

The previously reproduced first-miss deadlock fix is present in checkpoint `59aa6d8`: the missing-entry branch releases `c.mu` before taking it again. `TestReadCacheReusesCiphertextAcrossReadersAndKeepsPrivateBuffers` bounds the first authenticated fetch to five seconds. No additional production cache rewrite was justified in this milestone.

## Fresh offline verification

Evidence: ignored `output/milestone-a/cache/focused-fixed.log`. Tests ran in a Linux ARM64 container using `harbor:state-backend`, with no network, read-only root/source/tooling, UID 65532, all capabilities dropped and `no-new-privileges`. Limits: 3 GiB memory/swap, two CPUs, 256 PIDs, 2 GiB writable scratch tmpfs and 1 GiB per-file limit. Build-only dependency/cache inputs were mounted read-only; runtime fixtures were generated in scratch.

Environment: `env -i` with explicit `PATH`, `HOME`, `TMPDIR`, `GOMODCACHE=/go/pkg/mod`, `GOPATH=/scratch/go`, `GOCACHE=/scratch/go-cache`, `GOTOOLCHAIN=local`, `GOPROXY=off`, `GOSUMDB=off`, `GOGC=40`, `GOMEMLIMIT=1536MiB` and `DRIVE_ENV_FILE=-`. Each command also had an eight-minute outer timeout.

```sh
go test -count=1 -p 1 -timeout 90s ./internal/storage -run '^TestReadCache' -v
go test -count=1 -p 1 -race -timeout 90s ./internal/storage -run '^TestReadCache' -v
go test -p 1 ./internal/storage -run '^$' \
  -bench '^BenchmarkReadCache(Range|Eviction)Sequence100MiB$' \
  -benchtime=1x -count=3 -benchmem
```

| Check | Result |
| --- | --- |
| Focused cache tests | **PASS**, package time 0.032 s |
| Focused cache race tests | **PASS**, package time 1.552 s; default build tags |
| Both benchmarks, three samples/mode | **PASS**, package time 11.140 s |
| Eight readers, 96 short ranges across three chunks, concurrent independent upload/finalization | **PASS** under default and race checks; exact bytes, cache reservations, released slots and completed fills asserted |
| Full Telegram-tagged race suite | **PASS**, coordinator completed all packages in the adequately provisioned build environment; distinct from the focused default-tag run above |

An initial sandbox invocation failed package setup because its clean environment omitted `GOMODCACHE`. No source tests ran in that attempt. Corrected invocation above passed; failed setup evidence remains in ignored `output/milestone-a/cache/focused.log`.

## Synthetic repeated-range measurements

`BenchmarkReadCacheRangeSequence100MiB` reads four 256 KiB ranges from the same 100 MiB chunk. A fresh reader is opened per range; each benchmark iteration starts cold. The controlled adapter adds 20 ms per `Get`. This is a local encrypted-file fixture, **not live Telegram**.

| Cache | Elapsed samples (ms/sequence) | Median (ms) | Adapter Gets | Returned ciphertext (MiB) | Cumulative allocation (bytes/sequence) | Allocations/sequence |
| --- | --- | ---: | ---: | ---: | --- | --- |
| Disabled | 410.757, 397.397, 401.337 | 401.337 | 4 | 400 plus envelopes | 419,500,904; 419,497,832; 419,497,528 | 578; 568; 571 |
| Enabled | 368.203, 375.215, 368.660 | 368.660 | 1 | 100 plus envelope | 419,502,016; 419,502,480; 419,504,128 | 621; 623; 630 |

Remote fetch count and returned ciphertext fall 75%. Median synthetic elapsed time falls 8.14%. Allocated bytes remain approximately 419.5 MB per sequence; cache does not remove full-chunk authentication/decryption or per-reader plaintext buffers. `B/op` is cumulative allocation, not peak RAM. These elapsed numbers cannot establish Telegram throughput or a sustained preview speedup.

## Synthetic eviction measurements

`BenchmarkReadCacheEvictionSequence100MiB` uses three distinct 100 MiB chunks and seek sequence `0,1,2,0`, reading 256 KiB each time. This exceeds the two-entry cache. Unlike the preceding benchmark, this adapter has **no injected delay**; compare modes within this workload only.

| Cache | Elapsed samples (ms/sequence) | Median (ms) | Adapter Gets | Returned ciphertext (MiB) | Cumulative allocation (bytes/sequence) | Allocations/sequence |
| --- | --- | ---: | ---: | ---: | --- | --- |
| Disabled | 311.244, 315.774, 311.158 | 311.244 | 4 | 400 plus envelopes | 419,501,368; 419,500,104; 419,500,504 | 674; 662; 668 |
| Enabled | 542.807, 514.364, 543.817 | 542.807 | 4 | 400 plus envelopes | 419,515,688; 419,514,552; 419,513,704 | 819; 820; 813 |

First-pass cache thrashing provides no fetch reduction and adds 74.40% median elapsed cost from local fills/evictions. Repeated benchmark iterations retain the final chunk-zero entry: expected total Gets become `3*N+1` rather than disabled `4*N`; the reported one-iteration samples intentionally measure a cold sequence. Do not generalize either result to all preview workloads.

One `docker stats` observation during the benchmark showed 989.1 MiB working set and 100.87% CPU under the 3 GiB limit. This single sample is **not a peak measurement** or a mixed-workload memory qualification.

## Current live qualification

Coordinator completed `TestLiveTransferQualification` against actual Telegram using fresh authorization through the supported secure QR flow. Test duration: **189.64 s**. Runtime limits: **768 MiB hard memory, no swap, 512 MiB Go soft limit, two CPUs**. Ignored evidence: `output/milestone-a/live-authorized/live-test.log` and `output/milestone-a/live-authorized/result.json`. Original authorization owner was stopped/locked before its isolated copy was used; copied authorization was removed and source authorization remained unchanged. No Telegram messages were deleted.

An earlier attempt stopped at `telegram-connect` with sanitized classification `session_invalid`, before any transfer, after 0.15 s. Its evidence remains in `output/milestone-a/live-current/{live-test.log,result.json}`. That revoked-session attempt supplied no throughput/integrity result; fresh supported authentication resolved the blocker. No secrets or QR images belong in public evidence.

The live pair reads four overlapping 256 KiB ranges at offsets `0`, `128 KiB`, `256 KiB`, `0` from the same generated 100 MiB Telegram chunk. Each range opens a fresh reader; exact plaintext bytes are checked. Cache-off runs first, then cache-on starts with an empty local cache. Counters below measure adapter calls/returned ciphertext, not network wire traffic or native internal RPCs.

| Mode | Total elapsed (s) | Adapter Gets | Returned ciphertext (bytes) | Cache hits after first fetch |
| --- | ---: | ---: | ---: | ---: |
| Disabled | 90.893460474 | 4 | 419,430,528 | 0 of 3 |
| Enabled, initially cold | 23.025143760 | 1 | 104,857,632 | 3 of 3 |

| Range | Offset | Cache-off elapsed (s) | Cache-on elapsed (s) | Gets, off/on | Returned bytes, off/on |
| --- | ---: | ---: | ---: | --- | --- |
| First | 0 | 22.829963577 | 22.773743274 | 1 / 1 | 104,857,632 / 104,857,632 |
| Second | 128 KiB | 20.556803989 | 0.097472105 | 1 / 0 | 104,857,632 / 0 |
| Third | 256 KiB | 23.083309539 | 0.075899862 | 1 / 0 | 104,857,632 / 0 |
| Fourth | 0 | 24.423365411 | 0.078015977 | 1 / 0 | 104,857,632 / 0 |

This pair demonstrates **75% fewer adapter fetches/returned ciphertext bytes** and **74.67% less total elapsed time**. Cold first reads remain approximately 22.8 s; warm reads take 76–97 ms while still authenticating the complete chunk. It is one fixed-order pair, not randomized repeated trials or evidence of faster Telegram network throughput. The logical range payload is 1 MiB total; returned ciphertext remains approximately 100 MiB even with cache.

| Scenario | Current result |
| --- | --- |
| Authorized live connection | **PASS**, fresh supported QR authorization; source unchanged/copies removed |
| Live cold first range | **PASS**, exact bytes; 22.773743274 s with initially empty cache |
| Live repeated/overlapping ranges | **PASS**, four Gets reduced to one; every range byte-checked |
| Live full-download checksum and range equality | **PASS**, `100 MiB + 17` file SHA-256 and 43-byte cross-chunk range verified |
| Live interrupted worker and engine/adapter recreation | **PASS**, canceled 1 MiB save actually required recovery and completed without reselecting parts; distinct from OS-crash test |
| Exact production-image HTTP/recreation check | **PASS**, separate 5 MiB fixture, full SHA-256 and actual HTTP 206 before/after recreation; not a 100 MiB production-image memory test |
| Live PDF viewer request pattern | **NOT TESTED**; byte-range tests are not a PDF browser run |
| Live video request pattern | **NOT TESTED**; byte-range tests are not a video player run |
| Live mixed preview/upload workload at deployment memory budget | **NOT TESTED** |
| Live cross-datacenter retrieval | **NOT TESTED** |

During the direct-storage live test, 95 resource samples had maximum **311.1 MiB working set** and **64.50% CPU**; no OOM occurred under the stated limit. These are sampled container values, not lifetime RSS peak or sustained CPU utilization. The separate 5 MiB production-image HTTP check sampled 8.941 MiB idle, maximum 36.88 MiB working set and 9.99% CPU. It cannot establish full-size combined-load memory or CPU efficiency. Cache disk reservations/eviction correctness remain covered by controlled tests; no live three-chunk eviction timing was measured.

## Decision and remaining limits

Keep existing two-entry/256 MiB bound; no measurements justify increasing it. Correctness tests and this live overlapping-range pair support retaining the cache as a disposable optimization. Three-chunk thrashing exposes a measurable cost; do not advertise universal preview acceleration. This scoped PASS does not decide overall v1.3.0 acceptance or establish live PDF/video, cross-DC or full-size mixed-load coverage.

Cold small-range requests still require fetching and authenticating a whole 100 MiB encrypted chunk. The cache cannot reduce first-read granularity, first-byte network cost, per-reader full-chunk allocations or Telegram durability risk. A restart intentionally discards warm entries. Further tuning requires representative workload measurements; a negative result should favor simplifying/removing the optimization over adding speculative policy.

See [Telegram performance comparison](TELEGRAM_PERFORMANCE_COMPARISON.md) for adapter amplification, protocol routing review and historical context.
