# Harbor v1.0 release qualification

2026-10-21, Asia/Phnom_Penh. **HARBOR_V1_RELEASE = PASS for local, private personal use.** Mandatory runtime gates passed on the exact release image below. Live cross-datacenter retrieval remains **NOT TESTED** separately. This release is not independently audited or qualified for public internet deployment, and cannot guarantee Telegram durability.

## Release identity and changes

Runtime source checkpoint: `6fe39cf2b0876fea7068739de7c0e6e0b6752c2d`; baseline `2e2a5df`. Final documentation/qualification-only changes do not alter the runtime payload. The accepted source commit is identified by the local annotated `v1.0.0` tag and final artifact manifest. No external publish or push.

| Item | Qualified value |
| --- | --- |
| Platform / build | `linux/arm64`, production `Dockerfile`, `GO_TAGS=telegram` |
| Image name | `harbor:v1.0.0` |
| Exact image ID / OCI manifest digest | `sha256:30f3c57ce9e1b5cb41b7de0e1678fa010bdb403fc5b8a091bb9647a9b833c924` |
| Image archive | `harbor-v1.0.0-linux-arm64.tar`, 13,218,816 bytes |
| Image archive SHA-256 | `06ee04f9fa168fbf3572ab0522508e4b9c74a1049da2163f6a9177fe6a9c07a7` |
| Drive binary SHA-256 | `088c0d1c1450b5c2e432cba10740c76da205ffe2850099f6a919f6696f54b159` |
| Recovery binary SHA-256 | `c43e5a8d4be0e45766031ebe9b53ef6db7e3804a8aa7e75539b7e5c8c068cb25` |

Validated checkpoints: `6fe39cf` runtime/release/protocol changes; `a22e1c3` resource-sampling and verified clone-cleanup guards. Final documentation checkpoint is the accepted tag target.

Runtime changes are narrow: version 1.0.0, immutable Docker bases/reproducible packaging, and secondary-media `FILE_MIGRATE` recovery through gotd's native primary invoker. No schema, encryption format, key, manifest, API contract or UI redesign. Independent review found no critical data-loss/authentication/startup defect in reviewed paths. Existing Phase 5 risk register and controls remain applicable; [known limitations](KNOWN_LIMITATIONS.md) consolidates residual risks.

The release script builds an explicit committed Git archive, rejects private/unsafe members, pins three base indices, rewrites timestamps with epoch 0, and emits source/image archives, `release.json` and `SHA256SUMS`. Repeated actual export was byte-identical. A separate normal Compose build from exported source matched all 194 `/app` files and runtime configuration except labels. Compose labels/provenance/layer metadata change its image ID; use the qualified image with `--no-build` to reproduce acceptance. Only arm64 was built/qualified.

## Mandatory acceptance

| Requirement | Result | Fresh evidence |
| --- | --- | --- |
| Exact final image, real Telegram | PASS | Intended Compose configuration plus image-only override; running container `.Image` equals qualified digest. Persisted authorization and channel guards accepted; diagnostics `1.0.0`, Telegram connected. |
| Encrypted upload/download and SHA-256 | PASS | Real TXT/PNG/PDF and 4,259,861-byte multipart; complete hash equality, cross-chunk Range, incomplete publication rejected. 1/10/100 MiB additionally verified. |
| Restart, resume and container recreation | PASS | One acknowledged 4 MiB multipart chunk survived clean restart; resumed without replacing it. Forced recreation preserved volume/session and four full-file hashes. Clean shutdown exit 0, no OOM. |
| Complete encrypted backup and fresh restore | PASS | 117,334-byte encrypted archive verified; wrong password rejected. Third fresh volume received only archive/password. Same release CLI restored DB/key/config/session/manifests/owner password; no original host state mounted. |
| Restored Telegram content and metadata | PASS | Four small/multipart files and all three benchmark files downloaded with SHA-256 equality. Nested hierarchy, favorite and Trash survived; normal API restored PDF. Another recovered-container restart retrieved four files again; clean exit 0. |
| Final-image local upgrade/rollback | PASS | Phase 4 schema 0 → v1 → restart → recreate → Phase 4 rollback → v1. Twenty full checksums, unchanged key/eight nodes/four uploads/five chunks, schema 0→1, encrypted CLI recovery, clean exit 0 and private 0700/0600 passed. |
| Security regressions | PASS, scoped | Auth/CSRF/expiry/revocation/throttle/limits, private state, AEAD/nonces and fail-closed recovery tests passed. Exact-image anonymous metadata/diagnostics returned 401; health/ready 200. No blocking finding in focused source review. |
| Browser regressions | PASS, tested views | 18 Chromium/WebKit suites; 22 scoped axe audits, zero violations/overflow. Real release-image browser upload, exact preview, actual saved checksum and HTTP 206 passed. |
| Release docs and reproducible artifacts | PASS | Checklist, limitations, recovery quickstart, changelog, README/operations and this report; local committed-source image/source artifacts and checksums. Final manifest records accepted source. |

