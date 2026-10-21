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

Baseline: hosted CI run [39584667776](https://github.com/seakleangnhak/harbor/actions/runs/39584667776) took 11m47s. Backend job took 11m43s, including 4m13s for two cold-cache builds. New measurements will be compared honestly; browser checks add coverage absent from that baseline.

## Upgrade and limitations

Back up first; preserve the same volume and effective storage configuration. Do not reinitialize existing data or replace keys. Account password changes revoke every web session; after restore use the password active at backup creation. Finish or pause transfers before changing it.

Pending transport parts require up to one storage chunk of local disk per upload. They are omitted from recovery archives; reselect the original file to resend missing parts after restore. Previews/Range can require one whole 100 MiB authenticated chunk. Native media support depends on browser codecs.

New live Telegram throughput/web authorization and live cross-datacenter retrieval are NOT TESTED in this release task. Synthetic local-adapter tests do not qualify remote Telegram behavior. Personal-use qualification is not an independent security audit, guaranteed provider durability or public-internet readiness.
