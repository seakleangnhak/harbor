# Harbor v1.0 release checklist

**Frozen historical checklist.** This records qualification in the original private repository. Its old-ref build/tag commands describe the completed process, not instructions for the sanitized public history. Do not recreate or import `v1.0.0`. New publication follows [first GitHub release](FIRST_GITHUB_RELEASE.md), with proposed `v1.0.1` and separate approval.

Qualification date: 2026-10-21, Asia/Phnom_Penh. Runtime checkpoint: `6fe39cf2b0876fea7068739de7c0e6e0b6752c2d`. **Release decision: PASS for local private personal use.** Mandatory acceptance evidence is complete. Annotated `v1.0.0` tagging follows final committed-source artifact verification and a clean worktree.

Scope: local, private, single-owner personal use. Passing this checklist does not establish independent security audit, public internet readiness, guaranteed Telegram durability, or physical Safari/iOS coverage.

## Acceptance gates

| Gate | Status | Evidence / remaining action |
| --- | --- | --- |
| Frozen runtime source and v1.0.0 application versions | PASS | Runtime checkpoint above; API, frontend package and image version label are `1.0.0`. |
| Immutable Docker bases and local release image | PASS | Three multiarch base indices pinned by digest; tagged MTProto build for `linux/arm64`; no image push. |
| Reproducible release image | PASS | Repeated actual build/export produced byte-identical image archive; exact digest and SHA-256 below. |
| Intended Compose build matches runtime bytes | PASS | Compose rebuilt from exported committed source, project environment disabled. All 194 `/app` files match SHA-256; runtime config matches except Compose labels. |
| Default/tagged Go unit, integration, race and vet | PASS | Fresh independent QA: both complete test/race/vet variants passed; `output/phase6/qa/QA_REPORT.md`. |
| Authentication, encryption and backup regressions | PASS, scoped | Focused attack-path review; synthetic recovery/auth/storage tests; default/tagged vulnerability scans found no affected symbols/imported packages, npm audit found zero advisories. `output/phase6/security/REVIEW.md`. |
| Frontend tests, typecheck and production build | PASS | 23 tests; existing bundle-size warning documented. `output/phase6/qa/build-and-python-results.json`. |
| Python qualification guards and recovery wrappers | PASS | Final discovery: 24 tests, exit 0; `output/phase6/qa/python-tests-final.json`. |
| Browser, accessibility and large-folder regressions | PASS, tested views | Chromium and WebKit each passed nine suites and 11 scoped axe audits; zero violations/overflow; saved-download checksum verified. `output/phase6/qa/qa-summary.json`. |
| Exact release image with live Telegram | PASS | Small/multipart transfer, restart/resume, recreation and hashes; clone/private-volume removal verified before original resumed. `output/phase6/live/evidence.json`. |
| Complete encrypted backup and fresh-environment restore | PASS | Final image restored archive/password alone into fresh volume; recovered Telegram/key/config/DB, seven file hashes, hierarchy/favorite/Trash and another restart passed. |
| Release performance and regression review | PASS | Startup 0.760 s; 100 MiB upload 1.626430 / download 4.212658 MiB/s; sampled working set 38.270 MiB. Browser 1,000-entry reload/load: Chromium 132 ms, WebKit 167 ms. Measurement limits in final report. |
| Exact-image local upgrade/rollback | PASS | Phase 4 → v1 → restart → recreate → 4 → v1; 20 full checksums, unchanged metadata/key, schema 0→1, encrypted recovery and clean shutdown. `output/phase6/deployment/evidence.json`. |
| Live cross-datacenter retrieval | NOT TESTED | Safe legitimate scenario unavailable. Protocol handling and deterministic redirect/reference/recovery tests pass; this exclusion is separate from mandatory release gates. |
| Release documentation and secret scan | PASS | Required docs complete; 231 tracked/proposed files scanned, zero actual configured-secret matches/forbidden artifacts; 79 tested runtime fingerprints unchanged. Final committed source identity belongs in artifact manifest. |
| Local tagging policy | PASS | Acceptance precedes clean committed-source artifact verification and annotated tag; commands below enforce final sign-off. Never overwrite existing `v1.0.0`; no external publish/push authorized. |

## Qualified image identity

