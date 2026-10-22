# Transfer engine stabilization — Milestone A

Date: 2026-10-22. Maintained sanitized export only. Build identity:
`1.3.0-dev.1`; prospective v1.3.0 candidate, not a published release.

**HARBOR_TRANSFER_ENGINE = PASS** for this bounded local qualification.
Prospective v1.3.0 candidate is ready for owner review, with limits below.
Published v1.2.1 and the original development repository/runtime remain intact.
No push, tag, registry publication or deployment upgrade is authorized here.

## Preserved state and implementation

Initial maintained HEAD was `9ff97a1`. Branch:
`codex/transfer-engine-stabilization`. Checkpoint `59aa6d8` preserves all
preexisting unreleased transfer/cache/frontend changes and audit work rather
than resetting to published v1.2.1 (`dbbe3d02786b21a4ea3db3721947ab320ed0fe75`).
The original repository's v1.0.0 is untouched.

| Change | Evidence and effect |
| --- | --- |
| Existing durable background queue retained | 100 MiB storage chunks, 4 MiB browser parts, two pending chunks/upload, four globally, one saving worker. No limit increase or adapter rewrite. |
| Cold-cache first-miss deadlock fixed in preserved audit work | Missing mutex unlock repaired; first authenticated fetch has a five-second bounded regression. Local-adapter browser checks would not exercise this Telegram cache. |
| Remote success/failure persistence unblocked | `c99caef` removes reacquisition of the browser-body lock after remote I/O. Slow next-part bodies previously blocked manifest/failure updates. Both reproduced regressions failed at 5.01 s before the fix; fixed and race variants pass. |
| Safety locks retained | Immutable queued parts plus per-upload work lock exclude finalization and cancellation cleanup; cancellation intent can still persist and signal active work. Failure SQL remains conditional on `state='storing'`. No relaxed publication checks. |
| Crash and concurrency qualification added | Nine actual child-process SIGKILL boundaries, concurrent copy/upload, six uploading clients with four-slot admission, duplicate retries, production-geometry mixed-memory test. |
| Cache/protocol/live tooling added | Authenticated-cache workloads, exact 100 MiB RPC accounting, opt-in live adapter test, serialized-authorization control harnesses with cleanup regressions. |
| Honest development identity | Backend/frontend/Docker/local CI use `1.3.0-dev.1`. Build checks accept numbered dev/rc versions; publishing gates still require approved stable releases and exact-commit remote CI. |

No new runtime dependencies, production failure-injection hooks, worker pools,
cache policy, chunk format, encryption scheme or completed-file migration.

## State-machine acceptance

[Transfer pipeline](TRANSFER_PIPELINE.md) defines durable evidence at each
boundary. Browser acknowledgement means an encrypted staged part and SQL row
are committed. `queued` and `storing` never count as remotely stored bytes.
Only committed chunk manifests count as `received_bytes`. Final verification
reads remote storage directly, checks authenticated envelopes, chunk digests
and whole-file SHA-256; API publication rechecks destination/conflicts and
verified storage identity in a short metadata transaction.

[Nine-boundary crash matrix](TRANSFER_CRASH_MATRIX.md): **PASS**, with real
SQLite WAL/FULL and controlled disk-backed remote fixtures. Every case checks
SQLite integrity, no premature visible node, exact recovered manifest/digest,
idempotent completion and unchanged completed sentinel. Fixture: 8 MiB + 17
bytes, 8 MiB storage chunks and 4 MiB browser parts. This is not nine live
Telegram process crashes or a power-loss test.

Recovery is conditional, not universally automatic:

- Fully accepted **queued** bytes restart without the original browser file.
  Interrupted saving retries within the three-attempt restart budget; exhausted
  work becomes explicitly failed and requires retry.
- Accepted **receiving** bytes survive, but require the chunk-commit request.
- Missing/unacknowledged input requires original-file reselection. Browser
  refresh cannot recover unavailable `File` bytes.
- Verification/publication requires `/complete`; restarting workers does not
  itself publish files. Completion retry is idempotent.
