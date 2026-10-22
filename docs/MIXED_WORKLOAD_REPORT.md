# Mixed-workload qualification — Milestone B

Date: 2026-10-22. Maintained sanitized export only.
Status: **PASS for the bounded synthetic workload below**. Live Telegram mixed
resource behavior and actual browser rendering require separate evidence.
No original files, Telegram messages/session, encryption key, database or backup
archive is used by these resource tests.

## Reproducible workload

`TestMilestoneBSustainedMemoryBudget` is Linux-only and opt-in through
`HARBOR_MILESTONE_B_MEMORY=1`. Both isolated final commands passed:

```sh
python3 scripts/milestone_b_memory_check.py baseline --run 2
python3 scripts/milestone_b_memory_check.py after --run 2
```

The controller reconstructs baseline `b6ae39e2d4bf0846448a916c6890e38b4d590a6a`
and applies the same new API harness to both modes. Source hashes confirm the
harness is byte-identical. Actual limits, compiler separation, source/binary
manifests and metric definitions: [Memory qualification](MEMORY_QUALIFICATION.md).

1. Create one generated folder containing three 100 MiB files using deterministic
   streamed input and AES-256-GCM storage. No 100 MiB input `[]byte` or buffered
   full HTTP response distorts memory.
2. Complete two sequential 100 MiB uploads through 4 MiB HTTP part requests,
   background queue, remote acknowledgement and full-file verification.
3. For 65 s, run two uploading clients, one streaming full-file HTTP downloader
   and two overlapping range clients. Start two real durable 300 MiB folder
   copies at approximately 0 s and 30 s. Worker/queue limits stay unchanged.
4. Full downloads compare streamed SHA-256; every 256 KiB HTTP 206 range compares
   exact expected bytes. Three distinct source chunks exceed the two-entry cache.
5. After each concurrent upload verifies/publishes, recycle only its freshly
   generated local node through real Trash/permanent-delete APIs. This bounds
   retained fixture growth. Sources, sequential uploads and both copies remain
   intact for final validation. Local fixture adapter cannot delete Telegram data.
6. Stop/join workers, accept all parts and queue a 100 MiB upload, recreate the
   storage engine with cold cache, resume accepted work, verify and publish.
   Already fully queued upload recovery does not require browser source bytes.
7. Run 15 s of post-restart range/cache pressure; fully checksum all three sources
   and six copied files. Assert pending/part rows empty, maximum remote writers
   one, pending chunks <=4, ciphertext cache <=256 MiB and fixture disk <=4 GiB.

The harness asserts actual upload and full-download completions after 60 s.
Context/test/controller timeouts bound deadlock or starvation. Every run uses a
fresh generated disk-backed volume, removed after its container stops.

## Final results

One runtime sample/mode; non-race runtime binaries, 768 MiB hard/no swap,
512 MiB Go soft memory and two CPUs. Raw reports/logs:
`output/milestone-b/memory/{baseline-2,after-2}/` (ignored).

| Result | Baseline | Current source |
| --- | ---: | ---: |
| Test duration | 89.54 s | 89.10 s |
| Continuous mixed upload completions, 100 MiB each | 42 | 60 |
| Last mixed upload completion | 66.39 s | 65.86 s |
| Continuous mixed full HTTP downloads, 100 MiB each | 85 | 123 |
| Last mixed download completion | 65.46 s | 65.28 s |
| Verified 256 KiB ranges, mixed + post-restart | 317 | 366 |
| Completed durable 300 MiB folder copies | 2 | 2 |
| Fully queued upload recovered/published after engine restart | PASS | PASS |
| Source and copied-file SHA-256; HTTP-range exact bytes | PASS | PASS |
| Adapter Get calls | 229 | 287 |
| Adapter returned ciphertext bytes | 24,012,397,728 | 30,094,140,384 |
| Adapter Put calls | 54 | 72 |
| Peak saving writers | 1 | 1 |
| Observed peak queue / global asserted bound | 2 / 4 | 2 / 4 |
| Final pending chunks / staged parts | 0 / 0 | 0 / 0 |
| Cgroup OOM events / OOM kills | 0 / 0 | 0 / 0 |

