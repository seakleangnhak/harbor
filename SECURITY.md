# Security policy

Harbor is qualified for trusted-host, local, private, single-owner personal use. This is not an independent security audit, public-internet deployment qualification, or guarantee of Telegram account/channel durability. Live cross-datacenter retrieval remains **NOT TESTED**. See [known limitations](docs/KNOWN_LIMITATIONS.md) and the [operations runbook](docs/PRODUCTION_OPERATIONS.md).

## Supported versions

| Version | Security support |
| --- | --- |
| Latest stable 1.x release | Best-effort fixes; upgrade to the latest patch |
| Earlier 1.x patches and prereleases | No separate backport commitment |
| Unreleased branches | Development only |

There is no response-time or long-term-support guarantee. Review changelog and recovery instructions before upgrading.

## Report a vulnerability privately

Use this repository's **Security → Advisories → Report a vulnerability** page when available. Administrators must enable and verify **Private vulnerability reporting** as soon as public visibility makes it available, before announcing the project. GitHub currently offers this feature for public repositories; while private or unavailable, use the fallback below. A draft advisory is preferable to a public issue or pull request.

If private reporting is unavailable, open a public issue titled **Private security reporting channel requested**, containing only that request. Wait for a verified maintainer to provide a private route before sending any details. Do not disclose an exploit, affected private installation, credentials, sample private content, or logs publicly. No private contact address is designated in this source tree.

In the private report, include:

- Affected Harbor version/commit and deployment type.
- The trust boundary, expected behavior, actual impact, and minimal reproduction using synthetic data.
- Relevant source locations and a proposed fix, if available.
- Whether disclosure has already occurred and a way to continue the private conversation.

Never include real Telegram API hashes, session files, phone/login codes, two-step passwords, invite links/channel identifiers, owner cookies, content keys, recovery passwords, SQLite files, or private archives. Redact diagnostic output before sharing. Use generated fixtures for proof of concept; do not test against another person's account, files, host, or channel.

Maintainers should acknowledge and triage privately when capacity allows, coordinate fixes and disclosure with the reporter, and publish an advisory when appropriate. Avoid active exploitation, excessive requests, or deliberately triggering Telegram restrictions.

## Protect an installation

- Keep Harbor bound to localhost. Deliberate remote exposure requires a reviewed HTTPS proxy, Secure cookies, and origin/streaming checks; public deployment is outside current qualification.
- Use a dedicated Telegram account and private storage channel with auto-delete disabled. Treat its persisted MTProto authorization as an account credential.
- Keep keys, sessions, owner credentials, metadata, and configuration in protected persistent storage, outside Git and Docker build contexts. Supply runtime secrets at startup, never as build arguments.
- Retain verified encrypted off-host recovery archives and protect their independent password separately. An archive of metadata/key/session cannot recover deleted Telegram ciphertext; keep a separate protected copy of irreplaceable content.
- Do not replace a missing content key, silently switch storage adapters, or run two copies of one Telegram authorization. Follow the [recovery quickstart](docs/RECOVERY_QUICKSTART.md).

For suspected compromise, stop exposure and preserve protected offline state. Revoke a stolen Telegram authorization from trusted Telegram **Settings → Devices**; changing API credentials alone does not revoke it. Owner-session/key rotation has documented limits. Follow the incident procedures in [production operations](docs/PRODUCTION_OPERATIONS.md) before returning to use.
