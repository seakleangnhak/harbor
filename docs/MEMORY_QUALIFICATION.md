# Memory qualification — Milestone B

Date: 2026-10-22. Maintained sanitized export; comparison baseline
`b6ae39e2d4bf0846448a916c6890e38b4d590a6a` (`1.3.0-dev.1`).
Status: **PASS for the bounded synthetic workload below; live Telegram sustained
memory headroom NOT TESTED**. Overall Milestone B acceptance remains separate.
No public internet qualification follows from these measurements.

## Targeted change

`transportReader` reuses one private encrypted-part/decryption buffer after its
preceding plaintext has been completely consumed. Previously each 4 MiB staged
part allocated another buffer: 25 buffers per 100 MiB storage chunk.
`LocalStore.getBoundedInto` retains regular-file/reference validation, bounded
reading, overflow rejection and context checks. Ordinary `Get` and duplicate-part
validation still allocate independent buffers.

Each reused part checks envelope length/magic, authenticates AES-256-GCM with
upload/chunk/part identity and verifies plaintext SHA-256. No plaintext disk
cache, format migration, dependency, worker increase or queue-size change.
Two pending chunks/upload, four globally, one remote saving worker, two engine
slots and two part-admission slots remain unchanged. Reader/cache optimizations
are described in [Preview performance](PREVIEW_PERFORMANCE_REPORT.md).

`TestTransportReadersKeepIndependentPartData` verifies caller-buffer ownership,
two independent readers and rejection/removal of a corrupt next encrypted part.
Existing transport tests cover nonces, retries, short/oversize input,
missing/swapped stages, interruption and completion integrity. Coordinator's
full default/tagged unit and race suites passed; opt-in resource results below
come from separate non-race binaries under the runtime limit.

## Reproduction and isolation

```sh
python3 scripts/milestone_b_memory_check.py baseline --run 2
python3 scripts/milestone_b_memory_check.py after --run 2
```

Both commands completed successfully. The trusted host controller verifies the
frozen baseline against Git and overlays the byte-identical new test harness.
Source/binary SHA-256 manifests, raw 200 ms samples, runtime logs and benchmark
logs remain in ignored `output/milestone-b/memory/{baseline-2,after-2}/`.
Only internal Go source and module manifests enter generated compiler contexts;
`.env`, real sessions, keys, owner databases and recovery archives are excluded.
Generated runtime containers and fixture volumes are removed in `finally`.

| Setting | Actual configuration |
| --- | --- |
| Host/VM | ARM64 host; dedicated Colima VM, 8 GiB RAM, three CPUs, 24 GiB task disk |
| Go | Go 1.26.10 linux/arm64, `CGO_ENABLED=1`, `-buildvcs=false -trimpath` |
| Compilation | 6.5 GiB hard/no swap, `GOMEMLIMIT=4608MiB`, `GOGC=30`, two CPUs, `-p=1` |
| Measured runtime | 768 MiB hard/no swap, `GOMEMLIMIT=512MiB`, two CPUs |
| Isolation | No network, read-only root/source/toolchain/binaries, UID/GID 65532, all capabilities dropped, `no-new-privileges`, 256 PIDs |
| Scratch | Fresh disk-backed generated volume; retained fixture bytes asserted below 4 GiB; 1 GiB per-file ceiling |
| Compiler prerequisite | Locally retained `harbor:milestone-a-reliability-final`; ID `sha256:ff5fa6b047dbb32cde879ea1fa2e735e8b11841dd770bf7f043db8fb6559800e`; offline module/cache inputs |

Compiler resources are separate from application runtime. A compiler OOM is not
an executed race-test failure. Normal unit suites skip the opt-in resource test.
The measured workload executes actual API, background queue, encrypted staging,
copy jobs, full HTTP downloads and range reads against a delayed local ciphertext
adapter. In-process streaming clients, generated input/hashing and sampler count
toward RSS, Go heap and CPU. Results are not exact production-server RSS or a
live Telegram memory benchmark.

## Staged-reader allocation comparison

`BenchmarkTransportReader100MiB` reads all 25 authenticated staged parts into
`io.Discard`; setup is excluded. Three samples per mode, three iterations/sample.
Timing includes local disk reads, decryption and hashing, not Telegram saving.

| Mode | Samples, ms/100 MiB | Median bytes/op | Allocation count/op |
| --- | --- | ---: | --- |
| Baseline | 90.305, 96.023, 89.706 | 105,094,562 | 388, 382, 380 |
| Buffer reuse | 72.031, 79.299, 76.951 | 4,231,314 | 329, 333, 330 |

Median cumulative allocation falls **95.97%**, approximately 96 MiB less per
100 MiB staged chunk. Median synthetic staged-reader time falls **14.79%** in
this pair. Cumulative allocation is not peak resident memory. Initial narrower
runs measured 7.31% timing reduction; disk/cache state, scheduling and small
sample count limit timing inference. No universal or Telegram throughput claim.

## Final sustained resource pair

