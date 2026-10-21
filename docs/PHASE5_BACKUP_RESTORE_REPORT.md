# Phase 5 — Encrypted backup and disaster recovery

Date: 2026-10-21. Synthetic recovery qualification: **PASS**. Coordinator isolated real Telegram disaster recovery: **PASS**; evidence below.

## Implemented recovery boundary

`cmd/harbor-recovery` and `internal/recovery` replace plaintext full-backup creation. `scripts/recovery.py` delegates to the Go tool; no shell execution, implicit dotenv loading, password arguments, or password environment variable. Legacy plaintext tar archives are rejected. Existing plaintext backups must remain protected and require a separately reviewed migration; no automatic conversion or deletion occurs.

Complete backup requires a stopped drive and obtains the same exclusive `.lock` as Harbor. SQLite opens read-only. The supported modernc `NewBackup(":memory:")`, incremental `Step`, `Commit`, and `Serialize` APIs capture committed WAL state. No plaintext SQLite backup snapshot or plaintext tar archive is written to disk. The database remains an exact schema/data snapshot, including durable uploads and copy-job tables.

Encrypted members include:

- `drive.db`, `storage.json`, original 32-byte `content.key`.
- `owner-password` when present; explicit external sources must exist and the saved password must match the initialized owner's bcrypt hash. Stale recovery password files are rejected.
- Allowlisted `recovery.env` configuration, with unrelated environment omitted.
- Telegram `telegram.session` for Telegram drives; missing or structurally invalid session state is rejected. Its auth-key fingerprint is validated using the protocol's SHA-1 tail, matching gotd v0.162.0; optional legacy `Addr` may be empty. Archive cryptography remains AES-256-GCM/SHA-256. App credentials and channel configuration are required and must agree with the storage binding.
- `chunk-manifests.jsonl`, containing ordered upload/chunk identities, sizes, SHA-256 and adapter references.
- Every referenced encrypted local blob for local drives. Telegram ciphertext remains in the verified channel; this archive does not independently mirror remote file content.

External content-key, owner-password and session paths can be supplied explicitly. Their paths are not preserved as restored dependencies. Restored configuration uses canonical paths inside the new directory, binds `127.0.0.1:8080`, and starts with local HTTP cookie policy. HTTPS deployments must reapply their documented cookie/proxy settings before exposure.

## Encryption and archive verification

Archive version 2 starts with `HBRBK002`, a fresh 256-bit random salt and random nonce prefix. Fixed scrypt parameters (`N=32768`, `r=8`, `p=1`) derive a separate AES-256-GCM key from a 16–4096-byte backup password. Records contain at most 1 MiB of tar plaintext. Per-record authenticated data binds the full header, record sequence and ciphertext length. Monotonic nonces under the independently derived archive key prevent record nonce reuse. An authenticated final record and mandatory end-of-file detect truncation, reordering, duplication and appended data.

Encrypted manifest stores member sizes and SHA-256 digests, archive version, timestamp and adapter. Backup reopens and verifies the complete encrypted archive before no-replace publication. `verify` authenticates every record and validates member hashes, required materials, key binding, configuration and session structure. Restore additionally runs SQLite integrity/schema checks and checks every referenced local blob's presence and encrypted size.

The password is accepted through a private regular `--password-file` or hidden terminal prompt with confirmation on backup. Passwords, keys, session contents, channel links and provider errors are absent from success/error output. Archive files are mode 0600; sensitive sources and restored files require private permissions; restore directories are mode 0700. Symlinks, unexpected member names/types, duplicate members, traversal and invalid sizes are rejected.

## Restore safety

Restore accepts only a fresh nonexistent directory. Decrypted working state is staged privately in a sibling mode-0700 directory. Every file is hashed and authenticated before database validation. Restored HTTP sessions are revoked; original database, keys, blobs, configuration, account authorization and HTTP sessions remain untouched. Atomic no-replace directory publication uses macOS `RENAME_EXCL` or Linux `RENAME_NOREPLACE`. Files and directory entries are synced before successful completion. An existing empty directory is not replaced.

