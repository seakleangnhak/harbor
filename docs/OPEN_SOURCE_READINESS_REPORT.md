# Open-source preparation qualification

Publication addendum, 2026-10-21: historical privacy review is now complete. Original history/old source archives remain private; publication uses a separately verified new source history. See [privacy audit](PUBLICATION_PRIVACY_AUDIT.md), [repository plan](PUBLIC_REPOSITORY_PLAN.md) and [publication gate](PUBLICATION_GO_NO_GO.md). Original remote execution/publication remains NOT TESTED. Historical evidence below is unchanged.

Date: 2026-10-21. Baseline: `f6c49fcd2a52e365c6cfe84395245112a105f929`; branch `codex/open-source-readiness`. **HARBOR_OPEN_SOURCE_PREPARATION = PASS for local preparation.** Remote execution/publication remains NOT TESTED and requires separate authorization. No push, repository creation, image publication, or new tag is authorized/performed. MIT license was explicitly approved.

## Changes

- Refreshed product README with accurate source-derived Compose/native/Telegram/recovery commands, compact architecture diagram, security scope and limitations.
- Three original SVG lighthouse assets and six reviewed local-fixture screenshots. Lossless WebPs retain exact pixels and total 232,468 bytes; [provenance](assets/README.md).
- MIT license, contribution/security/conduct policies, issue/PR forms and weekly Dependabot. Third-party license texts and exact Liberation 1.07.4 corresponding font source are retained, including GPL-2.0 with font exception for those separate unchanged fonts.
- Read-only PR/main CI and protected stable-tag publishing design. SHA-pinned actions, locked toolchains, immutable bases and pinned builder/scanner dependencies. No Telegram credentials required in ordinary CI.
- Native build stages cross-compile the existing MTProto build to Linux AMD64/ARM64. Explicit build version updates diagnostics/OCI labels; default development version stays 1.0.0. Existing data formats, encryption and UI are preserved.
- Local release packaging supports an explicit stable version and rejects nested private state. The immutable v1.0.0 release/tag is preserved; v1.0.1 remains proposed.

## Local evidence

Evidence is retained under ignored `output/open-source/`; it is not included in source releases or image contexts. Results describe this checkout, not an actual GitHub run.

| Check | Status | Evidence |
| --- | --- | --- |
| README desktop/mobile and light/dark visual review | PASS | Local Goldmark GFM preview; Chromium 1440 × 1000 and 390 × 844; all nine displayed images decode, correct picture sources, no body overflow; open gallery reviewed. `readme/browser-final.json`. |
| Screenshot privacy/pixel identity | PASS, reviewed captures | Six selected full-size generated-fixture captures reviewed; 83 tracked captures reviewed in contact sheets. Optimized pixels equal originals; no visible credentials/private filenames/account/channel identity. `readme/image-checks.json`. This is visual review, not a guarantee from automated OCR. |
| README local links, SVGs, shell syntax | PASS | 26 local resources, heading anchors, three safe SVGs and six `sh -n` shell blocks. Integrated `ci_check.py workflows` and README checker passed. |
| Community links/YAML/ignore rules | PASS | 129 relative file/notice-anchor checks, issue forms/Dependabot structure, 11 private-path Git exclusions plus example exception. `community/REVIEW.md`. |
| Action pins and syntax | PASS | Upstream commits/action metadata verified; official-checksummed actionlint 1.7.12. `workflows/action-pins.json`, `workflows/validation.json`. |
| Release-policy and tooling regressions | PASS | 38 Python tests; stable tag/input/main ancestry/no-backward-latest guards and nested private-archive rejection. Final helper also ran actionlint/policy/resource checks. |
| Go default/tagged tests/race/vet | PASS | Fresh Go 1.26.10, `-count=1`, serial default/tagged suites, both race suites and both vet variants; `qa/go-regression.json`. |
| Frontend CI-major Node 24 tests/types/build | PASS | Node 24.16.0; `npm ci`, 23 tests, typecheck and production build. Existing bundle-size warning retained. |
| Reproducible binaries | PASS | Both commands compiled using two distinct initially empty Go caches; exact SHA-256 equality. Attempts 44.217 / 42.072 s; initial warm-cache result explicitly superseded. `qa/reproducible.json`. |
| Committed-source packaging | PASS | Actual `release.py --ref a619068 --version 1.0.1 --platform linux/arm64` candidate export; all three SHA256SUMS entries passed; resulting image manifest exactly equals tested ARM64 manifest. No new tag. Details below. |
| Build-context exclusion | PASS | Actual BuildKit `COPY .` export from public-only copy excludes 21 synthetic `.env`, key/session/database/backup/test-secret/release-artifact paths. Original private `.env` was never copied. `docker/context-inspection.json`. |
| Linux AMD64/ARM64 builds and smoke | PASS | Actual production Dockerfile; 4,259,861-byte multipart checksums/ranges, anonymous/CSRF rejection, incomplete-finalization rejection, restart/resume/recreation, private modes, nonroot/health/recovery executable/logout/clean shutdown on each architecture. `docker/smoke-{amd64,arm64}.json`. |
| Final source/image/attestation secret check | PASS, scoped | Actual configured-material comparison: zero secret matches across source/reachable objects and all final OCI members plus decompressed layers. Redacted gitleaks final history/public snapshot: zero findings. Public dependency DC constants triaged separately below. |
| GitHub Actions, GHCR, remote caches and package permissions | NOT TESTED | No remote configured; no push/login/publication. Requires approved remote execution and settings. |
| Real Telegram regression this phase | NOT TESTED | Existing live qualification remains historical Phase 6 evidence; this change uses tagged tests and local adapters, preserving original authorization/data. |
| Live cross-datacenter retrieval | NOT TESTED | Separate existing limitation; deterministic protocol tests retained. |

