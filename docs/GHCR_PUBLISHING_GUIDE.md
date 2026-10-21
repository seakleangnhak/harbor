# GitHub and GHCR release guide

Status: **Private source publication PASS; hosted initial main/PR CI PASS; GHCR publication NOT TESTED.** The audited sanitized source now has an authorized private GitHub remote. No image or new tag has been published. See [remote evidence and release-control blockers](REMOTE_VALIDATION_REPORT.md). First proposed automated release: **v1.0.1**, subject to owner approval. Existing annotated `v1.0.0` stays at `f6c49fcd2a52e365c6cfe84395245112a105f929`.

**Publication lineage:** use the [sanitized source repository](PUBLIC_REPOSITORY_PLAN.md), which has no original commits or `v1.0.0` tag. The tag identity above belongs only to the retained private development checkout. Follow the [private-first sequence](FIRST_GITHUB_RELEASE.md); original history and old source archives are blocked from publication by the [privacy audit](PUBLICATION_PRIVACY_AUDIT.md).

## Before publication

1. Review [publication gate](PUBLICATION_GO_NO_GO.md), [CI evidence](GITHUB_ACTIONS_REPORT.md), [security policy](../SECURITY.md) and [third-party notices](../THIRD_PARTY_NOTICES.md). MIT and new public identity were explicitly approved. Original history remains private; only the independently checked new source history is eligible for an authorized push.
2. Obtain separate authorization to create/publish the repository, push commits/tags and publish images. Local preparation does not grant it. Configure the chosen GitHub remote only afterward.
3. Use `main` as the default branch. Enable Actions with a **read-only default GITHUB_TOKEN**, restrict Actions to approved publishers, and require full-SHA action pins. Do not grant fork pull requests write tokens or secrets. Do not enable automatic approval of fork workflows.
4. Protect `main`: require pull-request review and successful CI jobs; dismiss stale reviews; block force pushes/deletion. Protect `v*` tags against creation/update/deletion except approved maintainers. Review workflow changes as privileged code. No owner-specific `CODEOWNERS` is invented here; configure one after maintainers are chosen.
5. Create environment **`ghcr-release`** with required reviewers and prevention of self-review. Restrict deployment refs to protected release tags. The environment name alone does not enforce human approval. Private reviewer protection requires a compatible GitHub plan; stop private registry release if unavailable rather than bypassing this intended gate. Plan caveats are documented in [first release](FIRST_GITHUB_RELEASE.md).
6. Enable secret scanning/push protection and dependency alerts where available. Enable and verify private vulnerability reporting when public visibility supports it, before announcement; while private, use the security policy's fallback. Dependabot checks Go, npm, Docker and Actions weekly. Check branch-protection plan availability; no remote setting is claimed to be configured.
7. Review existing GHCR package ownership. The built-in token must be allowed Actions write access to this repository's `harbor` package. Do not adopt an unrelated existing package. New GHCR packages normally start private; change visibility explicitly only after publication approval and review of package/repository linkage.

## What runs

| Event | Checks | Registry access |
| --- | --- | --- |
| Pull request | Workflow validation; Go default/tagged tests, race, vet and reproducibility; frontend; both image architectures and local-adapter smoke | Read-only token, no login/push |
| Push to `main` | Same CI | No login/push; no `latest` update |
| Stable annotated tag newer than v1.0.0 | Release gate, complete reusable CI, protected publish job | Built-in `GITHUB_TOKEN`, `contents: read` + `packages: write` only in publish job |
| Manual dispatch | Same gates, only when selected workflow ref is the exact existing annotated stable tag named by input | Same protected publish job |

The gate rejects branch refs, prereleases, malformed tags, lightweight tags, historical v1.0.0, mismatched input/ref/commit, source outside `origin/main`, and a version older than another stable tag. It accepts no user-supplied registry, image name, build context or arbitrary Docker tag. Manual dispatch cannot turn `main` into a release. Repository rules remain necessary: maintainers who can modify a publishing workflow are trusted.

For repository `OWNER/REPOSITORY`, image is `ghcr.io/<lowercase-owner>/harbor`. No actual owner/package is assumed. Stable **v1.0.1** produces exactly `1.0.1`, `1.0` and `latest`. There is no automatic major `1` tag. Only stable successful releases update aliases; older patch backports are rejected by this workflow to prevent moving `latest` backward.

