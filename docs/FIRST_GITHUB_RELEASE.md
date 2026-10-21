# First GitHub release

**Runbook only. Nothing below has run remotely.** All commands operate from the audited sanitized repository, never the original Harbor checkout. Each authorization gate requires explicit owner approval; elapsed time is not approval. Proposed version: **v1.0.1**. Do not create or change `v1.0.0`.

## 1. Private repository and initial push

After approval of the owner/name, private repository creation and initial push, open a Terminal in the audited export. Replace `YOUR_GITHUB_OWNER` before running commands. Use existing secure GitHub authentication; never place tokens in URLs, commands or repository files.

```sh
OWNER=YOUR_GITHUB_OWNER
REPOSITORY=harbor
(
set -eu
test "$OWNER" != YOUR_GITHUB_OWNER
test "$(git branch --show-current)" = main
test "$(git rev-list --all --count)" = 1
test -z "$(git status --porcelain)"
test -z "$(git remote)"
test -z "$(git tag --list)"
gh auth status
gh repo create "$OWNER/$REPOSITORY" --private
test "$(gh api "repos/$OWNER/$REPOSITORY" --jq .private)" = true
)
```

The final assertion requires private visibility. Each mutating block uses a subshell with `set -eu`, so a failed precondition stops that block without closing the Terminal. In repository settings, enable Actions before pushing, with read-only default workflow permissions. Fork PRs must receive neither secrets nor write tokens. Review branch/tag protection availability for this GitHub plan; required private-repository controls may need a paid plan. Do not initialize a remote README/license or import the original repository.

```sh
(
set -eu
test "$(gh api "repos/$OWNER/$REPOSITORY" --jq .private)" = true
test "$(git branch --show-current)" = main
test "$(git rev-list --all --count)" = 1
test -z "$(git status --porcelain)"
test -z "$(git remote)"
test -z "$(git tag --list)"
git remote add origin "https://github.com/$OWNER/$REPOSITORY.git"
git -c push.followTags=false push -u origin main:main
gh run list --repo "$OWNER/$REPOSITORY" --workflow ci.yml --branch main
)
```

Record the actual CI run ID and commit SHA. These commands publish the audited source to a **private** GitHub repository and require the approval above.

## 2. Remote CI and repository controls

```sh
gh run watch RUN_ID --repo "$OWNER/$REPOSITORY" --exit-status
gh run view RUN_ID --repo "$OWNER/$REPOSITORY"
```

Replace `RUN_ID` with the actual run. Inspect every job: workflow/release policy, Go default/tagged tests/race/vet and reproducibility, frontend tests/types/build, and both Docker architecture builds/smoke tests. Keep failed checks blocked; do not infer a pass from local results. After owner-approved branch creation/push, use a small reviewed documentation PR to verify actual PR-trigger CI and read-only permissions too. Record its run separately; main CI does not establish PR event execution.

Protect `main` against force pushes/deletion and require review plus the actual successful CI check names. Restrict `v*` tag creation/update/deletion to approved maintainers. Review workflow changes as privileged code. Enable secret scanning/push protection, dependency alerts, and private vulnerability reporting where the repository visibility/plan supports them.

Create `ghcr-release` before any release tag. Configure required reviewers, prevent self-review, and restrict deployments to protected stable release tags. GitHub's current [environment documentation](https://docs.github.com/en/actions/how-tos/deploy/configure-and-manage-deployments/manage-environments) says private/internal environments need Pro/Team/Enterprise, and private required-reviewer protection is unavailable on Free/Pro/Team. Verify actual plan capabilities. If the intended protection is unavailable, stop the private image-release sequence and obtain an approved compatible plan/process. Do not silently remove the review gate or make the repository public to bypass it.

Inspect any existing `ghcr.io/<lowercase-owner>/harbor` package. Confirm correct ownership, repository linkage, private visibility and Actions write access for this repository; do not take over an unrelated package. No PAT is needed for workflow publishing. Only the publish job receives `packages: write`; ordinary CI remains read-only.

## 3. Approve and publish the first image

First finish the v1.0.1 changelog with actual release date, review the accepted source, and run CI on that final main commit. Obtain explicit owner approval for the **specific commit**, new annotated `v1.0.1` tag, tag push and private GHCR image publication. Confirm a second authorized reviewer can approve the environment when self-review is prohibited.

