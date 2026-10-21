# GitHub Actions qualification

Status: **LOCAL_CI_EQUIVALENT = PASS. Remote initial main/PR CI = PASS. GHCR = NOT TESTED.** [Actual hosted run evidence](REMOTE_VALIDATION_REPORT.md) records the separately authorized private publication and read-only PR permissions.

Initial local preparation: 2026-10-21, before the later authorized private push. The local results below predate remote execution; no GHCR upload or new release tag has been authorized. Existing `v1.0.0` remains historical; proposed first automated release is `v1.0.1`, subject to separate publication approval.

## Implemented workflow contract

[`ci.yml`](../.github/workflows/ci.yml) runs on pull requests, pushes to `main`, and trusted reusable-workflow calls. It validates pinned workflows and release policy, Go modules/tests/race/vet with and without the `telegram` build tag, independent deterministic Go binaries, Node 24 lockfile installs/tests/types/production assets, and the actual production Dockerfile for `linux/amd64` and `linux/arm64`. Architecture jobs load locally and smoke-test the image; they never push. Ordinary CI needs no Telegram credentials.

[`docker-publish.yml`](../.github/workflows/docker-publish.yml) accepts annotated stable release tags newer than `v1.0.0`, or manual dispatch selecting that same existing tag as its workflow ref. The gate rejects arbitrary branch refs, mismatched dispatch inputs/commit identities, tags outside `main` ancestry, and backwards stable-tag updates. It reruns the complete reusable CI before entering the protected `ghcr-release` publishing job, then repeats the gate immediately before registry authentication. Only that job receives `packages: write`; all defaults and validation jobs have `contents: read`. No `pull_request_target`, inherited secrets, PAT, OIDC or attestation-write permission is used.

External actions use full verified commit SHAs. Buildx, BuildKit, QEMU image and SBOM generator versions/digests are fixed. The publishing job derives the lowercase owner from trusted repository context, uses built-in `GITHUB_TOKEN`, and emits stable exact/minor/`latest` tags, OCI source/version/revision/description/license/documentation labels, BuildKit minimal provenance and SBOM. It checks the published immutable digest's architecture descriptors and records that digest in the job summary. Those remote behaviors require their first GitHub execution; syntax checks cannot establish successful publication.

## Local checks

Fresh local evidence lives under ignored `output/open-source/qa/`; raw private runtime inputs are never used. Commands run from repository root unless marked `web`.

