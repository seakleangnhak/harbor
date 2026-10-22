# Storage hygiene — Milestone B

Date: 2026-10-22. Scope: maintained source after Milestone A checkpoint `b6ae39e`. Coordinator owns milestone acceptance and commits. No owner data, key, Telegram session, private archive or remote object was read or changed for this workstream.

## Operational boundary

`drive storage-diagnostics` is an offline, read-only maintenance report. It opens the existing SQLite database with `mode=ro`, without migrations, engine construction, cache initialization, saved storage-configuration loading, key loading or Telegram access. The command requires an existing protected data directory, database, storage binding and lock file, then acquires Harbor's exclusive nonblocking drive lock before inspecting metadata. Running Harbor causes refusal. Missing state never becomes a new drive. Normal environment-file loading still occurs before command dispatch; the examples disable it with `DRIVE_ENV_FILE=-`.

SQLite WAL visibility is preserved; `immutable=1` is deliberately not used. SQLite may create or update WAL/SHM coordination sidecars when opening an otherwise read-only database. Read-only here means no logical database/content mutation, migration, cache clearing or initialization; it does not promise that SQLite coordination files remain byte-identical. Actual metadata database bytes and cache preservation are asserted in CLI regression fixtures.

```sh
# Stop the application using its normal service/container controls first.
DRIVE_ENV_FILE=- DRIVE_DATA_DIR=/data drive storage-diagnostics > storage-plan.json

# Review cleanup_candidates, cleanup_blocked and estimated reclaimable bytes.
# Pass the exact plan_sha256 printed by that diagnostic run.
DRIVE_ENV_FILE=- DRIVE_DATA_DIR=/data drive storage-cleanup --confirm PLAN_SHA256
```

For Docker, run the maintenance command in a one-shot container using the same protected data volume, runtime user and image after stopping the application container. Never run two mutable copies of an installation. A current encrypted offline backup remains prudent before maintenance. No actual owner installation was cleaned during qualification.

## Report and deletion policy

| Area | Diagnostics | Explicit cleanup |
| --- | --- | --- |
| SQLite metadata | Existing integrity, foreign-key, hierarchy and manifest checks; upload/pending/copy state counts; logical maintenance-state digest | Only eligible `storage_parts` rows; no node, upload, chunk manifest, auth or copy-job deletion |
| Encrypted staged parts | Physical size; tracked recoverable, missing and unresolved file counts | Only fully completed, published file leftovers with matching metadata, valid manifests, no pending chunks and no unfinished copy association |
| Ciphertext read cache | Physical size; exact Harbor marker, known names, envelope shape and regular single-link files | Owned disposable ciphertext files only; marker retained |
| Local encrypted blobs | Candidate count for references absent from chunk metadata | Never removed; absence from one snapshot does not prove abandonment |
| Telegram messages | Not enumerated; candidate count is `null` | No connection or deletion exists in this command |
| Interrupted restore/setup or unknown data | No automatic abandonment or age inference; unknown files are retained | No deletion; sibling restoration staging lies outside this data-directory inspection |
| Content-integrity history | `last_successful_content_integrity_check: null` | No fabricated timestamp; successful historical content checks are not persisted by the existing engine |

Incomplete/failed uploads and partially completed folder copies remain recoverable. Missing local staged data is reported; reselecting original browser files may still be required. Remote acknowledgement before a failed manifest transaction can leave an unknown Telegram object. Its ownership cannot be proven by this local report, so neither an orphan count nor reclaimable remote space is invented.

Cleanup is allowed only with the exact current unblocked SHA-256 plan. Confirmation binds candidate path class, opaque reference, inode/device, byte count, modification time, part association and the complete logical state of relevant maintenance tables/schema. Metadata/reference changes with unchanged aggregate counts still invalidate confirmation. Any reported metadata-integrity issue or missing maintenance table blocks deletion.

Every file is revalidated through an anchored directory handle before unlink. Unknown names, symlinks, hard-linked candidates and unowned cache contents are retained. The audit refuses symlinks and hard links; this prevents an audit append from modifying an unrelated file alias. Cleanup never truncates files.

`storage-cleanup.jsonl` is private (0600), append-only at the application level, and synced before each unlink and after each outcome. Its directory is synced before deletion starts. Records contain timestamps, the plan hash, opaque local references and outcomes, never source filenames, Telegram references, paths, key material or account data. The log is not a tamper-proof external audit service.

