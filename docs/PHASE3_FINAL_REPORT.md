# Harbor — Phase 3 acceptance

Date: 2026-10-21, Asia/Phnom_Penh. **HARBOR_PHASE3 = PASS.**

All six required gates passed. Real cross-datacenter retrieval is **NOT TESTED**; synthetic routing and source inspection do not establish live migration. Existing UI, API, original drive, encryption key and account session were preserved. No public deployment, remote push or preexisting Telegram deletion occurred.

## Acceptance evidence

| Required gate | Result | Executed evidence |
| --- | --- | --- |
| Real MTProto inside Docker | PASS, live | Isolated `harbor-phase3-tg` on loopback 8084 started with persisted authorization and verified target; authenticated diagnostics reported Telegram connected, SQLite ok, AES-256-GCM |
| Session/data survive recreation | PASS, live | Same named volume across clean restart and replacement container; all four new fixtures retrieved after each; older Phase 2 and 1/10/100 MiB host fixtures retrieved after recreation |
| Retry/restart recovery | PASS, controlled | SIGKILL during partial HTTP chunk body preserved committed chunk 0 and unpublished state; resume/full hash/Range passed. Live network detach interrupted download; reconnect/full hash passed. Synthetic RPC, FLOOD_WAIT, cancellation and manifest faults passed |
| Encrypted integrity | PASS | Real TXT/PNG/PDF and 4,259,861-byte multipart roundtrips, browser saved-byte equality, retained-file hashes; AEAD/nonces/AAD, corruption, ordering and missing-manifest regressions |
| Performance measured | PASS | Real baseline/optimized 1/10/100 MiB; controlled two-download comparison; local 512 MiB; synthetic allocation/RPC benchmarks. Timing and resource limits below |
| Existing regression | PASS | Default/tagged Go race/vet, frontend tests/typecheck/build, Python tests, process acceptance, real browser upload/preview/download and Docker checks |

Evidence remains under ignored `output/phase3/`. Reports contain measured results; secret/session/key files are excluded from Git. Isolated Docker volume `harbor-phase3-tg_drive-data` is retained for its fixture manifests and stopped. Its database must never overwrite the original drive. Docker was stopped before original Telegram service resumed at http://127.0.0.1:8081. Final original-host check retrieved the four retained Phase 2 runner files with exact hashes. Original port-8080 local deployment was untouched.

## Measured improvements

One sequential observation per size, same host/account/channel and 4 MiB application chunks. Upload includes finalization, which re-downloads and verifies every chunk. Network conditions vary; these are qualification observations, not statistically established sustained gains.

| MiB | Baseline upload MiB/s | Optimized upload MiB/s | Baseline download MiB/s | Optimized download MiB/s | Exact bytes/SHA-256 |
| ---: | ---: | ---: | ---: | ---: | --- |
| 1 | 0.554308 | 1.051779 | 2.526333 | 3.763081 | PASS |
| 10 | 1.085039 | 1.370491 | 3.073074 | 4.670550 | PASS |
| 100 | 1.121734 | 1.354059 | 3.952927 | 4.040140 | PASS |

Observed 100 MiB upload improvement: 20.71%; download: 2.21%. Final verification fell from 27.345539 to 21.607680 seconds, retained for integrity. Host sampled maximum RSS increased from 51.6875 to 61.5 MiB; reduced allocations do **not** establish reduced live peak RAM. Mean OS CPU sample was 4.976483% versus 3.717857%, across phases of 129.194348 and 109.315697 seconds. RSS/CPU sampled nominally every 250 ms; no lifetime-peak claim. Builds/tests overlapped portions of measurement; timings are not isolated laboratory results.

Two retained 10 MiB Docker downloads: sequential aggregate 4.380699 MiB/s (4.565482 s), concurrent pair 9.536368 MiB/s (2.097234 s); all four hashes matched. Browser activity overlapped this single observation. Existing four engine slots already permit independent requests; no adaptive concurrency layer was justified or added.

Local-only 512 MiB roundtrip: 128 chunks, upload 209.604835 MiB/s, download 1208.400389 MiB/s, exact bytes/hash PASS. Thirteen samples over 3.377865 seconds recorded maximum RSS 35.734375 MiB and CPU 128.3%; short loopback/cache-sensitive result, not Telegram bandwidth or sustained capacity. **Live 512 MiB NOT TESTED**, to limit account traffic.

Synthetic medians for 4 MiB chunks: upload allocated bytes fell 18,405,691 → 12,613,976 (31.5%); HTTP preflight/seek/read fell 16,808,899 → 8,407,034 (50.0%). Counting regression proves first-chunk retrieval fell from two to one across `ServeContent` preflight. Telegram 512 KiB parts reduce one encrypted full-chunk upload from 33 part RPCs to nine; default documents remain sequential inside gotd's small-file loop. Configured uploader worker ceiling is two, effective part concurrency one for these documents.

Full methods, latency tables and bounds: [performance report](PHASE3_PERFORMANCE_REPORT.md).

## Implementation and review

- `engine.go`: bounded plaintext preallocation and one pre-sized AES-GCM envelope, fresh random nonce unchanged. Persisted upload geometry checked before division/allocation to reject corrupt metadata safely.
- `reader.go`: one authenticated chunk cache survives size/preflight seeks; loading another chunk invalidates both bytes and index before a fallible fetch. Demand-driven reads, four slots, context propagation and HTTP Range remain intact.
- `telegram.go`: 512 KiB upload parts reduce RPC overhead; download FLOOD_WAIT status updates before native sleep without counting the same event twice. Primary/media routing retained; no speculative connection pool rewrite.
- New engine/Telegram failure tests and benchmarks exercise real storage code with synthetic RPC/blob failures. Stdlib-only benchmark and Docker checker validate exact generated bytes, preserve original observations on recheck, revoke temporary sessions and emit allowlisted diagnostics only.

