# Publication privacy audit

2026-10-21. **Original-history publication: BLOCKED.** Use the separately prepared, independently checked source export described in [public repository plan](PUBLIC_REPOSITORY_PLAN.md). No remote publication is authorized or performed.

## Scope and method

Baseline `6a8c988348f77d3fcab84f82a78455041a7edf36`: seven local branches, one annotated tag, 33 commits, 652 stored Git objects, including one unreachable blob. All 33 commits are also the complete reflog-reachable commit set. Commit, committer, tagger, message and reflog metadata were included; a clean working tree was not treated as history clearance.

Checks covered current tracked files, deleted/replaced historical blobs, all historical image assets, the bundled third-party source archive, and twelve existing source/image archives under ignored release/build output. Gitleaks 8.30.1 ran with full redaction against all refs and separately against every stored object. Coordinator compared actual configured credentials, key/password/session material and raw/base64/hex encodings without retaining values in reports. Seven public dependency datacenter routing fields were excluded only after comparison against locked gotd source; account authorization material was not excluded.

## Redacted findings

| Finding | Evidence | Disposition |
| --- | --- | --- |
| Historical machine/home path | Blob `04c7c3f5372afb30db686b7a15a38df540b3714a`, `docs/PHASE5_FINAL_REPORT.md:53`, in revisions `2e2a5df`, `6fe39cf`, `a22e1c3`, `f6c49fc`; absent from current source | Do not publish original history. No history rewrite. |
| Historical author/committer/tagger identity | 33 commits and one annotated tag contain ordinary email identity; local reflogs also retain it | Exclude all original Git metadata. New commits use only owner-approved public identity. Approval for new identity is not blanket approval of old history. |
| Private path in old release sources | `output/releases/v1.0.0/harbor-v1.0.0-source.tar.gz` and `output/releases/v1.0.0-final/harbor-v1.0.0-source.tar.gz`, member `docs/PHASE5_FINAL_REPORT.md` | Keep archives private. Do not attach them to GitHub releases. Rebuild from accepted public history. |
| Configured secrets | All 652 baseline stored objects, 260 baseline files and twelve source/image archives: zero matches. Subsequent 264-file source/export scans also pass | PASS within stated matching scope. No historical credential requiring rotation was identified. |
| General secret scanner | All-ref Git scan: zero findings. Raw-object scan: two candidates, both verified `go.sum` public module checksums | Zero unresolved credential findings. Preserve checksum lines. |
| Runtime artifacts | No tracked historical private database, key, session or backup identified | Export excludes all runtime and ignored output regardless. |
| Endpoint/identifier literals | Telegram link/ID examples and test inputs, loopback/example endpoints, public dependency routes and attribution contacts | Retain verified synthetic/public material; no private deployment endpoint identified. |
| Historical screenshots and branding | 94 unique image blobs: 84 PNGs, six WebPs and four SVGs. All PNGs reviewed in seven contact sheets, flagged surfaces and all README WebPs checked full-size; one replaced screenshot beyond current tree | Current reviewed assets eligible for export. Historical-only blob is not copied. Five WebKit PNG EXIF blocks contain only color/dimension fields, no private metadata. |

No secret value, personal metadata value, private channel identity or machine path is reproduced here. If later evidence reveals a real committed credential, revoke/rotate it even when its commit is excluded from public history. Revoke Telegram authorizations through trusted Telegram Devices settings; changing an API hash alone does not revoke sessions.

## Distribution review

Owner approved MIT. Root `LICENSE` covers original Harbor code/assets; dependencies retain their licenses and attribution in `THIRD_PARTY_NOTICES.md`. PDF.js's bundled Liberation 1.07.4 fonts use GPL-2.0 with font exception, not OFL. Exact corresponding font source remains distributed at `web/public/fonts-liberation-1.07.4-source.tar.gz`, SHA-256 `ad98b7498dc2992f7f0868f79b65ce4a720a3acdb63ab3f1f1cb6881117a5406`. Do not remove it or relicense dependencies as MIT. No reference-repository code or artwork was identified as copied; references describe architectural inspiration only. This is a provenance/compliance review, not legal advice.

README images use generated demonstration fixtures. Original SVG branding has no scripts or external resources. Examples contain no private defaults. Compose binds host loopback, preserves a named volume, runs non-root/read-only with bounded resources, and receives credentials only at runtime. `.dockerignore` excludes Git, private configuration, databases, keys, sessions, backups and local artifacts. Installation does not assume an unpublished GHCR package exists.

## Evidence and limits

Fresh local checks: `gitleaks git --log-opts=--all --redact=100` (zero findings); `gitleaks dir --redact=100` over all stored raw objects (two verified checksum false positives); independent coordinator configured-material matching (zero); export-tree Gitleaks (zero); `python3 scripts/ci_check.py workflows` (actionlint, policy, 38 Python tests, 29 README resources, heading anchors and three SVGs pass); seven release-gate tests pass. Changed-document check resolves 91 local links and validates shell-block syntax. No backend/frontend/runtime code changed, so prior compiled/regression results are retained as historical evidence rather than claimed as fresh reruns.

Local redacted evidence: `output/publication-gate/coordinator/`, `history-review/`, `assets-review/` and `workflow-review/` beneath `output/publication-gate/`. This ignored evidence is not part of the public export. Export file hashes and Git identity/ref checks are recorded separately in its manifest/verification evidence.

Matching/scanners/visual inspection cannot prove absence of every secret, steganographic content or future vulnerability. Unrelated host files and untracked private runtime directories are not publication inputs. Existing phase reports are historical local qualification records; their ignored raw evidence and original commit refs are not imported. This audit does not certify public-internet deployment, independently audited security, Telegram durability or live cross-datacenter retrieval.
