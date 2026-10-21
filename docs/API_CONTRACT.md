# API Contract

Base: `/api/v1`. JSON snake_case. Root folder ID is empty string (`""`), never a real node. Dates are UTC RFC3339. No response envelope except lists. Error: `{ "error": "Readable message", "code": "invalid_request" }`. Use 400 invalid input, 401 unauthenticated, 403 forbidden origin, 404 absent resource, 409 conflict, 413 oversized body, 429 throttled, 500 internal. Do not leak SQL, filesystem paths, keys or Telegram errors.

## Types

```ts
type Node = { id: string; parent_id: string; name: string; kind: 'file'|'folder'; size: number; mime_type: string; sha256: string; favorite: boolean; trashed_at: string|null; created_at: string; updated_at: string };
type Crumb = { id: string; name: string };
type Upload = { id: string; name: string; parent_id: string; mime_type: string; size: number; sha256: string; chunk_size: number; total_chunks: number; received_chunks: number[]; received_bytes: number; status: 'uploading'|'complete'; created_at: string };
```

## Authentication

- `GET /ready` outside API: 200 `{status:"ready"}` or503 `{status:"unready"}`; SQLite ping and cached Telegram connection, no identities.
- `GET /health` outside API: `{status:"ok"}`; no secrets, health only.
- `GET /auth/session`: 200 `{ authenticated:true, owner:"Owner" }`, or 401.
- `POST /auth/login` `{ password:string }`: 200 session response, sets `drive_session` HttpOnly cookie. Password initialized server-side; no registration/setup endpoint.
- `POST /auth/logout`: 204, revokes session and clears cookie.
- Browser fetch uses `credentials:'same-origin'`. Same-origin browser mutation requests accepted. Cross-origin Origin/Sec-Fetch-Site rejected; no blanket CORS.

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
- `GET /uploads` returns `{items:Upload[]}` (incomplete sessions; may include completed unpublished sessions).
- `GET /uploads/{id}` returns Upload, including received indices for resume.
- `PUT /uploads/{id}/chunks/{index}` raw octet-stream, exact expected chunk size; optional `X-Chunk-SHA256` for client integrity. Returns Upload, 200. Identical retry idempotent; different retry rejected, 409. Limit one chunk body to chunk size + 1. Indices zero-based.
- `POST /uploads/{id}/complete` `{}`: verify all chunks and whole digest, then publish Node; 200 Node. Missing chunks/conflicting name =>409. Retry returns same Node.
- `DELETE /uploads/{id}` cancels incomplete session and deletes chunks, 204. Must never delete a published file.
- `GET|HEAD /nodes/{id}/content?download=1`: seekable decrypted bytes; `download=1` attachment, otherwise safe inline MIME allowlist (raster images, PDF, plain text, audio, video; never HTML/SVG). `Accept-Ranges: bytes`, Range support, ETag digest, `X-Content-SHA256`. Authentication mandatory. Nosniff. CSP sandbox for previews. Corrupted chunks fail before their plaintext is emitted.

## Operations

- `GET /diagnostics`: `{adapter:"local"|"telegram",telegram_configured:boolean,storage_connection:{state:"local"|"unknown"|"connecting"|"connected"|"retrying"|"disconnected",last_checked_at?:string,retry_count:number},encryption:"AES-256-GCM",database:"ok",version:"1.0.0",files:number,folders:number,used_bytes:number,trash_bytes:number,pending_uploads:number,chunk_size:number}`. State comes from cached transport callbacks; configuration alone never implies a connected account. No credentials, channel identity or local paths. Retry count is cumulative for current process and can reset on restart.
- `GET /backup`: authenticated HTTP 410 `encrypted_backup_required`. Use offline `harbor-recovery` for complete encrypted archives; no plaintext metadata download.
- Telegram setup is terminal-only; settings explains dedicated account, server command and readiness. No browser accepts API hash/phone/session secrets.

## Go integration

Coordinator creates DB with `api.OpenDB(path) (*sql.DB,error)`. Storage `New(Config{DB,Key,Backend,ChunkSize})`. API `New(Config{DB,Storage,OwnerPassword,SecureCookies,StaticDir,Adapter,TelegramConfigured,StorageStatus}) (*Server,error)`; optional `StorageStatus func() domain.StorageStatus` supplies cached transport diagnostics. `Server.Handler() http.Handler`. API initializes its schema; Engine initializes storage schema, both on shared DB. `cmd/drive` handles shutdown, secrets and adapter startup.

## Durable copy jobs

All routes require owner authentication and same-origin mutation policy.

- `POST /copy-jobs` `{operation_id:string,source_ids:string[],parent_id:string}`: 201 new job, 200 idempotent existing job. ID is 32 lowercase hexadecimal characters; maximum 100 roots, 10,000 tree entries, 512 levels and eight unfinished jobs. Same operation ID with different input returns 409.
- `GET /copy-jobs`: `{items:CopyJob[]}`; unfinished first, then newest terminal jobs, maximum 100.
- `GET /copy-jobs/{id}`: CopyJob.
- `POST /copy-jobs/{id}/retry` `{}`: failed job returns to queued; failed cancellation cleanup resumes cancelling.
- `DELETE /copy-jobs/{id}`: 204 cancellation request. Complete jobs cannot be cancelled. Cancellation is durable and may require retry after storage failure.

`CopyJob = {id,status,total_entries,completed_entries,total_bytes,completed_bytes,error_code,created_at,updated_at,result_ids}`. Status: `queued|running|failed|cancelling|cancelled|complete`; error codes: `integrity_failed|source_missing|destination_conflict|storage_unavailable|cleanup_failed`. Fresh encrypted stages remain hidden until whole hierarchy commits atomically. User upload routes reject internal copy stages. Retry reuses verified chunks; cancellation never deletes originals.

Main starts `Server.RunCopyJobs(ctx)` once and joins it before DB/adapter close. Supported API schema version 1; migrations transactional, future versions rejected. Storage port remains unchanged.