Staged-row deletion uses a guarded SQL transaction before unlink. Failure before unlink preserves the local part. Interruption after unlink but before SQL commit can leave a missing disposable part row; a fresh diagnostic/confirmation safely removes that row. Published manifests and original ciphertext remain unchanged. An incomplete final audit event must be treated as an unknown outcome and reconciled by a fresh dry-run.

## Bounds and limitations

Directory inspection is bounded to 100,000 direct entries per inspected area; tracked part inspection is capped at 100,000 references. Chunk/reference and logical fingerprint scans are capped at 1,000,000 rows per table. The existing metadata integrity checker retains its own 250,000-node/upload ceiling. Byte accounting rejects integer overflow. These are diagnostic bounds, not a declaration of tested operation at those maxima.

The CLI deliberately requires exclusive offline access; online reconciliation, automatic abandonment, remote garbage collection, recursive unknown-directory accounting and sibling restoration cleanup are not implemented. It cannot prove that Telegram still holds a previously verified published chunk without a separate authenticated content-integrity run. No plaintext preview/staging cache or new storage format was introduced.

## Fresh verification

Focused generated-fixture checks run using Linux ARM64 `harbor:state-backend`, image `sha256:654b0f3911ef0263c931f99e8b3811f6724d4514b921befa2200e3a9bf8db8f5`. Sandbox: no network, UID/GID 10001, read-only source/root/tooling, all capabilities dropped, `no-new-privileges`, two CPUs, 3 GiB memory/no swap, 256 PIDs and private 3 GiB scratch tmpfs. Go dependency resolution is offline; no runtime secrets enter the environment. Source manifests and logs live in ignored `output/milestone-b/hygiene/`.

```sh
go test -count=1 -p 1 -timeout 120s ./internal/storage ./cmd/drive \
  -run 'TestHygiene|TestStorageHygiene' -v
go test -count=1 -p 1 -timeout 5m ./...
go test -count=1 -p 1 -race -timeout 5m ./...
go test -count=1 -p 1 -tags telegram -timeout 5m ./...
go test -count=1 -p 1 -race -tags telegram -timeout 5m ./...
go vet -p 1 ./...
go vet -p 1 -tags telegram ./...
```

The coordinator's complete default, race, Telegram-tagged default/race and both vet runs passed on the matching source snapshot. Full verification took **635.966 s**, including formatting/module checks and binary builds. Evidence: ignored `output/milestone-b/qualification/{go.log,go-result.json,source-manifest.json}`. That compiler environment used the separate Colima VM, Linux ARM64, two CPUs, **6,656 MiB** hard memory/no swap, 4,608 MiB Go soft limit, UID/GID 65532, no network, read-only source/root/tooling, a bounded build-cache volume, 256 PIDs, capability removal and `no-new-privileges`. `OOMKilled=false`, exit code zero. Compiler provisioning is distinct from application runtime-memory qualification.

| Check | Current result |
| --- | --- |
| Focused default tests | **PASS**: `internal/storage` 0.048 s, `cmd/drive` 0.009 s; ignored `default.log` |
| Full default/race, Telegram-tagged default/race and both vets | **PASS** in coordinator full-suite run; all four hygiene source/test files match its immutable source snapshot |
| Read-only diagnostics and protected offline CLI | **PASS** in focused default fixtures |
| Active, queued, saving, failed, unpublished and copy-state retention | **PASS** in focused default fixtures |
| Wrong/stale confirmation, changed references/files, symlinks and hard links | **PASS** in focused default fixtures |
| SQL abort, controlled unlink-before-commit interruption and SQL-only retry | **PASS** in focused default fixtures |
| Original ciphertext/content integrity after cleanup | **PASS** in generated-fixture remote-independent integrity check |
| Actual SIGKILL during hygiene cleanup | NOT TESTED; controlled failure injection is not process-termination evidence |
| Actual owner-installation cleanup / Telegram orphan enumeration | NOT TESTED / intentionally unsupported |

Initial focused execution caught a test-fixture multi-statement parameter-binding error in pending-state setup. Test statements were split, preserving the intended retention checks. This was a fixture failure, not a deployed storage defect.

## Cleanup-result review

Read-only review found no remaining critical deletion or data-integrity defect in the tested offline protocol. A failure after unlink or SQL commit but before the corresponding audit sync can leave real progress absent from the returned counters. Counters represent fully recorded successful steps, not proof that an errored step changed nothing. Likewise, `applied: true` means every candidate operation finished; a later final-audit write can still fail, and the CLI returns an error. Always honor the command exit status and generate a new dry-run after any error. The durable pre-unlink intent and preserved original manifests allow conservative reconciliation; neither the CLI nor its audit is presented as a cross-filesystem/SQLite atomic transaction.
