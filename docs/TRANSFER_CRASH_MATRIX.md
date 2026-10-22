# Transfer crash qualification

Status: **PASS for controlled child-process crash coverage**. This report does
not claim nine live Telegram crash tests or power-loss durability.

## Method and scope

`internal/api/milestone_a_crash_test.go` starts the compiled Go test executable as
a child, opens real SQLite WAL databases and encrypted staging files, and drives
the production HTTP handlers through `httptest`. Test-only SQLite triggers and
storage wrappers stop execution at deterministic boundaries. The parent sends
`SIGKILL`, opens the database again, resumes storage, completes publication, and
checks recovered plaintext against its original SHA-256.

Remote storage in these tests is a controlled disk-backed local adapter. It can
stop halfway through a write, return an acknowledged object, or stop during
verification. No production failure hooks, Telegram credentials, owner files,
or existing remote messages are involved. The child is a test executable rather
than the release server executable.

The deterministic crash fixture is **8 MiB + 17 bytes**, with **8 MiB storage
chunks** and **4 MiB browser parts**. Smaller storage geometry keeps nine real
process kills inexpensive while exercising multiple parts and multiple chunks.
The separate memory qualification uses the production **100 MiB** storage
geometry; it does not substitute for live Telegram memory measurement.

## Nine-boundary matrix

Every row passed SQLite `integrity_check`, preservation of a previously
completed sentinel, rejection of premature file lookup, final encrypted
roundtrip, exact manifest chunk count, and idempotent completion producing one
visible node.

| SIGKILL boundary | Accepted bytes recovered | Remote bytes recorded | Original browser bytes needed | Remote orphan candidates | Result |
| --- | ---: | ---: | --- | ---: | --- |
| Before first part acknowledgement, inside its SQL transaction | 0 | 0 | Yes; no part was acknowledged | 0 | PASS |
| After first part acknowledgement | 4,194,304 | 0 | Yes; remaining parts were never sent | 0 | PASS |
| After all parts accepted, before queueing | 8,388,625 | 0 | No; commit request must be retried | 0 | PASS |
| After queueing, before worker starts | 8,388,625 | 0 | No | 0 | PASS |
| During remote write | 8,388,625 | 0 | No | 1 | PASS |
| After remote acknowledgement, before local manifest acknowledgement | 8,388,625 | 0 | No | 1 | PASS |
| Inside manifest transaction before commit | 8,388,625 | 0 | No | 1 | PASS |
| During final integrity verification | 8,388,625 | 8,388,625 | No | 0 | PASS |
| Verified, before API node publication | 8,388,625 | 8,388,625 | No | 0 | PASS |

Accepted bytes include durable staged parts and remotely committed chunks;
remote bytes count only committed manifests. The distinction is intentional.
An acknowledged browser part is never inferred to be remotely stored.

The final strengthened default run completed all nine cases in **2.73 s**. Concurrent copy and
queued-upload coverage completed in **0.26 s**. These durations describe local
test execution, not Telegram transfer throughput.

## Recovery semantics

- Queued and interrupted `storing` chunks restart through `RunPending` using
  persisted staging data, subject to the three-attempt restart budget. Exhausted
  attempts become explicitly failed and require retry. No browser file
  reselection is needed when all bytes were accepted and the corresponding
  chunks were queued.
- Accepted `receiving` parts survive restart, but they are not automatically
  queued. The client must resend the chunk-commit request.
- Final verification and visible file publication require the existing
  `/complete` request. Restarting the worker alone does not publish a file.
  Retrying completion after verification is idempotent.
- Parts never sent or never acknowledged must be supplied again. Browser source
  `File` handles do not survive refresh; automatic recovery cannot reconstruct
  those bytes. These tests explicitly supply the original generated source for
  the two incomplete-input cases.
- Explicit remote failure is retained as failed/retryable state rather than
  silently declared complete. Existing pending-pipeline tests verify retry,
  cancellation, missing/corrupt staging, duplicate commits, and restart states.

## Orphan and durability limits

Remote acknowledgement and SQLite commit cannot form one transaction. The
controlled tests deliberately observe one orphan candidate after an interrupted
remote write, a lost remote acknowledgement, and a precommit crash. Retrying
restores file integrity and never deletes the original completed file. Automatic
remote orphan deletion is outside this qualification; uncertain objects must
not be deleted indiscriminately.

