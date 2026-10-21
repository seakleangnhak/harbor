# Harbor v1.2.0 release qualification

2026-10-22. **HARBOR_V1_2_RELEASE = PASS** within the synthetic local-adapter scope below. [GitHub release](https://github.com/seakleangnhak/harbor/releases/tag/v1.2.0). Owner authorized sanitized-source push, a new release and GHCR image publication. Original repository, deployment, keys, SQLite, sessions and historical tags remain excluded and preserved. Source repository remains private; existing GHCR package remains public. No visibility change or deployment.

## Scope

- Encrypted resumable 4 MiB browser transport, preserving 100 MiB Telegram storage geometry and existing manifests.
- Preview compatibility, bounded text streaming, thumbnails and retry feedback.
- Owner profile/password editing, atomic session revocation and compatible encrypted recovery.
- Responsive Account/Storage/Recovery UI and simplified fresh-install environment.
- Faster CI with exact-commit release validation, retained package/tag gates and isolated synthetic browser qualification.

## Verification

Local Docker Desktop is unavailable. Fresh checks ran in disposable GitHub-hosted containers: no network during target execution, synthetic local-adapter fixtures, read-only source/tooling, nonroot users and bounded memory/CPU/scratch. Earlier local results below remain historical. Successful exact-commit CI preceded the annotated `v1.2.0` tag; no tag was moved.

### Accepted source and hosted checks

- Release commit: `335b1a423f3cb530384ef395bcd0e9e91b78986b`.
- **PASS:** [main CI 39655816126](https://github.com/seakleangnhak/harbor/actions/runs/39655816126), all four jobs, 4m12s overall.
- **PASS:** workflow/actionlint/README/policy checks and 58 Python tests; Go module verification, default tests, default/tagged race tests and both vet variants. [Cold backend 39652969598](https://github.com/seakleangnhak/harbor/actions/runs/39652969598/job/119406211019) executed 123 default-race and 160 tagged-race top-level tests. Accepted main run reused cached Go test results; its Python checks ran afresh.
- **PASS:** 41 frontend tests, TypeScript checks and production build using locked dependencies.
- **PASS:** native AMD64 and ARM64 production-image builds and isolated encrypted local-adapter smokes: 104,923,157-byte multipart checksum, Range, guarded onboarding, auth/CSRF rejection, private state permissions, restart/resume, container recreation, health and clean shutdown.
- **PASS:** all eight Chromium/WebKit account, preview, transport and large-directory suites; 110 screenshots and 106 axe checkpoints, zero reported browser exceptions. Reviewed 12 key desktop/mobile light/dark screenshots; no critical visual defects found. WebKit here is Linux automation, not physical Safari/iOS certification.
- **PASS:** each browser transferred 105,906,299 bytes using requests at most 4,194,304 bytes, two storage chunks, injected part/commit failures, pause/reload/reselection and an actual process restart. Download SHA-256: `6029196e8fead8bd9005dd9f756b85228a075503dada2f5de6dbc7ab6e9f6994`. Accepted parts were not resent after recovery.
- **PASS:** each browser's 1,000-folder view kept 17 rendered list rows and 50–80 grid cards, keyboard/selected-drag behavior and zero automatic content requests with thumbnails disabled.
- **PASS:** trusted Gitleaks 8.30.1 scanned all 28 sanitized reachable commits immediately before tagging; zero findings. No original development history or runtime state entered the release.

### Measured CI changes

| Measurement | Before | Current | Scope |
| --- | --- | --- | --- |
| Backend tooling preparation | 7m16s | 58s in accepted run | Removed Buildx tooling export/load and GHA tooling-image export; earlier diagnostic about 47s; network/cache conditions differ |
| Backend job | 17m42s cold | 1m45s warm | Cold run actually executed Go suites; warm run reused cached results, not a controlled compiler-only comparison |
| Complete accepted CI | Historical baseline 11m47s | 4m12s warm | New run adds isolated browser coverage; not an equivalent cold-cache benchmark |
| Repeated binary builds | Historical independent cold builds 125.836s + 126.032s | Cold qualification 122.784s + 1.213s; warm 1.352s + 1.340s | Both current builds share compilation cache; no independent-cold-build claim |

Default main/PR CI has four parallel jobs, native architecture runners, branch-scoped Go cache and cancellation of superseded ordinary runs. Stable publishing reuses successful CI for the exact commit, rejects a newer failed/running attempt, and retains serialized noncanceling publication and owner/tag/package gates. PR checks receive no publishing credentials.

Repeated `drive` SHA-256: `ec20086533e4c63e10bcb02db403d939804d1d1dacd691f01f336d72a0ce7881`; `harbor-recovery`: `2ffb2de8c600131347edb44953f0d62b2c889d7ec87e130f648c965f32911209`. Binary equality does not imply globally identical image attestations.

### Registry qualification

**PASS:** [Docker release 39656653561](https://github.com/seakleangnhak/harbor/actions/runs/39656653561), all three jobs, 5m39s. Owner-only annotated-tag/main-ancestry gate, exact successful CI proof, pinned package identity/visibility and built-in-token publication all passed. Annotated tag object `0afc346932ed2e4fc3f0cc9c00d493ebcb34f5d0` resolves to the accepted commit above.

Image tags `ghcr.io/seakleangnhak/harbor:1.2.0`, `:1.2` and `:latest` resolve to the same immutable index:

```text
sha256:13ea2d9146ce0151684cc232d44b79bc3edd68e026dfb7fba2a6f75c04d27378
```

| Platform | Image manifest | Attestation manifest |
| --- | --- | --- |
| `linux/amd64` | `sha256:c6324806ae033fb402a48e0ed4bd198da86a57af287fb21792f9e1c3944622ff` | `sha256:b85f6eeac75acc4bb642457fc3640bc61d319e800fc76a299406f82f04f9ae7b` |
| `linux/arm64` | `sha256:0e15653565c792fa74c1a01ae1406ffc5bd85b8266de0c96189caa5f55f34594` | `sha256:9fe08306008309b19b0f72e5143cba97e13213e8d4a96dc622120200544a3d26` |

Both actual published images were pulled by immutable reference and passed the 104,923,157-byte encrypted multipart checksum, Range, guarded onboarding, auth/CSRF, permissions, health/executable/version/OCI-label, restart/resume, container recreation and clean-shutdown checks. The release runner used native AMD64 and QEMU ARM64; preceding main CI additionally smoke-tested native ARM64. All use synthetic local storage, not Telegram authorization.

Independent anonymous registry reads verified index/child bytes, tag aliases, nonroot UID 65532, `/app/drive`, version/revision/MIT labels and absence of sensitive environment keys in image configuration. Both platform attestations contain SPDX 2.3 (54 packages) and SLSA v1 (four materials) with raw in-toto subjects matching the platform digest. Workflow verification checks descriptor bindings/predicates; independent review additionally checked raw subjects. Cryptographic signature verification is **NOT TESTED**. Builds received no runtime credentials.

Package ID `16107140` remains public and linked to the intended private repository. Annotated source and published digest are preserved; final report receipts are committed after the immutable release tag. Original local `v1.0.0` remains `f6c49fcd2a52e365c6cfe84395245112a105f929` and is absent from sanitized remote history.

Ignored evidence: `output/release-1.2.0/accepted-ci.json`, `backend-qualified-{cold,warm}.log`, `frontend-qualified.log`, `browser/run-39655816126/`, `published-qualified.log`, `published-run.json` and `independent-registry-receipt.json`. Raw hosted logs/artifacts have GitHub retention limits; this tracked report preserves essential receipts. No secret-bearing runtime artifact is committed.

### Earlier diagnostic runs

Initial hosted candidate run [39646590283](https://github.com/seakleangnhak/harbor/actions/runs/39646590283) exposed a browser-test reload race: the helper checked navigation visibility before React mounted. The helper now waits for attachment. Its tmpfs evidence export also omitted screenshots; streamed export and fail-on-missing artifacts replace the silent path. Both production architecture builds and encrypted checksum/restart/recreation smoke checks passed in that run; browser acceptance requires the corrected rerun.

That run's backend failed in Python synthetic test discovery before executing the Go suites. Only those synthetic test diagnostics are now streamed; arbitrary image/tool output stays redacted. Tooling preparation took 7m16s, including 277.7s Docker image export/load and 31.3s GHA cache export. Native Docker tooling builds remove that export/load path; production architecture builds retain Buildx caching. Later successful runs above resolved those failures and established timings.

Diagnostic run [39649098387](https://github.com/seakleangnhak/harbor/actions/runs/39649098387) reduced tooling preparation to 48s. Its 57 Python tests exposed four environment errors: three recovery subprocesses blocked by Docker's implicit `noexec` tmpfs, and a release-archive API unavailable in Python 3.11. Explicit test tmpfs `exec` and validated, no-overwrite regular-file extraction address these without changing the production runtime. Browser fixes also handle tooltip-layer dismissal and hidden mobile navigation; acceptance remained pending at that stage.

Run [39650017445](https://github.com/seakleangnhak/harbor/actions/runs/39650017445) completed the account interactions and 32 account screenshot/axe contexts before finding invalid grid semantics in the final empty workspace. The file manager now declares grid/header semantics only when rows exist. Screenshot review also found retained Settings scroll hiding a newly selected section's heading; section-keyed content resets that scroll. New checks cover heading/Refresh visibility. Independent suites and both browser engines now finish collecting evidence before reporting an aggregate failure.

Its completed logs confirmed policy/recovery checks (58 Python tests), regular Go tests and both race suites passed. The live log view had stopped updating; the known-red browser run was canceled during later verification. Vet/reproducibility were not verified by that canceled run. Verbose race output, five-minute package test deadlines and a fifteen-minute command deadline now bound diagnostics. Superseded ordinary CI runs cancel; release publication remains serialized and never cancels an in-progress publication.

Run [39652969598](https://github.com/seakleangnhak/harbor/actions/runs/39652969598) passed all four Chromium suites and WebKit preview/transport/large-directory suites. WebKit account testing found a strict test-locator error when three closed tooltips coexisted during their exit transition. The wait now requires every tooltip to be hidden or detached; modal focus trapping and the single-Escape assertion remain unchanged. Both engines verified 105,906,299-byte encrypted upload/download equality, two storage chunks, retry/resume and actual process restart. Accepted rerun 39655816126 passed every suite.

Baseline: hosted CI run [39584667776](https://github.com/seakleangnhak/harbor/actions/runs/39584667776) took 11m47s. Backend job took 11m43s, including 4m13s for two cold-cache builds. New measurements will be compared honestly; browser checks add coverage absent from that baseline.

## Upgrade and limitations

Back up first; preserve the same volume and effective storage configuration. Do not reinitialize existing data or replace keys. Account password changes revoke every web session; after restore use the password active at backup creation. Finish or pause transfers before changing it.

Pending transport parts require up to one storage chunk of local disk per upload. They are omitted from recovery archives; reselect the original file to resend missing parts after restore. Previews/Range can require one whole 100 MiB authenticated chunk. Native media support depends on browser codecs.

New live Telegram throughput/web authorization and live cross-datacenter retrieval are NOT TESTED in this release task. Synthetic local-adapter tests do not qualify remote Telegram behavior. Personal-use qualification is not an independent security audit, guaranteed provider durability or public-internet readiness.

Video seeking/MOV playback were **NOT TESTED** in this hosted browser run because optional media fixtures were absent. Earlier successful local media checks retain their earlier scope. No deployment or visibility change was performed.
