# Phase 5 storage integrity qualification

Status: **PASS** for controlled local copy/recovery and read-only integrity checks. Live Telegram qualification belongs to the coordinator's Phase 5 acceptance run. Controlled failures below use synthetic fixtures; they do not imply a live Telegram rate-limit or cross-datacenter test.

## Changes

Folder copies now use authenticated server jobs with immutable source metadata snapshots, durable destination mappings and opaque operation IDs. Ancestor/descendant selections collapse to selected roots. The server plans at most 10,000 entries and 512 levels, including destination depth, before writing content. Up to eight unfinished jobs are accepted; the worker processes one chunk at a time. File-only browser copies retain their existing transfer path.

`POST /api/v1/copy-jobs` accepts `operation_id`, `source_ids` and `parent_id`. Repeating an identical normalized request returns the same job; changing its specification returns 409. GET/list, retry and cancellation expose persisted status and absolute acknowledged byte progress. Unfinished jobs appear before completed history. Job states are `queued`, `running`, `failed`, `cancelling`, `cancelled`, and `complete`.

Copied files pass through the existing encrypted engine and fresh upload identities. A reserved internal parent marker reconciles process loss between upload creation and recording its ID. Persisted chunks are resumed rather than sent again. Indexed parent-marker lookup reconciles only the current file, avoiding a scan of every stored upload per copied file. Names, favorites and hierarchy come from the captured plan; later source renames/moves do not silently change the copy. Originals are never removed.

Staged folders and files remain invisible in ordinary listings and upload recovery. One SQLite transaction publishes the entire tree and marks the job complete, after checking completed manifests and destination state. A concurrent name collision rolls back all visible metadata. Retry preserves the plan and verified chunks; remove the conflict or cancel/recreate the job to select another name. Cancellation persists before cleanup and deletes only matching generated staging uploads. Failed cleanup enters `failed` with `cleanup_failed`; it does not hammer storage automatically. Retry resumes cleanup, preserving cancellation intent. Process interruption during cleanup resumes from `cancelling` after restart.

The coordinator starts and joins one explicit `RunCopyJobs(ctx)` worker; initialization starts no goroutines. Cancellation is observed between bounded engine operations. Telegram engine deadlines/retries remain unchanged.

Read-only storage tools:

- `storage.CheckIntegrity(ctx, db)` checks SQLite, folder parents/cycles/depth, file-manifest agreement, chunk geometry/order/hashes and unpublished manifests without contacting Telegram.
- `Engine.VerifyIntegrity(ctx)` additionally reads published and trashed files serially through authenticated decryption and checks whole-file SHA-256.
- `LocalStore.OrphanCandidates(ctx, db)` lists unreferenced local entries for review. It never deletes them. Run offline: an in-flight upload can legitimately have an uncommitted ciphertext file.

## Executed evidence

| Check | Result | Evidence |
| --- | --- | --- |
| 100, 1,000 and 1,001-entry trees | PASS | Exact subtree counts; no visible nodes before publication; selected ancestor/descendant pruned; repeated POST idempotent. |
| 512-level tree | PASS | Complete immutable plan/publication; deeper plan rejected before writes. |
| More than 10,000 entries | PASS | Bounded rejection; no partially created job. |
| Duplicate destination names | PASS | Collision returns conflict and transaction leaves destination unchanged; completion succeeds after conflict removed. |
| Source rename/move after planning | PASS | Copy retains captured name/hierarchy; original metadata remains available. |
| Multipart restart and Begin crash window | PASS | SQLite closed/reopened; existing orphan staging identity reconciled; acknowledged chunk skipped; final bytes/hash equal. |
| Cancellation before/during transfer | PASS | Generated upload cleanup, repeated cancel safe, cancelled/complete jobs cannot be retried/destructively cancelled; injected cleanup outage attempts once and explicit retry continues cancellation. |
| Corrupt source content/plan | PASS | Invalid source JSON and target mappings rejected; worker records `integrity_failed`; no completed node published; original node retained. |
| Publication transaction failure | PASS | Injected SQLite abort after inserts rolls back visible nodes; staged complete content survives restart and can publish after retry. |
| Concurrent cancellation/rename | PASS | Focused Go race run reports no races; original remains. |
| Metadata/content audit | PASS | Missing/corrupt blob, absent chunk, wrong hash, invalid geometry, missing parent, cycle and missing manifest detected. Audit performs no deletion. |
| Real process SIGKILL during copy | PASS | Fresh local 33,554,449-byte file; killed after 4,194,304 acknowledged bytes/one chunk; restart completed nine chunks with SHA-256 equality and no premature visibility. |
| Telegram cross-datacenter retrieval | NOT TESTED | No legitimate new live cross-DC scenario used in this workstream. |