A killed part transaction can leave an encrypted staging file that has no
committed row. Current queue limits bound live reservations, not an unlimited
number of filesystem leftovers accumulated across repeated crashes. No
automatic orphan sweep is claimed. Keep spare disk capacity and investigate
untracked staging through controlled maintenance before deleting anything.

`SIGKILL` verifies process termination and SQLite transaction rollback. Host
power failure, filesystem failure, full disk, and loss of the persistent volume
are different failures and are **NOT TESTED** by this matrix.

## Production-geometry mixed workload

`TestMilestoneAProductionMemoryBudget` is opt-in through
`HARBOR_MILESTONE_MEMORY=1`. It runs:

- One **100 MiB + 17 byte** background upload with 4 MiB transport parts.
- A simultaneous **100 MiB** persisted folder-copy job.
- Two overlapping **256 KiB** HTTP ranges of a completed file.
- Full SHA-256 checks of the original file, new upload, and copied file.

Result: **PASS**, 2.24 s test duration. Observed maximum concurrent remote
writers: **1**. Pending chunks while the controlled writer was blocked: **2**;
the test checks the global pending count is at most **4** at that boundary.

Runtime containment: Linux arm64 container, non-root UID 65532, read-only root,
no network, two CPU limit, 768 MiB hard memory/no swap, `GOMEMLIMIT=512MiB`,
disk-backed isolated fixture volume. It exited zero without OOM kill.

| Measurement | Observed value | Interpretation |
| --- | ---: | --- |
| Cgroup lifetime memory peak | 805,310,464 bytes / approximately 768 MiB | Includes Go process, file cache, fixture setup, and sampling helpers; reached the hard limit with one-page kernel accounting overshoot |
| Sampled anonymous memory maximum | 498,253,824 bytes / 475.17 MiB | Includes process heaps/stacks and anonymous allocations; not an exact application RSS or lifetime peak |
| Sampled file cache maximum | 272,330,752 bytes / 259.71 MiB | Separate sampled maximum; need not coincide with anonymous maximum |
| Sampled scratch maximum | 315,244,177 bytes / 300.64 MiB | Sampled every approximately 0.5 s; not a guaranteed lifetime disk peak |
| Final cgroup CPU usage | 2.428 CPU seconds | Includes fixture setup and sampling helpers; controller elapsed 2.339 s |

The pre-start shell measured approximately 1.36 MiB cgroup memory; this is a
waiting test wrapper, **not Harbor application idle RSS**. The short local run
does not establish long-duration steady-state memory, full four-slot staging
disk pressure, Telegram adapter buffers, or an adequate production memory
recommendation. Those require separate application/live measurements.

## Reproduce

Ordinary focused crash and mixed-workload coverage:

```sh
go test -count=1 -run '^TestMilestoneA(UploadProcessCrashMatrix|ConcurrentCopyAndQueuedUpload)$' -v ./internal/api
go test -count=1 -race -run '^TestMilestoneA(UploadProcessCrashMatrix|ConcurrentCopyAndQueuedUpload)$' -v ./internal/api
```

Production-memory coverage must run separately in an isolated container with
the documented memory budget and a writable fixture volume:

```sh
HARBOR_MILESTONE_MEMORY=1 GOMEMLIMIT=512MiB \
  go test -count=1 -run '^TestMilestoneAProductionMemoryBudget$' -v ./internal/api
```

Precompile the test executable outside the restricted runtime for comparable
measurements. Running the compiler inside the runtime memory budget changes the
measurement and can fail independently of application behavior.

## Evidence

Frozen production Go source and binaries for the full-suite rerun:

| Artifact | SHA-256 |
| --- | --- |
| `tested-source.json` | `0a504191fdeb14849a6052486563dcbac3f93a97c25af8f407b622cd66774e09` |
| Crash test before child-race strengthening | `1a16c1cac1ffbc8394cc189520f787e34e50f2de496ee5c1f9dbb98f7baa2a4c` |
| `internal/storage/pipeline.go` | `8398a57029fb67cd0bed5e6fb925965cbb3c24fc123bba683e412dc6d43d50a5` |
| Compiled `api.test` | `55c90f6f4b98647d6420656f4ff2e6e6c60231476b1498d7fe2de8eff57cbd98` |
| Telegram-tagged `storage-live.test` | `82f771fb5901a82370b2feea7f27a9c1bf0132134cbf8e6c670cb178a13daf20` |