Wrong passwords, corrupt/truncated archives, missing keys, bad binding/configuration/session state and invalid SQLite data abort publication. Graceful interruption removes staging material. SIGKILL or host power loss can leave a private `.harbor-restore-*` staging directory; it remains unpublished and must be removed locally after confirming no recovery process is active. Start a new restore into a new nonexistent destination. Never point the original installation at incomplete staging state.

Telegram authorizations must be used serially: stop original before starting a restored copy, stop recovery instance before resuming original. An archive cannot recover a deleted Telegram channel, account ban, remote ciphertext loss, or revoked account authorization. Reauthorization requires the dedicated account's supported secure login flow.

## Commands

```sh
go build -o bin/harbor-recovery ./cmd/harbor-recovery
mkdir -m 700 backups
bin/harbor-recovery backup --data-dir /private/harbor-data \
  --output backups/harbor-20261021.harbor \
  --password-file /private/backup-password \
  --config-file /private/harbor.env
bin/harbor-recovery verify --archive backups/harbor-20261021.harbor \
  --password-file /private/backup-password
bin/harbor-recovery restore --archive backups/harbor-20261021.harbor \
  --destination /private/recovery/harbor-restored \
  --password-file /private/backup-password
DRIVE_ENV_FILE=/private/recovery/harbor-restored/recovery.env bin/drive
```

Stop Harbor before backup. Output and restore-parent directories must be private mode 0700. Choose an isolated port for recovery qualification. Never run copied Telegram authorization concurrently. Prefer an independently generated high-entropy backup password stored outside the drive and outside the backup archive; losing it makes the archive unrecoverable.

Python compatibility entry points:

```sh
python3 scripts/recovery.py backup /private/harbor-data backups/harbor.harbor \
  --password-file /private/backup-password --config-file /private/harbor.env
python3 scripts/recovery.py restore backups/harbor.harbor /private/recovery/harbor-restored \
  --password-file /private/backup-password
```

No scheduler is installed automatically. A local launchd/systemd-timer or cron job can stop the single instance, execute the built CLI using protected password/config files, restart in a trap/finally handler, and retain dated archives. Treat successful `verify` and periodic fresh-environment restore drills as separate checks. Retention/deletion must apply only to explicitly designated backup files; no remote cleanup is part of backup.

## Executed checks

| Check | Result | Evidence |
| --- | --- | --- |
| Complete encrypted local roundtrip | PASS | Real engine encrypted fixture restored, decrypted and compared byte-for-byte after two database reopens |
| SQLite committed WAL snapshot | PASS | Source connection remained open with nonempty committed WAL during supported backup API call |
| Hierarchy, favorites and Trash | PASS | Nested node parent, favorite flag, Trash timestamp/root preserved |
| Session revocation boundary | PASS | Restored session count zero; original session count unchanged |
| Backup confidentiality | PASS | Archive lacks plaintext fixture content, filename, owner password, content key and SQLite signature |
| Wrong password and tamper | PASS | Wrong password, bit flip, truncation, missing terminator, reordered/duplicate records and appended data rejected |
| Fresh-directory safety | PASS | Existing installation and existing empty destination not overwritten; failed restore destination absent |
| Interrupted extraction | PASS | Deterministic cancellation during record extraction leaves no published directory or staging files |
| Missing/corrupt recovery materials | PASS | Missing key/session/app config/blob and authenticated archives with missing required members, wrong key or invalid SQLite rejected |
| Permission and source safety | PASS | Public key permissions, symlink key, live lock and recursive output rejected; restored files private |
| Synthetic Telegram recovery | PASS | Synthetic structurally valid account session and allowlisted app/channel configuration restored exactly/canonically; mismatched expected channel rejected |
| Telegram link compatibility | PASS | Private invite and `t.me/c/<channel>/<message>` links accepted; embedded channel must match binding; queries/fragments/public aliases/credentials/ports/escaped paths rejected |
| Python wrapper | PASS | Three subprocess tests: encrypted roundtrip, required secure noninteractive password input, wrong-password/legacy rejection |
| Linux recovery binary | PASS | `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build` cross-build; macOS binary exercised directly |
| Live Telegram restoration | PASS, coordinator | Fresh isolated restore, four real Telegram files, full SHA-256, hierarchy/favorites/Trash and restart; no original installation writes |

