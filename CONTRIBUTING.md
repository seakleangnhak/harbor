# Contributing to Harbor

Harbor is a lightweight, single-owner private drive. Small, focused changes are easier to review and keep recovery trustworthy. Discuss substantial changes in an issue before starting; explain the user problem and how it fits this scope.

Read the [architecture](docs/ARCHITECTURE.md), [API contract](docs/API_CONTRACT.md), [known limitations](docs/KNOWN_LIMITATIONS.md), and project instructions in [AGENTS.md](AGENTS.md). For security reports, use [SECURITY.md](SECURITY.md), not a public bug report.

## Local setup

Requirements: Go 1.26.10, Node.js 24 with npm, Python 3.12+ for release/check scripts, and Docker with Compose/Buildx for container checks. Linux and macOS are the supported development environments. Go may download the matching toolchain when necessary.

```sh
npm --prefix web ci
npm --prefix web run build
go build -o bin/drive ./cmd/drive
DRIVE_ENV_FILE=- DRIVE_DATA_DIR=./data-contributor bin/drive init
DRIVE_ENV_FILE=- DRIVE_DATA_DIR=./data-contributor bin/drive
```

`init` creates private local-adapter state and an owner password file. The server binds to `127.0.0.1:8080` by default. Read the generated password locally for login; never put it in an issue, command argument, screenshot, or commit. Use a new disposable data directory; do not point development checks at an existing drive. For hot reload, run `npm --prefix web run dev` in another terminal; Vite proxies API requests to port 8080.

The local adapter requires no Telegram account or API credentials. Telegram changes must also compile and pass controlled tests with the `telegram` build tag. Real-account checks are separate, explicitly authorized qualification work. Never run them on another person's account/channel or share one mutable Telegram authorization between running processes.

## Verification

Run checks relevant to a change, then the complete local suite before requesting review:

```sh
go test ./...
go test -race ./...
go vet ./...
GOGC=40 go test -p 1 -tags telegram ./...
GOGC=40 go test -race -p 1 -tags telegram ./...
GOGC=40 go vet -p 1 -tags telegram ./...
npm --prefix web test
npm --prefix web run typecheck
npm --prefix web run build
python3 -m unittest discover -s scripts -p '*_test.py'
python3 scripts/acceptance.py --binary bin/drive
docker compose --env-file /dev/null config --quiet
```

The tagged build is large; serial compilation limits memory use. Browser scripts in `scripts/` require an authenticated, generated local fixture as described in the phase reports. Exercise changed interactions in light/dark themes and desktop/mobile viewports. Use synthetic filenames/content, keep screenshots free of private information, and state browser/platform coverage accurately. Never claim emulation as physical-device testing or a mock as live Telegram evidence.

## Pull requests

- Keep one purpose per pull request; preserve API/storage compatibility unless an explicit migration is reviewed.
- Add a small regression check for meaningful behavior changes. Cover interruption, retry, authorization, or recovery when relevant.
- Include actual commands/results, screenshots for UI changes, and remaining limitations. Explain any skipped checks.
- Preserve authenticated encryption, per-chunk integrity, key/session permissions, and durable manifests. Never make a failed operation look complete.
- Do not include `.env`, keys, sessions, owner passwords, databases, backups, dependency directories, build artifacts, or raw diagnostic logs.
- Review dependency changes and notices together. New third-party source/assets need verified licenses; retain required attribution in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
- Follow the [code of conduct](CODE_OF_CONDUCT.md). Contributions are provided under Harbor's [MIT license](LICENSE); third-party components keep their own terms.

No contributor is expected to publish a package, push a release tag, or expose an installation publicly. Maintainers follow the release/publishing runbooks and obtain publication approval separately.