| Check | Result | Evidence |
| --- | --- | --- |
| Official `actionlint` 1.7.12 | PASS | `quality-1.log`; verified official tool checksum recorded separately under `output/open-source/tools/` |
| `python3 -m unittest discover -s scripts -p '*_test.py'` | PASS | 38 tests, `python-final.log`, 3.794 s; initial `quality-2.log` recorded 37 before the final reproducibility test |
| `go mod verify` | PASS | `go-0.log`, 2.611 s |
| `go test -count=1 -p 1 ./...` | PASS | `go-1.log`, 15.425 s |
| `go test -count=1 -p 1 -tags telegram ./...` | PASS | `go-2.log`, 17.436 s |
| `go test -count=1 -p 1 -race ./...` | PASS | `go-3.log`, 126.103 s |
| `go test -count=1 -p 1 -race -tags telegram ./...` | PASS | `go-4.log`, 148.727 s |
| `go vet -p 1 ./...`; `go vet -p 1 -tags telegram ./...` | PASS | `go-{5,6}.log`, 2.984 s / 6.845 s |
| Node 24 `npm ci`, `npm test`, `npm run typecheck`, `npm run build` in `web` | PASS | Node 24.16.0; 23 tests; `frontend-node24-{0,1,2,3}.log` |
| `docker compose --env-file /dev/null -p harbor-ci-config config --quiet` | PASS | `quality-0.log`; sanitized environment, no project `.env` loaded |
| Two independent release Go builds per executable, version `1.0.1` | PASS | `reproducible.json`; separate fresh Go caches, both binaries byte-identical; native attempts 44.217 s / 42.072 s |
| Both architecture production images and Docker smoke | PASS | `output/open-source/docker/smoke-{amd64,arm64}.json`; version 1.0.1, 4,259,861-byte multipart integrity/resume/recreation; 6.314 s / 3.162 s |
| Local multiarch index, SBOM and provenance | PASS | `output/open-source/docker/final-inspection.json`; index `sha256:dff2846dd26a31ae3622e39b56994cb81949204dacc533597ee6aff54a205afd`, both architecture configurations match smoke images; SPDX/SLSA v1 subjects bind to platform manifests. No registry push. |
| Actual committed-source release packaging | PASS | `a619068` exported as an untagged 1.0.1 candidate; source/image/manifest SHA256SUMS passed, ARM64 image manifest equals tested candidate. `output/releases/v1.0.1-preparation/release.json`. |
| README local links/heading anchors and original SVG safety | PASS | `workflows-final.json`; 26 linked local resources exist, README heading anchors valid, three SVG assets contain no active/external resources |
| Remote GitHub initial main CI | PASS | All five jobs passed at audited root `15aefb0`: [run 39552906176](https://github.com/seakleangnhak/harbor/actions/runs/39552906176). See [remote evidence](REMOTE_VALIDATION_REPORT.md). |
| Remote PR event CI | PASS | All five jobs passed: [run 39553007811](https://github.com/seakleangnhak/harbor/actions/runs/39553007811). Observed token permissions only Contents/Metadata read; dependency proposal remains unmerged. |
| Private branch/tag/environment protection | BLOCKED | Current plan rejects branch/ruleset controls; intended reviewer environment is not configured. |
| GHCR login/upload/visibility, registry manifest, remotely delivered SBOM/provenance | NOT TESTED | No publication authorized |

Fresh-cache native binary SHA-256: `drive` — `6d56b8d4ad20b9fa95f2c51f5db376286e3072b0eb031bb7a3f3e41ef25be608`; `harbor-recovery` — `35916e672c52bf24e92b330214be20a3b453a921fab28a038efab8ea32ace1fd`. The two attempts use separate empty `GOCACHE` directories; only immutable module downloads are shared. Native binary equality does not imply byte-identical cross-architecture images. An initial warm-cache comparison was not accepted as independent evidence; a resource-paused attempt was safely stopped and restarted before the final successful run.

Local Go toolchain: `go1.26.10 darwin/arm64`. Node 24 qualification uses installed `v24.16.0`; workflow selects major 24. Python local tooling uses 3.14.7; helpers use stdlib and support Python 3.11+ except release packaging's documented Python 3.12+ requirement.

## Repeat checks

```sh
go install github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
python3 scripts/ci_check.py workflows
python3 scripts/ci_check.py reproducible --version 1.0.1
docker buildx build --platform linux/amd64 --load \
  --build-arg GO_TAGS=telegram --build-arg HARBOR_VERSION=1.0.1 \
  --build-arg SOURCE_DATE_EPOCH=0 --provenance=false -t harbor:ci-amd64 .
python3 scripts/ci_check.py docker-smoke \
  --image harbor:ci-amd64 --platform linux/amd64 --version 1.0.1
```

Repeat the last two commands for `linux/arm64` and `harbor:ci-arm64`; emulation may be needed. Build from a clean checkout containing no private state. Do not provide runtime secrets as build arguments. `workflows` includes actionlint, README resource/SVG checks, fail-closed policy guards and Python tooling tests. `ACTIONLINT` may select a trusted installed binary.

The Docker helper chooses a fresh random local volume and loopback host port, generates private owner/key files through `drive init`, and never loads `.env` or Telegram authorization. It checks non-root image/version/health/executable configuration, anonymous rejection, same-origin protection, incomplete-finalization rejection, encrypted multipart resume across restart, SHA-256/range downloads, metadata/content after container recreation, private state permissions, clean shutdown and session revocation. It removes only its generated containers and volume. It is local-adapter verification, **not live Telegram qualification**.

## Required repository settings

Before any publication, review branch protection/rulesets for `main`, require the CI jobs, and protect release tag creation against unreviewed contributors. Configure `ghcr-release` as a protected environment requiring an explicit reviewer and stable release tags; a YAML environment name alone does not configure those protections. Enable private vulnerability reporting and dependency/security alerts. Keep pull-request workflow permissions read-only and avoid enabling workflow approval bypasses for untrusted forks.

The initial GHCR package may be private. Review package ownership, repository linkage, Actions access and visibility explicitly; publishing an image does not authorize changing visibility. See [GHCR publishing guide](GHCR_PUBLISHING_GUIDE.md) for exact approval flow, immutable-digest verification, older-release handling and permissions.

Release image publication rebuilds the same gated source/Dockerfile/build arguments after successful architecture smoke jobs. It is not a cryptographic promotion of the exact local smoke-test manifest; compare recorded source revision, immutable digest and attestations, and smoke-test the pulled digest before deployment. npm/Go lockfiles and pinned base/action digests constrain inputs; independent binary equality does not claim globally reproducible registry attestations or network-independent builds.
