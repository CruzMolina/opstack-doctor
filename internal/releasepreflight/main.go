package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const image = "ghcr.io/cruzmolina/opstack-doctor"

func main() {
	if err := run("."); err != nil {
		fmt.Fprintf(os.Stderr, "release preflight failed: %v\n", err)
		os.Exit(1)
	}
}

func run(root string) error {
	version, err := readVersion(root)
	if err != nil {
		return err
	}
	if err := validateSemver(version); err != nil {
		return err
	}
	if err := checkMainVersion(root, version); err != nil {
		return err
	}
	if err := checkREADME(root, version); err != nil {
		return err
	}
	if err := checkChangelog(root, version); err != nil {
		return err
	}
	if err := checkReleaseNotes(root, version); err != nil {
		return err
	}
	fmt.Printf("release preflight passed for v%s\n", version)
	return nil
}

func readVersion(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, "VERSION"))
	if err != nil {
		return "", fmt.Errorf("read VERSION: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

func validateSemver(version string) error {
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(version) {
		return fmt.Errorf("VERSION %q must be MAJOR.MINOR.PATCH", version)
	}
	return nil
}

func checkMainVersion(root, version string) error {
	data, err := os.ReadFile(filepath.Join(root, "cmd", "opstack-doctor", "main.go"))
	if err != nil {
		return fmt.Errorf("read cmd/opstack-doctor/main.go: %w", err)
	}
	matches := regexp.MustCompile(`var\s+version\s*=\s*"([^"]+)"`).FindStringSubmatch(string(data))
	if matches == nil {
		return errors.New("cmd/opstack-doctor/main.go does not declare var version")
	}
	if matches[1] != version {
		return fmt.Errorf("cmd/opstack-doctor/main.go version %q does not match VERSION %q", matches[1], version)
	}
	return nil
}

func checkREADME(root, version string) error {
	data, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		return fmt.Errorf("read README.md: %w", err)
	}
	readme := string(data)
	if err := requireAllVersionMatches(readme, regexp.MustCompile(`(?m)^VERSION=([0-9]+\.[0-9]+\.[0-9]+)$`), version, "README VERSION snippet"); err != nil {
		return err
	}
	if err := requireAllVersionMatches(readme, regexp.MustCompile(regexp.QuoteMeta(image)+`:(v?[0-9]+\.[0-9]+\.[0-9]+)`), version, "README GHCR image snippet"); err != nil {
		return err
	}
	return nil
}

func requireAllVersionMatches(text string, pattern *regexp.Regexp, version, label string) error {
	matches := pattern.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return fmt.Errorf("%s not found", label)
	}
	for _, match := range matches {
		got := strings.TrimPrefix(match[1], "v")
		if got != version {
			return fmt.Errorf("%s uses %q, want %q", label, match[1], version)
		}
	}
	return nil
}

func checkChangelog(root, version string) error {
	data, err := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
	if err != nil {
		return fmt.Errorf("read CHANGELOG.md: %w", err)
	}
	released, heading, err := latestReleasedChangelogVersion(string(data))
	if err != nil {
		return err
	}
	if released != version {
		return fmt.Errorf("latest released CHANGELOG.md heading %q uses %q, want %q", heading, released, version)
	}
	return nil
}

func latestReleasedChangelogVersion(changelog string) (version, heading string, err error) {
	matches := regexp.MustCompile(`(?m)^## ([0-9]+\.[0-9]+\.[0-9]+) - ([^\n]+)$`).FindAllStringSubmatch(changelog, -1)
	if len(matches) == 0 {
		return "", "", errors.New("CHANGELOG.md has no release headings")
	}
	for _, match := range matches {
		if strings.EqualFold(strings.TrimSpace(match[2]), "Unreleased") {
			continue
		}
		return match[1], match[0], nil
	}
	return "", "", errors.New("CHANGELOG.md has no dated release headings")
}

func checkReleaseNotes(root, version string) error {
	path := filepath.Join(root, "docs", "releases", "v"+version+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	notes := string(data)
	required := []string{
		fmt.Sprintf("# opstack-doctor v%s Release Notes", version),
		fmt.Sprintf("%s:v%s", image, version),
		fmt.Sprintf("%s:%s", image, version),
	}
	for _, want := range required {
		if !strings.Contains(notes, want) {
			return fmt.Errorf("%s missing %q", path, want)
		}
	}
	return nil
}
