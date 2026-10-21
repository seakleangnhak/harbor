# Harbor v1.0.1 registry release

2026-10-21. **ARTIFACT_QUALIFICATION = PASS. PUBLISHING_AUTOMATION = PARTIAL: original run failed; follow-up verifier passed.** Repository and package remain **private**. Public visibility has not been authorized.

## Immutable release identity

- Repository: [seakleangnhak/harbor](https://github.com/seakleangnhak/harbor).
- Published GitHub Release: [Harbor v1.0.1](https://github.com/seakleangnhak/harbor/releases/tag/v1.0.1); no manual attachments.
- Annotated tag: `v1.0.1`; tag object `6a15cc057aca4af371bb14c7d5b21de1ee8393c7`.
- Tagged source: `dca22a54069935bccfe4247c56bef5375f62cfa0`.
- Package: [Harbor container](https://github.com/users/seakleangnhak/packages/container/package/harbor).
- Index: `ghcr.io/seakleangnhak/harbor@sha256:d5c56ecd655e83f298e63f3e97f7df7582aecd9ebdd45b0d6aed6801b36bc855`.
- Version `1.0.1`, minor `1.0` and `latest` aliases were independently checked against that index.

| Platform | Runnable child digest |
| --- | --- |
| linux/amd64 | `sha256:1427eb40b9d631624c45ef3b78cfbe95d3d9c679cb392daa3c10320d03f0a24e` |
| linux/arm64 | `sha256:bd7023f048e49a5439e8c8e309b621ce4b686db9dbed0e693ca2de6ed06278ca` |

## Actual test evidence

| Check | Result | Evidence and scope |
| --- | --- | --- |
| Prepared main CI | PASS | [Run 39557324295](https://github.com/seakleangnhak/harbor/actions/runs/39557324295), exact release commit; all five jobs |
| Follow-up main CI | PASS | [Run 39561199403](https://github.com/seakleangnhak/harbor/actions/runs/39561199403), exact verifier/fix source `e6cc0fda896540768680c259a8404f2c7a0ee6dd`; all five jobs, including independent cold-cache binary equality |
| Tagged-source CI | PASS | [Run 39557910079](https://github.com/seakleangnhak/harbor/actions/runs/39557910079): workflow policy, Go default/tagged tests/race/vet/reproducibility, 23 frontend tests/types/build, AMD64/ARM64 production builds and local-adapter smoke |
| Built-in-token multiarch upload | PASS | Same run's first attempt successfully uploaded both platforms and attestations |
| GitHub Release creation | PASS | Actual release page created for existing annotated v1.0.1 after tagged CI, follow-up main CI and hosted immutable-image verification; private repository and no manual assets confirmed through API. |
| Original full release workflow | FAIL | First attempt stopped at post-upload package privacy/linkage guard; retry stopped at existing-package preflight, before login/build/push. Neither a successful upload nor independent tests makes this workflow green. No image or Git tag was changed by retry. |
| Owner-authenticated package policy | PASS | Actual API HTTP 200: private package, linked to this repository; normal existing Docker authentication retrieved immutable index |
| Index/platform/attestation inspection | PASS | SHA-256 of raw registry index matches recorded digest; unique AMD64/ARM64 children; one bound attestation descriptor per child. Each predicate has SPDX 2.3 with 54 packages and BuildKit SLSA v1 with four resolved dependencies. This checks descriptor bindings/predicates, not signatures or raw statement subjects. |
| Pulled-image local tests | PASS | Both actual immutable child digests, sequential execution on Docker Desktop; AMD64 emulated, ARM64 native. Each passed 4,259,861-byte encrypted multipart SHA-256, ranges, auth/CSRF rejection, interrupted-upload restart/resume, container recreation, private modes, nonroot health/executables, all six release labels and clean shutdown. Disposable resources removed. |
| Hosted immutable-image verification | PASS | [Run 39561199774](https://github.com/seakleangnhak/harbor/actions/runs/39561199774), verifier source `e6cc0fda896540768680c259a8404f2c7a0ee6dd`. Exact tagged source/index/children and both pulled-image smoke tests passed. Observed token permissions: Contents/Metadata/Packages read only. No rebuild or registry-tag movement. |
| Published image content/security scan | PASS, scoped | All 36 historical layers across both images reviewed; no runtime credential/data paths. Both configs contain only six safe environment defaults. Gitleaks 8.30.1 redacted directory scans plus printable strings from all four ELF binaries found zero findings; positive control detected. Empty pinned-base `var/backups/` directory explicitly reviewed. This is pattern/context review, not proof against unknown encoded secrets. |
| New hosted live Telegram tests | NOT TESTED | CI and release smoke deliberately use local adapter; earlier live qualification remains separately scoped |
| Live cross-DC Telegram retrieval | NOT TESTED | No legitimate safe cross-DC live fixture available |

## Verification failure and safe recovery

[Diagnostic run 39560582768](https://github.com/seakleangnhak/harbor/actions/runs/39560582768) confirmed HTTP 200 and private visibility under `GITHUB_TOKEN`, with the repository field entirely absent. The original guard safely refused unverifiable linkage. Owner-authenticated API inspection independently confirmed the exact linked repository and package ID. Nonsecret repository variable `HARBOR_GHCR_PACKAGE_ID` now pins that approved package.

Updated guard permits the absent-field response only for the pinned positive integer package ID and private `harbor` package. Changed IDs, pinned-package 404s, wrong names/public visibility and any present invalid repository metadata are rejected. ID continuity does not prove current linkage; owner must recheck association before each approved release. No PAT or broader workflow token was introduced. GitHub GraphQL does not support GHCR's granular-permission registry.

Helper/workflow fixes are later main commits, outside immutable v1.0.1 source. Fresh actionlint, policy and 44 Python tests passed locally. Shared verifier also passed actual registry index/SBOM/provenance fixtures. The separate hosted verifier qualified the original uploaded digest; it did not rerun or turn the failed publisher green. Future publishing changes require their own authorized release validation; no replacement tag was created.

## Distribution and privacy

Only sanitized source was pushed. Original private development commits, original local `v1.0.0`, runtime installation and Telegram authorization were preserved. The tagged GitHub-generated source archive was independently checked: all 265 files match tagged Git blob IDs, including MIT and third-party notices; no unexpected runtime files or symlinks. No private source archive, log, backup or manual release asset was uploaded.

Existing v1.0.1 Git tag and registry version will not be moved to repair workflow verification. Subsequent verifier fixes belong to later commits on main. Remote validation of an existing digest must identify its original tagged source and retain the failed publishing-run record.

## Use the exact qualified image

While package is private, use an already authorized GHCR client. GitHub credentials belong in the client credential store, never Harbor configuration, image build arguments, URLs or source files.

```sh
docker pull ghcr.io/seakleangnhak/harbor@sha256:d5c56ecd655e83f298e63f3e97f7df7582aecd9ebdd45b0d6aed6801b36bc855
```

Keep deployments pinned to a verified digest; aliases can change on future approved releases. Existing Compose instructions build source. [Operations](PRODUCTION_OPERATIONS.md) cover trusted-image overrides, persistent volume identity and recovery. Pulling an image alone does not start or migrate the existing drive.

Harbor is qualified for local personal use within documented tests. It has no independent security audit, public-internet qualification or guaranteed Telegram durability. Protect encryption keys, session state and encrypted off-host backups; remote channel/account loss requires separate content copies.
