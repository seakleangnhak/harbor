# Harbor operations and recovery

## Offline storage diagnostics and scoped cleanup

Development builds include `storage-diagnostics` and `storage-cleanup`.
Published v1.2.1 does not include these commands. See
[Milestone B](MILESTONE_B_FINAL_REPORT.md) for tested build identity and limits.

1. Finish or cancel active work, take a verified encrypted backup, and stop Harbor.
2. Run diagnostics with the same image, protected volume and runtime user. For Compose:

   ```sh
   umask 077
   docker compose stop drive
   docker compose run --rm --no-deps drive storage-diagnostics > storage-plan.json
   ```

3. Require a successful command exit. Review `cleanup_blocked`, candidates,
   reclaimable bytes and recoverable/missing counts. Unknown local/remote
   candidates are informational and are not deletion targets.
4. Only after reviewing the current plan, substitute its exact `plan_sha256`:

   ```sh
   docker compose run --rm --no-deps drive storage-cleanup --confirm PLAN_SHA256
   ```

5. Inspect the result and private `storage-cleanup.jsonl` audit, rerun diagnostics,
   then start Harbor with `docker compose up -d drive`.

Diagnostics open existing SQLite metadata without logical mutation; SQLite may
update WAL/SHM coordination sidecars. No key, Telegram authorization or network
connection is needed. Cleanup refuses stale plans or an active drive lock.
Interruption requires a fresh diagnostic/confirmation; never reuse an old plan
or remove unknown files manually. These commands cannot repair missing remote
chunks, infer abandonment from age, enumerate Telegram or reclaim unknown
remote objects. Unpublished recovery staging outside the data directory stays
an explicit operator investigation. The last successful content-integrity
timestamp remains unknown because current storage does not persist it.

Scope: single-owner personal use on a trusted local host. This runbook does not establish public internet readiness, independent security audit, guaranteed Telegram durability, or live cross-datacenter coverage. Current qualification belongs in the [v1.0 release report](PHASE6_FINAL_REPORT.md), [release checklist](RELEASE_CHECKLIST.md) and [known limitations](KNOWN_LIMITATIONS.md). [Recovery quickstart](RECOVERY_QUICKSTART.md) gives clean-environment/off-host steps; Phase 5 reports remain historical evidence.

## Protected installation

Keep the data directory, backup directory, recovery parent, and secret-file parents private (0700); sensitive files must be regular files with mode 0600. Do not use symlinks for keys, sessions, passwords, or SQLite files. Preserve `drive.db` together with its WAL state, `storage.json`, the original content key, and Telegram authorization. Startup rejects missing metadata on an already bound drive; `init` is not a repair operation.

Build trusted binaries with Go 1.26.10 or newer compatible Go:

```sh
go build -tags telegram -o bin/drive ./cmd/drive
go build -o bin/harbor-recovery ./cmd/harbor-recovery
```

Direct execution loads a private literal `.env` by default; existing process variables override it. Select a private file with `DRIVE_ENV_FILE=/absolute/private/harbor.env`, or disable loading with `DRIVE_ENV_FILE=-`. Avoid inherited overrides during recovery. Docker disables implicit loading and uses explicit Compose configuration; see the [deployment report](PHASE5_DEPLOYMENT_REPORT.md) for volume, image, and permission procedures.

Store credentials only on the server. Use a dedicated Telegram account and owned private storage channel with auto-delete disabled. Never switch adapters, replace `content.key`, delete WAL files, or reinitialize a bound drive to bypass an error. Content-key loss makes existing encrypted chunks unrecoverable.

## Encrypted offline backup

Backups contain private metadata, content key, account authorization, and allowlisted configuration. Use an independent high-entropy backup password, held separately from the archive and drive. Losing it makes the archive unrecoverable. Password values must never appear in command arguments, environment variables, chat, or logs.

1. Finish transfers and settle copy jobs where possible. Stop Harbor gracefully; verify the process/container has exited. The backup command takes Harbor's exclusive data lock and refuses a running installation.
2. Choose an existing private backup directory outside the source data directory and a new archive filename. Create private parents if needed with `install -d -m 0700 /absolute/private/backups`.
3. Run backup from a local Terminal. The password prompt is hidden and asks for confirmation:

   ```sh
   bin/harbor-recovery backup \
     --data-dir /absolute/private/harbor-data \
     --output /absolute/private/backups/harbor-unique.harbor
   ```

