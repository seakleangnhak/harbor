# Dokploy deployment

Use **Docker Compose** mode with [compose.dokploy.yaml](../compose.dokploy.yaml). Harbor v1.1.0 adds browser setup: supply one bootstrap token, deploy, then choose your owner password and connect Telegram in the web UI. No `init`, generated-password command or terminal Telegram login is needed for a fresh drive. The template pulls the release image for AMD64/ARM64 and routes through Dokploy HTTPS to service `drive`, port `8080`; it publishes no host port.

The v1.1.0 template is prepared for release qualification. An image reference alone does not establish a successful remote build or deployment; use the release report and its verified digest before rollout.

Harbor is qualified for local personal use, without an independent security audit or public-internet security qualification. Prefer VPN or restricted access even with HTTPS.

## 1. Choose new installation or upgrade

For a **new drive**, use a new, empty persistent volume. The default external volume is named `harbor-data`; it survives changes in Dokploy/SSH Compose project names. On the selected deployment server, inspect it first:

```sh
sudo docker volume inspect harbor-data --format '{{.Name}}'
```

If it exists, establish which installation owns it. Do not initialize, delete or reuse an existing drive's volume for an unrelated installation. For a new unused volume name:

```sh
sudo docker volume create harbor-data
```

To create a separate drive, choose an unused name and change `volumes.harbor-data.name` in the template. The named volume inherits the image's protected `/data` ownership, UID/GID `65532:65532`, and mode `0700`. Do not use `volume-nocopy`, root execution or `chmod 777`. The image is distroless and has no shell.

For an **existing installation**, make a complete encrypted backup before upgrading and preserve the same volume, key, database, storage binding and Telegram session. Keep its effective `DRIVE_STORAGE` and Telegram configuration in Dokploy Environment; the old template hardcoded `DRIVE_STORAGE=telegram`, so preserve that setting explicitly when replacing it. Retain existing owner/key/session overrides where used. The new template forwards optional legacy variables but does not infer an old account's settings. Do not blindly clear overrides or supply new credentials. An initialized drive opens its existing login and cannot be reclaimed through setup. New uploads use 100 MiB chunks; existing 4 MiB files and unfinished uploads keep their geometry.

To migrate from another installation, restore a complete encrypted backup using [Production operations](PRODUCTION_OPERATIONS.md), then point this service at the restored state. An empty installation cannot reconstruct your library merely by pointing at the same Telegram channel. If restored under `/data/restored`, set `DRIVE_DATA_DIR=/data/restored` and `DRIVE_ENV_FILE=/data/restored/recovery.env` in the template and keep the HTTPS settings. Never initialize restored state. Stop the original before any restored process uses its copied Telegram authorization.

## 2. Configure Dokploy

In your Dokploy project, create a **Docker Compose** service with **Raw** source and paste the template. Keep ordinary Compose mode and one instance. Stop any earlier Application/Compose service using the same data before deploying this one.

The upstream [GHCR package](https://github.com/users/seakleangnhak/packages/container/package/harbor) is public; pulling it needs no GHCR credential. A private fork or privately scoped package may require a Dokploy Registry entry with package-read credentials. Never put registry tokens in Compose, application environment, build arguments or Git.

For a **fresh installation**, generate a private random token on a trusted machine:

```sh
openssl rand -hex 32
```

Save it securely in the service's **Environment** as `DRIVE_SETUP_TOKEN`. Leave storage, owner-password and Telegram variables absent/empty so the browser can set them. Do not share the token in chat, screenshots, issues or URLs. It is a first-run authorization secret, not your eventual owner password. Dokploy Environment values reach the container through the template's explicit interpolation.

For an **upgrade**, retain existing values instead. The template accepts these optional legacy overrides:

```text
DRIVE_STORAGE
DRIVE_OWNER_PASSWORD
DRIVE_OWNER_PASSWORD_FILE
DRIVE_KEY_FILE
TELEGRAM_APP_ID
TELEGRAM_APP_HASH
TELEGRAM_SESSION_FILE
USERNAME_OR_CHANNEL_LINK
TELEGRAM_CHANNEL_ID
TELEGRAM_CHANNEL_ACCESS_HASH
```

Use a private channel link or the legacy numeric ID/access-hash pair, never both. File overrides refer to protected paths inside the mounted data volume; retain their files and permissions. A bootstrap token is unnecessary for initialized state.

## 3. Add HTTPS and deploy

In **Domains**, add your domain, select service `drive`, container port `8080`, and enable HTTPS with a valid certificate. Configure DNS to reach the deployment server. Keep browser and API on the same origin; credential entry and secure cookies require HTTPS for remote access. Do not add a host `ports` mapping.

Configure proxy request-size limits to accept at least a 100 MiB request body, plus any proxy overhead, and timeouts long enough for Telegram transfer latency. Chunk processing can take several minutes on slow connections; the Telegram operation deadline is bounded to ten minutes. The server authenticates a whole chunk before releasing plaintext, so a small preview can wait for a full 100 MiB fetch.

Use **Preview Compose** to check proxy labels/network, external volume `harbor-data`, runtime variables and one replica. Do not share the rendered preview; it may contain interpolated secrets. Click **Deploy**.

For a new drive, open the HTTPS domain, paste its setup token, create your owner password, and choose local or Telegram storage. Telegram setup accepts your API ID/hash and owned private channel link, then displays a QR for **Telegram Settings → Devices → Link Desktop Device**. Approve through your dedicated account, enter any two-step password only in Harbor's protected form, and wait for private-channel verification. Finish setup and sign in using your chosen password. See [Browser onboarding](WEB_ONBOARDING.md) for details.

An existing drive should show its usual login. If readiness fails after upgrading, preserve the volume and review effective configuration; do not delete data or run initialization.

Ordinary Compose must stop the previous service before starting its replacement. Do not scale, enable overlapping blue/green deployment or run a second process against this volume/session. Allow at least 20 seconds for shutdown.

## 4. Verify and maintain

1. Confirm the built-in `/app/drive health` check becomes healthy. `/health` also succeeds during onboarding; `/ready` returns 503 until setup and normal storage startup finish. No `curl` or shell exists inside the image.
2. After setup/login, check `/ready` through HTTPS and confirm Telegram status is connected when selected.
3. Upload a small generated file, download it and compare SHA-256 locally.
4. Redeploy, then confirm the same file remains downloadable with the same checksum.
5. Keep encrypted off-host backups using [Production operations](PRODUCTION_OPERATIONS.md). Telegram backups preserve metadata, key, session and remote references; they do not independently mirror Telegram content.

Never remove `harbor-data`, use `down -v` or apply `docker volume prune` without reviewing its scope. Protect Dokploy's runtime environment files. Preserve current image digest and configuration alongside a pre-upgrade backup for rollback. Changes to setup inputs after initialization do not reset the owner or change the bound channel.

## Verification scope

Earlier v1.0.1 checks verified isolated local-adapter checksums, Range, authentication rejection, restart, forced recreation and private file permissions on ARM64. They do not qualify this newer image. Browser onboarding and 100 MiB compatibility have separate [source qualification evidence](WEB_ONBOARDING_REPORT.md) and [UI evidence](ONBOARDING_UI_REVIEW.md). New-image and registry results belong in the current release report. Deployment to your Dokploy server, HTTPS issuance and live Telegram operation on that server require their own checks; this guide performs none of them.

References: [Dokploy Compose](https://docs.dokploy.com/docs/core/docker-compose), [Compose domains](https://docs.dokploy.com/docs/core/docker-compose/domains).
