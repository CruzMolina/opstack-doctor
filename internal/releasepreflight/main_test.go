package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunPassesWithUnreleasedSectionAboveCurrentRelease(t *testing.T) {
	root := writeFixtureRepo(t, "0.1.12")
	writeFile(t, root, "CHANGELOG.md", `# Changelog

## 0.1.13 - Unreleased

### Added

- Future work.

## 0.1.12 - 2026-05-11

### Added

- Current release.
`)
	if err := run(root); err != nil {
		t.Fatalf("run() error = %v", err)
	}
}

func TestRunFailsOnMainVersionDrift(t *testing.T) {
	root := writeFixtureRepo(t, "0.1.12")
	writeFile(t, root, "cmd/opstack-doctor/main.go", `package main

var version = "0.1.11"
`)
	err := run(root)
	if err == nil || !strings.Contains(err.Error(), "main.go version") {
		t.Fatalf("run() error = %v, want main.go version mismatch", err)
	}
}

func TestRunFailsOnREADMEVersionDrift(t *testing.T) {
	root := writeFixtureRepo(t, "0.1.12")
	writeFile(t, root, "README.md", `VERSION=0.1.11
docker run --rm ghcr.io/cruzmolina/opstack-doctor:v0.1.12 version
`)
	err := run(root)
	if err == nil || !strings.Contains(err.Error(), "README VERSION snippet") {
		t.Fatalf("run() error = %v, want README version mismatch", err)
	}
}

func TestRunFailsWhenReleaseNotesDoNotMentionBothTags(t *testing.T) {
	root := writeFixtureRepo(t, "0.1.12")
	writeFile(t, root, "docs/releases/v0.1.12.md", `# opstack-doctor v0.1.12 Release Notes

ghcr.io/cruzmolina/opstack-doctor:v0.1.12
`)
	err := run(root)
	if err == nil || !strings.Contains(err.Error(), "ghcr.io/cruzmolina/opstack-doctor:0.1.12") {
		t.Fatalf("run() error = %v, want missing unprefixed image tag", err)
	}
}

func writeFixtureRepo(t *testing.T, version string) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "VERSION", version+"\n")
	writeFile(t, root, "cmd/opstack-doctor/main.go", `package main

var version = "`+version+`"
`)
	writeFile(t, root, "README.md", `VERSION=`+version+`
docker run --rm ghcr.io/cruzmolina/opstack-doctor:v`+version+` version
`)
	writeFile(t, root, "CHANGELOG.md", `# Changelog

## `+version+` - 2026-05-11

### Added

- Current release.
`)
	writeFile(t, root, "docs/releases/v"+version+".md", `# opstack-doctor v`+version+` Release Notes

ghcr.io/cruzmolina/opstack-doctor:v`+version+`
ghcr.io/cruzmolina/opstack-doctor:`+version+`
`)
	return root
}

func writeFile(t *testing.T, root, path, data string) {
	t.Helper()
	fullPath := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(fullPath), err)
	}
	if err := os.WriteFile(fullPath, []byte(data), 0o644); err != nil {
		t.Fatalf("write %s: %v", fullPath, err)
	}
}
