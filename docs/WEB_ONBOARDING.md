# Browser onboarding

Harbor v1.1.0 can initialize a fresh drive entirely through its web UI: create your owner password, choose storage, and authorize Telegram with QR and optional two-step verification. Existing initialized drives retain their login, files, key, session and storage binding. See the [onboarding UI review](ONBOARDING_UI_REVIEW.md) for current light/dark screenshots and scoped browser qualification.

The v1.2.1 release source uses `ghcr.io/seakleangnhak/harbor:1.2.1`; use its release report to confirm completed image qualification and the immutable digest before deployment. Earlier image `1.0.1` retains terminal setup. Pulling the upstream public package needs no GHCR credentials; private forks may require package-read access.

## Start with Compose

The source Compose file builds the checkout. Run on a trusted machine:

```sh
export DRIVE_SETUP_TOKEN="$(openssl rand -hex 32)"
docker compose -p harbor-local up -d --build
printf 'Setup token: %s\n' "$DRIVE_SETUP_TOKEN"
```

Open [http://127.0.0.1:8080](http://127.0.0.1:8080). Paste the token into setup; do not put it in a URL, chat, screenshot or issue report. The token authorizes first-run setup, while your chosen owner password authorizes later sign-ins. No `init`, generated-password command, or terminal Telegram authentication is required.

With `--build`, Compose builds the checkout and tags it using `HARBOR_IMAGE`, which defaults to `ghcr.io/seakleangnhak/harbor:1.2.1`. This builds locally; it does not push an image. Compose binds host HTTP to loopback and persists `/data` in `harbor-local_drive-data`. The runtime is non-root with a read-only root filesystem, protected data directory, dropped capabilities, 2 CPU/768 MiB limits and a 512 MiB soft Go memory limit. Compilation needs more memory than the running service.

Keep the same Compose project name and volume on later starts:

```sh
docker compose -p harbor-local up -d
docker compose -p harbor-local ps
```

Do not remove the volume or use `down -v` to troubleshoot an existing drive. After successful setup, its token is no longer used; initialized metadata prevents reopening setup. A fresh installation without a valid token refuses startup.

## Run a container directly

Use the release image with a fresh named volume; no source build or terminal account setup is needed:

```sh
docker pull ghcr.io/seakleangnhak/harbor:1.2.1
export DRIVE_SETUP_TOKEN="$(openssl rand -hex 32)"
docker run -d --name harbor-web \
  --publish 127.0.0.1:8080:8080 \
  --env DRIVE_SETUP_TOKEN \
  --env DRIVE_ALLOW_REMOTE_HTTP=true \
  --mount source=harbor-web-data,target=/data \
  --read-only \
  --tmpfs /tmp:rw,noexec,nosuid,nodev,size=128m \
  --cap-drop ALL \
  --security-opt no-new-privileges:true \
  --memory 768m --cpus 2 --pids-limit 128 \
  --restart unless-stopped \
  ghcr.io/seakleangnhak/harbor:1.2.1
printf 'Setup token: %s\n' "$DRIVE_SETUP_TOKEN"
```

The image defaults to UID/GID `65532:65532`, includes its health-check command, and listens on container port 8080. `DRIVE_ALLOW_REMOTE_HTTP=true` allows the container's internal listener; the example still exposes HTTP only on host loopback. Keep `harbor-web-data` across container replacement. Do not mount an initialized drive into two Harbor processes. Prefer the immutable digest recorded after release qualification for long-lived deployments. To test source instead, build `docker build -t harbor:local .` and substitute `harbor:local` in the run command.

## Complete setup

1. **Owner account:** enter the deployment token, then choose and confirm a unique 12–72-byte password. Passwords cannot contain line breaks. Store the password in your password manager.
2. **Storage:** choose Telegram or local. Local stores encrypted blobs in the persistent volume. Telegram requires your own API ID/hash from [my.telegram.org/apps](https://my.telegram.org/apps), a dedicated account, and a private channel owned by that account. Disable channel auto-delete and linked discussion.
3. **Telegram authorization:** enter the current private invite link or a private channel message link. An expected numeric channel ID is optional. In the dedicated Telegram account, open **Settings → Devices → Link Desktop Device**, scan the displayed QR, and approve. Enter any two-step password only in the protected form. Treat the temporary QR as a login credential.
4. **Finish:** wait for private-channel verification, review the selected storage, and open Harbor. Setup saves once, then waits for the normal API to become ready. Sign in using your chosen owner password.

Telegram access hashes resolve server-side. Public channel usernames are rejected; Harbor does not join channels through an invite. The account must already own the selected channel. Setup verifies any supplied channel ID against the resolved target before storage can start.

The setup token expires for the current process after one hour. Restart an unfinished installation to begin again. An interrupted, unfinished authorization does not mark the drive initialized; resubmit owner/storage details after restart. A QR flow has its own three-minute timeout and can be cancelled. Avoid repeated Telegram authentication attempts after a flood-wait response.

## HTTPS and Dokploy

The browser permits credential entry only over HTTPS or loopback HTTP. For remote access, configure your existing reverse proxy or Dokploy domain with HTTPS and set `DRIVE_SECURE_COOKIES=true`. Keep one application replica and one protected persistent volume at `/data`; route to container port 8080.

Supply a random `DRIVE_SETUP_TOKEN` as a runtime secret/environment variable. Leave owner-password, storage and Telegram variables unset for a fresh browser-driven setup. For an existing initialized installation, preserve its effective variables instead: take an encrypted backup, keep the same volume, adapter, key and Telegram authorization, and stop the old process before replacement. The old Dokploy template hardcoded `DRIVE_STORAGE=telegram`; keep that setting explicitly when upgrading to the optional-variable template. Do not use `VITE_` variables, Docker build arguments, a query string or a public repository for credentials. Ensure proxy request-size/time limits accommodate 100 MiB application chunks and Telegram transfer latency.

The [Dokploy Compose template](../compose.dokploy.yaml) targets v1.2.1 and preserves external volume `harbor-data`. Follow [Dokploy deployment](DOKPLOY.md) for HTTPS, bootstrap-token setup and safe upgrades. This guide does not deploy a server or qualify public-internet security.

## Configuration and persistent state

`DRIVE_SETUP_TOKEN` must contain 32–256 non-space characters; `openssl rand -hex 32` produces a suitable value. Setup requests send it through `X-Harbor-Setup-Token`, never a URL. Token comparisons are constant-time, failed attempts are rate-limited, mutations require the same origin, and setup responses are not cached. Tokens, API inputs and QR material remain in component memory rather than browser persistence. Telegram two-step passwords are not saved.

After verification, Harbor writes a private pending bundle, syncs its files, and activates the directory with an atomic rename:

```text
/data/
  content.key
  drive.db
  storage.json
  setup/
    config.env
    owner-password
    telegram.session     # Telegram storage only
```

Directories require mode `0700`; secret files require `0600`. The saved config holds the adapter and Telegram API/channel configuration. On restart it fills unset environment values and selects its bundled owner-password/session paths. Keys remain outside SQLite. Do not copy this bundle into Git, browser assets, build contexts or unencrypted backups.

Process environment and a deliberately loaded private `.env` take priority over the saved bundle. During fresh setup, conflicting nonempty storage/password/Telegram overrides are rejected; `DRIVE_OWNER_PASSWORD_FILE`, `TELEGRAM_SESSION_FILE` and a legacy access-hash override also conflict with web setup. Remove conflicting overrides only for an uninitialized browser-driven installation. Preserve existing initialized installations' effective configuration and saved state. Existing drives fail closed if their key, metadata or immutable adapter/channel binding is missing or mismatched.

`/health` reports process liveness during setup. `/ready` returns 503 until normal storage/API startup completes. The public setup-status endpoint reveals only whether setup is required. Protected QR/status responses do not return saved API hashes, private channel selectors or session data. Once initialized, only the normal owner-authenticated API remains available.

## Backups and recovery

Stop Harbor before using its offline recovery tool. Browser-onboarded drives automatically supply the saved bundle's configuration, owner password and Telegram session to encrypted backup creation; do not pass an unrelated legacy `.env`:

```sh
go build -o bin/harbor-recovery ./cmd/harbor-recovery
mkdir -m 700 backups
bin/harbor-recovery backup --data-dir ./data-web \
  --output backups/harbor-web.harbor
bin/harbor-recovery verify --archive backups/harbor-web.harbor
```

This native example uses `./data-web`. For Docker, use the stopped volume and the recovery procedure in [Production operations](PRODUCTION_OPERATIONS.md); the container's `/data` is not the checkout's local data directory. Recovery packages the required materials in its existing portable archive format. Follow [Recovery quickstart](RECOVERY_QUICKSTART.md) for fresh-directory restoration and loading the restored `recovery.env`. Preserve your archive password separately and never run original/restored Telegram authorizations concurrently.

Telegram backups contain encrypted-file references, not an independent copy of Telegram content. Protect an off-provider content copy against account/channel loss.

## 100 MiB chunks

New uploads use **100 MiB plaintext chunks**, with an authenticated-encryption envelope added before Telegram upload. This is an application chunk size, not a 100 MiB file limit. The existing API file ceiling remains 1 TiB; Telegram's own account limits and availability still apply.

Existing 4 MiB files and incomplete uploads retain their stored chunk size and can still resume/download. No chunk migration or re-encryption of existing files occurs. New Telegram documents use bounded 512 KiB multipart requests. The default engine shares two active chunk-buffer slots across upload, finalization and download readers, with in-place authenticated encryption and decryption to limit copies. The runtime memory limit is not a guarantee that arbitrary transfer workloads fit every host.

Each chunk must be authenticated before any of its plaintext can be released. A small Range request or preview may therefore fetch/decrypt up to one 100 MiB chunk; first-preview latency can be higher than with 4 MiB chunks. Interrupted uploads retry the current chunk while committed chunks remain resumable. Browser progress advances on backend acknowledgements, not speculative Telegram completion.

## Troubleshooting

| Symptom | Next step |
| --- | --- |
| Fresh startup asks for a setup token | Set a random `DRIVE_SETUP_TOKEN` in runtime environment and restart. |
| Setup token rejected | Use the exact token supplied to this running container, without spaces. |
| Setup expired | Restart the unfinished installation and reload; setup authorization lasts one hour. |
| Browser requires HTTPS | Use a configured HTTPS origin or access through local loopback. Do not send credentials over remote plaintext HTTP. |
| Telegram channel rejected | Check owner account, private type, fresh link, disabled auto-delete/discussion, and optional expected ID. |
| Telegram code/QR expires | Start a fresh QR attempt. Do not use Telegram test datacenters or trigger repeated login requests. |
| Final setup cannot save | For a fresh installation, check volume write permissions and conflicting storage/password/Telegram overrides. Do not clear an existing drive's configuration or data. |
| “Setup saved; Harbor is still starting” | Reload after startup finishes; inspect readiness/secret-safe server diagnostics if it persists. Do not repeat initialization or delete the volume. |
| Setup does not appear for an old drive | Expected: existing metadata/binding prevents takeover. Sign in using its existing owner account. |
| New feature missing from a pulled image | Version 1.0.1 predates web onboarding. Check the running image reference against the verified v1.2.1 digest; do not retag an older binary as a newer build. |
