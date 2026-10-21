# Phase 5 deployment qualification

Scope: private, single-owner, loopback-only Docker deployment. Tests use a fresh local-adapter volume and generated fixtures. Original host Telegram process, original Docker project/volume, real account session and existing files are excluded from this workstream.

## Changes

- Go builder pinned to 1.26.10; runtime includes `/app/drive` and `/app/harbor-recovery`.
- Distroless non-root UID/GID 65532, read-only root filesystem, all Linux capabilities dropped, `no-new-privileges`, private persistent `/data`, bounded `noexec,nosuid,nodev` tmpfs.
- Compose publishes only `127.0.0.1:${DRIVE_PORT:-8080}:8080`. `DRIVE_ALLOW_REMOTE_HTTP=true` explicitly permits the internal container listener; it does not change the host publication address.
- Runtime limits: 2 CPUs, 768 MiB RAM, 128 PIDs. These are default bounds, not a demonstrated maximum workload.
- Go process receives SIGTERM directly. Application shutdown allows 15 seconds; Compose allows 20 seconds. Copy worker is joined before database/storage closure.
- Image disables implicit dotenv loading. Compose still performs explicit server-side configuration substitution and never forwards host filesystem secret paths into the container.
- `/health` is liveness; `/ready` is a minimal DB/transport readiness gate. Neither discloses account, channel, credentials or local paths. Docker liveness does not force restarts during a temporary Telegram outage. Authenticated diagnostics provide detailed transport state.

## Legacy volume permissions

**Verified defect:** the previous empty-directory `COPY` created `/data` as 0755 despite the builder source's 0700. A Phase 4 installation starts, but Phase 5's private-directory guard rejects this volume. The rejected isolated upgrade left data intact.

Image staging now copies a parent containing a real `/data` child, preserving directory permissions on a fresh volume. Existing volumes require an explicit offline owner-permission migration before upgrade: stop Harbor, preserve a verified encrypted backup, then change only the volume root's mode to 0700. Do not replace keys, reinitialize metadata, widen permissions, or weaken the startup guard. The qualification harness performs this migration only on its own newly generated, stopped local volume.

## Reproducible local qualification

```sh
docker compose --env-file /dev/null -p harbor-phase5-ops config --quiet
docker compose --env-file /dev/null -p harbor-phase5-ops build
python3 -m unittest discover -s scripts -p 'phase5_deployment_check_test.py'
python3 scripts/phase5_deployment_check.py \
  --candidate-image harbor-phase5-ops-drive:latest \
  --previous-image harbor-phase4-drive:latest
```

The process harness refuses an existing test project or volume. It disables project `.env` and strips inherited application variables, selects the local adapter explicitly, uses loopback 8092, and never imports real data. Four generated fixtures (TXT, PNG, PDF and multipart binary), nested folders, a favorite and Trash state are created through authenticated APIs.

Observed sequence: retained Phase 4 image → Phase 5 upgrade → clean restart → container recreation → Phase 4 rollback → Phase 5 upgrade. Every transition checks container/image identity, same named volume, byte equality/SHA-256, hierarchy, favorites, Trash, unauthenticated rejection, content-key fingerprint and runtime security settings. Stopped SQLite snapshots check `quick_check`, schema version and persisted row counts. Generated owner-password copies are removed; test containers/networks are stopped and removed; isolated encrypted fixture volume is retained.

## Evidence

Final frozen-source qualification: **PASS**, October 21, 2026. The complete six-stage suite was rerun after the final transfer-status and keyboard-focus UI fixes; all server build stages remained cached. Runtime is the tagged Telegram-capable image, with local adapter selected for this isolated workload. Final image: `sha256:d28e2d2a9907dd076cc5278fec9b18e23c966c96903207b06aabe70dfac5011d`; image size 51,073,203 bytes (48.71 MiB).

| Check | Result |
|---|---|
| Compose parse | PASS |
| Synthetic qualification guards | PASS — 3 tests |
| Initial legacy-volume upgrade guard | PASS — mode 0755 rejected without data replacement |
| Fresh-volume private mode | PASS — directory 0700; generated secret files 0600 |
| Tagged candidate build | PASS — Go 1.26.10, MTProto-enabled server and standalone recovery CLI |
| Local upgrade/restart/recreation/rollback SHA-256 | PASS — all six stages; four files, 4,260,662 bytes, five encrypted chunks |
| Final graceful shutdown / SQLite integrity | PASS — exit 0, no OOM; `quick_check=ok`, schema 0→1; 8 nodes / 4 uploads / 5 chunks unchanged |
| Offline container backup / verify / fresh restore | PASS — final image CLI, no network, fresh private destination |
| Live Telegram inside final Phase 5 image | NOT TESTED by this workstream |
| Public deployment / real Safari / real iOS | NOT TESTED |

Final evidence: `output/phase5/deployment/evidence.json`, `docker-build-qualified-final-a11y.log`, `docker-qualification-final-a11y.log`; preliminary run and permission-rejection evidence retained separately. Four generated SHA-256 values match after every transition; root key fingerprint stays unchanged, and nested hierarchy, favorite, Trash and private-access checks pass throughout. Container recovery creates, authenticates and restores a complete encrypted local archive while the source application is stopped. The temporary recovery volume and backup password are removed afterward. Evidence logs stay in ignored `output/phase5/`. Actual account credentials, session files, keys, password copies and private backups never belong in Git or reports.

For another clean run, remove only the generated stopped `harbor-phase5-ops_drive-data` test volume and select a new `--output` directory. The harness intentionally refuses reuse; it never removes an existing application volume automatically.

## Operational limits

Rollback evidence covers only the additive Phase 4 → Phase 5 migration. Stop active transfers/copy jobs and make a verified encrypted backup before upgrades. Arbitrary future schema downgrades are unsupported; the current server rejects a newer schema. A retained old image does not replace a validated data recovery plan and may contain already fixed dependency vulnerabilities.

Do not use `docker compose down -v` during normal upgrades/recovery. Persistent volumes hold the only local content key, owner state, SQLite metadata and default Telegram authorization unless separately backed up. Copying metadata alone does not provide disaster recovery.

HTTPS reverse-proxy setup is documented separately by the coordinator. It must preserve same-origin behavior, pass streaming/Range requests, disable proxy buffering where needed and enable Secure cookies before deliberate remote access. This qualification does not expose Harbor publicly, assert internet-production readiness, provide independent audit, guarantee Telegram durability or establish live cross-datacenter coverage.
