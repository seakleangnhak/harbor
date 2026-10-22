# API Contract

Base: `/api/v1`. JSON snake_case. Root folder ID is empty string (`""`), never a real node. Dates are UTC RFC3339. No response envelope except lists. Error: `{ "error": "Readable message", "code": "invalid_request" }`. Use 400 invalid input, 401 unauthenticated, 403 forbidden origin, 404 absent resource, 409 conflict, 413 oversized body, 415 unsupported content type, 429 throttled, 500 internal. Do not leak SQL, filesystem paths, keys or Telegram errors.

## Types

```ts
type Node = { id: string; parent_id: string; name: string; kind: 'file'|'folder'; size: number; mime_type: string; sha256: string; favorite: boolean; trashed_at: string|null; created_at: string; updated_at: string };
type Crumb = { id: string; name: string };
type Upload = { id: string; name: string; parent_id: string; mime_type: string; size: number; sha256: string; chunk_size: number; total_chunks: number; received_chunks: number[]; received_bytes: number; part_size?: number; staged_chunk?: number; staged_parts?: number[]; staged_bytes?: number; status: 'uploading'|'complete'; created_at: string };
```

## Authentication

- `GET /ready` outside API: normal API returns 200 `{status:"ready"}` or503 `{status:"unready"}`; SQLite ping and cached Telegram connection, no identities. First-run server returns 503 until completion and normal startup; clients must not assume a JSON body during setup.
- `GET /health` outside API: 200 liveness; normal API returns `{status:"ok"}`, first-run server may return an empty body. No secrets, health only.
- `GET /auth/session`: 200 `{ authenticated:true, owner:string }`, or 401. `owner` is the saved display name (default `"Owner"`).
- `POST /auth/login` `{ password:string }`: 200 session response, sets `drive_session` HttpOnly cookie. Password comes from guarded first-run setup or existing legacy initialization; no public registration.
- `POST /auth/logout`: 204, revokes session and clears cookie.
- `PATCH /auth/profile` `{display_name:string}`: authenticated, same-origin; 200 session response with updated `owner`. Trimmed name must contain 1–80 Unicode characters, valid UTF-8 and no controls. Changes only the owner's display name.
- `POST /auth/password` `{current_password:string,new_password:string}`: authenticated, same-origin; verifies current password with bounded attempts. New password must contain 12–72 UTF-8 bytes without CR/LF. Wrong current password returns403 `incorrect_password`; the existing session remains valid. Success atomically updates the bcrypt hash and revokes **all** web sessions, returns204 and clears the current cookie. Sign in again with the new password. Invalid input returns400; exhausted attempts return429. Passwords never enter URLs, logs or browser persistence.
- Browser fetch uses `credentials:'same-origin'`. Same-origin browser mutation requests accepted. Cross-origin Origin/Sec-Fetch-Site rejected; no blanket CORS.

## Guarded first-run setup

Setup exists only for a fresh installation without `drive.db`, `storage.json` or an activated private setup bundle. Existing owner/file APIs continue to require an owner session; a setup token never grants file access. New startup requires a random runtime `DRIVE_SETUP_TOKEN` of 32–256 non-space characters. Legacy initialized drives and explicit legacy initialization remain supported.

- `GET /setup/status` is public and returns only `{required:boolean}`. Initialized servers always return `{required:false}` and do not expose protected setup routes.
- All other setup endpoints require `X-Harbor-Setup-Token` and same-origin request checks. Tokens are never accepted in query parameters. The first-run server hashes/constant-time compares the token, expires setup authorization after one hour, rate-limits failed attempts and sends `Cache-Control: no-store`.
- Account, Telegram and completion JSON bodies require `Content-Type: application/json`, at most 8 KiB, no unknown fields and exactly one JSON value. Errors use fixed safe messages/codes. `setup_expired` is HTTP401; restart the unfinished installation before retrying. Setup cannot reset an initialized drive.

Protected status type:

```ts
type SetupState = {
  state: 'waiting' | 'scanning' | 'password' | 'verified' | 'failed';
  account_ready: boolean;
  flow_active: boolean; // protected status only; active connection/QR/2FA/verified flow
  qr_image?: string; // temporary PNG data URI, only while scanning
  error?: string; // fixed safe guidance
  error_code?: string;
};
```