## Proposed first automated release

After owner authorization and a successful real `main` CI run:

```sh
# Inside the sanitized publication repository; original v1.0.0 is deliberately absent.
git status --porcelain
git branch --show-current
git rev-parse HEAD
git tag --list v1.0.1
```

Status must be empty, branch must be `main`, HEAD must match the explicitly approved successful-CI commit, and v1.0.1 must be absent. Finalize its changelog, review all changes, commit, and obtain release/publication sign-off. Use the guarded commands in [first release](FIRST_GITHUB_RELEASE.md); never overwrite an existing tag.

Use the [first-release tag sequence](FIRST_GITHUB_RELEASE.md#3-approve-and-publish-the-first-image), which aborts on failed private-repository, approved-SHA, branch, clean-tree and tag checks. Never force-push a release tag.

A dispatch retries an already approved tag; select that tag as workflow ref:

```sh
gh workflow run docker-publish.yml --ref v1.0.1 -f release_tag=v1.0.1
```

The original v1.0.0 tree predates these workflows. Dispatching the new workflow against `main` with `release_tag=v1.0.0` intentionally fails; never move v1.0.0 to add automation.

## Image identity and attestations

Publishing uses pinned action commits, immutable Node/Go/distroless base indices, pinned BuildKit/QEMU/SBOM scanner images, locked npm/Go dependencies, native build stages and cross-compilation. GHA BuildKit caches contain source/build output only. Cold parallel MTProto compilation caused memory pressure in the local 4 GiB Docker VM; one compiler was briefly paused during local qualification. Ordinary CI uses one architecture per job; use sequential platform builds on constrained hosts. Runtime `.env`, databases, keys, sessions, backups and release artifacts are excluded. **Never pass application credentials as build arguments.** Configure them at runtime through protected environment/files and persistent volumes.

The job rebuilds the same validated release source/version and arguments for both platforms. OCI source/version/revision/description/MIT/documentation labels are added. This published index includes SBOM and minimal BuildKit provenance; its digest differs from a no-attestation local smoke image. Attestations describe build contents, not an independent security review or signature. There is no OIDC/keyless signing or GitHub artifact-attestation claim, and no corresponding extra token permissions.

After a successful push, the workflow inspects the immutable index for `linux/amd64` and `linux/arm64`, permitting only attestation descriptors alongside them. Its summary records `ghcr.io/<owner>/harbor@sha256:…`. Save this actual digest in release notes and deployment records; do not invent one before a remote run. Registry delivery/retention and GHA cache behavior remain NOT TESTED locally.

After approved publication, verify from a clean client:

```sh
# Replace reference with the actual approved package and recorded digest.
docker buildx imagetools inspect ghcr.io/OWNER/harbor@sha256:DIGEST
```

Set the actual lowercase owner and full SHA-256 digest, then inspect index platforms, OCI labels and attached attestations. Pin deployments to the digest. The repository's Compose file continues to **build from source**; it does not assume a GHCR package exists. Follow [operations](PRODUCTION_OPERATIONS.md) to use a trusted immutable image with preserved volume identity and `--no-build`.

## Reproducible local packaging

The local release script never publishes or tags. It packages a trusted committed ref, rejects private source members, validates immutable bases and exports image/source archives plus `release.json` and `SHA256SUMS`. Existing output paths/image tags fail closed.

```sh
# After an approved v1.0.1 tag exists; choose fresh output/image names.
python3 scripts/release.py --ref v1.0.1 --version 1.0.1 \
  --platform linux/arm64 --image harbor-rebuild:v1.0.1-arm64 \
  --output output/releases/v1.0.1-arm64
(cd output/releases/v1.0.1-arm64 && shasum -a 256 -c SHA256SUMS)
```

Repeat with `linux/amd64` and separate output/image names. `--version` injects diagnostics and OCI version metadata; the ordinary development default remains 1.0.0 until a release version is chosen. Go binary reproducibility is checked in CI. Multiarch index/attestation bytes and registry tags are different artifacts from the historical single-platform v1.0.0 image.

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
