# Release Checklist

This checklist is for maintainers cutting an `opstack-doctor` release.

## Before Tagging

Start from `main` with a clean worktree:

```sh
git status --short --branch
```

Run the local verification suite:

```sh
make release-check
```

`make release-check` includes Go tests, vet, smoke checks, schema contract checks, release metadata preflight, Prometheus rule syntax validation through `make promtool-check`, and representative alert firing tests through `make promtool-test`.

`VERSION` is the release version used by Make targets and should match the tag without the leading `v`.

You can run just the release metadata preflight with:

```sh
make release-preflight
```

The preflight verifies `VERSION`, `cmd/opstack-doctor/main.go`, README install snippets, the latest dated changelog heading, and `docs/releases/v$(cat VERSION).md` agree. It also requires release notes to mention both GHCR tags: `v$(cat VERSION)` and `$(cat VERSION)`.

CI also runs `make release-preflight` directly so release metadata drift is caught on pull requests, even before a maintainer runs the full local release checklist.

If Docker is available, verify the local image path too:

```sh
make docker-build
make docker-smoke
```

Review operator-facing docs touched by the release:

```sh
git diff -- README.md docs examples
```

Review version and release notes:

```sh
cat VERSION
sed -n '1,120p' CHANGELOG.md
sed -n "1,160p" "docs/releases/v$(cat VERSION).md"
```

## Tag And Publish

Use semantic version tags prefixed with `v`:

```sh
VERSION="$(cat VERSION)"
test "$(cat VERSION)" = "${VERSION}"
git tag "v${VERSION}"
git push origin "v${VERSION}"
```

Pushing the tag starts the release workflow. It builds:

- `linux/amd64`
- `linux/arm64`
- `darwin/amd64`
- `darwin/arm64`

The workflow also uploads `SHA256SUMS`, creates a GitHub release for tag pushes, and publishes multi-arch container images to GHCR.

## Verify The Release

After the workflow finishes:

1. Open the GitHub release for the tag.
2. Confirm all four `.tar.gz` archives are attached.
3. Confirm `SHA256SUMS` is attached.
4. Download the archive for your platform and verify it:

```sh
VERSION="$(cat VERSION)"
OS=linux
ARCH=amd64
sha256sum -c SHA256SUMS --ignore-missing
tar -xzf "opstack-doctor_${VERSION}_${OS}_${ARCH}.tar.gz"
./opstack-doctor version
./opstack-doctor demo --scenario healthy --output prometheus
```

5. Confirm `opstack-doctor version` prints the tagged version.
6. Compare the GitHub release notes against `docs/releases/v$(cat VERSION).md` and `CHANGELOG.md`.

If Docker is available, verify the container image too:

```sh
OWNER_REPO=CruzMolina/opstack-doctor
docker run --rm "ghcr.io/${OWNER_REPO}:v${VERSION}" version
docker run --rm "ghcr.io/${OWNER_REPO}:${VERSION}" version
```

## Patch Releases

For a bad release:

1. Leave the existing tag in place unless the release never left private testing.
2. Fix the issue on `main`.
3. Run the full pre-tag checklist.
4. Tag the next patch version, for example `v0.1.1`.
5. In release notes, say what changed and whether operators should upgrade urgently.

Avoid silently replacing release artifacts. Operators should be able to trust that a tag and checksum remain stable.

## Manual Workflow Runs

The release workflow also supports `workflow_dispatch`. Manual runs build and upload workflow artifacts, but they do not create a GitHub release unless the workflow is running for a tag.