4. Browser-onboarded drives automatically load the private `<data>/setup` bundle's configuration, owner password and Telegram session; omit `--config-file` instead of supplying an unrelated legacy file. Legacy Telegram installations require `--config-file /absolute/private/harbor.env` describing their effective runtime configuration; no implicit dotenv loading occurs. The supplied account/channel configuration must match the existing storage binding. Supply `--key-file`, `--session-file`, and/or `--owner-password-file` with explicit private paths when those materials are stored outside the data directory. If no saved owner-password file exists, the owner must still know the existing login password after recovery.
5. Verify the archive, then restart the original installation:

   ```sh
   bin/harbor-recovery verify \
     --archive /absolute/private/backups/harbor-unique.harbor
   ```

For unattended execution, every recovery command accepts `--password-file /absolute/private/backup-password`; the file must be regular, private, and contain 16–4096 password bytes. Provision it locally through a protected mechanism. It must not reside inside an archive or repository. The CLI also accepts interactive passwords of that length.

The version-2 archive uses scrypt and AES-256-GCM with authenticated record order/end marker and SHA-256 member manifests. SQLite's supported backup API captures committed WAL contents. No plaintext backup archive or SQLite snapshot is written to disk. Backup verifies the encrypted archive before publishing it without replacing an existing path. Legacy plaintext tar backups are rejected; protect any existing copies and handle migration separately.

Contents include SQLite, ordered chunk manifests, original key, storage binding, allowlisted recovery configuration, optional owner-password file, and Telegram session/configuration. Local-adapter backups also include all referenced encrypted blobs. Unfinished browser parts and disposable cache are excluded; restoring incomplete uploads can require reselecting original files. A restart on the original intact volume can reuse its accepted encrypted parts. **Telegram archives do not copy remote ciphertext.** A deleted channel, lost account, or missing remote message can make content unavailable despite a valid archive. Maintain an independent, encrypted off-provider content backup; Harbor does not automate that mirror.

Current ceilings: 256 MiB SQLite snapshot, 64 MiB per manifest, 250,000 archive members, and 1 TiB uncompressed payload. The snapshot is held in memory; peak memory can exceed twice database size and has not been qualified at the ceiling. Large metadata requires a reviewed backup change rather than raising limits blindly.

## Fresh-environment restore drill

Use an isolated directory and port. Restore never overwrites an existing installation or even an existing empty directory.

1. Run `verify` with the chosen archive/password. Keep the original data directory unchanged. Stop the original instance before any recovered instance uses its copied Telegram authorization; two processes must never use copies concurrently.
2. Create a private recovery parent, then restore into a **nonexistent** child:

   ```sh
   install -d -m 0700 /absolute/private/recovery
   bin/harbor-recovery restore \
     --archive /absolute/private/backups/harbor-unique.harbor \
     --destination /absolute/private/recovery/harbor-restored
   ```

3. Start the recovered installation using its canonical configuration and an unused loopback port. Ensure stale process environment does not override recovered key/session/configuration paths:

   ```sh
   /usr/bin/env -i PATH=/usr/bin:/bin \
     DRIVE_ENV_FILE=/absolute/private/recovery/harbor-restored/recovery.env \
     DRIVE_ADDR=127.0.0.1:8093 DRIVE_STATIC_DIR=/absolute/harbor/web/dist \
     /absolute/harbor/bin/drive serve
   ```

4. Log in using the existing owner password. Restored HTTP sessions are revoked; original installation sessions are unchanged. Verify nested folders, favorites, names, Trash, and expected transfer/copy-job state.
5. Download representative small and multipart files and compare full SHA-256 against independently retained fixture hashes. On macOS, `shasum -a 256 /path/to/download` computes the digest. Successful archive verification alone does not prove current Telegram access or content availability.
6. Stop and restart the recovered instance and repeat downloads. Stop it fully before resuming the original. Keep recovered metadata separate; never copy it back over the original database.

Restore authenticates every member, checks required materials/binding/SQLite, validates referenced local blobs, and publishes the directory atomically without replacement. Wrong passwords, corruption, incomplete archives, and missing keys fail without publishing a destination. Graceful cancellation removes staging. SIGKILL or power loss can leave private `.harbor-restore-*` staging directories: confirm no recovery process is active before removing only that abandoned staging directory, then retry into a new destination. Never start Harbor from incomplete staging.

Restored `recovery.env` uses internal canonical paths, `127.0.0.1:8080`, and local HTTP cookie settings. Reapply documented HTTPS/proxy policy before any deliberate remote exposure. A structurally valid saved Telegram session can still be expired or revoked; use the supported local `telegram-setup-qr` Terminal flow when reauthorization is necessary. Never send QR images, codes, or passwords through the browser/chat.

## Integrity and copy recovery

Stop Harbor before offline checks; commands use the same exclusive lock:

```sh
DRIVE_ENV_FILE=/absolute/private/harbor.env bin/drive integrity
DRIVE_ENV_FILE=/absolute/private/harbor.env bin/drive integrity-content
```