Commands executed:

```sh
DRIVE_ENV_FILE=- go test ./internal/api -run CopyJob -count=1
DRIVE_ENV_FILE=- go test ./internal/storage -run Integrity -count=1
DRIVE_ENV_FILE=- go test -race ./internal/api ./internal/storage -run 'CopyJob|Integrity' -count=1
DRIVE_ENV_FILE=- go vet ./internal/api ./internal/storage
python3 -m unittest discover -s scripts -p 'phase5_copy_check_test.py'
DRIVE_ENV_FILE=- go build -o /tmp/harbor-phase5-copy-test ./cmd/drive
python3 scripts/phase5_copy_check.py --binary /tmp/harbor-phase5-copy-test
```

Focused race evidence: API 29.401 s, storage 1.763 s; Python fixture checks 2/2. Later coordinator regression results supersede these focused timings.

## Practical limits

Plans are bounded and stored in SQLite, with serial network work and chunk-sized plaintext buffering. A 10,001-entry tree must be split into independent selected roots/jobs; no silent truncation occurs. The 10,000-entry ceiling was exercised as rejection at 10,001, while 1,001-entry success and 512-level success were qualified. No claim of live 10,000-file throughput is made.

Source content must remain available until its chunks are copied. Permanently deleting the source can fail the job safely. Completed copy content remains independent of the original. Failed jobs retain staged content until retry/cancellation; this intentionally trades temporary encrypted storage for recovery.

Telegram remote orphan enumeration remains unavailable. Ciphertext left by process loss after a Telegram send but before a manifest commit cannot be safely inferred or broadly deleted. Missing remote references fail authenticated reads and cannot produce a newly completed copy; operator recovery requires a valid backup or surviving source. No automatic repair, remote garbage collection or durability guarantee is claimed.

## Coordinator integration and final regression

`bin/drive integrity` exposes read-only metadata checks; `integrity-content` also verifies decrypted published/trashed content. Both require existing complete metadata and Harbor's exclusive data lock. Current Telegram startup still initializes its adapter even for metadata-only CLI checks. No automatic repair, orphan deletion or remote garbage collection was added.

The API filters internal `copy:` uploads from browser transfer recovery and rejects their user-upload mutation/status routes. Browser persistence also discards stale internal staging entries. Durable folder jobs now appear in the existing transfer manager with acknowledged byte/item counts, bounded history, explicit retry/cancel and actionable errors. Closing/reloading the tab does not stop server jobs; file-only copies preserve the existing browser resume path.

Final `go test`/tagged tests, full default/tagged race and vet passed. `python3 scripts/phase5_copy_check.py --binary bin/drive` repeated actual SIGKILL/restart recovery: 33,554,449 bytes, one 4,194,304-byte chunk acknowledged before kill, nine final chunks, exact SHA-256, atomic visibility and original retention. `python3 scripts/acceptance.py --binary bin/drive` passed interrupted uploads, restart/idempotency, Range, hierarchy, Trash and encrypted recovery. Complete Python discovery passed 16 tests.

Fresh Chromium and WebKit CRUD/nested-copy checks plus server-job history/status failure/retry/reload screens passed. The isolated live Telegram disaster-recovery exercise resumed a multipart upload after restart, restored four file manifests into fresh metadata, passed full-content integrity audit and repeated four checksum downloads after another restart. Details: [backup report](PHASE5_BACKUP_RESTORE_REPORT.md) and [final report](PHASE5_FINAL_REPORT.md). Live cross-datacenter retrieval remains **NOT TESTED**.
