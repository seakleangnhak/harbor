# Harbor — Phase 3 reliability qualification

Date: 2026-10-21, Asia/Phnom_Penh. **PHASE3_RELIABILITY = PASS** for the controlled synthetic and live recovery scope below. Actual cross-datacenter retrieval, live rate-limit induction and termination during a Telegram RPC/SQLite commit remain **NOT TESTED**.

Existing encryption format, key, SQLite state, upload/API contracts, 4 MiB application chunks, four engine slots and configured Telegram uploader worker ceiling of two remain unchanged. gotd uses sequential part RPCs for the default 4 MiB encrypted documents; the configured worker ceiling does not imply two active workers for these files. Coordinator executed all live checks using generated fixtures. Agent B inspected source and explicitly authorized sanitized evidence only; no live configuration, session, keys, passwords or logs were accessed.

## Changes and invariants

`PutChunk` now allocates one bounded plaintext buffer and a pre-sized encrypted envelope. Input remains limited to expected chunk bytes plus one overflow byte. `bytes.Buffer.ReadFrom` receives capacity `expectedSize + 1 + bytes.MinRead`; the extra 512 bytes avoid its final growth while preserving original EOF and reader-error behavior. Encryption still uses AES-256-GCM, fresh random nonce and upload-ID/chunk-index authenticated data. Plaintext decryption remains separate from ciphertext; no in-place AEAD change or new concurrency was introduced.

Persisted upload geometry is validated before division or allocation: nonnegative file size, chunk size from 1 byte through 64 MiB, and the same chunk-count ceiling enforced by `Begin`. Invalid values return `domain.ErrIntegrity`. This matters because SQLite's original positive-size constraints alone do not bound a corrupted positive chunk size; a fixed-size preallocation must not trust an unbounded persisted value. Tests bypass SQLite CHECK constraints deliberately to cover zero and negative values as well.

`chunkReader.Seek` retains its single cached plaintext chunk until a subsequent read needs another chunk or `Close` releases it. The API's initial integrity read followed by size-seeking previously caused the first encrypted chunk to be fetched twice. A counting backend reproducer failed before the change with two fetches and passes with one. Before loading a different chunk, both cached bytes and loaded index are invalidated. A failed next-chunk fetch followed by a seek back cannot reuse a stale index with an empty buffer.

## Executed synthetic and local checks

All engine failure injection uses the real encrypted engine and temporary SQLite/local ciphertext files with a small `BlobStore` test double. Network errors below are controlled test errors, not Telegram outages.

| Behavior | Result | Executed test/evidence |
| --- | --- | --- |
| Remote put succeeds as request is canceled | PASS, synthetic | `TestCanceledSuccessfulPutRollsBackBeforeRetry`: canceled SQLite insertion leaves zero received bytes; cleanup uses a non-canceled context, removes the generated blob and releases its slot; retry uses a different nonce and completes |
| Duplicate concurrent chunk requests | PASS, synthetic | `TestConcurrentDuplicateChunksProduceOneBlob`: eight identical concurrent requests create exactly one blob/manifest entry and record bytes once |
| Failed final verification | PASS, synthetic | `TestFailedVerificationRemainsUnpublishedAndReaderRetries`: injected second-chunk retrieval failure keeps status `uploading`, rejects `Open`, then succeeds on retry |
| Interrupted reader retry position | PASS, synthetic | Same test: valid first chunk emitted; failed next chunk emits zero bytes; retry returns the exact next chunk without skipping bytes |
| Backpressure and same-chunk seek | PASS, synthetic | `TestReaderBackpressureSeekAndCancellation`: `Open` fetches nothing; first one-byte read fetches one chunk without prefetch; same-chunk seek reuses it |
| Concurrent cancellation and slot release | PASS, synthetic | `TestBlockedReadersCancelReleaseAllSlots`: four backend reads block, cancel, close and release all four transfer slots; existing fifth-transfer timeout/ceiling checks remain passing |
| HTTP preflight/size-seek pattern | PASS, synthetic | `TestServeContentSeekPatternKeepsFirstChunk`: initial byte read, `SeekStart`, `SeekEnd`, `SeekStart`, then range read fetch the first chunk once; this models the API/standard-library seek sequence rather than a live HTTP benchmark |
| Seek back after failed different-chunk fetch | PASS, synthetic | `TestReaderCanSeekBackAfterFailedFetch`: missing loaded cache cannot cause empty reads or panic |
| Corrupted/incomplete manifests | PASS, synthetic | `TestCorruptManifestNeverCompletes`: missing chunk, out-of-range index, sizes with correct aggregate but wrong individual lengths, invalid digest, empty reference and wrong whole-file digest all prevent completion/open |
| Input size/error semantics | PASS, synthetic | `TestChunkInputBoundsAndReadErrors`: exact bytes, one-byte reads and EOF with final bytes accepted; short/oversize rejected; custom errors, explicit `io.ErrUnexpectedEOF` and cancellation preserved; at most expected bytes plus one consumed |
| Corrupt upload geometry | PASS, synthetic | `TestPersistedUploadGeometryBounds`: zero/negative/huge chunk size, negative file size and excessive chunk count rejected by `Status`, `PutChunk`, `Complete` and `Open` without large allocation; valid empty/max-chunk/max-count boundaries accepted |
| Existing local state/crypto regression | PASS, local adapter | Existing resume with SQLite close/reopen, seek/Range reconstruction, digest mismatch, wrong key, ciphertext alteration, missing blob, missing manifest entry, chunk identity/AAD, reverse ordering and fresh nonce tests all remain passing |
| FLOOD_WAIT, retry bounds and terminal errors | PASS, simulated MTProto | Tagged suite includes `TestTelegramFloodWaitCancellationAndTerminalErrors`, `TestTelegramRetryBoundAndStatusLifecycle` and `TestTelegramStatusConcurrentOperationsAndCancellation`; synthetic FLOOD_WAIT honors deadline, terminal errors stop immediately, retry count is bounded |
| Session persistence and datacenter routing | PASS, simulated MTProto | Existing tagged session atomic replacement/private mode, primary/media datacenter routing, truncated/oversized ciphertext and file-reference tests remain passing; actual cross-DC placement is separately unverified |

