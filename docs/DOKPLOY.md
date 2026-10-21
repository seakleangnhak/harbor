# Dokploy deployment

Use Docker Compose mode, not Docker Stack. [compose.dokploy.yaml](../compose.dokploy.yaml) pulls the immutable, published v1.0.1 multi-platform image; it does not build source or read local application state. Dokploy supplies HTTPS routing to service `drive`, port `8080`. There is no host-published port.

This guide creates a **new drive**. To migrate existing files, restore a complete encrypted backup using the [operations runbook](PRODUCTION_OPERATIONS.md) instead of initialization. The original database, key, storage binding and Telegram configuration/session are required; pointing an empty installation at the same channel cannot reconstruct the library. Stop the original before using its copied Telegram authorization. Never initialize restored data. If restoring under `/data/restored`, change `DRIVE_DATA_DIR` and `DRIVE_ENV_FILE` in this template to `/data/restored` and `/data/restored/recovery.env` respectively; retain the HTTPS settings.

Harbor remains qualified for personal use, without an independent security audit or public-internet qualification. Prefer VPN or restricted access.

## 1. Create Compose service

In your Dokploy project, create a **Docker Compose** service. Select **Raw** source and paste `compose.dokploy.yaml`. Keep ordinary Compose mode and one instance. Do not deploy yet. If an earlier Application service uses this same data, stop it before continuing.

In Dokploy **Registry**, add `ghcr.io`, username `seakleangnhak`, and a GitHub classic PAT with `read:packages`; test and save. The package is private. Never put the token in Compose, application environment, build arguments or Git. Make sure the registry credentials are available on the selected deployment server.

In the service's **Environment**, securely copy your existing values for:

```text
TELEGRAM_APP_ID
TELEGRAM_APP_HASH
USERNAME_OR_CHANNEL_LINK
TELEGRAM_CHANNEL_ID
```

Use a dedicated account and its owned private channel. No access hash is needed with a private channel link. Compose explicitly passes these variables to the container; Dokploy's Environment editor alone does not inject variables automatically.

## 2. Bootstrap on the deployment server

Use a private SSH terminal on the same server where Dokploy will run this service. Save the same Compose file as `/opt/harbor/compose.yaml`. Save the four configured Telegram values in `/opt/harbor/.env`, with the directory mode `0700` and file mode `0600`. Edit locally on the server; do not paste credentials or QR output into chat, screenshots, deployment logs or Git.

```sh
sudo install -d -m 0700 /opt/harbor
sudo nano /opt/harbor/compose.yaml
sudo nano /opt/harbor/.env
sudo chmod 600 /opt/harbor/.env
sudo docker login ghcr.io --username seakleangnhak
```

At Docker's password prompt, enter the package-read token. This SSH user's Docker login is separate from Dokploy's registry configuration.

The template uses the **external** Docker volume `harbor-data`, keeping the same data across Dokploy and SSH Compose project names. Before a new installation, check whether it already exists:

```sh
sudo docker volume inspect harbor-data --format '{{.Name}}'
```

If it exists, establish which installation owns it before proceeding. Do not initialize existing drive data. For a separate installation, choose an unused name and change `volumes.harbor-data.name` in both Compose copies. Do not reuse a bind directory prepared for a previous deployment accidentally.

For a new, unused volume name:

```sh
sudo docker volume create harbor-data
sudo docker compose -f /opt/harbor/compose.yaml --env-file /opt/harbor/.env config --quiet
sudo docker compose -f /opt/harbor/compose.yaml --env-file /opt/harbor/.env pull drive
sudo docker compose -f /opt/harbor/compose.yaml --env-file /opt/harbor/.env run --rm --no-deps drive init
sudo docker compose -f /opt/harbor/compose.yaml --env-file /opt/harbor/.env run --rm --no-deps drive telegram-setup-qr
sudo docker compose -f /opt/harbor/compose.yaml --env-file /opt/harbor/.env run --rm --no-deps drive password
```

Scan the QR through Telegram **Settings → Devices → Link Desktop Device** on the dedicated account. Complete any two-step password privately in this terminal. The last command displays Harbor's generated owner password. Save it securely. These commands require an interactive terminal; do not use `-T` or pipe their output.

The image's non-root UID/GID is `65532:65532`. An empty named volume inherits `/data` mode `0700` and ownership from the image. Secret files are `0600`. Do not use `chmod 777`, `volume-nocopy`, root execution or a shell entrypoint. The image is distroless and has no shell.

Do not start `serve` or run `compose up` through SSH; Dokploy owns the long-running service. Never run another server or Telegram setup concurrently against the same state.

## 3. Add HTTPS and deploy

In Dokploy's **Domains**, add your domain and select service `drive`, container port `8080`, and HTTPS with a valid certificate. Configure DNS to reach your server. Keep browser and API on the same origin; secure cookies require HTTPS. Do not add a host `ports` mapping.

Use **Preview Compose** to verify Dokploy added the proxy labels/network, preserved the external volume name, and retained one replica. Do not share the rendered preview: it may contain interpolated secrets. Click **Deploy** after setup has finished.

Ordinary Compose recreates this single service; do not scale it or enable overlapping blue/green deployments. If using any custom rollout mechanism, stop the old instance before starting its replacement and allow at least 20 seconds for shutdown.

## 4. Verify and maintain

1. Confirm Dokploy reports the image's built-in `/app/drive health` check healthy. There is no `curl` or shell inside the image.
2. Check `/health` and `/ready` through the HTTPS domain, then sign in and confirm Telegram status is connected.
3. Upload a small generated file, download it, and compare SHA-256 locally.
4. Redeploy and confirm the same file remains downloadable with the same checksum.
5. Maintain encrypted off-host backups using Harbor's recovery CLI. Telegram backups preserve metadata/key/session and remote references; they do not independently mirror Telegram content.

Never remove `harbor-data`, use `docker volume prune` without reviewing its scope, or restore over existing data. Protect both Dokploy's runtime environment files and `/opt/harbor/.env`. Reauthorization requires stopping the service and repeating only the QR setup command, never `init`.

Changing this deployment file does not rebuild or change the v1.0.1 release. Future upgrades should use an approved tested image digest, a fresh encrypted backup, and the same persistent volume.

## Verification scope

Local checks on 2026-10-21:

- **PASS:** `docker compose --env-file /dev/null -f compose.dokploy.yaml config --format json` with synthetic variables; immutable image pin, external volume, one replica, explicit runtime interpolation, missing required-variable rejection, and no host port/build context.
- **PASS:** isolated ordinary Compose run against published ARM64 child `sha256:bd7023f048e49a5439e8c8e309b621ce4b686db9dbed0e693ca2de6ed06278ca`, using a unique disposable volume, local adapter and loopback HTTP test override. Fresh `init`, readiness/health, 68,200-byte encrypted upload/download with SHA-256 equality, Range, unauthenticated rejection, restart, forced container recreation, private file modes and exit-code-zero shutdown passed. Test objects were removed.
- **PASS:** separately precreated empty external volume inherited image ownership/modes under read-only filesystem, dropped capabilities and no-new-privileges.
- **NOT TESTED:** deployment to your Dokploy server, HTTPS issuance and live Telegram connectivity on that server. No credentials or original application state were used in these tests.

References: [Dokploy Compose](https://docs.dokploy.com/docs/core/docker-compose), [Compose domains](https://docs.dokploy.com/docs/core/docker-compose/domains).
