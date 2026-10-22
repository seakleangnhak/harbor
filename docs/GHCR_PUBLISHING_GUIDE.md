# GitHub and GHCR release guide

Release source: **v1.2.1**; [current qualification](RELEASE_1.2.1.md) records actual execution/publication status. [v1.1.0 receipt](RELEASE_1.1.0.md) and [v1.0.1 receipt](GHCR_RELEASE_REPORT.md) remain historical. No historical tag/image is overwritten.

Owner API preflight found the existing GHCR package **public** and linked to this repository; the source repository remains **private**. This release does not change either visibility. `HARBOR_GHCR_PACKAGE_ID` pins the existing package; `HARBOR_GHCR_PACKAGE_VISIBILITY=public` pins its observed state. Forks default to private. Recheck linkage and visibility with the owner API before each release; a different/invalid value fails closed.

**Publication lineage:** use the [sanitized source repository](PUBLIC_REPOSITORY_PLAN.md), which has no original commits or `v1.0.0` tag. The original `v1.0.0` tag belongs only to the retained private development checkout; sanitized `v1.0.1` and `v1.1.0` have separate immutable identities. Follow the [private-first sequence](FIRST_GITHUB_RELEASE.md); original history and old source archives are blocked from publication by the [privacy audit](PUBLICATION_PRIVACY_AUDIT.md).

## Before publication

1. Review [publication gate](PUBLICATION_GO_NO_GO.md), [CI evidence](GITHUB_ACTIONS_REPORT.md), [security policy](../SECURITY.md) and [third-party notices](../THIRD_PARTY_NOTICES.md). MIT and new public identity were explicitly approved. Original history remains private; only the independently checked new source history is eligible for an authorized push.
2. Obtain separate authorization to create/publish the repository, push commits/tags and publish images. Local preparation does not grant it. Configure the chosen GitHub remote only afterward.
3. Use `main` as the default branch. Enable Actions with a **read-only default GITHUB_TOKEN**, restrict Actions to approved publishers, and require full-SHA action pins. Do not grant fork pull requests write tokens or secrets. Do not enable automatic approval of fork workflows.
4. Protect `main`: require pull-request review and successful CI jobs; dismiss stale reviews; block force pushes/deletion. Protect `v*` tags against creation/update/deletion except approved maintainers. Review workflow changes as privileged code. No owner-specific `CODEOWNERS` is invented here; configure one after maintainers are chosen.
5. Current personal-repository release process requires explicit owner approval, then both `github.actor` and `github.triggering_actor` must equal `github.repository_owner` in the gate and publish jobs. Successful main CI for the exact release commit and source/tag checks remain mandatory. Current private plan lacks the intended environment-review protection; this owner-only process does not claim independent review or branch protection. It fails closed for organization-owned repositories until an approved maintainer/reviewer policy replaces it. Add protected environments/rulesets when supported; do not change visibility or billing to bypass restrictions.
6. Enable secret scanning/push protection and dependency alerts where available. Enable and verify private vulnerability reporting when public visibility supports it, before announcement; while private, use the security policy's fallback. Dependabot checks Go, npm, Docker and Actions weekly. Check branch-protection plan availability; no remote setting is claimed to be configured.
7. Review existing GHCR package ownership. The built-in token must be allowed Actions write access to this repository's `harbor` package. Do not adopt an unrelated existing package. New GHCR packages normally start private. Workflows compare visibility against `HARBOR_GHCR_PACKAGE_VISIBILITY` (default `private`, only `private` or `public` accepted), without changing it. Pin an existing owner-approved package ID; change visibility only through a separately approved owner action.

## What runs

| Event | Checks | Registry access |
| --- | --- | --- |
| Pull request | Workflow validation; Go default/tagged tests, race, vet and reproducibility; frontend; both image architectures and local-adapter smoke | Read-only token, no login/push |
| Push to `main` | Same CI | No login/push; no `latest` update |
| Stable annotated tag newer than v1.0.0 | Owner-only release gate, successful exact-commit main CI proof, owner-only publish job | Built-in `GITHUB_TOKEN`; verification gets `actions: read`, publishing gets `packages: write` |
| Manual dispatch | Same gates, only when selected workflow ref is the exact existing annotated stable tag named by input | Same owner-only publish job |