Executed targeted commands:

```sh
DRIVE_ENV_FILE=- go test -count=1 ./internal/recovery ./cmd/harbor-recovery
DRIVE_ENV_FILE=- go test -count=1 -race ./internal/recovery ./cmd/harbor-recovery
DRIVE_ENV_FILE=- go vet ./internal/recovery ./cmd/harbor-recovery
python3 -m unittest discover -s scripts -p recovery_test.py
```

## Practical ceilings and remaining limitations

- Metadata snapshot capped at 256 MiB. In-memory SQLite plus serialization can temporarily require roughly twice that limit, plus manifests/crypto buffers; actual process peak depends on SQLite/Go. No peak-memory benchmark claimed. Larger metadata needs a separately reviewed encrypted SQLite VFS or alternate snapshot transport.
- Export and archive manifests each capped at 64 MiB. Archive capped at 250,000 members and 1 TiB uncompressed payload. Blobs stream through bounded records; complete file contents do not accumulate in RAM.
- Offline backup deliberately pauses Harbor. There is no automatic retention, remote replication or secret-rotation workflow.
- File-backed owner password is optional when absent; its salted hash remains in SQLite. The owner must still know that password after recovery.
- The archive includes sensitive account authorization and decryption key under one backup password. Protect/archive password separately; encryption does not mitigate an already-compromised host or compromised backup password.
- Correct session JSON structure does not prove current Telegram authorization or channel availability. Live restore must reconnect and hash restored downloads before declaring live disaster recovery PASS.

## Coordinator live disaster-recovery evidence

Final rebuilt binaries executed:

```sh
python3 output/phase2/live_control.py stop
python3 scripts/phase5_live_recovery_check.py --source-env .env
```

The harness holds the stopped original's exclusive lock while using copied Telegram authorization. It creates independent empty metadata, a fresh content key and owner password, and uploads generated TXT/PNG/PDF plus a 4 MiB+65,557-byte multipart fixture. One chunk is acknowledged, the isolated process restarts, and remaining upload resumes. No preexisting remote objects are deleted.

A complete **104,022-byte encrypted archive** was created and verified from the stopped clone. Fresh restore received only archive/password; original key, configuration, owner password and session were recovered from authenticated members. All four files downloaded from real Telegram with exact SHA-256; nested folders, favorite and Trash state matched. A trashed PDF was restored using normal API before its checksum download. An offline full-content audit verified four files, then another application restart and four downloads passed. Wrong password, ciphertext bit flip, missing source key and SIGTERM during restoration all failed without publishing a bad destination. Private clone state/archive/password copies were removed after shutdown.

Sanitized evidence: `output/phase5/live-dr-evidence.json`, `live-dr.log`, and tracked reproducible harness. Original service was then upgraded only after making a separate encrypted rollback archive. Its owner, existing HTTP sessions, nodes, storage manifests/chunk rows and original encryption key were verified unchanged across migration; final `/ready` returned200. Original runs alone on loopback8081.

| Fixture | Bytes | SHA-256 |
| --- | ---: | --- |
| TXT | 68 | `3b6a71807883d0e90152520d2645a2576f14ab25eab852c9fe55fc839e8e4c85` |
| PNG | 121 | `317529469342c7b56488a4caa1e1161b7203007faba60e848622c642ece27713` |
| PDF | 612 | `c166d36a1af90db904d04ae50a6f8988ea5bdaff1d6548cdd60abe7a412ad3b1` |
| Multipart | 4,259,861 | `e609159371697ca949b503bcabf27ddfde224ea08f00dcdf5efa6328da1f9e05` |

This proves restore against currently available same-channel Telegram ciphertext. It does not prove availability after channel/account loss or live cross-DC retrieval. Forced parent-harness SIGKILL cannot run cleanup; stop any surviving clone before original restart. Normal SIGTERM/SIGINT run ordered shutdown before releasing original lock.