- Adapter acknowledgement and SQL commit cannot be atomic. Three controlled
  windows produce a remote orphan candidate; interrupted part transactions can
  leave untracked encrypted staging. No automatic orphan collector is claimed.

Queue tests cover cancellation, missing/corrupt staged data, duplicate commits,
failure persistence, bounded admission and slot release. Cleanup failure after
manifest commit stops processing rather than silently accumulating active work.
Queue bounds do not bound leftovers accumulated across repeated crashes.

## Fresh regression results

All target/test execution uses generated fixtures in isolated non-root
containers. Synthetic execution has no network, read-only source/root,
explicit clean environment and bounded writable scratch. Reviewed host
control scripts orchestrate Docker; production dependency builds receive no
runtime secrets. Live authorization is a separate coordinator-only exception.

| Check | Result and evidence |
| --- | --- |
| `go mod verify` | PASS |
| `go test -count=1 -p 1 -timeout 8m ./...` | PASS, five packages |
| `go test -count=1 -p 1 -race -timeout 8m -v ./...` | PASS |
| `go test -count=1 -p 1 -tags telegram -timeout 8m ./...` | PASS; ordinary tagged tests use doubles, opt-in live test skips |
| `go test -count=1 -p 1 -race -tags telegram -timeout 8m -v ./...` | PASS; storage 18.461 s, API 148.523 s |
| `go vet -p 1 ./...`, `go vet -p 1 -tags telegram ./...` | PASS |
| Strengthened child-race assertion | Four focused tag/race variants and both API vets PASS after test-only change; production Go unchanged |
| `npm test`, `npm run typecheck`, `npm run build` | PASS, 48 frontend tests; Node 24 |
| Chromium / WebKit `browser_check.cjs <engine> all` | PASS: account, preview, transport and large-folder suites; 49.042 / 103.698 s |
| Browser accessibility/visual review | 100 in-suite axe checks plus eight final-view checks PASS; 112 screenshots retained, 12 representative views reviewed, no critical layout defect in tested scope |
| Browser transfer correctness | Retry, refresh/reselection, restart, next-file handoff, accepted/stored progress and full checksums PASS with encrypted local fixtures |
| Large directory | 1,000-folder reload: Chromium 182 ms, WebKit 307 ms; rendering/keyboard checks PASS |
| Exact production Dockerfile image, local adapter | PASS: 104,923,157-byte multipart checksum, actual HTTP Range, authentication/CSRF, onboarding lockout, restart/resume, recreation, non-root permissions, health and clean shutdown |
| Exact-image process backup/restore and copy SIGKILL | PASS with final executables: encrypted fresh restore/checksum/session revocation; 200 MiB + 17-byte, three-chunk copy killed after one committed 100 MiB chunk, recovered without changing original |
| Python tooling / workflow checks | 65 Python tests PASS, harness self-check PASS, workflow policy/actionlint 1.7.12 PASS, README 34 resources/three SVG checks PASS |
| Go formatting / diff hygiene | PASS; repeated before final commit |
| Final source/history secret scan | See final hygiene evidence below; no credentials or private fixture state committed |
| Clean-environment Compose configuration | PASS with empty env file and no inherited runtime variables; published pull default preserved |

Final full Go suite ran in a dedicated **8 GiB / three-CPU / 24 GiB disk**
Colima VM. Test/compile container: **6.5 GiB hard/no swap**, 4.5 GiB Go soft,
two CPUs, `GOGC=30`, `-p 1`, Go **1.26.10 linux/arm64**, `CGO_ENABLED=1`,
`GOFLAGS='-buildvcs=false -trimpath'`. Tagged race compilation succeeded.
Earlier compiler memory kills, including a 3 GiB attempt, are resource
failures rather than application race failures. Race detection was retained.

Full-suite source manifest:
`0a504191fdeb14849a6052486563dcbac3f93a97c25af8f407b622cd66774e09`.
Test-only child-race strengthening manifest:
`6d99f6f4683a81bf6ef88753b688889ef9967aa3828871c5afceca454de7e437`.
All 66 current Go files match the latter. Production sources did not change
between those runs or after the final production-image build.

