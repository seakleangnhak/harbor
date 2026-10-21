# Onboarding UI review

2026-10-21 · Baseline `87f8f29` · First-run web onboarding only.

**ONBOARDING_UI_REFINEMENT = PASS** for the local scope below.

## Initial audit

- Storage form exceeded the 900px desktop viewport; primary action was below the fold. Stacked provider cards and credential fields wasted available width.
- Mobile repeated long guidance and large gaps; the storage page reached roughly 1,230px at a 390px width.
- Secret reveal controls shared generic names. Owner-password reveal remained enabled during submission. Token-to-owner and Telegram authorization transitions lacked focus handoffs.
- Local storage, QR authorization, verified connection and review states needed clearer hierarchy. Review declared the drive ready before saving and exposed internal chunk geometry instead of useful recovery guidance.
- Existing light/dark colors were consistent, but setup offered no theme control.
- Protected status could not distinguish idle `waiting` from an active connection or two-step check. Reloading could hide an unfinished Telegram flow and expose a conflicting Connect action.

## Implemented

- Retained Harbor's evergreen/stone tokens, serif introduction, brand and existing components. Scoped styling; no dependencies or global workspace redesign.
- Balanced setup shell, numbered progress with descriptions, compact native provider cards, desktop API credential row and touch-friendly mobile fields. Storage action fits both tested desktop viewport heights without scrolling; mobile forms scroll normally.
- Persistent theme preference through the existing `harbor:theme` key. Credentials stay in memory; no credential URL, browser storage or server-error echo added.
- Distinct local, QR/loading, two-step, verified and final-review states. Verified connections can be changed before saving through the existing cancellation endpoint.
- Unique reveal labels, pressed states, disabled pending controls, validation associations and focus on the relevant password field or error summary. Step transitions focus the heading; two-step transitions focus its input.
- Protected `flow_active` status restores an unfinished connection after token re-entry. Public status remains only `{required:boolean}`. Existing worker/session exclusion remains unchanged.
- Correct retry guidance while a canceled worker finishes and for the server's 15-minute setup throttle. Accepted trailing-slash channel links normalize before submission.
- Explicit heading/review color tokens fix WebKit retaining light-theme text colors during switching. Reduced-motion preference remains supported.

## Verification

All target code ran inside resource-limited Docker sandboxes: no external network, clean environment, read-only source/tooling and disposable writable scratch. Tests used synthetic secrets and generated files. Original application/data/session and remote deployment were not changed.

| Check | Result | Evidence |
| --- | --- | --- |
| `go test -p 1 ./...` | PASS | All default-build packages; startup/recovery/storage regressions included |
| `go test -race -p 1 ./...` | PASS | All default-build packages; onboarding state/cancellation tests included |
| `go vet -p 1 ./...` | PASS | Default build |
| `go build -p 1 -o /scratch/drive-browser ./cmd/drive` | PASS | Actual fixture server used by browser checks |
| `npm test` | PASS | 29 tests; added channel normalization and safe retry assertions |
| `npm run typecheck`, `npm run build` | PASS | Existing main-bundle size warning remains; no new dependencies |
| Chromium browser acceptance | PASS | 90 layout/theme combinations; 90 axe audits with zero violations; zero page errors |
| WebKit browser acceptance | PASS | Same 90 combinations/audits; zero violations/page errors; matching Linux WebKit driver |
| Actual local setup, login and transfer | PASS, both browsers | Setup closes; 200 KiB UI upload, complete download bytes and SHA-256 match |
| Documentation and retained assets | PASS | 28 local links resolve; seven reviewed PNGs total 666,834 bytes, no text/EXIF metadata; `git diff --check` clean |
| Live Telegram authorization / remote deployment | NOT TESTED | QR, password and verified-state responses are controlled doubles |

Browser acceptance uses `scripts/onboarding_ui_check.js` against a fresh loopback installation, with a small sandbox launcher that starts the built backend and final frontend assets. Public fixture token/password are intentionally synthetic; the helper refuses initialized/non-loopback drives. Run only on disposable data, never an existing installation.

Covered states: token, owner account, Telegram details, QR, active connection after reload, two-step input, verified connection, local storage and final review. Each is tested in light/dark at 1440×900, 1280×800, 768×900, 390×844 and 320×740. Checks include horizontal overflow, primary-action reachability/touch height, desktop storage-action visibility, keyboard reveal, pending locks, password validation/focus, optional channel ID, cancellation, verified-account reset, actual local completion/login, closed setup and no credentials in browser storage. Every layout runs axe WCAG A/AA. The launcher additionally uploads generated text through the UI and verifies its downloaded bytes and SHA-256 through the real local adapter.

Tooling issues corrected during qualification: missing sandbox C compiler, mismatched WebKit driver, held-route cleanup race and CSP-blocked axe injection. Final runs use Go 1.26.10 with GCC for race checks, Node 24 for frontend builds, matching Playwright 1.56.1/browser binaries and a context initialization script for axe; application CSP remains intact. Failed runs do not count as passes. WebKit contrast findings led to scoped heading/review color fixes. Both final browser runs passed after those changes.

## Screenshots

Before: [desktop light](screenshots/onboarding-storage-light.png), [desktop dark](screenshots/onboarding-storage-dark.png), [mobile dark](screenshots/onboarding-storage-mobile.png).

Final captures were visually inspected for spacing, clipping, contrast and private content. Empty fields/generated fixtures only; no real QR, account identity, channel selector, password or token. Captures show full pages; image height can exceed the tested viewport height.

- [Welcome, light](screenshots/onboarding-premium-welcome-light.png)
- [Owner account, light](screenshots/onboarding-premium-owner-light.png)
- [Telegram details, desktop light](screenshots/onboarding-premium-storage-light.png)
- [Telegram details, laptop dark](screenshots/onboarding-premium-storage-dark.png)
- [Telegram details, mobile dark](screenshots/onboarding-premium-storage-mobile.png)
- [Verified connection, dark — simulated Telegram](screenshots/onboarding-premium-verified-dark.png)
- [Final review, WebKit dark](screenshots/onboarding-premium-review-dark.png)

## Limits

This is local UI qualification. New live Telegram web sign-in, live 100 MiB Telegram transfer, exact production-image build, remote publishing and deployment remain separately unverified as recorded in [web onboarding qualification](WEB_ONBOARDING_REPORT.md). Published image `1.0.1` still predates these changes. No live Telegram, real Safari/macOS, real iOS, assistive-device certification or public-internet security claim follows from emulated browser checks.

Cancellation can briefly return `setup_busy` until its existing session worker exits; the UI gives retry guidance without weakening that guard. Reloading clears browser-held credentials and requires setup-token re-entry. Persistent configuration is saved only by the existing completion flow.
