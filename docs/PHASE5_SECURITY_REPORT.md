# Harbor Phase 5 security hardening

2026-10-21, Asia/Phnom_Penh. **PASS for the focused source review and synthetic controls below.** Complete disaster recovery, Docker, browser and live Telegram acceptance belong to the coordinator's Phase 5 reports. This is local personal-use qualification, not independent security certification or public-internet deployment approval.

Agent C reviewed source and generated fixtures only. No actual `.env`, owner password, content key, Telegram authorization, live log or private backup was read. No production endpoint or account was probed.

## Fixed boundaries

| Risk | Change | Executable evidence |
| --- | --- | --- |
| Existing permissive/symlinked data directory or metadata paths | Startup requires a real private data directory; rejects nonregular/symlinked sensitive leaves. Existing regular SQLite DB/WAL/SHM permissions are tightened to 0600 without changing bytes. Keys, owner password and Telegram authorization with permissive modes fail closed instead of being silently accepted. | `TestPrivateDataDirectoryBoundaries` covers new/existing directories, all protected leaves, symlinks, directories, permissive files and byte-preserving SQLite permission repair. |
| SQLite creation and sidecar path handling | `OpenDB` checks DB/WAL/SHM before SQLite opens them, rejects symlinks/nonregular paths and precreates the DB exclusively with mode 0600. | `TestPhase5PrivateSQLiteFiles`; `TestPhase5SQLiteRejectsSymlinkAndSpecialFiles` checks rejection and preservation of an unrelated valid SQLite fixture. |
| Accidental cleartext non-loopback listener | Literal-IP/localhost listener validation requires secure cookies for an HTTPS reverse proxy or explicit `DRIVE_ALLOW_REMOTE_HTTP=true`. Default remains loopback. Container-internal opt-in does not alter Compose's loopback-only publication. | `TestListenConfigurationRequiresRemoteOptIn` checks IPv4/IPv6 loopback, unspecified/private addresses, opt-in, secure-cookie proxy setup and invalid addresses/ports. |
| Plaintext metadata backup retained through old UI/API | Authenticated `GET /backup` now fails with 410 `encrypted_backup_required`; Settings directs owners to encrypted offline recovery tooling. Anonymous access remains rejected. No plaintext fallback. | Coordinator-owned endpoint regression test; complete encrypted-archive qualification is documented separately. |
| Go standard-library certificate advisory | Root upgraded Go from 1.26.9 to 1.26.10 after scanner identified GO-2026-6809 in `crypto/x509`. | Default and Telegram-tagged `govulncheck` both return exit 0 with zero affected symbols/imported packages on Go 1.26.10. |

Filesystem checks assume trusted application owner and protected parent directories. They do not defend against malware running as that same OS user, root, a hostile filesystem or offline disk theft. Use disk encryption and protected off-device encrypted backups. No private runtime state was changed by this agent.

## Coordinator integration review

Late integration findings were fixed before final qualification:

- A bound installation with missing `drive.db` now fails closed; it cannot silently initialize empty metadata. `TestBoundDriveMissingDatabaseFailsClosed` verifies preserved binding/key state.
- Offline integrity commands refuse missing/incomplete metadata rather than creating an installation. `TestIntegrityCannotInitializeMissingDrive` covers this boundary. Storage binding creation now follows first-boot password-presence check.
- Reserved `copy:` staging uploads are omitted from ordinary transfer recovery and rejected by user-upload status/chunk/complete/cancel endpoints. `TestPhase5InternalCopyStageCannotBeMutatedAsUserUpload` prevents accidental publication or mutation outside the durable copy worker.
- Recovery validates saved owner-password/hash agreement and gotd-compatible session fingerprints; malformed recovery inputs fail without publication. Private channel-link compatibility was exercised with synthetic tests and the actual isolated restore.
- The live qualification harness checks private secret sources, holds the original data lock, and orders clone shutdown before releasing authorization ownership on normal exit/SIGTERM/SIGINT. Forced harness SIGKILL still requires manual verification that no clone remains before original restart.

Fresh complete default/tagged Go tests, race tests and vet passed after these source changes. Final race durations: API 78.076/71.852 s, recovery 36.629/33.579 s (default/tagged). Chromium and WebKit Settings/copy-history/interruption screens passed scoped axe with zero violations. A horizontal terminal-command scroll area initially lacked keyboard focus; adding `tabIndex={0}` resolved that finding without weakening CSP. Final image was rebuilt and fully requalified afterward.

Evidence: `output/phase5/go-*.log`, `phase5-surfaces-chromium.json`, `phase5-surfaces-webkit.json`, and linked [final report](PHASE5_FINAL_REPORT.md). Live restore, host preservation and real browser SHA-256 checks passed separately; simulated RPC results remain identified as simulations.

## Existing controls rechecked

