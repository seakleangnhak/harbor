# Harbor recovery quickstart

Scope: private, single-owner recovery on a trusted macOS or Linux host. Use a fresh directory; keep the original installation unchanged. Qualification evidence and exact release image identity belong in the [Phase 6 report](PHASE6_FINAL_REPORT.md). Detailed procedures: [operations](PRODUCTION_OPERATIONS.md), [encrypted recovery](PHASE5_BACKUP_RESTORE_REPORT.md), [limits](KNOWN_LIMITATIONS.md).

## Keep these materials independently

- A successfully verified `.harbor` encrypted archive, copied off the Harbor host.
- Its independent high-entropy backup password, stored separately from both archive and drive, for example in a protected password manager or offline recovery record.
- Trusted compatible Harbor release source/image and artifact checksums. Native recovery below needs Telegram-enabled `drive`, `harbor-recovery`, and matching `web/dist` assets; build instructions are in [README](../README.md). Use absolute paths appropriate to the recovery host.
- Access to the dedicated Telegram account and storage channel. If no owner-password file was included, retain the existing owner login password too.

**Telegram archives contain metadata, encryption key, configuration, session and message references; they do not mirror remote ciphertext.** Account/channel/message loss requires an independent encrypted content copy. Harbor does not automate that mirror. Losing both the original content key and a usable archive makes existing ciphertext unrecoverable. Losing the archive password has no bypass.

## Create an off-host recovery copy

1. Settle active copy jobs and transfers. Stop the sole Harbor process/container gracefully and confirm it exited. Do not run host and container instances against the same data or copied Telegram authorization.
2. Choose private mode-0700 parents outside the source data directory and a new archive filename. Secret files must be regular mode-0600 files with protected parents; avoid symlinks.
3. Run the trusted recovery binary from a local Terminal. Replace the example absolute paths. The hidden password prompt asks for confirmation; no password value belongs in command arguments, environment, logs or chat.

   ```sh
   /usr/bin/env -i PATH=/usr/bin:/bin \
     /absolute/harbor/bin/harbor-recovery backup \
     --data-dir /absolute/private/harbor-data \
     --output /absolute/private/backups/harbor-unique.harbor \
     --config-file /absolute/private/harbor.env
   ```

   The explicit config must describe the running drive's actual bound adapter and Telegram target, including any previously applied runtime overrides. The recovery CLI never implicitly loads `.env`. Add `--key-file`, `--session-file` or `--owner-password-file` with explicit private paths if those sources are stored outside the data directory. Local-adapter backup does not require Telegram configuration.
4. Verify, then restart the original installation through its normal supervisor:

   ```sh
   /usr/bin/env -i PATH=/usr/bin:/bin \
     /absolute/harbor/bin/harbor-recovery verify \
     --archive /absolute/private/backups/harbor-unique.harbor
   ```

5. Copy only the verified encrypted archive off-host using a trusted transfer/storage mechanism. Keep the password elsewhere. Record and compare the archive's SHA-256 after copying; on macOS:

   ```sh
   /usr/bin/shasum -a 256 /absolute/private/backups/harbor-unique.harbor
   ```

   Keep the destination private even though the archive is encrypted. Preserve dated copies and a trusted release artifact; never use unreviewed retention commands that could delete original data. Successful `verify` is separate from a real restore/download drill.

For automation, the CLI accepts `--password-file /absolute/private/backup-password` for every command. Provision that regular mode-0600 file outside the repository and archive; it must contain 16–4096 password bytes. No scheduler or rotation is installed automatically. Use the [operations runbook](PRODUCTION_OPERATIONS.md) before scheduling offline backups.

## Restore on a fresh host or into an isolated installation

1. Confirm the original process/container is stopped before any restored instance uses the copied Telegram authorization. Keep it stopped throughout the drill. Do not restore over original data or copy a restored database back over it.
2. Obtain the encrypted archive and password separately; verify the copied archive using the `verify` command above and compare its retained SHA-256. Create a private recovery parent. The child destination must not exist, even as an empty directory:

   ```sh
   /usr/bin/install -d -m 0700 /absolute/private/recovery
   /usr/bin/env -i PATH=/usr/bin:/bin \
     /absolute/harbor/bin/harbor-recovery restore \
     --archive /absolute/private/backups/harbor-unique.harbor \
     --destination /absolute/private/recovery/harbor-restored
   ```

   Restore receives only archive/password. It restores internal key/session/config paths and revokes restored HTTP sessions. It requires neither the original machine's `.env` nor separately copied original key/session files. A compatible binary and Telegram account/channel access are still required.
3. Start using a clean environment and an unused loopback port. These explicit paths prevent inherited variables or the current directory from selecting old state or missing frontend assets:

   ```sh
   /usr/bin/env -i PATH=/usr/bin:/bin \
     DRIVE_ENV_FILE=/absolute/private/recovery/harbor-restored/recovery.env \
     DRIVE_ADDR=127.0.0.1:8093 \
     DRIVE_STATIC_DIR=/absolute/harbor/web/dist \
     /absolute/harbor/bin/drive serve
   ```

   Open `http://127.0.0.1:8093`, log in with the existing owner password, and check nested folders, names, favorites, Trash and transfer/copy-job state. Existing original web sessions remain unchanged; restored sessions require a new login. Recovered configuration defaults to local HTTP cookies; configure/test HTTPS policy before intentional remote access.
4. Download representative small and multipart files. Compare full SHA-256 against independently retained original hashes. Archive authentication alone does not establish current remote content availability.
5. Stop with Ctrl+C, confirm exit, then restart with the same clean-environment command and repeat downloads. Stop the restored instance fully before resuming the original. A successful restore never requires `drive init`, replacing `content.key`, changing adapters, or deleting SQLite/WAL files.

If Telegram authorization has expired or was revoked, keep the original stopped and use the dedicated account's supported local Terminal flow:

```sh
/usr/bin/env -i PATH=/usr/bin:/bin \
  DRIVE_ENV_FILE=/absolute/private/recovery/harbor-restored/recovery.env \
  /absolute/harbor/bin/drive telegram-setup-qr
```

Run setup only after stopping the restored server; both use the exclusive data lock. Approve QR and enter any two-step password locally. Do not share QR screenshots, codes or session files. Refresh protected recovery archives after reauthorization.

Wrong password, tamper, missing recovery materials or invalid database state must fail without publishing the destination. Keep the original intact and investigate. Graceful interruption removes staging; SIGKILL/power loss can leave a private unpublished `.harbor-restore-*` directory. Confirm no recovery process is active before removing only that abandoned staging directory, then retry into a new destination. After any qualification failure, verify no cloned authorization process/container remains before restarting the original.