Coordinator reviewed production diff; independent reliability review found no blocking defect. Security-audit guidance supported focused source review, not a comprehensive penetration-test certification. Encryption format/API/schema/keys did not change. Docker session, key and owner-password were independently checked as regular mode-0600 files owned by UID/GID 65532. Runtime verified non-root, read-only root, all capabilities dropped, no-new-privileges, bounded tmpfs and loopback exposure.

Final scan compared all candidate tracked files against actual configured credentials, owner password, content key and long session values without printing them: PASS. Original private-file modes also passed; temporary Docker password and browser storage-state copies were absent.

Repeated isolated QA logins eventually hit the existing eight-attempt/15-minute limit: an additional post-network checker returned HTTP 429. No limiter bypass, weakening or immediate retry occurred. Earlier lifecycle checks and the network helper's authenticated post-reconnect checksum/diagnostics had passed. Browser session was revoked; its temporary storage-state file and Docker password copy were removed. Expected anonymous session check after browser logout returned 401, the sole browser console error.

## Recovery qualification boundaries

| Scenario | Result | Precise scope |
| --- | --- | --- |
| Active multipart process termination | PASS, live HTTP interruption | Sent 1,024 of 65,557 tail bytes, then SIGKILL; committed 4 MiB chunk retained, partial chunk absent, file unpublished; resumed exact bytes/hash |
| Clean restart/recreation | PASS, live | Persisted session/key/SQLite manifests; new and older files retrieved |
| Network detach/reconnect | PASS, live | Docker network removed during 100 MiB download; both HTTP and MTProto affected. Client timed out after 10.194820 s with 1 MiB received, then reconnected full download passed in 24.723560 s (4.044725 MiB/s) |
| Browser/client disconnect | PASS, live | Four HTTP streams closed after 256 KiB each; subsequent 1 MiB full checksum and 49-byte cross-chunk Range passed |
| FLOOD_WAIT / RPC classification | PASS, simulated | Intercepted MTProto invokers test wait cancellation, pending status, bounded retries, terminal errors and publication identity; no real rate restriction deliberately triggered |
| Missing/expired/corrupt data | PASS, simulated/local | Missing message, refreshed expired reference, truncated/oversized ciphertext, wrong digest, malformed geometry and manifest failures reject unsafe publication/read |
| Actual cross-datacenter retrieval | NOT TESTED, live | No safe known-other-DC fixture identified; successful Docker/older-file retrieval does not prove DC migration |
| FILE_MIGRATE behavior | PARTIAL | gotd primary invoker supports cached authorized DC redirection by source inspection; synthetic routing passes. Secondary redirect from a raw media pool remains unsupported and fails safely |
| Kill during Telegram write/SQLite commit | NOT TESTED, live | HTTP-body crash does not locate termination inside either critical section; cancellation/rollback fault tests cover narrower synthetic cases |

More detail: [reliability](PHASE3_RELIABILITY_REPORT.md), [Docker operations](PHASE3_DOCKER_REPORT.md).

## Executed regression commands

All PASS unless explicitly scoped above. Final Go source was frozen before image/live qualification. Repeated final commands may use Go's valid test cache; no later production source changes occurred.

```sh
env DRIVE_ENV_FILE=- go test ./...
env DRIVE_ENV_FILE=- go test -race ./...
env DRIVE_ENV_FILE=- go vet ./...
env DRIVE_ENV_FILE=- GOGC=40 go test -race -p 1 -tags telegram ./...
env DRIVE_ENV_FILE=- GOGC=40 go vet -p 1 -tags telegram ./...
python3 -W error::ResourceWarning -m unittest discover -s scripts -p '*_test.py'
cd web && npm test && npm run typecheck && npm run build
```

Python: 15 tests. Frontend: eight tests. Production build retains existing >500 kB chunk warning. Default/tagged binaries and final MTProto Docker image built successfully. `python3 scripts/acceptance.py --binary bin/drive` passed auth/origin checks, interrupted upload/restart/idempotency, checksum/Range, folder move/rename/trash, empty files, SQLite snapshot, offline backup/restore and session revocation. `docker compose config --quiet` passed. Chromium real-Docker UI upload/preview/verified saved download passed for 59,392-byte TXT; desktop screenshot and 320px layout checked, no horizontal overflow. No new UI design changes.

Core checkpoint: `2214c6f`. Qualification harness checkpoint: `e894b6d`. Final documentation checkpoint follows these. No remote push.

## Remaining limits

- Finalization still re-reads every Telegram chunk and holds API metadata-mutation lock; large completion can delay metadata mutations. No integrity shortcut or lock redesign added.
- Crash after remote creation but before manifest commit may leave orphan ciphertext. Fixtures remain tracked; broad remote garbage collection is absent.
- Six outer attempts plus two-minute operation deadline bound application retries. Native gotd flood/timeout/false-ack loops can issue more than six wire requests within that deadline.
- One authenticated chunk cached per reader; managed browser verified downloads cap at 32 MiB. Larger downloads use browser streaming; small-download pause restarts safely.
- Cached diagnostics are liveness/readiness hints, not proof of every RPC; retry counters reset on restart. Docker CPU/RSS, sustained load, lifetime peak RAM and actual live cross-DC migration remain NOT TESTED.
- SQLite metadata stays readable to host owner; key loss still loses content. Preserve offline backups, protected key and dedicated account/channel access.