```sh
(
set -eu
APPROVED_SHA=REPLACE_WITH_APPROVED_COMMIT_SHA
test "$(git branch --show-current)" = main
test "$(git remote get-url origin)" = "https://github.com/$OWNER/$REPOSITORY.git"
test "$(gh api "repos/$OWNER/$REPOSITORY" --jq .private)" = true
git pull --ff-only origin main
test -z "$(git status --porcelain)"
test -z "$(git tag --list v1.0.1)"
test "$(git rev-parse HEAD)" = "$APPROVED_SHA"
git tag -a v1.0.1 -m 'Harbor v1.0.1 — audited source and CI integration'
git -c push.followTags=false push origin refs/tags/v1.0.1:refs/tags/v1.0.1
gh run list --repo "$OWNER/$REPOSITORY" --workflow docker-publish.yml
)
```

Use the approved public identity for tagger metadata. Stop if the tag exists, main moved beyond approved source, CI failed, or identity/protection differs. Never force a tag. Inspect release gate and all reusable CI jobs, then approve the protected publish job only after its checks pass. The workflow publishes `1.0.1`, `1.0` and `latest` for AMD64/ARM64, plus SBOM/minimal provenance. Record the actual immutable image digest from the workflow summary; partial registry publication is possible if later verification fails.

Retry only the same approved annotated tag after review:

```sh
gh workflow run docker-publish.yml --repo "$OWNER/$REPOSITORY" \
  --ref v1.0.1 -f release_tag=v1.0.1
```

Branches, prereleases, arbitrary tags and v1.0.0 cannot bypass release checks. New code fixes require a new approved patch tag.

## 4. Verify registry delivery

Set `IMAGE` to the actual lowercase package name and `DIGEST` to the recorded full `sha256:...` digest. Authenticate Docker through a secure credential helper when pulling a private package; do not print credentials. Workflow `GITHUB_TOKEN` is for Actions, not a reusable local login token.

```sh
IMAGE="ghcr.io/$(printf '%s' "$OWNER" | tr '[:upper:]' '[:lower:]')/harbor"
DIGEST=sha256:REPLACE_WITH_RECORDED_DIGEST
docker buildx imagetools inspect "$IMAGE@$DIGEST"
docker buildx imagetools inspect --raw "$IMAGE@$DIGEST" > manifest.json
```

Require `linux/amd64` and `linux/arm64`; other descriptors must be attestations. Inspect both platform configurations for source/revision/version/description/MIT/documentation labels, non-root user, expected entrypoint and health check. Inspect attached SBOM/provenance for source integrity and sensitive inputs. Record digest/architectures/commit in release notes. Attestations are not independent security certification.

Pull and smoke-test both published platforms on a compatible host/emulator. Use each platform's manifest digest from the index to avoid Docker resolving a different locally cached platform. Smoke uses only random fresh volumes and local generated fixtures, including auth, encrypted multipart, ranges, checksum, restart and recreation. It does not touch original state or use Telegram credentials.

```sh
AMD64_DIGEST=$(python3 -c 'import json; m=json.load(open("manifest.json")); d=[x["digest"] for x in m["manifests"] if x.get("platform",{}).get("os")=="linux" and x["platform"].get("architecture")=="amd64"]; assert len(d)==1; print(d[0])')
ARM64_DIGEST=$(python3 -c 'import json; m=json.load(open("manifest.json")); d=[x["digest"] for x in m["manifests"] if x.get("platform",{}).get("os")=="linux" and x["platform"].get("architecture")=="arm64"]; assert len(d)==1; print(d[0])')
docker pull --platform linux/amd64 "$IMAGE@$AMD64_DIGEST"
python3 scripts/ci_check.py docker-smoke --image "$IMAGE@$AMD64_DIGEST" \
  --platform linux/amd64 --version 1.0.1
docker pull --platform linux/arm64 "$IMAGE@$ARM64_DIGEST"
python3 scripts/ci_check.py docker-smoke --image "$IMAGE@$ARM64_DIGEST" \
  --platform linux/arm64 --version 1.0.1
```

Preserve console results and successful run links. If optional live Telegram requalification is requested later, use the documented exclusive-session handoff; never share active authorization copies.

## 5. Public visibility is a separate approval

Keep repository and package private until remote CI, publication, registry architecture checks and pulled-image smoke all pass. Re-scan the exact accepted source, tags/tagger metadata, release attachments, logs/attestations and package metadata. Obtain separate explicit owner approval for making **both** repository and GHCR package public. Review GitHub's visibility warning: public package visibility cannot be reverted to private.

Only then change repository visibility and package visibility in their respective settings. Confirm unauthenticated source and image access from a clean client, verify both architectures by immutable digest, enable public vulnerability reporting and publish release notes containing measured evidence/limitations. Do not upload original history, private source archives or ignored audit evidence. Public source availability does not make Harbor qualified for public-internet hosting.