Live harness: `scripts/phase6_live_check.py`. Original host was stopped before session copy; its exclusive data lock remained held through all clone lifetimes and cleanup. Only a copied authorization entered fresh isolated volumes; original database/key were never mounted. All live containers/helpers/private volumes and temporary browser state were removed before original resumed. No preexisting remote messages were deleted. Generated remote fixtures were retained; remote cleanup is intentionally conservative.

Complete archive SHA-256: `80630fb8560ff5493582d850345ba6c3e99479897bb9e78fd24853f81376d9e9`. This sensitive archive/password/session were removed after the isolated drill; sanitized evidence remains. A valid archive does not independently preserve Telegram ciphertext.

| Generated fixture | Bytes | SHA-256 after restoration/restart |
| --- | ---: | --- |
| TXT | 68 | `3b6a71807883d0e90152520d2645a2576f14ab25eab852c9fe55fc839e8e4c85` |
| PNG | 121 | `317529469342c7b56488a4caa1e1161b7203007faba60e848622c642ece27713` |
| PDF | 612 | `c166d36a1af90db904d04ae50a6f8988ea5bdaff1d6548cdd60abe7a412ad3b1` |
| Multipart | 4,259,861 | `e609159371697ca949b503bcabf27ddfde224ea08f00dcdf5efa6328da1f9e05` |

Live browser: 126,976-byte generated text, SHA-256 `02d983fe8c49693f3e36b08fb15fe5cda9493e282950fa2bab13e407ed4137c9`. Browser saved bytes independently hashed equal; preview exact, Range 206, queue completed. Screenshot visually reviewed, no critical layout defect. Temporary HTTP session revoked (204; subsequent session request 401), browser closed.

## Performance, measured final image

One controlled sequential run, same deterministic bytes and HTTP benchmark method as Phase 3. Application chunks 4 MiB; 512 KiB MTProto parts; four engine slots, uploader worker ceiling two, default small-document part requests sequential. No concurrency increase or speculative optimization. Upload timing includes completion's full remote re-read; download timing includes streaming byte/hash checks. Network conditions vary; these are observations, not capacity guarantees.

| Size | Upload seconds | Upload MiB/s | Included verification seconds | Download seconds | Download MiB/s | SHA-256 |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| 1 MiB | 0.876543 | 1.140846 | 0.297002 | 0.271795 | 3.679246 | PASS |
| 10 MiB | 5.813045 | 1.720269 | 1.905482 | 2.301617 | 4.344771 | PASS |
| 100 MiB | 61.484365 | 1.626430 | 24.592902 | 23.737983 | 4.212658 | PASS |

100 MiB Phase 3 optimized host: 1.354059 upload / 4.040140 download MiB/s. Final container observed +20.12% / +4.27%; host-vs-container and sequential network variability prevent attributing this to the routing fix. No major observed throughput regression. Total three-size benchmark 94.577 seconds; retry telemetry stayed 0→0, transport connected. Counters are process-local observations, not wire-request counts.

Compose start to `/ready`: **0.760 seconds**, image already loaded. Two idle `docker stats` samples over a three-second window: maximum **5.059 MiB working set**, 0.53% CPU. During benchmark, 48 samples: maximum **38.270 MiB working set**, 10.5% CPU. Docker no-stream sampling is coarse; working set differs from host RSS and does not establish lifetime peak or CPU time. Phase 3 host sampled RSS 61.5 MiB and CPU 14.8% are not directly comparable. True lifetime peak, sustained load, ceiling concurrency and live 512 MiB transfer remain NOT TESTED.

Synthetic 1,000-entry browser reload/load: Chromium **132 ms**, WebKit **167 ms**; 17 rendered list rows and 50/80/50 sampled grid cards, no overlap/automatic content fetch, last entry reachable. Phase 5 Chromium 134 ms is comparable single-run evidence. Main JS remains 535.85 kB / 170.40 kB gzip; PDF code/worker lazy. Browser 23 tests and production typecheck/build passed without new dependencies.

## Fresh regression commands

