# Harbor reliability qualification

Date: 2026-10-21. Phase 2. Existing local data and preexisting Telegram messages preserved.

Status: **PASS for required Phase 2 functional/reliability scope**. Live MTProto authentication, intended-channel validation, encrypted storage, checksums, multipart resume, session/restart persistence and browser upload/preview/download passed. Local regressions remain successful. Untested failure modes below remain explicitly outside this acceptance.

## Verified without Telegram

| Check | Result | Executed evidence |
| --- | --- | --- |
| Storage race tests | PASS | `go test -race ./internal/storage` (2.095s) |
| Qualification runner checks | PASS | `python3 scripts/telegram_qualification_test.py`: 3 tests |
| Controlled HTTP qualification | PASS (local adapter) | `upload`, `resume`, `restart-check` phases against disposable local server, two real process restarts |
| TXT, PNG, PDF and multipart roundtrips | PASS (local adapter) | All four generated fixtures downloaded byte-for-byte and SHA-256 equal; multipart size 4,259,861 bytes |
| Interrupted transfer persistence | PASS (local adapter) | First 4 MiB chunk persisted, incomplete finalization returned 409, file remained unpublished (404), restart retained exactly chunk 0, resume completed |
| Range retrieval | PASS (local adapter) | 43 bytes spanning plaintext chunk boundary matched fixture |
| Nonce uniqueness/ciphertext-only backend | PASS | 32 chunks with identical plaintext used distinct AES-GCM nonces; plaintext absent from stored blobs |
| Chunk identity/order | PASS | Reverse-order upload reconstructed correctly; same-index cross-upload and same-upload cross-index ciphertext substitutions rejected |
| Missing/corrupt data | PASS | Missing manifest entry rejected before open; missing blob returned no plaintext; altered ciphertext/wrong key rejected before emitting affected chunk |
| Bounded transfers/cancellation | PASS | Four blocked independent uploads reached backend; fifth timed out without backend call or received chunk; existing four-reader limit test passed |
| Retry/rollback | PASS | Existing idempotent identical chunk retry, conflicting retry, failed backend put, failed delete and retained cancellation state tests passed |

Fresh tests extend existing engine checks; no production storage-engine change was needed.

Coordinator's additional fresh checks: full default/tagged Go race suites PASS; isolated `scripts/acceptance.py` PASS; Python discovery PASS (ten tests). Final Docker local runner passed upload, resume after container restart, restart-check after second restart, then restart-check again after final image replacement. Same volume retained. Evidence: `output/phase2/docker-qualification.json`.

Final local browser checks PASS: TXT/PNG/PDF and 4,259,861-byte multipart upload/download checksums; corrupt/truncated response produced an actionable error and zero download events, retry then saved exact multipart bytes. A delayed non-abortable hash read exposed a Pause/Resume race during review; fixed before acceptance and reproduced successfully using runnable `scripts/browser_transfer_check.js`. Paused managed downloads restart rather than trusting unverified partial browser bytes. Browser checksum evidence: `output/phase2/browser-checksums.json`.

## Live qualification

| Check | Result | Evidence |
| --- | --- | --- |
| Real authentication and target validation | PASS | Coordinator completed native QR authentication, securely retained session, verified channel ownership, retention and link/expected-ID agreement before writes |
| Real encrypted upload/download | PASS | Generated TXT (68 bytes), PNG (121 bytes), PDF (612 bytes) and multipart (4,259,861 bytes) completed using `adapter=telegram`; all downloaded bytes and SHA-256 values matched |
| Multipart manifests/reconstruction | PASS | Multipart fixture used two application chunks (4,194,304 + 65,557 bytes); finalization and complete download matched SHA-256 `e609159371697ca949b503bcabf27ddfde224ea08f00dcdf5efa6328da1f9e05` |
| Interrupted transfer resume | PASS | Staged chunk 0 persisted as exactly 4,194,304 received bytes; incomplete completion rejected with 409 and unpublished node returned 404; resume continued remaining chunk and completed |
| Application restart/session persistence | PASS | Coordinator performed graceful application restarts retaining data/session; `restart-check` retrieved all four fixtures again with byte equality and matching SHA-256 |
| Idle abrupt termination recovery | PASS | Coordinator verified owned server was idle, sent SIGKILL, restarted with retained session/data and downloaded all five generated fixtures with exact SHA-256 equality |
| Cross-chunk Range | PASS | HTTP 206 returned 43 expected bytes spanning the 4 MiB plaintext boundary |
| Failed completion cannot publish | PASS | Initial TXT finalization failed with HTTP 500 while upload remained unpublished; coordinator diagnosed same-DC media authorization routing (`DC_ID_INVALID`), fixed routing, then retrieved/decrypted the retained remote chunk and safely completed same upload |
| Same-DC/cross-DC routing regression | PASS synthetic; PARTIAL live | Primary API used for same-DC reads; cross-DC media connection remains bounded and closed. Synthetic tests cover both routes, multipart reads, truncation and oversize; retained live same-DC read passed |
| Telegram connection failure/flood wait | PASS synthetic; NOT TESTED live | Tagged retry/cancellation/deadline tests pass; no deliberate real flood waits or account stress |
| Abrupt kill during live chunk/network failure | NOT TESTED | Idle SIGKILL and staged-chunk restart passed; no termination during a Telegram request or database commit was induced |
| Real browser transfer/preview | PASS | Production UI uploaded `harbor-live-browser.txt` (135,168 bytes) to Telegram, showed exact text preview and downloaded byte-equal content through managed verification; queue reported completed upload and verified download; diagnostics reported Telegram connected |
| Actual remote ciphertext | PASS | Read-only probe of six tracked remote chunks, including UI fixture, verified Telegram references, HD01 envelopes, distinct nonces, upload-ID/index authenticated data, AES-GCM authentication and plaintext SHA-256 |
| Post-crash access control | PASS | Fresh anonymous content and diagnostics requests returned HTTP 401 after idle SIGKILL/restart |