The gate rejects branch refs, prereleases, malformed tags, lightweight tags, historical v1.0.0, mismatched input/ref/commit, source outside `origin/main`, and a version older than another stable tag. It accepts no user-supplied registry, image name, build context or arbitrary Docker tag. Manual dispatch cannot turn `main` into a release. Repository rules remain necessary: maintainers who can modify a publishing workflow are trusted.

For repository `OWNER/REPOSITORY`, image is `ghcr.io/<lowercase-owner>/harbor`. This repository publishes `ghcr.io/seakleangnhak/harbor`. Stable **v1.2.1** produces exactly `1.2.1`, `1.2` and `latest`. There is no automatic major `1` tag. Only stable successful releases update aliases; older patch backports are rejected by this workflow to prevent moving `latest` backward.

## Current and future releases

`v1.0.1` and `v1.1.0` already exist; never recreate, move or force-push them. [Current qualification](RELEASE_1.1.0.md) records the approved v1.1.0 source and publication checks. The [first-release sequence](FIRST_GITHUB_RELEASE.md) is historical v1.0.1 guidance, not a command to repeat that tag.

For a future release, obtain owner authorization, select an unused stable version, finalize its changelog and review the exact sanitized source commit. Require an empty working tree, `main`, a successful hosted CI run for that commit, absent local/remote target tag, and approved package identity/visibility. Create and normally push one annotated tag at the approved SHA. The publisher checks the newest main push CI run for that exact SHA and rejects missing, queued, running or failed results; it does not rerun an already successful complete suite. Image construction and delivered-image verification still run before declaring release success.

Manual dispatch can retry an approved existing tag only after reviewing the previous run and registry state; select that same tag as the workflow ref. Do not blindly rerun after upload or overwrite an immutable version. The original v1.0.0 tree predates these workflows; never move it to add automation.

## Image identity and attestations

Publishing uses pinned action commits, immutable Node/Go/distroless base indices, pinned BuildKit/QEMU/SBOM scanner images, locked npm/Go dependencies, native build stages and cross-compilation. GHA BuildKit caches contain source/build output only. Cold parallel MTProto compilation caused memory pressure in the local 4 GiB Docker VM; one compiler was briefly paused during local qualification. Ordinary CI uses one architecture per job; use sequential platform builds on constrained hosts. Runtime `.env`, databases, keys, sessions, backups and release artifacts are excluded. **Never pass application credentials as build arguments.** Configure them at runtime through protected environment/files and persistent volumes.

The job rebuilds the same validated release source/version and arguments for both platforms. OCI source/version/revision/description/MIT/documentation labels are added. This published index includes SBOM and minimal BuildKit provenance; its digest differs from a no-attestation local smoke image. Attestations describe build contents, not an independent security review or signature. There is no OIDC/keyless signing or GitHub artifact-attestation claim, and no corresponding extra token permissions.

Before publication, a built-in-token package API check permits not-found only for an unpinned first package. Existing packages must be named `harbor`, have the owner-approved expected visibility, and match the approved identity. When REST supplies repository linkage, mismatches fail closed. Actual hosted testing found that `GITHUB_TOKEN` entirely omits the repository field, although owner authentication returns it. GitHub's GraphQL API does not support granular-permission registries such as GHCR.

For that response shape, set nonsecret repository variable **`HARBOR_GHCR_PACKAGE_ID`** to the immutable package ID only after the owner independently verifies current expected visibility and exact repository association through the Packages API/settings. Automation then requires that exact integer ID and `HARBOR_GHCR_PACKAGE_VISIBILITY` (private by default); any present invalid linkage, changed ID or pinned-package 404 fails closed. This verifies continuity of an owner-approved package, **not current linkage under the workflow token**. Refresh the owner-authenticated association check before each approved release. Never add a PAT to Actions to obtain a larger API response.

The owner-verified pin is configured for the first package. A new installation whose first-push response omits linkage must stop after upload, have its owner review/pin the new package, then verify that existing immutable digest. Do not blindly rerun publishing: creation labels/provenance may change image digests even at the same source. The existing v1.0.1 workflow is immutable; fixes on main apply to later releases.