Synthetic commands disable implicit project secrets with `DRIVE_ENV_FILE=-`. Raw logs and sanitized JSON retained under ignored `output/phase6/`; reusable harnesses are tracked.

| Command / suite | Result |
| --- | --- |
| `go test -count=1 ./...` | PASS, 6.960s command |
| `GOGC=40 go test -count=1 -p 1 -tags telegram ./...` | PASS, 19.889s |
| `go test -count=1 -race ./...` | PASS, 82.435s |
| `GOGC=40 go test -count=1 -race -p 1 -tags telegram ./...` | PASS, 124.395s |
| Default and tagged `go vet` | PASS, no diagnostics |
| `cd web && npm test`, `npm run typecheck`, `npm run build` | PASS, 23 tests; existing bundle warning |
| `python3 -m unittest discover -s scripts -p '*_test.py'` | PASS, 24 tests, 2.470s |
| Local process `scripts/acceptance.py` | PASS, auth/origin, restart/resume/hash/Range, hierarchy, zero bytes, encrypted restore/revocation |
| `scripts/phase5_copy_check.py` on final local binary | PASS, actual SIGKILL after one 4 MiB chunk; 33,554,449 bytes recovered in nine chunks, hash/atomic visibility/cancellation/original preservation |
| Native Playwright CLI, nine suites per engine | PASS, Chromium and WebKit; CRUD/clipboard/previews/mobile/themes/retries/copy recovery/1,000 items/Settings |
| Scoped axe, 11 audits per engine | PASS, zero violations in tested views; not full accessibility certification |
| Default/tagged `govulncheck`, `npm audit --json` | PASS, no affected Go symbols/imported packages; unused OpenPGP module-only advisory remains; npm zero advisories/220 dependencies |
| Tagged migration/reference/reconnect controlled tests | PASS; exact reference/offset/partial bytes, bounded fallback, terminal errors and cancellation; upstream gotd reconnect tests passed |
| `scripts/phase5_deployment_check.py`, isolated Phase 6 project/volume override | PASS, local adapter; final-image upgrade/restart/recreate/rollback/CLI restore and 20 checksums; owned containers/volumes/password removed |
| `scripts/release.py`, repeated export and archived-source Compose build | PASS, exact archive repetition and application-payload parity |
| `scripts/phase6_live_check.py --source-env .env --image sha256:30f3c57ce9e1b5cb41b7de0e1678fa010bdb403fc5b8a091bb9647a9b833c924 --output output/phase6/live --browser-hold` | PASS, live final-image transfer/restart/recreate/restore/cleanup |

Evidence index: `output/phase6/live/{evidence,fixtures,benchmark,browser-complete,http-boundaries}.json`; `output/phase6/qa/QA_REPORT.md`; `output/phase6/security/REVIEW.md`; `output/phase6/storage/REVIEW.md`; `output/phase6/release/actual-release-comparison.json`; `output/phase6/deployment/{evidence,summary,cleanup}.json`. Logs/artifacts remain local and ignored; no credentials belong in reports/Git. Actual configured-secret scan covered 231 tracked/proposed files with zero matches/forbidden artifacts; 79 runtime source fingerprints still matched full QA.

## Cross-datacenter result and residual scope

**Live cross-datacenter retrieval = NOT TESTED.** No safe legitimate other-DC fixture available. Installed gotd v0.162.0 source routes primary `FILE_MIGRATE` through cached authorized target connections. A selected media pool now delegates once to that primary path, preserving pending offset/reference/context; deterministic tests cover partial reads, reference refresh after expired-reference failure, reconnect and safe terminal missing/access/session failures. No custom redirect loop or attempt to force provider placement/rate limits. CDN delivery is not qualified.

Provider account/channel/message loss, key/password loss, host plaintext metadata, backup/copy ceilings, orphan ciphertext, browser-managed 32 MiB limit and WebKit-versus-real-Safari scope remain explicit in [known limitations](KNOWN_LIMITATIONS.md). Recovery depends on trusted compatible software, separately held archive password and continued Telegram content/account access; it does not depend on undocumented original-machine key/config paths. Follow [recovery quickstart](RECOVERY_QUICKSTART.md) and [operations](PRODUCTION_OPERATIONS.md).

Original Telegram service is preserved on loopback 8081; original Docker 8080 untouched. Test deployments stop and private temporary state is removed before handoff. Final release artifacts stay local; release checklist governs clean-tree annotated tagging. Public readiness, independent audit and live cross-DC remain outside this PASS.