Each mode sustains two uploading clients, one full HTTP downloader and two range
clients for 65 s, starts two 300 MiB durable folder copies, restarts queued work
and fully verifies source/copy hashes. Exact workload and latency scope:
[Mixed workloads](MIXED_WORKLOAD_REPORT.md). One runtime sample per mode.

| Value | Baseline | Current source |
| --- | ---: | ---: |
| Measured test duration | 89.54 s | 89.10 s |
| Completed mixed 100 MiB uploads | 42 | 60 |
| Completed mixed 100 MiB HTTP downloads | 85 | 123 |
| Verified 256 KiB ranges, including post-restart | 317 | 366 |
| Peak sampled process RSS | 521.83 MiB | 495.47 MiB |
| Peak process lifetime RSS (`VmHWM`) | 521.83 MiB | 520.05 MiB |
| Peak cgroup anonymous memory | 510.15 MiB | 483.73 MiB |
| Peak cgroup file cache | 530.32 MiB | 531.06 MiB |
| Peak Go heap allocation (`HeapAlloc`) | 458.53 MiB | 503.47 MiB |
| Peak live heap after last GC (`/gc/heap/live:bytes`) | 212.89 MiB | 208.84 MiB |
| Peak reserved Go heap (`HeapSys`) | 835.19 MiB | 527.25 MiB |
| Peak Go system memory (`Sys`) | 842.28 MiB | 533.88 MiB |
| Maximum sampled cgroup memory | 768.004 MiB | 768.027 MiB |
| Kernel cgroup `memory.peak` | 770.50 MiB | 770.00 MiB |
| `memory.max` events | 195,556 | 248,698 |
| Cgroup OOM / OOM-kill events | 0 / 0 | 0 / 0 |
| Whole-run average CPU, 100% = one CPU | 122.50% | 134.87% |
| Peak short-sample CPU | 216.08% | 215.07% |
| Mixed-phase average CPU | 134.29% | 154.73% |
| Peak / settled goroutines | 72 / 11 | 84 / 11 |
| Observed peak pending chunks / staged part rows | 2 / 50 | 2 / 50 |
| Maximum simultaneous remote saving workers | 1 | 1 |
| Peak encrypted staging bytes | 200.002 MiB | 200.002 MiB |
| Peak ciphertext-cache file bytes | 200.000 MiB | 200.000 MiB |
| Peak retained fixture disk | 1,576.09 MiB | 1,604.09 MiB |
| Retained fixture disk after completion | 1,404.09 MiB | 1,404.09 MiB |
| Final pending chunks / staged parts | 0 / 0 | 0 / 0 |
| Settled process RSS | 117.35 MiB | 147.30 MiB |
| Settled live Go heap after explicit GC | 0.551 MiB | 0.543 MiB |

Per-phase peak sampled RSS, baseline/current: sequential uploads
297.65/302.49 MiB; mixed transfers/copies/ranges 521.83/493.77 MiB;
post-restart ranges 459.73/423.21 MiB. Initial pre-seed RSS is approximately
13.3 MiB in both tests, a single fixture-process sample rather than production
idle-service qualification. Final forced GC and a one-second pause show retained
objects settling; this is not a multi-hour leak test.

## Accounting and practical limits

The container reaches its 768 MiB memory budget. Hundreds of thousands of
`memory.max` events show active reclaim pressure despite zero OOM events.
Disk-backed file cache explains much of the difference from process RSS, but
there is **no demonstrated spare cgroup-memory margin**. Do not increase engine,
queue or preview concurrency from this result. Kernel peak/current may briefly
exceed the configured limit by small accounting amounts. Short CPU samples may
exceed two-CPU quota due to accounting/scheduling windows.

`HeapSys` is reserved space, not resident memory; `HeapAlloc` contains uncollected
garbage. `HeapLive` describes the most recently completed GC and can miss live
objects created afterward. Peak columns occur at different instants and cannot
be added. Lifetime RSS captures a roughly 520 MiB spike the 200 ms current-source
RSS sampler misses. Similar lifetime peaks and different completed-operation
counts do not prove a general peak-RAM reduction. Current source allocates
70.11 GiB cumulatively over more operations versus baseline 60.42 GiB; these
totals cannot be compared as equal-work per-operation allocations.

Queue/cache/disk assertions pass, but sampling alone cannot catch every transient
queue state; existing queue/cache concurrency tests establish admission bounds.
The cache allows two entries and 256 MiB including reservations by implementation;
this workload records actual cache-file bytes, not a direct reservation gauge.
The 4 GiB fixture assertion and 1 GiB per-file ceiling are test constraints, not a
production disk quota. Retained library/SQLite/WAL growth is distinct from cache
and staging. Generated upload nodes are recycled through real local Trash/delete
APIs to bound this fixture's retained disk independently of duration.

Two engine slots bound active chunk buffers, not all Go, Telegram, clients or
kernel page cache. Sustained live Telegram mixed-load memory, application RSS
without in-process clients, PDF/video decoding, other hardware, 24-hour soaks and
physical Safari/iOS remain separately unverified. No concurrency or production
resource-limit changes are proposed from this single pair.