| Endpoint | Request | Successful response |
| --- | --- | --- |
| `POST /setup/verify` | `{}` | 200 `SetupState`; verifies token without creating an account |
| `POST /setup/account` | `{password:string}` | 200 `SetupState`; 12–72 UTF-8 bytes, no CR/LF; holds password only in setup memory until completion |
| `POST /setup/telegram` | `{app_id:number,app_hash:string,channel_link:string,channel_id?:number}` | 202 `SetupState`; starts one bounded QR authorization and channel-verification flow |
| `GET /setup/telegram` | none | 200 `SetupState`; poll for QR refresh, two-step request, verification or failure |
| `POST /setup/telegram/password` | `{password:string}` | 202 `SetupState`; accepted only while Telegram requests two-step verification; password is not persisted |
| `DELETE /setup/telegram` | none | 204 cancellation; new authorization/completion waits for the worker to release its session |
| `POST /setup/complete` | `{adapter:"telegram"|"local"}` | 200 `{required:false}` after durable activation; then first-run listener transitions to normal API |

Telegram input requires a positive 32-bit API ID, a 32-hex-character API hash, and an owned private channel invite or message link. Optional Bot API channel IDs normalize to positive MTProto IDs; any supplied ID must match the resolved channel. Public aliases, auto-joining, linked discussion and channel auto-delete are rejected. Account/session credentials are never returned in setup status; only a temporary QR is exposed to the token holder. QR authorization times out after three minutes; starts are limited to four per 15-minute window. Telegram two-step input is bounded to 1,024 bytes.

Protected `flow_active` distinguishes an idle `waiting` state from connection or two-step verification in progress, so a reloaded browser can resume polling after token verification. It remains true for a verified flow and becomes false after failure/cancellation. Cancellation can still be releasing its session worker; `setup_busy` requests must be retried after that worker exits. The browser preserves this guard and uses fixed retry guidance rather than suggesting a reload. Public status remains `{required:boolean}`.

Completion requires an owner password and, for Telegram, successful account/channel verification. An active authorization worker yields409 `setup_busy`. A private atomic `setup.pending`→`setup` directory activation saves configuration, owner credentials and verified session together. Conflicting nonempty environment overrides reject completion; existing metadata prevents takeover. Saved configuration is loaded on restart. Browser clients must wait for `/ready` after completion because listener/storage startup can briefly interrupt requests; after a successful save, retry readiness or reload rather than creating another account. See [web onboarding](WEB_ONBOARDING.md).

## Metadata

- `GET /nodes?parent_id=&view=all&q=&sort=name&direction=asc&type=` returns `{items:Node[],breadcrumbs:Crumb[],total:number}`. `view=all` lists direct children; nonempty `q` searches globally; `view=recent|favorites|trash` lists matching nodes globally. `type=folder|image|video|audio|document|archive|other` optional. Optional `limit` (default 10000, max 50000), `offset`. Breadcrumbs start `{id:"",name:"My drive"}`. Folders first. sort `name|updated_at|size`, direction `asc|desc`. Trash hides descendants of trashed roots from normal browsing.
- `GET /nodes/{id}` returns Node.
- `POST /folders` `{name,parent_id:""}` returns Node, 201.
- `PATCH /nodes/{id}` accepts any of `{name,parent_id,favorite}` and returns Node. Reject moving folder into itself/descendant and moving into trash/nonfolder. Active sibling names unique case-insensitively.
- `DELETE /nodes/{id}`: soft delete subtree, 204.
- `POST /nodes/{id}/restore`: recover subtree (to root if original parent unavailable), 200 Node. Reject name conflict safely.
- `DELETE /nodes/{id}/permanent`: only trashed subtree; explicit permanent removal, 204. Durable deletion intent survives interruption. Restore then returns 409 `deletion_started` until retry finishes deletion.
- `POST /nodes/bulk` `{ids:string[],action:"trash"|"restore"|"favorite"|"unfavorite"|"move",parent_id?:string}`: `{completed:string[],errors:{id:string,error:string}[]}`. Max 100 IDs, per-item outcomes.