Fresh final-source commands executed by Agent B:

```sh
go test -race ./internal/storage -count=1
go vet ./internal/storage
env GOGC=40 go test -p 1 -race -tags telegram ./internal/storage -count=1
env GOGC=40 go vet -p 1 -tags telegram ./internal/storage
```

Results: **PASS**, respectively storage race 2.256s, vet without diagnostics, tagged storage race 4.407s, tagged vet without diagnostics. Earlier focused geometry/input/cache/manifest race check passed in 4.540s. These times are test-command output, not transfer timings. Coordinator owns full application/frontend/Docker/browser regression results.

## Bounded streaming and measured overhead

No entire-file buffer was introduced. Each reader retains at most one authenticated plaintext application chunk, fetches on demand and drops the old cache before another fetch. A canceled read returns its context error; callers must still close the reader to release its engine slot. Double close is safe. `Seek` changes position without background fetches, so Range/video-seeking operations remain demand-driven. Valid earlier chunks can be emitted before a later corrupted or missing chunk is detected; full-download consumers must still verify total length and SHA-256.

Synthetic 4 MiB benchmarks used real SQLite/AEAD plus a cloning in-memory blob adapter. Across three runs of ten iterations, median allocated bytes per `PutChunk` decreased from 18,405,691 to 12,613,976 (31.5%). The preflight/size-seek/full-read benchmark decreased from 16,808,899 to 8,407,034 bytes (50.0%); direct decryption/stream paths remain approximately unchanged. These are cumulative allocations per operation, not process peak RAM or Telegram throughput. Exact command:

```sh
go test ./internal/storage -run '^$' -bench '^BenchmarkEngine' -benchmem -benchtime=10x -count=3
```

Baseline/optimized evidence: ignored `output/phase3/engine-baseline.txt` and `output/phase3/engine-optimized.txt`. See `PHASE3_PERFORMANCE_REPORT.md` for complete measurements and limits.

## Live Docker recovery qualification

Coordinator used isolated loopback Docker deployment `harbor-phase3-tg` and its separate persistent volume, with the host Telegram process stopped throughout authorization handoff. No cloned metadata was written over the original drive. Evidence below was inspected after coordinator completed the operations; simulated errors are not presented as live failures.