The isolated tooling image `harbor:milestone-a-reliability-final` has local
image ID
`sha256:ff5fa6b047dbb32cde879ea1fa2e735e8b11841dd770bf7f043db8fb6559800e`.
It uses Go **1.26.10 linux/arm64**, `CGO_ENABLED=1`, and
`GOFLAGS='-buildvcs=false -trimpath'`. This is a source/tooling image, not the
production release image.

One test-only strengthening followed that freeze: child processes explicitly
use `GORACE=halt_on_error=1`, and captured `WARNING: DATA RACE` output fails the
parent test even when termination was expected. This prevents intentional
`SIGKILL` from hiding a race warning. Production Go sources and the live storage
test binary did not change. The stronger test passed four focused variants and
both API vet variants:

| Focused run | Crash matrix | Mixed copy/upload | Result |
| --- | ---: | ---: | --- |
| Default | 2.73 s | 0.26 s | PASS |
| Race | 22.14 s | 1.77 s | PASS |
| Telegram-tagged | 3.03 s | 0.28 s | PASS |
| Telegram-tagged race | 22.54 s | 1.83 s | PASS |

The strengthened test source SHA-256 is
`e906801625e5d88c5370254f059dd450a2ff7da6b8b8ad31aafdbd19d086eae8`.
Its `focused-source.json` manifest SHA-256 is
`6d99f6f4683a81bf6ef88753b688889ef9967aa3828871c5afceca454de7e437`.
Its non-race `api-strict.test` binary SHA-256 is
`f6cdb5520208fe12a439682e3becedf23a226e86db9c01c9f6e58febf45f3d7e`.
Tooling image `harbor:milestone-a-reliability-final-tests` has local image ID
`sha256:118ca5112bc4a02db56d60b2238eb6ddded392be95746207a389882c66c0ef50`.
The opt-in memory workload was unchanged, so its result above remains tied to
the recorded `api.test` binary rather than relabeled as a new measurement.

## Full regression and compiler environment

All commands in the final full-suite chain exited zero:

```sh
go mod verify
go test -count=1 -p 1 -timeout 8m ./...
go test -count=1 -p 1 -race -timeout 8m -v ./...
go test -count=1 -p 1 -tags telegram -timeout 8m ./...
go test -count=1 -p 1 -race -tags telegram -timeout 8m -v ./...
go vet -p 1 ./...
go vet -p 1 -tags telegram ./...
```

This includes encrypted backup/restore, malformed archives, missing/corrupt
chunks, authentication, folder-copy recovery, bounded queues, cancellation,
retry, and manifest-validation regression tests. Ordinary Telegram-tagged tests
use doubles; gated live tests skip without explicit coordinator authorization.
The five tested packages passed in each of the four test variants. Final
Telegram-tagged race durations were: CLI 11.234 s, API 148.523 s, onboarding
1.036 s, recovery 54.231 s, and storage 18.461 s.

Compilation and tests used an isolated **8 GiB Colima VM**, dedicated 24 GiB
disk, a **6.5 GiB** hard/no-swap container, `GOMEMLIMIT=4608MiB`, `GOGC=30`,
two CPU limit, `-p 1`, 512 PID limit, non-root execution, read-only source/root,
and no network. Dependencies were already provisioned. Race compilation of
`github.com/gotd/td/tg` succeeded in this environment. A separate lower-budget
3 GiB priority compiler attempt was killed; that was a compiler resource failure,
not a race failure. The final adequate-memory tagged race run completed without
disabling or weakening race detection.

Ignored local evidence under `output/milestone-a/reliability/`:

- `crash-focus.log`: nine process kills and mixed-workload results.
- `memory-test.log`, `memory-result.json`: production geometry and resource
  samples; no private runtime values.
- `tested-source.json`: SHA-256 mapping of the frozen tested source.
- `full-go.log`, `qualification-result.json`: full regression output, exact
  command/resource/source metadata, and successful chain exit status.
- `focused-source.json`, `strict-crash.log`: the test-only strengthening and
  all four focused reruns; both API vet commands also exited zero.

The coordinator's final report records the final integrated source identity,
full-suite results, Docker qualification, and live Telegram coverage separately.