Production assets: main 561.87 kB / 178.92 kB gzip, PDF 517.24 kB / 156.89 kB
gzip. Existing Vite chunk-size warning remains; no new library was added.
Browser video seeking/MOV decoding is **NOT TESTED** in this fresh offline run;
audio metadata, unsupported-format guidance and image/PDF/text lifecycle pass.
Physical Safari/iOS and live Telegram PDF/video playback remain outside scope.

## Current-source Telegram and Docker evidence

Fresh private QR login and private-channel verification succeeded after the
owner revoked an earlier session. Initial `session_invalid` attempt stopped
before any transfer (0.15 s); it is not a throughput/integrity result. Isolated
setup stopped before its newly authorized session was copied. Each live harness
holds its source lock until the target stops and copied authorization is
removed. Original drive state and revoked original authorization are untouched.

Current-source direct storage test **PASS**, 189.64 s (controller 190.072 s):

| Live measurement | Actual result |
| --- | --- |
| 100 MiB + 17-byte encrypted multipart upload including final remote verification | 47.374262 s, **2.110851 MiB/s** |
| Full verified download | 23.439372 s, **4.266327 MiB/s** |
| Sequential / concurrent fixtures | Multipart followed by interrupted 1 MiB fixture and two concurrent 1 MiB uploads; all completed SHA-256 checks |
| Canceled worker / engine and Telegram recreation | Recovery actually required; persisted parts reused without file reselection |
| Four overlapping 256 KiB ranges, cache disabled | 90.893460 s, four Gets, 419,430,528 ciphertext bytes |
| Same sequence starting with cold cache enabled | 23.025144 s, one Get, 104,857,632 ciphertext bytes |
| Enabled per-range latency | Cold 22.773743 s; warm 97.472 / 75.900 / 78.016 ms |
| Cross-storage-chunk range | 43 bytes checked exactly |
| Retries / remote deletions | 0 / 0 |
| Total adapter accounting | 16 Gets / 740,295,219 returned bytes; six Puts / 109,052,113 offered bytes, including canceled attempts |

These are one bounded run and one fixed-order paired cache comparison, not a
controlled historical throughput experiment. Adapter byte counters exclude wire
framing and internal retry traffic; offered bytes are not accepted remote bytes.
Phase 3 optimized upload/download were 1.354059 / 4.040140 MiB/s with different
source and 4 MiB chunks, so no causal percentage gain is attributed to this
milestone. [Performance comparison](TELEGRAM_PERFORMANCE_COMPARISON.md) retains
raw timings and [cache qualification](CACHE_QUALIFICATION.md) covers costs.

Exact production-image live HTTP/crash test **PASS** with a fresh 5 MiB fixture:

- Two browser parts (4 MiB + 1 MiB) accepted in 49.209 ms; `received_bytes`
  remained zero, and premature `/complete` returned 409.
- Background commit returned 202. API `storing` was observed, then actual
  container SIGKILL returned exit 137 without OOM. This proves process
  interruption during observed saving, not an exact MTProto RPC byte offset.
- Restart recovered accepted input automatically without reselection; no node
  was visible before verified completion. Recovery plus verification 3.949789 s.
- Full download SHA-256 and actual HTTP 206 passed after restart and after
  container recreation with the same protected volume. Downloads: 1.603464 s
  (3.118248 MiB/s) after kill; 3.544189 s (1.410760 MiB/s) after recreation.
  Cache is intentionally cold after recreation.
- Startup-to-ready: 0.604581 / 0.372662 / 0.603173 s. Clean shutdown passed.
  This 5 MiB test qualifies browser multipart/HTTP behavior; the separate
  100 MiB + 17 test above qualifies multiple Telegram storage chunks.

Both harnesses confirmed source authorization unchanged and copied session/temp
authorization removed after target shutdown. Their retained protected fixture
volumes contain only generated data/metadata/keys, with no copied authorization;
no preexisting Telegram messages were deleted. Fresh authorization remains
private and separate from the owner's revoked original session.