Injected local adapter delays are 25 ms/Get and 75 ms/Put. Counters are adapter
operations/returned bytes, **not Telegram RPCs or wire traffic**. The workload
mixes cacheable reads, copies and uncached finalization. Different completed
operation counts mean aggregate Gets are not an equal-work cache comparison or
a cache-hit ratio. Current-source results include API preview preflight and
reader-boundary fixes as well as buffer reuse, so the mixed comparison cannot
isolate each individual change.

| Operation | Baseline count; p50 / p95 / max | Current count; p50 / p95 / max |
| --- | --- | --- |
| Upload routine | 45; 2,435.97 / 5,474.88 / 7,758.82 ms | 63; 2,102.37 / 2,830.32 / 3,087.06 ms |
| Full HTTP download | 85; 613.77 / 1,482.12 / 2,078.64 ms | 123; 476.77 / 917.84 / 1,612.82 ms |
| 256 KiB HTTP range | 317; 412.29 / 1,387.59 / 2,219.98 ms | 366; 397.88 / 823.42 / 1,481.33 ms |

Quantiles use sorted observations at index `floor((n-1)*p)`, the lower empirical
quantile, without interpolation. Upload timings include two sequential uploads
and one queue-only upload; the latter excludes post-restart verification.
Separate mixed counts above show sustained activity. Range timings include the
15 s post-restart phase. Copy completion samples are 5,867.34 and 7,625.95 ms
baseline, 4,796.70 and 5,687.27 ms current; n=2 does not establish a meaningful
95th-percentile tail estimate. Latencies include in-process clients, input/hash
generation, CPU contention and local disk/cache behavior. One runtime pair cannot
establish universal, real-network or Telegram speed improvement.

Peak lifetime RSS is 521.83/520.05 MiB; cgroup usage reaches approximately
768 MiB with 195,556/248,698 `memory.max` events. Cache stays 200.000 MiB and
staging 200.002 MiB; fixture-disk peaks 1,576.09/1,604.09 MiB, then settles to
1,404.09 MiB. No OOM, checksum error, deadlock or fixture-bound violation.
This demonstrates completion under reclaim pressure, **not spare memory
headroom**. Full memory, CPU, goroutine and GC measurements appear in the memory
report.

Earlier `baseline-1`/`after-1` results are retained as preliminary evidence only:
nine uploads/one copy finished early while ranges continued. They are not relabeled
as sustained-transfer tests or combined with final-pair quantiles.

## Correctness and security boundaries

The test executes actual owner-session HTTP APIs, persistent SQLite state,
pending worker and copy worker. Accepted/staged bytes do not count as remotely
stored; completion still authenticates remote data and verifies full SHA-256
before visible file publication. Folder copies retain staged hierarchy/atomic
metadata publication. Original completed files stay unchanged.

No persisted plaintext preview or derivative is introduced. Staging/cache stay
encrypted; final remote verification bypasses cache. Staging-buffer reuse is
private per reader and cannot mutate a caller's copied data or another reader's
plaintext. Corrupt next parts fail before returning bytes.

## Evidence limits

| Scenario | Evidence scope |
| --- | --- |
| Sustained uploads + full HTTP downloads + ranges + copies | PASS, final identical-harness pair, one runtime sample/mode |
| Queue/cache/disk assertions and one saving worker | PASS in final pair; concurrency-bound unit tests separately |
| Queue restart with complete accepted parts | PASS, real engine recreation; not automatic recovery for missing browser bytes |
| Full-file/copy/range integrity | PASS in both final runs |
| SIGKILL during saving/preview, disk full, corruption | Separate failure-injection/crash suites owned by coordinator |
| Encrypted backup/fresh restore | Separate recovery suite, not this memory harness |
| Actual PDF render/video decode/seek; browser cancellation | Browser qualification; byte ranges alone do not establish these |
| Live Telegram mixed resource behavior | NOT TESTED by this delayed local-store harness |
| Multi-hour soak, physical power failure, other hardware | NOT TESTED by this bounded run |

No overall Milestone B release decision follows from a successful compiler build
or absence of OOM. Browser, exact Docker image, backup and authorized Telegram
qualification remain separate final-acceptance evidence.