`integrity` checks SQLite, node parents/cycles/depth, published file/manifests, chunk order/geometry/hash metadata, and unpublished manifests. It performs no blob fetch, but the current Telegram CLI still initializes the adapter and therefore needs Telegram connectivity at startup. `integrity-content` additionally reads published and trashed files serially, verifying chunk AEAD and whole-file SHA-256. It can generate substantial Telegram traffic; schedule controlled checks rather than frequent full scans. Nonzero exit requires investigation; neither command repairs or deletes data. Keep JSON reports private even though issue codes contain no credentials.

Folder copies run as durable server jobs: at most 100 top-level selections, 10,000 planned entries, depth 512 including the destination, and eight unfinished jobs. Processing is serial and chunk-bounded. Captured names/favorites/hierarchy stay immutable; sources must remain accessible until copied. Copied metadata becomes visible together after verification. File-only browser copies retain the existing client chunk flow.

Use the UI's retry/cancel controls or authenticated copy-job APIs. A retry reuses committed chunks. For `destination_conflict`, resolve the conflicting name or cancel and create a new operation. Cancellation persists and cleans only generated staging; `cleanup_failed` needs explicit retry. Originals remain intact. Failed/cancelling jobs may retain encrypted remote objects, so investigate the recorded operation rather than deleting messages broadly. Automatic remote garbage collection is not provided; local orphan-candidate enumeration is a library facility, not a cleanup CLI.

If metadata is missing or corrupt, stop and preserve the original directory. Restore a verified archive elsewhere. Never delete SQLite/WAL, replace keys, change adapter, or run `init` as an attempted recovery.

## Health, HTTPS, and shutdown

- `GET /health`: unauthenticated liveness. It does not prove database/content integrity.
- `GET /ready`: minimal database ping and, for Telegram, cached transport state. Returns 200 when ready or 503 when unready. It does not fetch a remote file or guarantee channel durability.
- Authenticated diagnostics: safe cached connection/retry state. Investigate outages before repeated restarts; Docker uses liveness to avoid outage-driven restart storms.

Keep host publication on localhost. Compose's explicit `DRIVE_ALLOW_REMOTE_HTTP=true` permits its internal listener while the host port remains bound to `127.0.0.1`. Do not treat that opt-in as permission to expose plaintext HTTP.

For an intentionally configured HTTPS reverse proxy:

1. Keep Harbor on a trusted private/loopback connection and terminate HTTPS at the proxy.
2. Set `DRIVE_SECURE_COOKIES=true` in protected server configuration before exposure. This marks cookies; it does not provide TLS itself.
3. Preserve same origin, Host/Origin, cookies, methods, HEAD, Range and 206 responses. Support streaming/backpressure, appropriate body/time limits, and disable proxy buffering where needed. Do not add permissive CORS or trust arbitrary forwarded headers.
4. Recheck login/logout, CSRF rejection, uploads, ranged previews, and downloads through that exact origin. Login throttling uses the actual TCP peer, so clients behind one proxy share its limit.

SIGTERM allows up to 15 seconds of HTTP shutdown and joins the copy worker before closing storage; Compose grants 20 seconds. Prefer graceful stop. Do not run host and container instances against the same data directory or copied Telegram authorization.

## Upgrade and rollback

1. Record the trusted current image/binary and candidate version. Read migration/release notes. Finish or cancel active uploads/copy jobs and verify no unfinished work remains.
2. Stop Harbor, create and verify a complete encrypted backup, and retain the prior trusted image/binary. Use the [deployment report](PHASE5_DEPLOYMENT_REPORT.md) for tested Docker upgrade/recreation commands and legacy-volume permission handling.
3. Start one candidate instance using the protected persistent data volume. Check readiness, owner authentication, old-file SHA-256, hierarchy/Trash, and restart behavior before accepting the upgrade.
4. Before rollback, finish or cancel candidate jobs and confirm they drain, then stop the candidate. Preserve newer file references in a verified post-upgrade archive. Restore the pre-upgrade archive into a fresh volume with matching old configuration/image; retain the newer installation/archive until post-snapshot files have an explicit recovery/export plan. Never run v1.2.1 against pending background queue state, overwrite candidate metadata with the older snapshot, or run copied Telegram authorizations concurrently. Current cycle evidence and exact images: [v1.3 upgrade/rollback report](V1.3_UPGRADE_ROLLBACK_REPORT.md). Arbitrary downgrades remain unsupported.

Never use `docker compose down -v`, delete/replace the persistent volume, or reinitialize data during an upgrade. A previous image may contain fixed vulnerabilities; prefer a reviewed forward fix when safe. A retained image alone is not a recovery plan.

## Scheduling and incident response