Coordinator reproduction uses private configuration outside Git and a fresh
output directory each time. Never run both commands concurrently or supply a
session still owned by another process:

```sh
python3 scripts/transfer_live_check.py \
  --source-env /protected/isolated-authorization/.env \
  --image sha256:ff5fa6b047dbb32cde879ea1fa2e735e8b11841dd770bf7f043db8fb6559800e \
  --context colima-harbor-a --binary-volume harbor-a-race-cache \
  --output output/milestone-a/live-authorized
python3 scripts/milestone_a_docker_live.py \
  --source-env /protected/isolated-authorization/.env \
  --image sha256:7ee9bf35716e1497cdb3c250258a84b0526c8cc8fd2ff76f9e05a7f6df2cc11e \
  --helper-image sha256:654b0f3911ef0263c931f99e8b3811f6724d4514b921befa2200e3a9bf8db8f5 \
  --context desktop-linux --output output/milestone-a/docker-live-authorized
```

These IDs refer to local reviewed artifacts, not images available from a public
registry. Storage test executable SHA-256:
`82f771fb5901a82370b2feea7f27a9c1bf0132134cbf8e6c670cb178a13daf20`.
Final-image drive/recovery executables were also verified in the process
acceptance tooling image before executing:

```sh
python3 scripts/acceptance.py --binary /app/drive
python3 scripts/phase5_copy_check.py --binary /app/drive
```

Those process checks used an explicit clean environment including
`DRIVE_ENV_FILE=-` and `HARBOR_RECOVERY_BINARY=/app/harbor-recovery`.

Immutable **local ARM64 production image/index ID**:
`sha256:7ee9bf35716e1497cdb3c250258a84b0526c8cc8fd2ff76f9e05a7f6df2cc11e`.
Tag: `harbor:milestone-a-dev`, version `1.3.0-dev.1`. This is not a published
registry digest or a multi-platform release. The production image precedes
only the test-only child-race assertion; its production executables match the
qualified implementation. No remote release/image has been created.

## Memory, CPU, disk and cache

Controlled production-size mixed workload **PASS**: 100 MiB + 17 upload,
100 MiB copy, two overlapping 256 KiB ranges, full SHA-256 checks. One remote
writer, two pending chunks observed while blocked, asserted global count ≤4.
Test 2.24 s; 2.428 CPU seconds including fixture/control helpers.

| Resource | Observed measurement |
| --- | ---: |
| Hard / Go soft memory | 768 MiB / 512 MiB, no swap |
| Cgroup lifetime peak, including file cache/helpers | 805,310,464 bytes, approximately hard limit |
| Sampled anonymous maximum | 475.17 MiB |
| Sampled file cache maximum | 259.71 MiB |
| Sampled fixture disk maximum | 300.64 MiB; not a guaranteed lifetime peak |
| OOM kill | None |

This demonstrates bounded execution of the fixture, **not spare production RAM
or sustained full four-slot load**. Anonymous/file maxima need not coincide.
The pre-start shell is not an application idle measurement. Live samples are
reported separately and do not turn Docker working-set samples into RSS peaks.

| Live Docker sampling | Observed value | Scope |
| --- | ---: | --- |
| Current-source 100 MiB storage test | 311.1 MiB maximum working set; 64.50% maximum sampled CPU, 95 samples | Entire test executable/adapter, not lifetime peak RSS or stage-specific upload/download peak |
| Exact-image idle | 8.941 MiB working set, 0.53% CPU | One ready application sample, not a steady-state average |
| Exact-image 5 MiB HTTP/crash workload | 36.88 MiB maximum working set; 9.99% CPU, seven samples | Small fixture; sampling gaps around restart |

