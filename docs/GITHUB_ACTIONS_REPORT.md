# GitHub Actions qualification

Current v1.2.1 corrective qualification: **PENDING**. Documentation-only CI 39657728596 found uncaught WebKit PDF text-stream cancellation errors after v1.2.0 publication. Historical passing runs below remain unchanged. [Patch report](RELEASE_1.2.1.md) records the minimal API-contract fix, additional close/zoom regression and fresh qualification.

Current v1.2.0 source and registry qualification: **PASS** — [main CI 39655816126](https://github.com/seakleangnhak/harbor/actions/runs/39655816126), exact `335b1a4`, all four jobs in 4m12s. Backend 1m45s with warm caches; 41 frontend tests; native AMD64/ARM64 image smokes; all eight Chromium/WebKit suites and 106 axe checkpoints passed. Cold backend execution separately passed default/tagged race, vet and reproducibility. [Release run 39656653561](https://github.com/seakleangnhak/harbor/actions/runs/39656653561) passed all three jobs in 5m39s, including publication and actual immutable pulled-image checks on both platforms. Index `sha256:13ea2d9146ce0151684cc232d44b79bc3edd68e026dfb7fba2a6f75c04d27378` is shared by `1.2.0`, `1.2` and `latest`. [Release report](RELEASE_1.2.0.md) records evidence, attestations, cache caveats and limitations. Local Docker Desktop is unavailable; target execution used authorized disposable hosted containers. No new live Telegram qualification or deployment.

Historical v1.0.1 status: **LOCAL_CI_EQUIVALENT = PASS. Hosted main/PR/tagged CI = PASS. GHCR artifact qualification = PASS. Original publisher workflow = FAIL; follow-up read-only verification = PASS.** [Registry delivery and failure evidence](GHCR_RELEASE_REPORT.md). Historical local/initial CI measurements below retain their original scopes. [Actual hosted run evidence](REMOTE_VALIDATION_REPORT.md) records the separately authorized private publication and read-only PR permissions.

Initial local preparation: 2026-10-21, before the later authorized private push. The local results below predate remote execution. Owner subsequently approved `v1.0.1` and private GHCR publication; release success still requires actual tagged CI and registry checks. Original `v1.0.0` remains historical and absent from this sanitized repository. Public visibility is not authorized.

## Implemented workflow contract

[`ci.yml`](../.github/workflows/ci.yml) validates pinned workflows and release policy, Go modules/default tests/race/vet with and without the `telegram` build tag, two deterministic binary outputs using a shared compilation cache, Node 24 lockfile installs/tests/types/production assets, and the actual production Dockerfile for `linux/amd64` and `linux/arm64`. Architecture jobs smoke-test the image; they never push. Chromium/WebKit checks cover account settings, previews, staged transfers/process restart and large directories. Target checks use bounded no-network containers with synthetic local storage; ordinary CI needs no Telegram credentials. Frontend build includes typechecking, and tagged race tests include the tagged unit suite.

[`docker-publish.yml`](../.github/workflows/docker-publish.yml) accepts annotated stable release tags newer than `v1.0.0`, or manual dispatch selecting that same existing tag as its workflow ref. Both actor and triggering actor must be the personal repository owner. The gate rejects arbitrary branch refs, mismatched dispatch inputs/commit identities, tags outside `main` ancestry, and backwards stable-tag updates. Verification requires the newest main push CI run for the exact SHA to be completed-success; it rejects stale success behind a newer failed/running run. This avoids repeating complete CI after tagging. The publisher repeats tag checks and checks expected package identity/visibility before registry authentication. Owner-verified immutable package-ID pin covers workflow-token responses that omit repository metadata; current association still requires owner API review. Only publishing receives `packages: write`; CI-proof verification receives `actions: read`. No `pull_request_target`, inherited secrets, PAT, OIDC or attestation-write permission is used.

External actions use full verified commit SHAs. Buildx, BuildKit, QEMU image and SBOM generator versions/digests are fixed. The publishing job derives the lowercase owner from trusted repository context, uses built-in `GITHUB_TOKEN`, and emits stable exact/minor/`latest` tags, OCI source/version/revision/description/license/documentation labels, BuildKit minimal provenance and SBOM. It checks the published immutable digest's architecture descriptors and records that digest in the job summary. Historical v1.0.1 build/upload passed, but its post-upload repository-field guard failed. Follow-up owner-guarded, read-only `verify-published.yml` validated that unchanged digest and both platform images on GitHub. Subsequent approved release evidence supersedes that historical publisher failure without reclassifying it.

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
| Private branch/tag/environment protection | LIMITED | Current plan rejects branch/ruleset controls; intended reviewer environment is unavailable. Approved owner-only release process is documented below. |
| GHCR v1.0.1 artifact qualification | PASS | Built-in-token upload; owner API private/linkage verification; actual immutable index, SBOM/provenance and both pulled-image smokes. [Verifier 39561199774](https://github.com/seakleangnhak/harbor/actions/runs/39561199774). Original publisher remains FAIL as documented. |

Fresh-cache native binary SHA-256: `drive` — `6d56b8d4ad20b9fa95f2c51f5db376286e3072b0eb031bb7a3f3e41ef25be608`; `harbor-recovery` — `35916e672c52bf24e92b330214be20a3b453a921fab28a038efab8ea32ace1fd`. The two attempts use separate empty `GOCACHE` directories; only immutable module downloads are shared. Native binary equality does not imply byte-identical cross-architecture images. An initial warm-cache comparison was not accepted as independent evidence; a resource-paused attempt was safely stopped and restarted before the final successful run.

Historical local Go toolchain: `go1.26.10 darwin/arm64`. Node 24 qualification used installed `v24.16.0`; workflow selects major 24. Python local tooling used 3.14.7. Current helpers and release packaging support Python 3.11+; source extraction validates regular files/directories and uses exclusive writes with controlled modes rather than applying archive ownership or special permissions.

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

Review branch protection/rulesets for `main`, required CI jobs and release-tag controls when supported. Current private plan lacks the intended protected reviewer environment. Approved personal-repository process requires explicit owner release approval and matching owner identities for both `github.actor` and `github.triggering_actor`, while retaining all CI/source/tag gates. This does not supply independent review or protected branches; organization-owned forks fail closed until an approved policy replaces owner equality. Enable stronger protections when available, private vulnerability reporting where supported and dependency/security alerts. Keep pull-request workflow permissions read-only; never enable approval bypasses for untrusted forks.

The initial GHCR package may be private. Review package ownership, repository linkage, Actions access and visibility explicitly; publishing an image does not authorize changing visibility. See [GHCR publishing guide](GHCR_PUBLISHING_GUIDE.md) for exact approval flow, immutable-digest verification, older-release handling and permissions.

Release image publication rebuilds the same gated source/Dockerfile/build arguments after successful architecture smoke jobs. It is not a cryptographic promotion of the exact local smoke-test manifest; compare recorded source revision, immutable digest and attestations, and smoke-test the pulled digest before deployment. npm/Go lockfiles and pinned base/action digests constrain inputs; independent binary equality does not claim globally reproducible registry attestations or network-independent builds.

## Registry verification follow-up

Fresh local actionlint/policy/README checks and 44 Python tests passed after the metadata fix. `verify-published` validates an annotated stable tag in main ancestry, raw index SHA-256, source/version labels, unique platform children, attestation descriptor bindings and SPDX/SLSA predicates before both actual pulled-image local-adapter smoke checks. Hosted verifier permissions were exactly Contents/Metadata/Packages read. New nonsecret repository variable `HARBOR_GHCR_PACKAGE_ID` pins owner-approved package continuity; it does not establish current linkage under a token that omits that field. [Guide](GHCR_PUBLISHING_GUIDE.md).