## Transfers

- `POST /uploads` `{name,parent_id:"",size,mime_type,sha256?:string}` returns Upload, 201. Expected SHA-256 optional (server always computes whole-file digest). Validate parent/name before creating upload.
- New upload sessions use `chunk_size=104857600` (100 MiB). Existing 4 MiB sessions/files retain their recorded geometry. This is not a total-file cap; the API ceiling remains `1099511627776` bytes (1 TiB). Clients must use each returned session's chunk size when slicing/resuming.
- `GET /uploads` returns `{items:Upload[]}` (incomplete sessions; may include completed unpublished sessions).
- `GET /uploads/{id}` returns Upload, including received indices for resume.
- `part_size` advertises resumable client transport, normally `4194304` (4 MiB), or the persisted storage chunk size if smaller. `received_bytes`/`received_chunks` count only encrypted chunks committed to the storage adapter. `pending_chunks:[{index,parts,bytes,state,error_code?}]` describes encrypted local acceptance; state is `receiving|queued|storing|failed`. `max_pending_chunks=2` bounds the per-upload pipeline; the engine also limits pending reservations globally to four. Legacy `staged_chunk` (including index `0`), `staged_parts` and `staged_bytes` mirror the oldest pending chunk, not the total pending bytes. These bytes are never published file content. Validate geometry and refresh status after ambiguous failures.
- `PUT /uploads/{id}/chunks/{index}/parts/{part}` raw octet-stream, required `X-Part-SHA256` for this HTTP body's plaintext digest. Exactly one part, at most `part_size` bytes; final part of each storage chunk may be smaller. Returns Upload, 200, after encrypted local persistence. Identical retry idempotent; different bytes, immutable queued/storing chunks or a full pending window => 409. A part for an already committed chunk also returns 409; refresh status and skip that chunk. When full, wait for server progress or cancel abandoned uploads before retrying. Authentication and same-origin policy apply.
- `POST /uploads/{id}/chunks/{index}/commit` `{}`: authenticate and assemble all staged parts in order, then encrypt/upload one existing storage chunk and persist its manifest entry. Returns Upload, 200, only after storage acknowledgement. Retry of an already committed chunk is idempotent. A remote failure retains staged parts; missing/corrupt parts must be resent from the original file. This request carries no file body and can take longer than part uploads.
- The same commit endpoint accepts `{background:true}`: returns Upload, 202, after durable queueing. One joined backend worker saves queued chunks independently of the browser connection. The client can send the next chunk meanwhile. Poll status; retry a failed queue explicitly (bounded attempts), repair missing parts from the original file, and call `/complete` only after all chunks are remotely stored. Queue acknowledgement is not storage acknowledgement. Restart requeues interrupted saves; cancellation persists intent and waits for active work/cleanup before returning 204. Pausing browser sending does not pause already queued server work.
- `PUT /uploads/{id}/chunks/{index}` raw octet-stream, exact expected chunk size; optional `X-Chunk-SHA256` for client integrity. Returns Upload, 200. Identical retry idempotent; different retry rejected, 409. Limit one chunk body to chunk size + 1. Indices zero-based.
- `POST /uploads/{id}/complete` `{}`: verify all chunks and whole digest, then publish Node; 200 Node. Missing chunks/conflicting name => 409. Retry returns same Node.
- `DELETE /uploads/{id}` cancels incomplete session and deletes chunks, 204. Must never delete a published file.
- Client transport parts are temporary encrypted local state. Restart preserves existing parts; recovery archives retain completed content/manifests but omit pending part files. After restore, status excludes absent parts, so resume resends them. No key/manifest geometry migration is required. Old clients and copy jobs retain the whole-chunk route.
- `GET|HEAD /nodes/{id}/content?download=1`: seekable decrypted bytes; `download=1` attachment, otherwise safe inline MIME allowlist (raster images, PDF, plain text, audio, video; never HTML/SVG). `Accept-Ranges: bytes`, Range support, ETag digest, `X-Content-SHA256`. Authentication mandatory. Nosniff. CSP sandbox for previews. Corrupted chunks fail before their plaintext is emitted.