Both live runtimes used 768 MiB hard/no swap, 512 MiB Go soft and two CPUs;
neither was OOM killed. Native RPC concurrency and actual network wire bytes
were not instrumented. Fixture volumes are disk-backed without a hard quota;
traffic and fixtures were deliberately bounded. Exact-image process acceptance
and copy checks used 2 GiB hard/no swap because their Python controller buffers
full responses; total-container peaks 882.76 / 1,018.90 MiB include controller
and file cache and are **not application RSS**. Each completed in about 3 s.

Synthetic cache comparison: repeated ranges reduce Gets 4→1 and median
401.337→368.660 ms; three-chunk thrashing retains four Gets and increases
311.244→542.807 ms. Both allocate approximately 419.5 MB cumulatively; cache
does not remove whole-chunk per-reader decryption. Disk bound remains two
entries / 256 MiB including reservations. Each hit authenticates GCM/AAD/SHA;
corruption is removed/refetched; final remote verification bypasses cache.
Restart clears cache, and backups omit it. No universal speedup claim.

## Security review and retained limitations

Focused source review found no new critical cache/upload security defect.
AES-256-GCM with fresh nonces, chunk identity as authenticated data and SHA-256
verification remain. Sensitive staging/cache/session/config permissions and
owner-authenticated same-origin API boundaries remain; regression suites cover
unauthorized access, CSRF, corruption and encrypted recovery. Runtime secrets
never enter image build arguments/cache or committed reports.

Remaining limits:

- A cold small read still fetches/authenticates a complete 100 MiB encrypted
  chunk; roughly 400× returned-ciphertext amplification for one 256 KiB range.
- Cache thrashing can add I/O without reducing remote traffic. Warm cache is
  discarded on restart. Live comparisons are bounded single runs, not sustained
  network A/B trials or real PDF/video page/seek workloads.
- Crash leftovers need conservative maintenance; no automatic remote/staging
  orphan collection, hard fixture-volume disk quota or full-disk qualification.
- Recovery archives omit pending input parts; fresh restore may require file
  reselection. Finish/cancel pending work before older-binary downgrade.
- Native downloads above 32 MiB do not offer managed accurate UI progress/hash.
- Live cross-datacenter retrieval remains **NOT TESTED**. gotd v0.162.0 routing,
  fresh references, migration fallback and reconnect have controlled coverage;
  successful same-account retrieval does not establish cross-DC coverage.
- No power-loss, sustained capacity stress, physical iOS, independent security
  audit, public-internet qualification or guaranteed Telegram durability.

## Evidence and release decision

Reviewed source export contains 349 allowlisted regular files; ignored runtime,
authorization, dependency, build and evidence directories were not copied.
Redacted Gitleaks source and complete maintained reachable-history scans report
**zero findings**. No original private history was scanned or rewritten in this
milestone. Six transfer documents' 16 local links resolve; diff hygiene passes.
The first clean-environment Compose command omitted Docker's CLI-plugin config
and failed before validation; retaining only its normal CLI config path and an
empty env file passed. No application configuration was changed to bypass it.

Implementation checkpoints: `59aa6d8` (preserved work), `c99caef` (production
fix), `bf1269d` (crash/cache/protocol/live tooling) and `cdb671f` (development
version/build checks). Final report checkpoint follows those commits; consult
local branch HEAD for its hash. Neither published history nor tags were changed.

Ignored local evidence under `output/milestone-a/`: `reliability/`,
`browser-final/`, `cache/`, `qa-operations/`, `docker-final-smoke.log`,
`frontend-tests.log`, `frontend-build.log`, authorized live result directories
and final source-scan reports. Private authorization/fixture state is excluded
from Git and public documentation. Reports contain generated metrics only.

Final independent read-only volume checks confirm both live clones contain no
copied session or session-temp files. No qualification container remains.
Dedicated Colima VM is stopped to release its 8 GiB reservation; default Docker
context remains Desktop and the original owner's container remains healthy.

See [candidate gate](V1.3_RELEASE_CANDIDATE.md) for criterion-by-criterion
decision and local commit ledger. Next milestone should address measured cold
preview/read amplification and conservative orphan maintenance, without changing
chunk formats or raising concurrency before memory/workload measurements.
