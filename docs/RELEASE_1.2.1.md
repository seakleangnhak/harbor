# Harbor v1.2.1 patch qualification

2026-10-22. **HARBOR_V1_2_1_RELEASE = PASS** within the synthetic local-adapter scope below. [GitHub release](https://github.com/seakleangnhak/harbor/releases/tag/v1.2.1). No deployment, visibility change or original runtime state access. Historical v1.2.0 tag/image remain immutable.

## Source and release identity

- Qualified source: `dbbe3d02786b21a4ea3db3721947ab320ed0fe75`.
- Annotated tag: `v1.2.1`; tag object `2005ea13d28e7d0e5d7c79121c572a184fdcaea5`.
- Exact-source [main CI 39661996927](https://github.com/seakleangnhak/harbor/actions/runs/39661996927): **PASS**, all four jobs; 21m03s from creation to final update.
- [Publisher 39665307955](https://github.com/seakleangnhak/harbor/actions/runs/39665307955): **PASS**, all three jobs; 5m12s from first job start to final completion. Exact-commit CI proof took six seconds; full CI was not repeated after tagging.
- Image tags: `ghcr.io/seakleangnhak/harbor:1.2.1`, `:1.2` and `:latest`; immutable index `sha256:87553d2564da6f990e6be18a7b8946833b7600a22b0287d72ad6042077253a74`. Registry and both actual pulled-image checks: **PASS**.

## Defect and fix

v1.2.0 passed its recorded exact-commit CI and published-image checks. Later documentation-only [CI 39657728596](https://github.com/seakleangnhak/harbor/actions/runs/39657728596) failed WebKit preview with two uncaught native stream-controller errors after all 15 screenshot contexts completed. That run remains **FAIL**; earlier passing evidence is not rewritten.

Installed PDF.js 6.4.299 requires an `Error` cancellation reason before setting its internal `isClosed` flag. Harbor supplied no reason. Native cancellation could close the underlying stream while the PDF.js callback rejected before updating that flag; late worker `ENQUEUE`/`CLOSE` messages then reached an already closed stream.

Both bounded PDF-text cancellation sites now supply an `Error`. Four repeated PDF open/zoom/close cycles per engine pass. Exception stack/current-preview diagnostics remain enabled; no browser error filtering, dependency change or broader lifecycle refactor. Existing 256 Ki-character text limit and preview layout remain unchanged.

## Exact-source verification

| Check | Result | Recorded evidence |
| --- | --- | --- |
| Workflow/policy checks, Python tools | PASS | 58 Python tests; 28.105s |
| Go modules, default tests, default/tagged race, both vet variants | PASS | 15 fresh package results, none cached; race suites contain 123/160 top-level tests respectively |
| Reproducible Go executables | PASS | Two identical builds per executable; shared compilation cache, 147.817s then 1.449s; not independent cold builds |
| Frontend tests, TypeScript, production assets | PASS | 41 frontend tests; locked build dependencies |
| Sanitized-history secret scan | PASS | Trusted Gitleaks 8.30.1; 31 reachable commits, zero findings before tagging |
| Native production images | PASS | AMD64 and ARM64 local-adapter image smokes, checksums and persistent restart/recreation |
| Browser regression | PASS | Account, preview, transport and large-directory suites in Chromium and WebKit: eight suites, 110 PNGs, 106 scoped axe checkpoints |
| Browser multipart integrity/restart | PASS | Each engine verified 105,906,299 bytes, two storage chunks, maximum client body 4,194,304 bytes and matching SHA-256 |

Account, preview and transport suites captured zero `pageerror` exceptions. Large-directory suites have no separate exception listener; do not extend that assertion to them. Both engines tested 1,000 folders with 17 rendered list rows, bounded grid cards, no grid overlap, End-key navigation and selected-item drag. Scoped axe checks do not establish full accessibility certification or physical Safari/iOS testing.

## Cache measurements and limits

The prior dependency-only cache could restore an old exact entry without saving newly compiled artifacts. Rolling keys now include platform, Go version, dependency locks and commit SHA, with restoration restricted to the compatible prefix. Go retains its own source/build-flag validation; test commands and release/security gates are unchanged.

| Measurement | Before: CI 39659167222, older cache restored | Final: CI 39661996927, new namespace cold |
| --- | --- | --- |
| Backend job | 1,080s / 18m00s | 1,259s / 20m59s |
| Run creation to final update | 1,084s / 18m04s | 1,263s / 21m03s |
| Tooling image preparation | 75s | 51s |
| Policy/Go/reproducibility step | 957s | 1,164s |
| API default/tagged race package | 134.735s / 139.320s | 163.597s / 163.022s |
| Repeated executable builds, shared cache | 127.117s / 1.281s | 147.817s / 1.449s |

Both runs freshly executed all 15 Go package results. The older dependency-cache hit was not a current-source warm run. Final migration started cold and successfully saved 333,000,107 bytes to main cache ID `9299923521`; the independent cache API receipt confirms persistence. Binary hashes match the prior candidate. Separate hosted runners/cache conditions differ; this is not a controlled throughput comparison and no cold-run speedup is claimed. Follow-up cache-reuse results and timings are recorded separately in the [GitHub release notes](https://github.com/seakleangnhak/harbor/releases/tag/v1.2.1).

## Registry qualification

**PASS:** anonymous registry reads independently verified raw index/child hashes, all three tag aliases, exactly two platform children, descriptor sizes and safe image configuration. Both use nonroot UID 65532, `/app/drive`, version 1.2.1, exact source revision and MIT labels; no sensitive environment keys.

| Platform | Image manifest | Attestation manifest |
| --- | --- | --- |
| `linux/amd64` | `sha256:40f298717bfe147bd5f3877919055810d1086c4386c8f90bafee336bce269a67` | `sha256:2abb5026de17253a177b7264681092ae0ff0e0ffb7fb9dec04ebd8e06fda8f3c` |
| `linux/arm64` | `sha256:56c95192d0a63d1de43e1cf8cc4c64258f092c63b915ef821a16e682ebf2ee2f` | `sha256:bad008312a1533c113cf2cdd8d0943013e4d8a16ace28e4017c91cbe220030d7` |

Both actual immutable platform images passed 104,923,157-byte encrypted multipart checksum, Range, guarded onboarding, auth/CSRF rejection, private permissions, executable/version/health/OCI-label checks, restart/resume, container recreation and clean shutdown. Release runner used native AMD64 and QEMU ARM64; source CI additionally tested native ARM64.

Both attestations contain SPDX 2.3 with 54 packages and SLSA v1 provenance with four materials. Independent review verified raw blob hashes, descriptor bindings and in-toto subjects against platform manifests. Cryptographic signature verification is **NOT TESTED**. Builds received no runtime secrets. Package ID `16107140` remains public and linked to the existing private source repository; no visibility or deployment change.

## Upgrade, recovery and limitations

Create and verify an encrypted backup; preserve the existing volume, effective storage configuration and original content key. Use `ghcr.io/seakleangnhak/harbor:1.2.1` or pin the qualified index digest above. Never reinitialize an existing drive. Account password changes revoke all web sessions; restored access uses the password active when its backup was created.

Pending transport parts require up to one storage chunk of local disk per upload and are omitted from recovery archives; reselect the original file to resend missing parts after restore. Previews/Range can require one complete 100 MiB authenticated storage chunk. Native media support depends on browser codecs.

New live Telegram throughput/web authorization, live cross-DC retrieval, optional MOV playback and optional WebM seeking are **NOT TESTED** in this task. Synthetic local-adapter qualification does not verify remote Telegram behavior, guarantee provider durability, establish an independent security audit or qualify public-internet deployment. Earlier live/recovery evidence retains its earlier scope; see [v1.2.0 qualification](RELEASE_1.2.0.md).

Ignored evidence: `output/release-1.2.1/backend-{before-rolling,rolling}-summary.json`, `backend-rolling-cache-receipt.json`, `ci-rolling-metadata.json`, `browser/run-39661996927/review-summary.json`, `published-run.json`, `published-receipt.json` and `independent-registry-receipt.json`. No secret-bearing runtime artifact is committed.