Read-only workflow `verify-published.yml` qualifies an existing image without rebuilding or moving registry tags. Dispatch it from main as repository owner, supplying an existing annotated stable tag and the actual SHA-256 index digest. It validates tag/main ancestry, package policy, exact raw index SHA-256, both distinct platform children, attestation bindings and SPDX/SLSA predicates, then pulls each child and runs isolated encrypted multipart/checksum/restart/recreation smoke checks. Its summary records source, digest and per-platform results. It receives only `contents: read` and `packages: read`; no build/push path. Original failed publishing runs retain their actual result. [Current evidence](RELEASE_1.1.0.md) · [historical v1.0.1 evidence](GHCR_RELEASE_REPORT.md).

```sh
gh workflow run verify-published.yml --ref main \
  -f release_tag=v1.0.1 \
  -f digest=sha256:d5c56ecd655e83f298e63f3e97f7df7582aecd9ebdd45b0d6aed6801b36bc855
```

After approved publication, verify from a clean client:

```sh
# Replace reference with the actual approved package and recorded digest.
docker buildx imagetools inspect ghcr.io/OWNER/harbor@sha256:DIGEST
```

Set the actual lowercase owner and full SHA-256 digest, then inspect index platforms, OCI labels and attached attestations. Pin deployments to the digest. The repository's Compose file defaults to the versioned GHCR image; `--build` remains available for trusted source. Follow [operations](PRODUCTION_OPERATIONS.md) to use a trusted immutable image with preserved volume identity and `--no-build`.

## Reproducible local packaging

The local release script never publishes or tags. It packages a trusted committed ref, rejects private source members, validates immutable bases and exports image/source archives plus `release.json` and `SHA256SUMS`. Existing output paths/image tags fail closed.

```sh
# Rebuild approved v1.1.0 locally; choose fresh output/image names.
python3 scripts/release.py --ref v1.1.0 --version 1.1.0 \
  --platform linux/arm64 --image harbor-rebuild:v1.1.0-arm64 \
  --output output/releases/v1.1.0-arm64
(cd output/releases/v1.1.0-arm64 && shasum -a 256 -c SHA256SUMS)
```

Repeat with `linux/amd64` and separate output/image names. `--version` injects diagnostics and OCI version metadata; the current local prepared-source default is 1.3.0; published v1.2.1 remains unchanged. CI compares two Go binary outputs using a shared compilation cache; this checks output equality, not independent cold compilation. Multiarch index/attestation bytes and registry tags are different artifacts from the historical single-platform v1.0.0 image.

## Older release, without changing history

**Original private development checkout only.** The following local rebuild needs its original trusted tag, absent from the sanitized repository. Never add a public remote to that private checkout or import its old objects into the public repository. Existing old source archives contain a historical path and must remain private.

```sh
python3 scripts/release.py --ref v1.0.0 --version 1.0.0 \
  --platform linux/arm64 --image harbor-legacy:v1.0.0-arm64 \
  --output output/releases/v1.0.0-manual-arm64
```

Repeat for AMD64 if needed; historical AMD64 must be tested independently. Historical source does not support another version override. This produces local artifacts only and leaves the original tag unchanged.

Historical redistribution is outside this workflow. It needs a separately reviewed sanitized source/compliance bundle, explicit approval and independent platform tests; an old private commit cannot be checked out by public CI. Retain required notices and exact font source, identify any modified rebuild honestly, and never update `latest`/minor aliases from it. Do not publish old archives or import private Git objects to enable a historical rebuild. No legacy publishing bypass is supplied or authorized.

## Failure and rollback

Failed CI must be fixed before publication; do not skip `needs: verify`. A build/push can fail after some tags/blobs reach GHCR. Do not claim registry publication is atomic. Inspect actual digest/tag state, keep deployment pinned to the last qualified digest, and rerun the same approved release only after review. Never repair by moving a Git tag. Forward fixes get a new patch version. Alias rollback, if ever needed, is a separate approved registry operation; preserve immutable version tags and the previous encrypted recovery archive.