| Control | Result and evidence |
| --- | --- |
| Owner authentication | PASS, synthetic. No signup/JWT issuance; bcrypt owner hash and random 256-bit session token. SQLite stores only SHA-256 token hashes. Query parameters and Bearer headers do not substitute for owner cookie. `TestPhase5SessionCredentialAndOriginBoundaries`. |
| Expiration and revocation | PASS, synthetic SQLite close/reopen. Expired cookie remains rejected after restart; new login removes expired records and creates a fresh token; logout stays revoked after another restart. `TestPhase5ExpiredAndRevokedSessionsSurviveRestart`. |
| Browser mutation boundary | PASS, synthetic. Origin must match scheme/Host; null, hostile hosts, userinfo, paths, query/fragment and cross-site/same-site fetches fail. Secure-cookie reverse-proxy origin works; spoofed forwarding headers do not change scheme policy. Existing cookie flags remain HttpOnly, SameSite=Strict, host-only, Path=/, seven-day fixed expiry. |
| Brute-force protection | PASS, synthetic. Eight attempts per actual TCP peer per 15 minutes, bounded map; changing `Forwarded`, `X-Forwarded-For` and `X-Real-IP` cannot bypass it. Failed/throttled login creates no session. Expired window permits login. `TestPhase5LoginThrottleIgnoresSpoofedProxyHeaders`. |
| Authorization and request bounds | Existing authenticated route group covers files, transfers, metadata, diagnostics and recovery guidance. JSON bodies capped at 64 KiB, unknown/trailing data rejected, raw uploads limited to expected chunk size plus one. SQL remains parameterized; filenames, parents, moves, digests and MIME types validated. Existing API tests cover anonymous operations, oversized bodies, folder cycles and corrupted streaming content. New copy-job routes require the same owner boundary. |
| Encryption and integrity | Existing AES-256-GCM uses fresh cryptographic 96-bit nonce and upload-ID/chunk-index authenticated data. SHA-256 is verified before publication and before releasing each decrypted chunk. Existing engine tests cover wrong key, altered ciphertext, substituted identity, missing/out-of-order chunks, duplicate retry and fresh nonce after rollback. Sampled uniqueness is probabilistic evidence, not a mathematical guarantee. |
| Sensitive error output | Existing API/Telegram classifications expose fixed public messages and fixed internal log code, never arbitrary provider text. Existing synthetic-secret tests cover HTTP, logs, setup and diagnostics. No authentication/session/configuration value is returned to browser. |
| Telegram authorization and channel failure | PASS, simulated RPC. `TestPhase5TelegramTerminalAccessFailuresReturnNoContent` covers expired/revoked authorization, invalid/duplicated auth keys and channel/permission denial: one attempt, no returned bytes, no retry. Invalid/foreign channel/message references cannot reach read/delete RPCs. These tests do not revoke any real account session. |
| File-reference recovery and rate limits | Existing tagged tests verify expired-reference re-resolution, absent message handling, bounded outer retries, deadline/cancellation and synthetic FLOOD_WAIT. No live rate restriction was deliberately induced. |
| Session and temporary state | Existing MTProto session implementation uses mode-0600 temporary files, file sync, atomic rename and directory sync; refuses public/nonregular sessions. Existing tests exercise persistence and symlink rejection. Local ciphertext writes use private directories and atomic rename; no plaintext content reaches blob adapter. |
| Schema safety | Coordinator added version guard and transactional API migration. Unknown future schema rejects rather than silently continuing. Coordinator-owned tests and upgrade/rollback qualification supply final evidence. |

## Fresh commands and results

```sh
DRIVE_ENV_FILE=- go test -count=1 ./internal/api -run '^TestPhase5'
DRIVE_ENV_FILE=- go test -count=1 ./cmd/drive
DRIVE_ENV_FILE=- GOGC=40 go test -count=1 -race -p 1 -tags telegram ./cmd/drive ./internal/api ./internal/storage
DRIVE_ENV_FILE=- GOGC=40 go test -count=1 -race -p 1 -tags telegram ./internal/storage -run '^TestPhase5'
DRIVE_ENV_FILE=- govulncheck ./...
DRIVE_ENV_FILE=- GOGC=40 govulncheck -tags telegram ./...
cd web && npm audit --json
```

All commands above **PASS**. Full tagged race results: cmd/drive 1.643 seconds, API 65.202 seconds, storage 4.566 seconds. The focused Telegram failure/reference tests also pass with the race detector (4.253 seconds). These are test durations, not transfer timings. Frontend audit reports zero advisories across 220 installed dependency entries. Both final Go scans report zero affected symbols/imported packages; one module-level advisory remains for **unused** `golang.org/x/crypto/openpgp` (GO-2026-5932). Harbor imports bcrypt, not OpenPGP. No unrelated runtime dependency was added or blindly upgraded.

Initial Go scan failed on Go 1.26.9 as described above; upgrade and reruns resolved the reached standard-library advisory. Initial integration compile failed while copy-job shared hooks were still being integrated. Two old terminal/lock tests then failed because `testing.T.TempDir` creates its numbered child using mode 0777 filtered by umask; fixtures now explicitly use 0700 and test their intended terminal/lock behavior. Production directory validation was retained.

## Telegram protocol review and limits

Source review of installed gotd/td v0.162.0 confirms primary `invokeDirect` routes `FILE_MIGRATE`/`STATS_MIGRATE` to an authorized target DC. Harbor reconstructs locations from the message's current document ID, access hash and file reference. Same-DC retrieval uses primary API; other DCs use a closed-after-use media connection. Existing routing tests check exact location, offsets, limits and truncated/oversized data. A secondary redirect returned from an already selected raw media pool still fails safely rather than adding another redirect layer.

**NOT TESTED:** legitimate live cross-datacenter placement/retrieval; deliberate real authorization expiry/channel revocation/rate-limit induction; independent penetration test; physical iOS; hostile same-user/root compromise. Other Phase 5 reports distinguish live Telegram and Docker checks from simulations.

Login throttle is process-local, resets on restart and counts successful attempts too. Behind a reverse proxy all peers may share its address; no untrusted forwarding header is used as identity. HTTP session expiry is fixed seven days; no owner password-rotation or all-session management UI was added. `DRIVE_SECURE_COOKIES=true` requires actual HTTPS termination; it does not provide TLS itself. Telegram account/channel loss can make ciphertext unavailable despite local backup. Preserve separate encrypted recovery archives and verify restore regularly.
