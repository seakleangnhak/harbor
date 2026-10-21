# Harbor — Telegram integration qualification

Date: 2026-10-21, Asia/Phnom_Penh. Local macOS execution; no public deployment or remote push. Existing data, keys, configuration values and preexisting Telegram messages preserved. Only generated qualification fixtures were uploaded after channel verification. All qualification objects retained.

## Acceptance

**HARBOR_TELEGRAM_INTEGRATION = PASS** for all nine required acceptance gates. This qualifies the exercised dedicated account, verified private channel and host application; untested service/failure paths remain explicit below.

| Required gate | Result | Executed evidence |
| --- | --- | --- |
| Real MTProto authentication | PASS | Native QR approval in private local Terminal; user confirmed success; persisted-session server startup accepted authorization |
| Correct channel resolution | PASS | Existing account owns private broadcast channel; link and configured expected ID agree; public aliases, linked discussion and auto-delete rejected |
| Real encrypted Telegram upload | PASS | Generated TXT 68 bytes, PNG 121 bytes, PDF 612 bytes, multipart 4,259,861 bytes, and browser TXT 135,168 bytes |
| Successful Telegram retrieval | PASS | Actual tagged adapter retrieved every fixture; read-only remote probe verified six tracked `tg:` chunk references |
| Decryption and SHA-256 equality | PASS | Every complete download matched original bytes/hash; all six fetched ciphertext envelopes passed AES-GCM with upload/index AAD and chunk SHA-256 |
| Multipart transfer | PASS | Two application chunks: 4,194,304 and 65,557 bytes; full reconstruction and cross-chunk HTTP 206 Range matched |
| Persistence across restart | PASS | Staged upload resumed after graceful restarts; all four runner fixtures retrieved again. Separate idle SIGKILL/restart recovered session and all five files |
| Real frontend storage | PASS | Chromium file input uploaded generated TXT; exact preview; UI download saved identical bytes after managed SHA-256 verification; queue showed completion and Telegram connected |
| Existing local MVP regression | PASS | Default/tagged Go race suites and vet, process acceptance, Python checks, frontend tests/typecheck/build, local browser/Docker gates |

## Fixes and configuration

Direct Go execution loads private literal `.env`; explicit process variables override it, including empty values. All four existing names map correctly: `TELEGRAM_APP_ID`, `TELEGRAM_APP_HASH`, `USERNAME_OR_CHANNEL_LINK`, `TELEGRAM_CHANNEL_ID`. Rendered Compose values were compared internally with actual configuration; equality PASS, no values printed or replaced. Private environment/session/content-key files are regular files, mode 0600.

Private link resolution discovers the access hash server-side. Configured expected channel ID is normalized and checked before writes; immutable drive binding also prevents target changes. Manual access hash cannot be combined with a link. Setup never joins a channel automatically. Account credentials/session never enter frontend responses.

Original OTP attempt failed after code entry; later app-delivered code did not arrive. Cause remains unknown. Phone now stays visible only in local Terminal; OTP/two-step password stay hidden. Fixed delivery notices identify supported app/SMS/call flows, reject unsupported methods before OTP, and never auto-resend. Native `telegram-setup-qr` resolved authentication through an already signed-in dedicated account. QR is rendered only in private Terminal, never as a URL/token in logs/files/browser. Buffered approval avoids a rendering race. QR network/approval has a three-minute context; local password input remains blocking.

First real TXT finalization failed safely with HTTP 500 before publication. Read-only retrieval of its tracked encrypted object classified `DC_ID_INVALID`: media authorization was exported back to the current datacenter. Same-DC downloads now use the primary authenticated API. Cross-DC media pools remain native and close after operation. Routing, part offsets, bounds, truncation and pool cleanup have synthetic regression coverage. Retained TXT chunk was recovered without duplicate upload, then qualification continued. Failure/repair time is excluded from successful throughput measurements; this was not an uninterrupted first run.

Chunk operations keep two-minute deadlines and six application attempts, honor cancellation/flood waits and refresh expired references. Native connection callbacks supply allowlisted state/retry diagnostics. Frontend shows acknowledged upload speed/ETA, remaining bytes, errors, and checksum-verified downloads up to 32 MiB. Large downloads use browser progress; paused managed downloads restart.

## Executed checks

