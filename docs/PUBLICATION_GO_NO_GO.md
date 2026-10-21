# Publication go/no-go

2026-10-21. **PUBLICATION = BLOCKED.** Local source preparation is complete; remote publication requires separate authorization and actual GitHub/GHCR validation. No repository, commit, tag or image was pushed or published during this gate.

| Gate | Result | Evidence / next action |
| --- | --- | --- |
| Original-history privacy | BLOCKED | Historical machine path and personal Git metadata. Do not publish this history or old source archives. |
| Historical credentials | PASS, scoped | All refs plus all 652 stored objects checked; zero configured-secret matches. Two general-scanner candidates are public module checksums. No credential rotation triggered by current evidence. |
| Screenshot/provenance review | PASS, scoped | Historical assets, metadata and current generated-fixture screenshots reviewed; exact font source/notices retained. |
| MIT and third-party distribution | PASS, scoped | Owner-approved MIT; dependency terms retained, Liberation exception/source included. No legal certification claimed. |
| Sanitized public history | PASS, local | Separate root on `main`, approved identity, reviewed files only, no original commits/tags/remotes/runtime. Independent file/history checks recorded locally. |
| Installation and runtime preservation | PASS, source review | Secure local defaults and source-build instructions retained. No application, database, content key or Telegram session changes. Runtime/source fingerprints unchanged. |
| CI and publishing policy | PASS, local | Fresh actionlint, workflow policy, release-gate tests, README/SVG/link checks; PR/main have no registry publishing path. |
| Actual GitHub main/PR/release CI | NOT TESTED | No remote run authorized. Record successful run URLs and exact accepted SHA after initial private push. |
| GitHub protections / GHCR access | NOT TESTED | Configure branch/tag/environment/package controls; private reviewer protection depends on plan. |
| GHCR publication / pulled-image smoke | NOT TESTED | Approved release, digest/platform/attestation inspection and both-platform smoke remain required. |
| Repository/package public visibility | BLOCKED | Separate explicit owner approval required after remote validation. |

## Release recommendation

Use only the separately audited source export. First proposed workflow-based version is **v1.0.1**. Original local `v1.0.0` is unchanged and deliberately absent from public history. No v1.0.1 tag exists yet. Do not import history to make historical report links resolve.

Approve repository owner/name plus initial **private** creation/push first. Later approve the exact release commit/tag and private GHCR publication. Approve public visibility separately after registry verification. Follow [first-release sequence](FIRST_GITHUB_RELEASE.md); local workflow checks do not establish remote execution success.

Harbor remains qualified for local private personal use within documented tests. It is not independently security-audited, guaranteed durable on Telegram or qualified for public-internet deployment. Live cross-datacenter Telegram retrieval remains **NOT TESTED**. These limitations must remain in README, security policy and release notes.

## Handoff evidence

The local export and its hash manifest/ref/identity verification are retained under ignored `output/publication-gate/`. Original baseline is `6a8c988348f77d3fcab84f82a78455041a7edf36`; local audit commits append documentation only. Original annotated tag and other branch tips remain recoverable. Git/source/image audits never modify existing runtime state. Details: [privacy audit](PUBLICATION_PRIVACY_AUDIT.md), [repository plan](PUBLIC_REPOSITORY_PLAN.md).
