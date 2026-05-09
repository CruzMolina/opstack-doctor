# Release Checklist

This checklist is for maintainers cutting an `opstack-doctor` release.

## Before Tagging

Start from `main` with a clean worktree:

```sh
git status --short --branch
```

Run the local verification suite:

```sh
gofmt -w ./cmd ./internal
go test ./...
go vet ./...
go build ./cmd/opstack-doctor
```

Run smoke checks:

```sh
go run ./cmd/opstack-doctor version
go run ./cmd/opstack-doctor demo --scenario healthy --output human
go run ./cmd/opstack-doctor demo --scenario warn --output json
go run ./cmd/opstack-doctor demo --scenario fail --output prometheus
```

Review operator-facing docs touched by the release:

```sh
git diff -- README.md docs examples
```

## Tag And Publish

Use semantic version tags prefixed with `v`:

```sh
VERSION=0.1.0
git tag "v${VERSION}"
git push origin "v${VERSION}"
```

Pushing the tag starts the release workflow. It builds:

- `linux/amd64`
- `linux/arm64`
- `darwin/amd64`
- `darwin/arm64`

The workflow also uploads `SHA256SUMS` and creates a GitHub release for tag pushes.

## Verify The Release

After the workflow finishes:

1. Open the GitHub release for the tag.
2. Confirm all four `.tar.gz` archives are attached.
3. Confirm `SHA256SUMS` is attached.
4. Download the archive for your platform and verify it:

```sh
VERSION=0.1.0
OS=linux
ARCH=amd64
sha256sum -c SHA256SUMS --ignore-missing
tar -xzf "opstack-doctor_${VERSION}_${OS}_${ARCH}.tar.gz"
./opstack-doctor version
./opstack-doctor demo --scenario healthy --output prometheus
```

5. Confirm `opstack-doctor version` prints the tagged version.

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