Exact sanitized evidence: `output/phase2/telegram-qualification.json`. All three live phases (`upload`, `resume`, `restart-check`) record PASS; flags `incomplete_publish_rejected`, `initial_failure_unpublished`, `resume_state_preserved`, `cross_chunk_range_equal` and `restart_retrieval_verified` are true. Every file records `completed=true`, `download.sha256_equal=true` and `restart_download.sha256_equal=true`. Live evidence was inspected read-only; no credentials, channel identity, session data or keys enter this report. Generated qualification objects remain available; no preexisting message was deleted.

Final browser evidence: `output/phase2/live-browser-checksums.json`, with `preview_exact=true`, `download_bytes_equal=true` and fixture SHA-256 `a92d9aa2f1854e73ba9aa6d4540fe2a9a525710c1efaeb0b66819b02c353b631`. Idle-crash evidence: `output/phase2/live-idle-crash.json`, recording `server_idle=true`, `owned_process_verified=true`, `signal=SIGKILL`, successful retrieval of all four runner fixtures, `browser_fixture_sha256_equal=true` and `unauthenticated_rejection=PASS`. Remote ciphertext evidence: `output/phase2/live-ciphertext-probe.txt` reports six tracked chunks verified. These artifacts distinguish actual live checks from synthetic failure injection.

`scripts/telegram_qualification.py` requires an explicit mode: `--live` verifies a Telegram adapter, while `--local-check` cannot qualify Telegram. It never reads `.env`, Telegram sessions or content keys. Only owner password file is read, then kept in memory for login; cookies never enter its saved state. Redirects and non-loopback origins are rejected.

The runner creates one uniquely named fixture folder and four generated fixture uploads. State tracks their IDs and hashes under ignored `output/`, mode 0600, atomically replaced after mutations. It contains no delete operation and never removes preexisting objects. Failed checks report HTTP status without response bodies or underlying connection details.

Coordinator sequence, after validating channel identity:

```sh
python3 scripts/telegram_qualification.py --base-url http://127.0.0.1:8081 --password-file /private/path/owner-password --state-file output/phase2/telegram-qualification.json --phase upload --live
# Restart Harbor while retaining its data directory and session.
python3 scripts/telegram_qualification.py --base-url http://127.0.0.1:8081 --password-file /private/path/owner-password --state-file output/phase2/telegram-qualification.json --phase resume --live
# Restart Harbor again, retaining the same state.
python3 scripts/telegram_qualification.py --base-url http://127.0.0.1:8081 --password-file /private/path/owner-password --state-file output/phase2/telegram-qualification.json --phase restart-check --live
```

Commands show reproducible structure; substitute actual private password-file path and loopback port locally. Do not rerun `upload` over existing qualification state: use a new ignored state file for a new controlled run. Runner verifies retrieval across external restarts; coordinator performed and recorded those restarts. Initial finalization failure was repaired and qualification continued against the tracked upload; state explicitly records recovery rather than representing an uninterrupted first attempt.

## Remaining limits

- A crash after backend write but before SQLite insertion can leave unreferenced ciphertext. Resume remains safe; conservative orphan cleanup remains deferred.
- Each plaintext chunk is authenticated before emission. A download can emit earlier valid chunks before discovering later missing/corrupt chunks; a caller must verify complete length/digest.
- Metadata completion re-reads every chunk from backend for whole-file SHA-256 verification. This costs a full backend read but prevents publication of incomplete or corrupt content.
- Controlled live checks demonstrate account/session persistence, remote encryption and browser storage for generated fixtures, including idle abrupt termination. They do not establish sustained availability, Telegram rate-limit behavior or crash safety during an in-flight network request/database commit.