| Scenario | Result | Executed evidence |
| --- | --- | --- |
| Encrypted real-Telegram roundtrip inside Docker | PASS, live | `docker-transfer.json`: adapter `telegram`; generated TXT 68 bytes, PNG 121 bytes, PDF 612 bytes and two-chunk multipart 4,259,861 bytes completed, with every downloaded byte count and SHA-256 equal |
| Multipart crash during incoming HTTP body | PASS, live HTTP/process interruption | `docker-crash.json`: chunk 0 committed; only 1,024 of 65,557 tail bytes sent; SIGKILL stopped the owned isolated container. Restart kept exactly chunk 0, discarded the incomplete tail and left the file unpublished. This was not termination during a Telegram RPC or SQLite commit |
| Multipart resume after crash | PASS, live | `docker-transfer.json`: `resume_state_preserved=true`, `incomplete_publish_rejected=true`, completion and full SHA-256 reconstruction passed; cross-chunk HTTP Range matched |
| Clean restart persistence | PASS, live | `docker-initial.json`/`docker-restart.json`: same container ID, changed start timestamp, same named volume/image, authenticated Telegram readiness PASS; `docker-after-restart.json` retrieved all four new fixtures with matching SHA-256 |
| Container recreation persistence | PASS, live | `docker-recreate.json`: new container ID, same named volume/image, authenticated readiness PASS; final `docker-transfer.json` restart-check retrieved all four fixtures again with SHA-256 equality. Persisted authorization, key, manifests and ciphertext references remained usable |
| Interrupted HTTP downloads and recovered capacity | PASS, live | `docker-client-disconnect.json`: four HTTP streams intentionally closed after 262,144 bytes each; a subsequent full download passed SHA-256 and a 49-byte Range spanning an application-chunk boundary matched. Synthetic tests separately prove exact slot release |
| Docker network disconnect/reconnect | PASS for the stated live scope | `docker-network.json`: Docker network detached, affecting both HTTP and MTProto. Client read timed out after 10.194820s with 1,048,576 of 104,857,600 expected bytes received. After reconnect, complete 100 MiB retrieval passed SHA-256 in 24.723560s (4.044725 MiB/s). This does not isolate an MTProto-only outage |
| Real frontend storage/preview/download | PASS, live | `browser.json`: 59,392 generated bytes uploaded with Telegram adapter, exact preview, saved UI download with matching SHA-256, completed transfer queue; 320px mobile layout had no horizontal overflow |
| Deliberately triggered real Telegram FLOOD_WAIT | NOT TESTED | Not induced; controlled doubles qualify handling without account stress |
| Live cross-datacenter retrieval | NOT TESTED | Actual document placement could not be established safely; same-DC/Docker results do not qualify it |
| Crash during Telegram write/SQLite commit | NOT TESTED | HTTP-body SIGKILL and deterministic write/rollback injections cover different boundaries |

All named evidence files are under ignored `output/phase3/`. Initial/restart/recreate snapshots record `connected`, retry count zero, database `ok`, AES-256-GCM, anonymous rejection and all isolated runtime checks PASS. Retry counters reset per process and snapshots do not prove absence of retries during the entire run. The post-network helper also observed `connected`, retry count zero and a checksum-equal complete download; no observed live flood wait is inferred.

An additional final Docker readiness-check login received HTTP 429 after QA exhausted the existing eight-logins-per-15-minutes limit. Coordinator did not bypass the limiter or retry that check. Earlier initial/restart/recreate checks each passed authenticated readiness; the network helper's successful authenticated download and diagnostics supply the post-network evidence. The extra rejected checker is recorded as an expected QA limiter result, not a successful late checker run.

See `PHASE3_DOCKER_REPORT.md` for lifecycle identity, runtime/security and older-file retrieval evidence, and `PHASE3_PERFORMANCE_REPORT.md` for measured host/local/Docker performance limits. Phase 2 historical results are retained separately and were not substituted for these Phase 3 checks.

## Remaining limits and security findings

- A crash after backend creation but before SQLite insertion can leave an orphan encrypted message/blob. Resume remains safe; broad remote cleanup remains intentionally absent. SQL-insertion failures attempt bounded cleanup, but cleanup failure can also leave ciphertext.
- Completion still downloads/decrypts all chunks and verifies the whole-file digest before setting `complete`. This verification cost is retained for integrity; failed/missing data cannot be published through normal finalization.
- Geometry validation rejects corrupted headers safely but does not repair SQLite corruption or lost content. Keep an offline backup and preserve the content key separately.
- Context cancellation does not itself close a reader or interrupt an arbitrary user-supplied `io.Reader`. HTTP handler/client cleanup and adapter context propagation remain required.
- No nonce, credential, Telegram reference, plaintext fixture content, session or key value enters this report. No preexisting remote message was deleted by these tests.