No backup scheduler or retention deletion is installed automatically. An optional local launchd/systemd timer or cron wrapper should stop the sole instance, acquire the offline backup, verify it, and restart in a trap/finally path even on failure. Use built binaries and explicit private config/password paths, dated new archive names, private logs with no secret content, and alerts for nonzero exit. Retain only explicitly designated archives; test retention on generated fixtures first. Keep a recent offline/off-host encrypted copy and perform periodic fresh-environment restore/download drills. Archive verification and a real restore drill are separate milestones.

For suspected compromise, contain exposure and stop Harbor first. Preserve protected offline state/evidence; avoid cleanup that destroys recoverability.

- **Telegram session theft:** revoke the affected authorization using trusted Telegram Settings → Devices, then reauthenticate the dedicated account through local Terminal. Changing app credentials does not revoke a stolen MTProto authorization. Replace protected recovery archives after reauthorization; old archives contain the old session.
- **Owner/browser-session theft:** logout revokes the current HTTP token. Settings → Account supports a current-password-verified password change and atomically revokes all HTTP sessions. Logout revokes only the current session. Treat wider host/browser compromise as affecting protected host state as well; a supported fresh restore also revokes restored HTTP sessions. Do not manipulate the original database blindly.
- **Content-key compromise:** no in-place key rotation/re-encryption command exists. Replacing the key breaks existing binding/decryption. A migration into a new verified drive requires controlled re-encryption and validation; it cannot undo prior disclosure.
- **Lost key or backup password:** no bypass exists. Use independently protected original recovery materials if available.
- **Account/channel loss:** restore independent off-provider content. Metadata/key/session archives alone cannot restore deleted Telegram ciphertext.

Track fixes and rerun affected authentication, integrity, recovery, and download checks before returning to normal operation. See the [security report](PHASE5_SECURITY_REPORT.md), [backup report](PHASE5_BACKUP_RESTORE_REPORT.md), and [storage-integrity report](PHASE5_STORAGE_INTEGRITY_REPORT.md) for evidence and remaining limits.

## Verified v1.0 image operations

Verify `SHA256SUMS` before loading a local release archive. `release.json` identifies committed source and the qualified image. Use the release image with a Compose image-only override and `--no-build --pull never`; ordinary rebuilding can change labels/provenance even when runtime files match. Clear inherited `DRIVE_`, `TELEGRAM_` and `COMPOSE_` overrides, then select an explicit private env file and the intended project/volume. Do not print resolved Compose configuration or container environment: it contains credentials.

Phase 6 qualifies real Telegram in the exact exported image: multipart resume, restart, persistent-volume recreation and encrypted archive-only recovery all passed. Existing original deployments were preserved. See release report for image digest, commands, timings and scope. Only linux/arm64 qualified; no public exposure performed. Preserve the prior trusted image and verified offline backup before changing the sole deployment.

## Open-source image updates

Compose targets `ghcr.io/seakleangnhak/harbor:1.2.1` by default. Use `--no-build --pull always` to pull the published release, or `--build` to build the checkout locally under the configured `HARBOR_IMAGE` reference; a local build does not push anything. [Publishing guide](GHCR_PUBLISHING_GUIDE.md) explains protected tags, image digests and approval requirements. Confirm completed release qualification, then set `HARBOR_IMAGE` to its independently verified immutable digest, retain the same project/service/volume identity, create and verify an encrypted offline backup, and follow the upgrade/rollback procedure above. Never run initialization against an existing bound volume or change a key to make an upgrade start. Published attestations are build metadata, not independent security certification.

For redistribution retain `LICENSE`, `THIRD_PARTY_NOTICES.md`, browser license/font notices and bundled `fonts-liberation-1.07.4-source.tar.gz`. Runtime images already contain these files. Supply configuration only at container startup, never through build arguments. Private package access, GitHub/GHCR delivery and public deployment need separate qualification.

## Local 1.3.0-rc.1 qualification

The unpublished candidate is described in [candidate/upgrade and rollback guide](V1.3_RELEASE_CANDIDATE.md)
and [Milestone B results](MILESTONE_B_FINAL_REPORT.md). Published Compose pull
default remains v1.2.1. For a local Compose build, explicitly set
`HARBOR_IMAGE=harbor:1.3.0-rc.1` so the candidate does not receive a stable local
tag; do not run it against owner data before approval and verified offline backup.
No tag, registry publication or deployment upgrade is performed by qualification.

The 768 MiB runtime fixture completes under page-cache reclaim; it does not
establish spare memory or justify more workers, queue slots or preview concurrency.
100 MiB authenticated chunks remain, so a cold tiny range can wait about 22 seconds
on the tested Telegram connection. Warm locality helps; two-entry cache thrash
can add I/O. Keep free persistent-disk capacity for encrypted staging, cache,
SQLite/WAL and backups; there is no application-wide disk quota. Run offline
maintenance only with a reviewed current plan; unknown/recoverable data is retained.
