# Private GitHub validation

2026-10-21. **PRIVATE_SOURCE_PUBLICATION = PASS. Remote main/PR CI = PASS. GHCR = NOT TESTED. Public visibility = NOT AUTHORIZED.**

## Audited source

The private [Harbor repository](https://github.com/seakleangnhak/harbor) was created empty; its initial `main` snapshot was the audited sanitized root `15aefb06b3bd813cf14e418611d928c185698274`. Initial remote inspection verified one parentless commit on `main`, all 264 source blobs matching the audited export, approved public commit identity, and no tags. Dependabot subsequently created dependency-update branches/PR refs; those changes are not part of the initial byte-equality audit and were not merged. Original development history, runtime data and the historical local `v1.0.0` tag remain outside this repository.

## Actual GitHub-hosted evidence

Successful initial main run: [CI 39552906176](https://github.com/seakleangnhak/harbor/actions/runs/39552906176), source `15aefb06b3bd813cf14e418611d928c185698274`, event `push`.

| Check | Result | Scope |
| --- | --- | --- |
| Workflow/release policy | PASS | Hosted actionlint, README/assets and Python tooling checks |
| Frontend | PASS | 23 tests, TypeScript and production build |
| Go modules/tests/race/vet/reproducibility | PASS | Default and Telegram build tags; synthetic tests only |
| Docker AMD64 | PASS | Actual production Dockerfile, local-adapter encrypted multipart/restart/recreation smoke |
| Docker ARM64 | PASS | QEMU-assisted execution of the same checks |
| PR event CI and token permissions | PASS | [Run 39553007811](https://github.com/seakleangnhak/harbor/actions/runs/39553007811), event `pull_request`; all five jobs passed. Completed policy job logs show only Contents/Metadata read. This unmerged Dependabot proposal is not the accepted release source. |
| Live Telegram on hosted runners | NOT TESTED | CI deliberately receives no Telegram credentials |
| GHCR publication/pulled registry image | NOT TESTED | No release tag or registry publication authorized |

Both architecture smoke results verified a 4,259,861-byte encrypted multipart fixture, SHA-256 equality, range retrieval, interrupted upload resume across restart, container recreation, authentication/CSRF rejection, private state modes and clean shutdown. Runtime version was `1.0.0`; release CI will separately supply `1.0.1` after an approved tag.

All five initial main jobs completed successfully, including two independent fresh-cache builds of each executable. Later commits require their own successful main runs before selecting a release SHA; this initial evidence applies specifically to `15aefb06b3bd813cf14e418611d928c185698274`.

Hosted Linux binary builds took 153.669 s and 156.580 s. Both attempts matched SHA-256: `drive` = `7517cf62235556927e8bf2eafcf44f88aab8541bccbd1599cce760c871b87f6f`; `harbor-recovery` = `833d44b24d17bb7e0ae32c97378a0749ad62c381e51f4e3fa9be1d684f83c682`. These are binary checksums, not registry image digests.

The completed frontend job reported token permissions `Contents: read` and `Metadata: read`. Source policy gives ordinary CI no package-write permission or registry login step. Hosted Docker checks use generated local fixtures, not live Telegram storage. No runtime or workflow source changes were needed for the initial hosted checks. Follow-up documentation anchors release-runbook Git commands to the audited source path and root identity; shell syntax and original-checkout rejection checks passed locally.

## Release controls and next approval

GitHub API confirmed private visibility, read-only default workflow permissions and `can_approve_pull_request_reviews=false` (GitHub Actions may not approve pull requests). The intended `ghcr-release` reviewer gate is **NOT CONFIGURED**: no environment exists, and its YAML name alone does not enforce approval. Branch protection and ruleset requests returned HTTP 403 with an upgrade requirement for the current private repository. A GHCR package lookup returned 404; package ownership, visibility and Actions write access remain unverified. Readable source configuration is not proof of successful registry authentication.

Keep image publication blocked until an approved compatible release-review process exists. Do not silently remove the intended reviewer gate or change repository visibility to bypass plan restrictions. Review [first-release instructions](FIRST_GITHUB_RELEASE.md) and [GHCR guide](GHCR_PUBLISHING_GUIDE.md).

First proposed automated version remains **v1.0.1**, with proposed image tags `ghcr.io/seakleangnhak/harbor:1.0.1`, `:1.0`, and `:latest`. Approve an exact clean main commit, annotated tag creation/push and private publication separately. Verify package visibility after authorized creation; it is not established by proposed tags. No v1.0.1 tag or image exists from this work. Public repository/package visibility needs another explicit approval.

Harbor remains qualified for local private personal use, not independently security-audited or qualified for public-internet deployment. Live cross-datacenter Telegram retrieval remains **NOT TESTED**.
