# Changelog

## 1.0.1 — 2026-10-21

- Original Harbor lighthouse branding, reviewed demonstration screenshots and refreshed setup/recovery README.
- MIT license approved; contribution/security policies, issue/PR templates, dependency updates and retained third-party notices. Exact Liberation 1.07.4 font source accompanies redistributed PDF assets.
- Read-only pull-request/main CI, default/tagged Go checks, frontend verification, reproducible binaries and credential-free AMD64/ARM64 image smoke checks.
- Owner-only stable-tag GHCR workflow with verified action pins, multiarch builds, SBOM/provenance, OCI labels and built-in token; no publication from PRs or main. Full release CI, private-package checks and pulled-image smoke are required.
- Runtime version injection and explicit-version local packaging preserve the immutable original v1.0.0 tag outside the sanitized repository.
- Authorized private publication of audited source; hosted main/PR CI passed all five jobs. Release-runbook Git commands require explicit audited source path and root identity. Owner approved v1.0.1 and private GHCR publication; actual registry results must be recorded after the release run. Public visibility requires separate approval. See [publishing guide](docs/GHCR_PUBLISHING_GUIDE.md).

## 1.0.0 — 2026-10-21

First local personal-use release. Final exact-image acceptance passed; see [qualification report](docs/PHASE6_FINAL_REPORT.md) and [release checklist](docs/RELEASE_CHECKLIST.md).

### Private drive

- Single-owner authenticated Go/Chi API, SQLite WAL metadata, local development adapter and real Telegram MTProto encrypted storage.
- AES-256-GCM application chunks, SHA-256 integrity verification, resumable uploads, bounded downloads and HTTP byte ranges for supported previews.
- Nested folders, search, favorites, recent files, Trash/recovery, list/grid views, clipboard actions and responsive light/dark file manager.
- Image, PDF, text/code, audio and supported video previews; persistent transfer history, truthful progress, retry, cancellation and supported pause/resume.
- Durable folder-copy jobs with idempotent operation IDs, restart recovery and atomic hierarchy publication. Limits: 100 roots, 10,000 entries, depth 512 and eight unfinished jobs; originals retained on failure.

### Recovery and security

- Complete version-2 encrypted offline recovery archives with supported WAL-safe SQLite snapshots, ordered manifests, original content key, recovery configuration and required session material.
- Fresh no-overwrite restore, authenticated archive termination, corruption/wrong-password rejection, revoked restored web sessions and read-only integrity audits.
- Private state permissions, protected Telegram sessions, missing-bound-state rejection, same-origin mutations, bounded request bodies, login throttling and revocable owner sessions.
- Loopback Docker publication, nonroot/read-only runtime, dropped capabilities, protected persistent data, health/readiness and graceful shutdown. Go 1.26.10 and recovery CLI included.

### Release qualification

- Pinned immutable Docker base indices, explicit committed-source builds, deterministic image timestamps and local image/source artifacts with release manifest and SHA-256 checksums. Repeated `linux/arm64` image export is byte-identical.
- Fixed cross-DC media-pool `FILE_MIGRATE` handling by delegating redirect recovery to gotd's native primary routing. Deterministic migration, reference refresh, bounds and cancellation tests cover protocol behavior; live cross-datacenter retrieval remains **NOT TESTED**.
- Fresh default/tagged Go tests, race and vet, frontend typecheck/build and 23 tests, 24 Python tests, 18 Chromium/WebKit suites and 22 scoped axe audits passed. Exact final-image live Telegram, encrypted multipart/checksums, restart/recreation and archive-only fresh-volume recovery passed. Local upgrade/rollback also passed 20 checksum retrievals.

### Operational boundaries

Telegram recovery archives preserve local recovery material and remote references; they do not mirror remote ciphertext or recover a lost channel/account. Keep independent protected content copies and an off-host verified encrypted archive with its password held separately. Metadata remains readable on the protected host; no public deployment, independent security audit, provider-durability guarantee, physical Safari/iOS certification or live cross-DC qualification is claimed. See [known limitations](docs/KNOWN_LIMITATIONS.md) and [production operations](docs/PRODUCTION_OPERATIONS.md).
