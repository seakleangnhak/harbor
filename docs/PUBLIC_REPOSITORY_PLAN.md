# Public repository plan

**Strategy: a new sanitized source history.** Preserve the original repository, commits, branches, annotated `v1.0.0`, application state and local deployment. Never add a public remote to the original repository. See [privacy findings](PUBLICATION_PRIVACY_AUDIT.md).

## Prepared export

The isolated repository is `output/publication-gate/public-export/harbor/` in the original local checkout. It has a new root commit on `main`, owner-approved public author/committer identity, no remote and no tags. It is a source snapshot, not a clone, fork, shallow clone, bundle, mirror or rewritten version of original history.

Files are read from reviewed Git index blobs, never recursively copied from the working directory. Allowlisted root files are `.dockerignore`, `.env.example`, `.gitignore`, `AGENTS.md`, `CHANGELOG.md`, `CODE_OF_CONDUCT.md`, `CONTRIBUTING.md`, `Dockerfile`, `LICENSE`, `Makefile`, `README.md`, `SECURITY.md`, `THIRD_PARTY_NOTICES.md`, `compose.yaml`, `go.mod` and `go.sum`; reviewed source below `.github/`, `cmd/`, `internal/`, `scripts/`, `web/` and `docs/` is included. Only regular Git files are allowed. Each exported file has a SHA-256 and source blob entry in the local export manifest.

Excluded: original `.git` objects/config/hooks/reflogs/remotes/branches/tags, `.env` overrides, data directories, Telegram authorization, keys/passwords, SQLite/WAL files, backups, ignored logs/evidence, dependencies, build output and existing release archives. No Git alternates or object links connect the repositories. Matching safe file blobs between repositories is expected; original commit/tag objects are absent.

New commit identity was explicitly approved by the owner. It is visible in Git metadata; approval does not make old machine paths or historical metadata suitable for publication. Global Git configuration is unchanged. The approved email is intentionally not repeated in documentation.

## History and version policy

Original annotated `v1.0.0` remains in its private repository at `f6c49fcd2a52e365c6cfe84395245112a105f929`; its tag object remains `87ec73c1e9f49d44dbfc55a4eaefc1045085bd66`. Do not move, recreate or import it into the new history. Historical phase reports retain old commit identifiers as provenance; those identifiers do not resolve in the public repository and are not claims of remote CI execution.

First proposed automated release is **v1.0.1**, after private GitHub CI and explicit release/publication approval. No new release tag is created during preparation. The gate's `origin/main` ancestry requirement works with the new lineage; no original `v1.0.0` ref is needed. Development runtime defaults remain 1.0.0; release jobs inject the approved version through existing build configuration.

Rebuild source/images from accepted public commits before distribution. Existing local source archives are not public release assets, including archives without a detected credential. Their historical manifests and provenance do not identify the new lineage. Legacy v1.0.0 rebuilds remain private work until separately reviewed and approved; no historical publishing bypass is added.

## Independent export acceptance

Verify exported file hashes/modes against the reviewed source index; compare all `cmd/`, `internal/`, `web/`, scripts, lockfiles and deployment bytes to the qualified baseline. Only publication documentation changes are expected. Scan the exported working tree and its complete Git history separately; inspect all author/committer fields against approved identity without printing values. Require one root commit, only `main`, no tags/remotes, no alternates, no original commit/tag objects and a clean worktree. Run README/link/SVG checks, actionlint and release-policy tests in the export.

Local manifest/evidence lives outside the export at `output/publication-gate/coordinator/`. Keep it private; it is a handoff record, not an application recovery backup. The export can be moved to a separate workspace by copying **that export only**. Do not copy the parent checkout or use `git push --mirror`, `--all` or `--tags`.

## Remaining approvals

Owner must approve repository owner/name and initial private creation/push separately; then approve first release tag/image publication, and finally approve repository/package public visibility separately. Git identity and MIT approval authorize neither network mutation nor publication. Follow [first GitHub release](FIRST_GITHUB_RELEASE.md). Final decision: [publication gate](PUBLICATION_GO_NO_GO.md).
