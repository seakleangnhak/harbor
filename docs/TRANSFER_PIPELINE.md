# Transfer pipeline qualification

Milestone A qualification: see [final report](TRANSFER_ENGINE_FINAL_REPORT.md) and [candidate decision](V1.3_RELEASE_CANDIDATE.md). Scope: maintained source after v1.2.1, development build `1.3.0-dev.1`. No release or deployment.

Milestone B extends this dated baseline with retained authenticated reader buffers,
metadata-only HTTP preflight, reusable private staged-part buffers and exclusive
offline local storage maintenance. Queue geometry, authenticated format and
publication invariants remain unchanged. Fresh sustained mixed workloads, real
browser media, full-disk retry and preview SIGKILL results live in the
[Milestone B final report](MILESTONE_B_FINAL_REPORT.md); use the
[candidate gate](V1.3_RELEASE_CANDIDATE.md) for current build identity.

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

Milestone A reproduced another lock hazard: recording a remote success or failure waited for the next browser request body while holding the worker operation open. Both outcomes now persist without reacquiring that body lock. Queued parts are immutable; the work lock still excludes finalization and cancellation cleanup, and failure writes remain conditional on `state='storing'`. Success and failure regressions deliberately block the next body and require the remote outcome to become durable first.

The persistence boundaries are explicit:

| State | Durable evidence | Recovery and visibility |
| --- | --- | --- |
| Browser part accepted / `receiving` | Encrypted staged part plus committed part row | Accepted bytes survive restart. Missing bytes require the original file; fully supplied parts still require chunk commit. No file published. |
| `queued` | Pending row and all expected encrypted parts | Single worker can restart without browser bytes. Not a remote acknowledgement. |
| `storing` | Persisted attempt count and pending row | Restart requeues within three attempts, otherwise explicit failure. No file published. |
| Remote acknowledged | Adapter reference returned, then manifest SQL transaction | Only committed manifests count as `received_bytes`. Remote/SQL interruption may leave an orphan ciphertext object. |
| Final verification | Remote reads bypass cache and verify every chunk and whole-file SHA-256 | Verification failure prevents completion. `/complete` must be retried after interruption. |
| Published | Verified storage identity rechecked and API node transaction committed | Idempotent completion exposes one file. Existing completed files stay unchanged. |

Before downgrading to an older binary, finish or cancel pending uploads. Older clients/servers understand only one staged chunk and cannot reconcile this queue. Completed file geometry is unchanged. Recovery archives omit pending part/cache files; restored failed chunks require original-file reselection to resend missing parts.

## Verification evidence

Fresh synthetic checks on 2026-10-22 use isolated offline containers and generated fixtures. Coordinator-only live qualification uses a fresh QR-authorized session, serialized through its source lock. No original runtime or deployment changes. [Crash matrix](TRANSFER_CRASH_MATRIX.md), [cache qualification](CACHE_QUALIFICATION.md) and [performance comparison](TELEGRAM_PERFORMANCE_COMPARISON.md) separate controlled and live evidence.

| Check | Result |
| --- | --- |
| Go formatting and `git diff --check` | PASS |
| Focused pipeline/cache/API tests | PASS |
| Full default Go tests and default race tests | PASS, fresh isolated execution |
| Telegram-tagged tests, race and both vet variants | PASS; adequate-memory 8 GiB isolated VM, 6.5 GiB hard/no-swap test container; earlier compiler kills remain resource failures |
| Frontend tests/typecheck/build | PASS; 48 tests, pinned Node 24 |
| Chromium/WebKit browser suites | PASS; eight suites, 112 PNGs and 108 scoped axe checkpoints |
| Browser upload/restart/checksum and next-file handoff | PASS; 105,906,299 bytes, two storage chunks, requests at most 4 MiB, injected part/503 retry, graceful process restart/reselection, overlap and both final checksums |
| Controlled blocked-backend overlap, queue limits, cancellation, cleanup failure and durable-state recovery | PASS in Go tests; recovery archive checks included |
| Read-cache cold miss, deduplication, corruption, eviction and cancellation | PASS after audit fixed reproduced cold-miss self-deadlock; first authenticated fetch has five-second regression bound |
| Synthetic cache benchmark | PASS; repeated ranges 401.337 → 368.660 ms median, Gets 4 → 1; cold three-chunk thrash 311.244 → 542.807 ms, Gets unchanged. ~419.5 MB cumulative allocation either mode; not Telegram speed evidence |
| Exact current ARM64 production image | PASS local-adapter smoke at 768 MiB limit: multipart integrity, auth/onboarding, Range, restart/recreation and clean shutdown |
| Encrypted backup/fresh restore and folder-copy SIGKILL | PASS with exact image executables; copy fixture spans three current-size chunks; distinct from new upload worker |
| Current-source live Telegram | See final report for authorized run and exact-image HTTP results; historical benchmarks do not qualify this source |
| Upload worker process termination | PASS at nine deterministic boundaries with controlled local adapter; live exact-image interruption recorded separately |
| Production-size mixed copy/upload/ranges | PASS under 768 MiB hard limit, one remote writer, two observed pending chunks; cgroup reaches limit, so no memory-headroom claim |

Initial focused execution exposed a missing mutex unlock on the first cache miss. Fix is confined to `read_cache.go` with a bounded existing regression. The local adapter bypasses this cache, so browser/image checks alone would not have found it. Source-level cache tests exercise it directly. All tagged race tests now finish successfully; no race coverage was disabled to work around compiler limits.

The crash matrix qualifies fully accepted queued bytes without file reselection. It also exposes encrypted staging leftovers and remote orphan windows; active queue limits do not bound accumulated leftovers after repeated crashes. Backups omit pending parts/cache, and older binaries cannot reconcile the queue. A synthetic cache benchmark cannot establish Telegram speed; browser byte-range checks are not live PDF/video playback qualification. Read final candidate limits before upgrading.
