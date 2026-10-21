# Harbor — Phase 3 Docker qualification

Date: 2026-10-21, Asia/Phnom_Penh. **PHASE3_DOCKER = PASS** for real Telegram storage, session persistence, container restart/recreation and integrity. Existing host drive and port-8080 local container stayed outside this deployment. Actual cross-datacenter retrieval remains **NOT TESTED**.

## Deployment boundaries

Coordinator used Compose project `harbor-phase3-tg`, loopback port 8084 and new named volume `harbor-phase3-tg_drive-data`. Host Telegram process stopped before the complete data directory was cloned; host and Docker never concurrently used that MTProto authorization. Import targeted only the new volume. Cloned metadata was not written back over the host drive. Stop Docker before restarting the host. Generated remote qualification objects remain tracked and retained; no preexisting Telegram message was deleted.

Production Dockerfile and Compose settings need no change: tagged MTProto binary, distroless UID 65532, writable `/data` volume, read-only root, 128 MiB `/tmp` tmpfs with `noexec,nosuid`, all capabilities dropped, `no-new-privileges`, loopback-only publish and 20-second stop grace period. Direct host paths are not inherited inside the container. Default session, SQLite, storage binding and encryption key all live under persistent `/data`.

`/health` is public liveness, not proof of a current Telegram connection. Startup resolves/validates the account/channel before opening the HTTP listener; later transport loss can leave liveness healthy. Authenticated `/api/v1/diagnostics` supplies cached connection state and checks SQLite. The qualification checker combines both with real byte roundtrips. No public identity-bearing readiness endpoint was added.

## Executed evidence

| Check | Result | Evidence |
| --- | --- | --- |
| Runtime/Compose source review | PASS | Existing isolation, non-root, read-only root, capability drop and persistent path verified from source |
| Checker synthetic security/readiness tests | PASS | `python3 -W error::ResourceWarning -m unittest discover -s scripts -p 'phase3_docker_check_test.py'`; three tests cover approved fields only, unsafe runtime rejection, anonymous denial, temporary-session logout, password permissions and transition identity |
| Numeric archive owner import | PASS, synthetic Docker | Fresh stopped container and new disposable volume, no app process; generated tar imported with `docker cp --archive`, then copied back as tar. `/data` UID/GID 65532 mode 0700 and generated file UID/GID 65532 mode 0600 verified. Probe container/volume removed |
| Native inspect-template redaction | PASS, synthetic Docker | Fresh stopped container supplied a synthetic API hash; selected-field template parsed correctly, returned env equality booleans and excluded both credential name/value. Probe container/volume removed |
| Real MTProto startup inside Docker | PASS, live | Persisted authorization accepted; authenticated diagnostics reported `adapter=telegram`, `connected`, `database=ok`, `AES-256-GCM`, 4 MiB chunks; `docker-initial.json` |
| Real encrypted upload/download and SHA-256 | PASS, live | Generated TXT 68 bytes, PNG 121 bytes, PDF 612 bytes and multipart 4,259,861 bytes; every download byte count/SHA-256 matched; `docker-transfer.json` |
| Multipart resume / incomplete publication | PASS, live | First 4 MiB chunk retained, incomplete finalization rejected, remaining 65,557 bytes resumed; complete reconstruction and cross-chunk Range matched; `docker-transfer.json` |
| Container restart and retained retrieval | PASS, live | Same container ID, start changed from 09:49:01 to 09:50:38 local, same image/volume; all four new fixtures retrieved with SHA-256 equality; `docker-restart.json`, `docker-after-restart.json` |
| Container recreation and retained retrieval | PASS, live | New container ID at 09:50:58 local, same image/volume; all four new fixtures retrieved again; `docker-recreate.json`, `docker-transfer.json` |
| Older manifests and encrypted content | PASS, live | After recreation, four Phase 2 runner fixtures and original Phase 3 host benchmark files of 1/10/100 MiB retrieved with exact SHA-256; `docker-phase2-files.json`, `docker-older-files.json` |
| Controlled multipart process crash | PASS, live HTTP interruption | SIGKILL during HTTP chunk-body read after 1,024 of 65,557 tail bytes sent; same container restarted, chunk 0 retained and node unpublished; `docker-crash.json`. This does not establish a crash during an in-flight Telegram write or SQLite commit |
| Runtime security / anonymous rejection | PASS, live | All 12 isolated runtime checks passed before/after lifecycle transitions; unauthenticated diagnostics/content returned 401; temporary checker login revoked |
| Live cross-datacenter retrieval | NOT TESTED | Docker success does not establish cross-DC document placement |

