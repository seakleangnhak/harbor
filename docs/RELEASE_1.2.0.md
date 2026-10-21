# Harbor v1.2.0 release qualification

2026-10-22. **Release status: PENDING.** Owner authorized sanitized-source push, a new release and GHCR image publication. Original repository, deployment, keys, SQLite, sessions and historical tags remain excluded and preserved. Repository/package visibility stays unchanged.

## Scope

- Encrypted resumable 4 MiB browser transport, preserving 100 MiB Telegram storage geometry and existing manifests.
- Preview compatibility, bounded text streaming, thumbnails and retry feedback.
- Owner profile/password editing, atomic session revocation and compatible encrypted recovery.
- Responsive Account/Storage/Recovery UI and simplified fresh-install environment.
- Faster CI with exact-commit release validation, retained package/tag gates and isolated synthetic browser qualification.

## Verification

Local Docker Desktop is unavailable. New account/UI/release changes require fresh hosted execution; earlier successful local preview/transport tests are historical evidence only. Tests must pass before creating the release tag. Actual run URLs, image digest, platform/attestation checks, smoke results and timings will be recorded here after execution.

Initial hosted candidate run [39646590283](https://github.com/seakleangnhak/harbor/actions/runs/39646590283) exposed a browser-test reload race: the helper checked navigation visibility before React mounted. The helper now waits for attachment. Its tmpfs evidence export also omitted screenshots; streamed export and fail-on-missing artifacts replace the silent path. Both production architecture builds and encrypted checksum/restart/recreation smoke checks passed in that run; browser acceptance requires the corrected rerun.

That run's backend failed in Python synthetic test discovery before executing the Go suites. Only those synthetic test diagnostics are now streamed; arbitrary image/tool output stays redacted. Tooling preparation took 7m16s, including 277.7s Docker image export/load and 31.3s GHA cache export. Native Docker tooling builds remove that export/load path; production architecture builds retain Buildx caching. The rerun must establish actual timings and resolve the test failure.

Diagnostic run [39649098387](https://github.com/seakleangnhak/harbor/actions/runs/39649098387) reduced tooling preparation to 48s. Its 57 Python tests exposed four environment errors: three recovery subprocesses blocked by Docker's implicit `noexec` tmpfs, and a release-archive API unavailable in Python 3.11. Explicit test tmpfs `exec` and validated, no-overwrite regular-file extraction address these without changing the production runtime. Browser fixes also handle tooltip-layer dismissal and hidden mobile navigation; full acceptance remains pending.

Run [39650017445](https://github.com/seakleangnhak/harbor/actions/runs/39650017445) completed the account interactions and 32 account screenshot/axe contexts before finding invalid grid semantics in the final empty mobile workspace. The file manager now declares grid/header semantics only when rows exist. Screenshot review also found retained Settings scroll hiding a newly selected section's heading; section-keyed content resets that scroll. New checks cover heading/Refresh visibility. Independent suites and both browser engines now finish collecting evidence before reporting an aggregate failure.

Its completed logs confirmed policy/recovery checks (58 Python tests), regular Go tests and both race suites passed. The live log view had stopped updating; the known-red browser run was canceled during later verification. Vet/reproducibility were not verified by that canceled run. Verbose race output, five-minute package test deadlines and a fifteen-minute command deadline now bound diagnostics. Superseded ordinary CI runs cancel; release publication remains serialized and never cancels an in-progress publication.

Run [39652969598](https://github.com/seakleangnhak/harbor/actions/runs/39652969598) passed all four Chromium suites and WebKit preview/transport/large-directory suites. WebKit account testing found a strict test-locator error when three closed tooltips coexisted during their exit transition. The wait now requires every tooltip to be hidden or detached; modal focus trapping and the single-Escape assertion remain unchanged. Both engines verified 105,906,299-byte encrypted upload/download equality, two storage chunks, retry/resume and actual process restart. Final rerun remains required for complete acceptance.

Baseline: hosted CI run [39584667776](https://github.com/seakleangnhak/harbor/actions/runs/39584667776) took 11m47s. Backend job took 11m43s, including 4m13s for two cold-cache builds. New measurements will be compared honestly; browser checks add coverage absent from that baseline.

## Upgrade and limitations

Back up first; preserve the same volume and effective storage configuration. Do not reinitialize existing data or replace keys. Account password changes revoke every web session; after restore use the password active at backup creation. Finish or pause transfers before changing it.

Pending transport parts require up to one storage chunk of local disk per upload. They are omitted from recovery archives; reselect the original file to resend missing parts after restore. Previews/Range can require one whole 100 MiB authenticated chunk. Native media support depends on browser codecs.

New live Telegram throughput/web authorization and live cross-datacenter retrieval are NOT TESTED in this release task. Synthetic local-adapter tests do not qualify remote Telegram behavior. Personal-use qualification is not an independent security audit, guaranteed provider durability or public-internet readiness.