| Check | Result | Scope |
| --- | --- | --- |
| `go test ./...`, `go test -race ./...`, `go vet ./...` | PASS | Current default CLI/API/storage source |
| `GOGC=40 go test -race -p 1 -tags telegram ./...` | PASS | Full tagged suite; synthetic auth/channel/routing/retry checks |
| `GOGC=40 go vet -p 1 -tags telegram ./...` | PASS | No diagnostics |
| Tagged host executable and final Docker build | PASS | QR/login/routing fixes included |
| `python3 scripts/acceptance.py --binary bin/drive` | PASS | Local real-process auth/origin, interrupted restart/resume, checksum/Range, hierarchy/trash, metadata snapshot, offline restore/session revocation |
| `python3 -W error::ResourceWarning -m unittest discover -s scripts -p '*_test.py'` | PASS | Ten tests: seven recovery and three qualification checks |
| `npm test`, `npm run typecheck`, `npm run build` | PASS | Eight tests; Vite bundle built; main-bundle size warning remains |
| Direct Go / Compose configuration | PASS | Private dotenv autoload and exact mapping; `docker compose config --quiet` passed |
| Final Docker local runtime | PASS | Separate QA project/volume on loopback 8083; healthy UID 65532, read-only root, capabilities dropped, no-new-privileges; four fixture restart checksums after final image replacement |
| Local browser regression | PASS | TXT/PNG/PDF/multipart checksums/previews, corruption rejection/retry, delayed-hash Pause/Resume, synthetic disconnected state, 320px layout |
| Live API qualification | PASS | Small/multipart/resume/Range/restart, unpublished failure rejection, exact bytes/SHA-256 |
| Live browser qualification | PASS | 135,168-byte TXT upload/preview/download; completed queue and observed Telegram connection |
| Remote ciphertext inspection | PASS | Six tracked chunks: exact HD01 lengths, distinct sampled nonces, identity AAD, AES-GCM and chunk hashes |
| Abrupt idle-process recovery | PASS | Owned server SIGKILL, persisted-session restart, all five fixture hashes equal; unauthenticated content/diagnostics returned 401 |

## Evidence and measurements

Ignored evidence: `output/phase2/telegram-qualification.json`, `live-browser-checksums.json`, `live-ciphertext-probe.txt`, `live-private-permissions.json`, `live-idle-crash.json`, `docker-qualification.json`, `final-docker-runtime.json`; screenshot `output/playwright/harbor-live-telegram.png`. Browser fixture SHA-256: `a92d9aa2f1854e73ba9aa6d4540fe2a9a525710c1efaeb0b66819b02c353b631`. Multipart SHA-256: `e609159371697ca949b503bcabf27ddfde224ea08f00dcdf5efa6328da1f9e05`.

Measured multipart upload API rate **0.454963485 MiB/s**, download **1.110956826 MiB/s**, graceful-restart download **0.885261124 MiB/s**. Sampled maximum RSS **45.25 MiB** and CPU **14.4%** across recorded runner windows; not lifetime peaks. One HTTP transfer at a time, two MTProto upload workers, 4 MiB application chunks. Retry snapshots were zero in sampled process lifetimes; counters reset on restart. Full per-file latency and sampling method: [performance report](PERFORMANCE_REPORT.md).

## Reproduce and limits

1. Keep dedicated account/channel, original key/session and disabled auto-delete. Run terminal-only `telegram-setup-qr` only if session needs authorization.
2. Start tagged server with `DRIVE_STORAGE=telegram DRIVE_ADDR=127.0.0.1:8081 ./bin/drive-telegram`, using existing private `.env`. Do not switch an existing local drive to another adapter.
3. Run `scripts/telegram_qualification.py --live --phase upload`, then restart/resume, restart/restart-check, with one new ignored state path and private owner-password file. Existing qualification state must resume rather than duplicate the upload phase.
4. Perform actual frontend upload/download and compare bytes/hash separately. Never upload personal content for qualification or broadly delete channel messages.

**NOT TESTED:** live Telegram inside Docker, actual cross-DC document storage, induced live connection loss/flood waits, crash during an in-flight Telegram request/SQLite commit, sustained throughput and true lifetime peak RAM. Synthetic retry/routing tests and host live authorization do not substitute for these paths. Compose uses a separate `/data` volume; authenticate there separately rather than copying a live host session.

Remaining limits: remote completion re-reads every chunk for whole-file verification; crash between remote creation and manifest commit can leave encrypted orphan; broad garbage collection deferred. SQLite metadata is readable to host owner. No preexisting remote object was deleted. See [reliability](RELIABILITY_REPORT.md), [security](SECURITY_REVIEW.md), [operation](../README.md).