All named evidence files above are under ignored `output/phase3/`; no credentials, channel identity, session bytes or encryption keys are included. Initial/restart/recreate all used image `sha256:0bd385843f83c4c43911c44c9929d3cbaba5ea480e034aa828529713bf7ef6ed`. Three sanitized snapshots independently confirmed the same named volume and expected process/security settings. Connection retry count was zero in these snapshots; it resets per process and does not prove absence of retries throughout execution.

The first readiness check immediately after a restart rejected the still-starting health state. Coordinator waited with Compose `up --wait`, then reran successfully. No checker assertion or health rule was weakened. Browser qualification and induced live network interruption are recorded separately; neither is inferred from these Docker results.

## Measured workload

One HTTP transfer at a time, 4 MiB application chunks, configured MTProto uploader worker ceiling two. gotd's small-document loop sends parts sequentially for these default chunks; effective part concurrency is one. Multipart size was 4,259,861 bytes, SHA-256 `e609159371697ca949b503bcabf27ddfde224ea08f00dcdf5efa6328da1f9e05`.

| Multipart operation | Seconds | MiB/s |
| --- | ---: | ---: |
| Upload API, including whole-file finalization | 3.260898 | 1.245829 |
| Finalization portion | 1.213440 | — |
| Initial complete download | 0.907329 | 4.477449 |
| Complete download after restart | 1.252037 | 3.244727 |
| Complete download after recreation | 1.015291 | 4.001337 |

After recreation, older 1/10/100 MiB files downloaded in 0.280764/2.169102/22.414804 seconds (3.561714/4.610204/4.461337 MiB/s). Original baseline timing/process samples remain intact; additional results append under `rechecks`. Docker CPU/RSS were **NOT TESTED** in these runner windows (`--server-pid` omitted). These are measured individual transfers, not a controlled host-versus-Docker speed comparison or sustained-throughput claim.

## Safe import and reproducibility

The synthetic owner probe established an import path without a root helper image or running process mounting host secrets. Create a stopped seed container from the Harbor image with networking disabled. Stream a tar whose root entry is `data`, with UID/GID 65532, empty owner/group names and private source modes, to `docker cp --archive - <seed>:/`. The seed must not have a read-only root during this daemon-side copy and must never start. Remove only that owned seed after import; actual Compose service keeps its read-only setting.

Importing archive `.` directly into `<seed>:/data` preserved file UID/GID but left destination mode 0755 in the first synthetic probe; that sequence is unsuitable when preserving the 0700 directory. Copying the `data` entry into `<seed>:/` with a stopped writable seed passed. No ownership helper or additional production image was needed.

After each externally performed lifecycle step, run:

```sh
python3 scripts/phase3_docker_check.py \
  --password-file /private/path/owner-password \
  --output output/phase3/docker-initial.json

python3 scripts/phase3_docker_check.py \
  --password-file /private/path/owner-password \
  --previous output/phase3/docker-initial.json --transition restart \
  --output output/phase3/docker-restart.json

python3 scripts/phase3_docker_check.py \
  --password-file /private/path/owner-password \
  --previous output/phase3/docker-restart.json --transition recreate \
  --output output/phase3/docker-recreate.json
```

The checker is read-only except private evidence files and temporary HTTP login/logout. It accepts only the isolated project, loopback port, expected volume and bounded diagnostics. Its Docker template never returns credential environment values, arbitrary labels, logs or host mount paths. Evidence includes container/image IDs, start timestamp, volume identity, security checks and allowlisted readiness metrics. A lifecycle identity check alone does not prove file persistence; repeat `scripts/telegram_qualification.py --live --phase restart-check` after each transition.

## Security and limits

Secrets remain in private files/volume and server environment. Docker administrators can inspect container environment; raw `docker inspect`, Compose-rendered configuration, session bytes and logs must not enter reports. The checker reads only a private regular owner-password file, rejects symlinks/permissive files, stores no password/cookie and revokes its temporary session. Live session/key/password metadata independently passed regular-file mode 0600 and UID/GID 65532 checks (`docker-private-modes.json`). Temporary Docker password and browser state were removed; browser session revoked. Isolated service stopped before original host resumed; retained test volume holds clone-only fixture metadata and must never merge into the host drive.

Network detach/reconnect, four early-closed downloads and real browser upload/preview/verified save subsequently passed; details in Phase 3 reliability/final reports. An additional post-network checker reached the existing login limit and returned HTTP 429. No bypass/retry occurred; preceding authenticated network recovery and lifecycle evidence remain independently recorded.

This report qualifies real Docker MTProto operations independently of Phase 2's local Docker checks. No Dockerfile/Compose production change was required. Live cross-DC document placement, induced Telegram rate restrictions and process termination during a Telegram RPC/SQLite commit remain unqualified here. Host must stay stopped while this cloned authorization runs; preserve the isolated volume for clone-only fixture manifests.