## Local image identity

Final named OCI export: `output/open-source/docker/harbor-final.oci.tar`, 29,099,008 bytes. Index digest: `sha256:dff2846dd26a31ae3622e39b56994cb81949204dacc533597ee6aff54a205afd`. Archive SHA-256: `8bee0f5524659a01cd71939340c4496c0810502908c89bd2855a9582bca6ed57`. These are local candidate artifacts, not a GHCR digest or new release.

| Platform | Manifest digest | Smoke time |
| --- | --- | --- |
| linux/amd64 | `sha256:d799522046f0e545b00acae22077f569f17d7c6cc5a18a0200d4fad809257046` | 6.314 s, emulated on ARM64 host |
| linux/arm64 | `sha256:a1af88320332cd5736b62e616604f96c630688c201fdfbd0984a95f21997156e` | 3.162 s, native |

Descriptor hashes/sizes, executable ELF architectures, 197 `/app` files per platform, nonroot/version/license metadata, license/font-source payload hashes and attestation subject links passed inspection. Both platforms have SPDX SBOM and SLSA v1 provenance statements. Local smoke exports and the attested export have identical image configurations/rootfs identities; additional attestation descriptors alter the index. Remote source/version/revision/documentation labels are configured in the workflow but require remote execution. `docker/final-inspection.json`.

Cold combined build took 499.045 s in a 4 GiB Docker VM with memory pressure; one compiler was briefly paused. This is build evidence, not application throughput. Cached final export took 2.397 s. CI architecture checks run in separate jobs. No original data or authorization was used. Runtime resources created by smoke helpers were removed.

Committed checkpoint `a619068240804516310810daf40a8cbfac0fac95` was packaged through the actual updated release script into ignored `output/releases/v1.0.1-preparation/`. Source archive SHA-256: `d6b316a76e554742f26187c0ca196f517d6e0adcbc80a4afe3dd516cfb089d00`; image archive SHA-256: `b57eb8e33a3a698963a5a2d530a6c4afa9e7d012b093e00b7494b294bae2d40f`. `SHA256SUMS` passed for source, image and `release.json`; `INCOMPLETE` absent. ARM64 manifest exactly matches the tested candidate above. This is an untagged local preparation artifact, not an approved v1.0.1 release. Subsequent evidence-only documentation does not change image inputs.

## Secret and privacy review

Official-checksummed gitleaks 8.30.1 found zero findings in all reachable history (413 blobs / 12,531,595 bytes at baseline), a public current/proposed source snapshot (257 files at scan checkpoint), and reachable commit/tag messages. Reports use full redaction. Literal Telegram/email matches in examples/tests were reviewed as synthetic fixtures, not accepted through a broad scanner allowlist. Third-party copyright contacts are attribution, not deployment credentials.

Coordinator independently compared actual configured account/channel/login/key/session material, including encoded forms, against reachable blobs: **zero matching blobs**. Secret values never enter agent messages, reports or test output. Final public-source/history/OCI-artifact rescan passed. A deliberately broad session-field comparison initially matched public datacenter IP constants in both binaries; each excluded field was verified against locked gotd `telegram/dcs/prod.go`. No authorization/key/account credential was excluded. Final scan covers metadata/configs, SBOM/provenance and 22 decompressed image layers; `security/final-configured-scan.json`, `security/artifact-triage.json`. These checks reduce exposure risk; they do not prove absence of every possible secret.

Current `docs/PHASE5_FINAL_REPORT.md` was made portable. **An older reachable blob still contains a developer machine path; Git author metadata also contains an ordinary non-noreply email.** Paths/identity values are not reproduced here. They are identifying metadata, not discovered authentication credentials. Owner must review this historical privacy exposure before public publication. No history rewrite or tag change occurred; it cannot be removed from immutable historical objects by editing the current file.

No real credential discovery currently blocks preparation. If a real secret is discovered later, stop publication, identify affected paths without values, rotate/revoke affected credentials and agree history remediation before proceeding. Never silently rewrite history.

## Provenance and license

Harbor original source, docs, SVGs and generated demo captures use user-approved MIT. Third-party components retain their own terms; [notices](../THIRD_PARTY_NOTICES.md) inventory 97 components/58 distinct full texts and linked modules on both architectures. PDF font source archive is unchanged, 2,937,949 bytes, SHA-256 `ad98b7498dc2992f7f0868f79b65ce4a720a3acdb63ab3f1f1cb6881117a5406`; included in browser public assets and runtime images. This is a scoped license inventory, not legal advice or an independent compliance audit.

Reference projects NimbusDrive and CamHost.space informed architectural research only. Fresh unauthenticated GitHub license endpoints returned 404; their README claims were not treated as permission. No code/design/artwork was borrowed. System font names reference host-installed fonts; no proprietary system font is bundled.

## Publication conditions and boundaries

Follow [GHCR publishing guide](GHCR_PUBLISHING_GUIDE.md) for default-branch/tag/environment protection, read-only tokens, private disclosure, package visibility and safe manual dispatch. Maintainers must approve workflow changes; code gates cannot defend against a trusted maintainer replacing their own workflow.

Before remote publication: owner privacy review, approved remote/repository identity, repository security settings, actual GitHub CI, explicit new-tag/image publication approval and changelog sign-off. `v1.0.0` remains unchanged at the baseline commit; no v1.0.1 tag is created in this task. Remote publication/attestation delivery remains NOT TESTED until performed.

Personal-use qualification is not independently audited security, public-internet readiness, guaranteed Telegram durability or full cross-DC coverage. Backups preserve remote references, not a provider-independent content mirror. Existing [limitations](KNOWN_LIMITATIONS.md) and [operations](PRODUCTION_OPERATIONS.md) remain binding.