## Operations

- `GET /diagnostics`: `{adapter:"local"|"telegram",telegram_configured:boolean,storage_connection:{state:"local"|"unknown"|"connecting"|"connected"|"retrying"|"disconnected",last_checked_at?:string,retry_count:number},encryption:"AES-256-GCM",database:"ok",version:string,files:number,folders:number,used_bytes:number,trash_bytes:number,pending_uploads:number,chunk_size:number}`. Version identifies the binary's injected build version (currently defaults to `1.3.0-rc.1`), not proof that its working tree matches a release. State comes from cached transport callbacks; configuration alone never implies a connected account. No credentials, channel identity or local paths. Retry count is cumulative for current process and can reset on restart.
- `GET /backup`: authenticated HTTP 410 `encrypted_backup_required`. Use offline `harbor-recovery` for complete encrypted archives; no plaintext metadata download.
- Fresh browser onboarding accepts API/channel input only behind the bootstrap-token guard. After initialization, settings exposes safe diagnostics and setup guidance; there is no general browser credential/session export or storage reconfiguration endpoint. Existing terminal setup/discovery commands remain available under the exclusive data lock.

## Go integration

Coordinator creates the shared single-connection DB with `api.OpenDB(path) (*sql.DB,error)`; status snapshots depend on that existing connection limit. Storage `New(Config{DB,Key,Backend,ChunkSize,StagingDir,ReadCacheDir})`; production uses `<data>/upload-parts` and enables `<data>/read-cache` only for Telegram; blank directories disable their respective optional features. `Engine.RunPending(ctx)` runs once and is joined before DB/adapter shutdown. API `New(Config{DB,Storage,OwnerPassword,SecureCookies,StaticDir,Adapter,TelegramConfigured,StorageStatus}) (*Server,error)`; optional `StorageStatus func() domain.StorageStatus` supplies cached transport diagnostics. `Server.Handler() http.Handler`. API initializes its schema; Engine initializes storage schema, both on shared DB. `cmd/drive` handles shutdown, secrets and adapter startup.

`onboarding.New(Config{Token,SecureCookies,SetupTelegram,Commit,Completed})` isolates first-run HTTP state from normal API authentication. Injected Telegram callbacks publish QR data, await two-step input and honor cancellation. `Commit` durably activates private state; `Completed` signals listener shutdown only after the success response is flushed. `Close` cancels and joins active callbacks before normal storage can reuse the session.

## Durable copy jobs

All routes require owner authentication and same-origin mutation policy.

- `POST /copy-jobs` `{operation_id:string,source_ids:string[],parent_id:string}`: 201 new job, 200 idempotent existing job. ID is 32 lowercase hexadecimal characters; maximum 100 roots, 10,000 tree entries, 512 levels and eight unfinished jobs. Same operation ID with different input returns 409.
- `GET /copy-jobs`: `{items:CopyJob[]}`; unfinished first, then newest terminal jobs, maximum 100.
- `GET /copy-jobs/{id}`: CopyJob.
- `POST /copy-jobs/{id}/retry` `{}`: failed job returns to queued; failed cancellation cleanup resumes cancelling.
- `DELETE /copy-jobs/{id}`: 204 cancellation request. Complete jobs cannot be cancelled. Cancellation is durable and may require retry after storage failure.

`CopyJob = {id,status,total_entries,completed_entries,total_bytes,completed_bytes,error_code,created_at,updated_at,result_ids}`. Status: `queued|running|failed|cancelling|cancelled|complete`; error codes: `integrity_failed|source_missing|destination_conflict|storage_unavailable|cleanup_failed`. Fresh encrypted stages remain hidden until whole hierarchy commits atomically. User upload routes reject internal copy stages. Retry reuses verified chunks; cancellation never deletes originals.

Main starts `Server.RunCopyJobs(ctx)` once and joins it before DB/adapter close. Supported API schema version 1; migrations transactional, future versions rejected. Copy jobs continue using the existing whole-chunk storage methods.