| Item | Value |
| --- | --- |
| Local image name | `harbor:v1.0.0` |
| Platform | `linux/arm64` |
| Image ID / OCI manifest digest | `sha256:30f3c57ce9e1b5cb41b7de0e1678fa010bdb403fc5b8a091bb9647a9b833c924` |
| Image archive | `harbor-v1.0.0-linux-arm64.tar`, 13,218,816 bytes |
| Image archive SHA-256 | `06ee04f9fa168fbf3572ab0522508e4b9c74a1049da2163f6a9177fe6a9c07a7` |
| `/app/drive` SHA-256 | `088c0d1c1450b5c2e432cba10740c76da205ffe2850099f6a919f6696f54b159` |
| `/app/harbor-recovery` SHA-256 | `c43e5a8d4be0e45766031ebe9b53ef6db7e3804a8aa7e75539b7e5c8c068cb25` |

Final artifact directory: ignored `output/releases/v1.0.0-final/`. Initial candidate/repeated-export evidence remains under `output/releases/v1.0.0/` and `output/phase6/release/`. `release.json` records source commit/tree, platform, immutable base references, image identity and artifact hashes; `SHA256SUMS` covers source archive, image archive and manifest. `INCOMPLETE` indicates an unfinished build and must be absent from a completed release.

Source packaging uses an explicit trusted Git ref, excluding untracked private state and rejecting unsafe archive members. Final source archive includes final documentation; its hash changes from the runtime checkpoint archive while the qualified runtime image remains byte-identical. Final `release.json` and `SHA256SUMS` are authoritative for source artifact identity.

Release builds use `GO_TAGS=telegram`, `SOURCE_DATE_EPOCH=0`, timestamp rewriting and disabled build provenance. Image creation time therefore reads 1970-01-01; this is deliberate reproducibility metadata. Normal Compose adds project/service/version labels and provenance and can retain different layer metadata. Its image ID is different despite identical application payloads. Deploy the qualified immutable image with `--no-build` when reproducing release acceptance.

Reproducibility evidence: `output/phase6/release/actual-release-comparison.json`, `repeated-release-build.log`, `unit-final.log`. All three artifact checksum checks passed. Only the stated `linux/arm64` release was built and qualified; pinned multiarch bases do not qualify other platforms.

## Local build and final sign-off

Requires Python 3.12+, Git and Docker with Buildx. Use a fresh ignored output directory and a new local image tag:

```sh
python3 scripts/release.py --ref v1.0.0 \
  --output output/releases/v1.0.0-rebuild \
  --image harbor-rebuild:v1.0.0 --platform linux/arm64
```

The ref must already exist and be trusted. Packaging never creates Git tags, starts services or pushes images. Existing output paths and image tags are rejected. A different local image name changes archive tag metadata; compare the OCI manifest digest for runtime identity, and compare archive SHA-256 when exporting with the same name/settings. Verify a completed artifact directory with `shasum -a 256 -c SHA256SUMS` from that directory.

1. Complete mandatory live Docker, encrypted restore, checksum, security and browser gates. Record evidence and explicit limitations in [Phase 6 acceptance](PHASE6_FINAL_REPORT.md).
2. Stop isolated authorization clones before resuming the original. Verify original readiness, remove temporary credentials/browser sessions, and preserve original data and intentional encrypted rollback material.
3. Commit reviewed source and final documentation without credentials, keys, sessions, sensitive archives or generated build artifacts. Regenerate and verify source artifact metadata from that accepted documentation commit while retaining the qualified image bytes. Verify all mandatory gates, `git diff --check` and empty `git status --porcelain`.
4. Check `git tag --list v1.0.0`. If present, stop and report conflict; never delete, move or overwrite it.
5. Only after acceptance passes and the worktree is clean, create annotated local tag `git tag -a v1.0.0 -m "Harbor v1.0.0 — verified private personal-use release"`. Verify its commit target and annotation.
6. Verify tag target matches accepted documentation/source metadata and recheck clean worktree. Do not publish or push without separate explicit authorization.

Operations: [production runbook](PRODUCTION_OPERATIONS.md), [recovery quickstart](RECOVERY_QUICKSTART.md), [known limitations](KNOWN_LIMITATIONS.md).

## Current automated releases

The v1.0.0 checklist above is historical; its original tag is not imported into sanitized history. v1.0.1 was published, and v1.1.0 uses the corrected owner-gated publishing workflow. For current results and image identity, see [v1.1.0 qualification](RELEASE_1.1.0.md). Follow [GHCR publishing](GHCR_PUBLISHING_GUIDE.md): explicit owner authorization, clean audited source, successful hosted CI, an unused annotated stable tag, complete release validation, expected package identity/visibility, immutable manifest checks and pulled-image smoke. Never move historical tags. Repository/package visibility and remote application deployment are separate owner actions.
