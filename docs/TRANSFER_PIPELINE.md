# Transfer pipeline qualification

Status: **PARTIAL — fresh synthetic qualification passes; current live Telegram and process/resource gaps remain**. Scope: current source only, after v1.2.1. No release or deployment.

## Bottlenecks found

- Browser awaited each 100 MiB commit before sending the next 4 MiB part. The upload mutex covered remote I/O and status allowed only one pending chunk, so frontend concurrency alone could not fix the stall.
- Separate PDF/video Range requests downloaded the same 100 MiB encrypted chunk repeatedly. PDF.js also cancels its initial metadata GET after headers; Harbor's integrity preflight has already fetched that first chunk by then.
- Final remote verification held the global API metadata-mutation lock, blocking folder operations during unrelated network I/O.

## Source changes

Keep storage geometry, encrypted envelopes, manifests and final remote verification unchanged. A durable opt-in queue allows one accepted chunk to save while browser parts for the next are received. Limits: two pending chunks per upload, four globally, one backend saving worker. Two separate 4 MiB part buffers keep client acceptance from waiting for large reader/save buffer slots. Queue pressure waits rather than growing without limit. Failed jobs preserve parts and require bounded explicit retry; cancelling persists intent and waits for cleanup. Stale saving jobs resume after process restart.

UI distinguishes bytes sent, accepted and stored. One browser sender yields after a file is fully accepted and queued, so the next file can send while a serial monitor handles prior saving and finalization. Pause stops sending; queued server work continues. Fully accepted uploads can reconcile server saving without reopening the original file. Fully accepted background work offers no pause control. A completed file is published only after all remote chunks and the whole-file digest are verified. Late initial-list responses cannot replace newer transfer progress or resurrect cancelled initial transfers.

The four-chunk staging window bounds active reservations, not crash leftovers. A failure cleaning staged parts after manifest insertion stops the worker/application rather than silently accumulating more files. The committed chunk remains tracked and finalization can retry cleanup after restart. A crash between adapter upload and manifest commit can still orphan remote ciphertext, as before; automatic remote garbage collection remains out of scope.

For Telegram, private encrypted disk read cache holds at most two entries/256 MiB including reserved writes. Local-adapter reads already use local ciphertext and bypass this extra cache. It coalesces same-chunk fetches, verifies AES-GCM and SHA-256 on every read, repairs corrupt cache from remote once, and degrades to direct reads if unavailable. Finalization and integrity auditing bypass it. It is disposable and omitted from recovery archives. Cold preview latency still includes a whole encrypted chunk; this change does not claim faster Telegram network throughput or change encryption granularity.

Metadata publication locks only its short database checks/write. Destination, duplicate-name and completed-storage identity checks repeat after remote verification so concurrent mutations/cancellation remain safe. SQLite status snapshots rely on the existing single-connection database configuration. Shutdown joins the HTTP drain routine and both background workers before closing storage resources.

Before downgrading to an older binary, finish or cancel pending uploads. Older clients/servers understand only one staged chunk and cannot reconcile this queue. Completed file geometry is unchanged. Recovery archives omit pending part/cache files; restored failed chunks require original-file reselection to resend missing parts.

## Verification evidence

Fresh checks on 2026-10-22 use isolated offline containers and generated fixtures. Docker is available again; no host execution bypass, private-state access or deployment change was needed. [Current project state](CURRENT_PROJECT_STATE.md) contains source/image identity, full commands and final tagged-test/vet/benchmark results.

| Check | Result |
| --- | --- |
| Go formatting and `git diff --check` | PASS |
| Focused pipeline/cache/API tests | PASS |
| Full default Go tests and default race tests | PASS, fresh isolated execution |
| Telegram-tagged tests and vet | PASS with production flags and exact-source dependency cache; fresh tagged race remains NOT TESTED after three compiler memory failures |
| Frontend tests/typecheck/build | PASS; 48 tests, pinned Node 24 |
| Chromium/WebKit browser suites | PASS; eight suites, 112 PNGs and 108 scoped axe checkpoints |
| Browser upload/restart/checksum and next-file handoff | PASS; 105,906,299 bytes, two storage chunks, requests at most 4 MiB, injected part/503 retry, graceful process restart/reselection, overlap and both final checksums |
| Controlled blocked-backend overlap, queue limits, cancellation, cleanup failure and durable-state recovery | PASS in Go tests; recovery archive checks included |
| Read-cache cold miss, deduplication, corruption, eviction and cancellation | PASS after audit fixed reproduced cold-miss self-deadlock; first authenticated fetch has five-second regression bound |
| Synthetic cache benchmark | PASS; four ranges require 4 → 1 remote Gets (~400 → 100 MiB); noisy 386.60–388.97 ms versus 362.19–390.62 ms, ~419.5 MB cumulative allocation either way; no live speed claim |
| Exact current ARM64 production image | PASS local-adapter smoke at 768 MiB limit: multipart integrity, auth/onboarding, Range, restart/recreation and clean shutdown |
| Encrypted backup/fresh restore and folder-copy SIGKILL | PASS with exact image executables; copy fixture spans three current-size chunks; distinct from new upload worker |
| Real Telegram current-source performance | NOT TESTED; historical numbers do not qualify this source |
| New upload worker actual SIGKILL during remote save; mixed copy/upload/preview load | NOT TESTED |

Initial focused execution exposed a missing mutex unlock on the first cache miss. Fix is confined to `read_cache.go` with a bounded existing regression. The local adapter bypasses this cache, so browser/image checks alone would not have found it. Source-level cache tests exercise it directly. Tagged-race compiler failures are retained as resource failures, not hidden or interpreted as an application test result.

Before release, qualify the fully accepted queue through actual process termination without reselecting the original file. Measure mixed workloads at deployment memory limits, then run owner-authorized isolated Telegram checksum/cold-warm tests with the exact build. A synthetic cache benchmark cannot establish Telegram upload/download speed. Do not infer current live performance or absence of remote orphan windows from local PASS results.
