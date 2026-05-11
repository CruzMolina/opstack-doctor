package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunDemoPrometheus(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"demo", "--scenario", "healthy", "--output", "prometheus"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run demo code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "opstack_doctor_execution_candidate_lag_blocks") {
		t.Fatalf("demo prometheus output missing lag metric:\n%s", stdout.String())
	}
}

func TestRunDemoFailOnWarn(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"demo", "--scenario", "warn", "--fail-on", "warn"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("run demo warn code = %d, want 1; stderr = %s", code, stderr.String())
	}
}

func TestRunGenerateAlertsMatchesExample(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "prometheus-rules.example.yaml")
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"generate", "alerts",
		"--config", "../../examples/doctor.example.yaml",
		"--out", outPath,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("generate alerts code = %d, stderr = %s", code, stderr.String())
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read generated rules: %v", err)
	}
	want, err := os.ReadFile("../../examples/prometheus-rules.example.yaml")
	if err != nil {
		t.Fatalf("read example rules: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("generated alerts differ from examples/prometheus-rules.example.yaml; run go run ./cmd/opstack-doctor generate alerts --config examples/doctor.example.yaml --out examples/prometheus-rules.example.yaml\n%s", firstDifference(want, got))
	}
}

func TestRunGenerateRunbookMatchesExample(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "runbook.example.md")
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"generate", "runbook",
		"--config", "../../examples/doctor.example.yaml",
		"--out", outPath,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("generate runbook code = %d, stderr = %s", code, stderr.String())
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read generated runbook: %v", err)
	}
	want, err := os.ReadFile("../../examples/runbook.example.md")
	if err != nil {
		t.Fatalf("read example runbook: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("generated runbook differs from examples/runbook.example.md; run go run ./cmd/opstack-doctor generate runbook --config examples/doctor.example.yaml --out examples/runbook.example.md\n%s", firstDifference(want, got))
	}
}

func firstDifference(want, got []byte) string {
	wantLines := strings.Split(string(want), "\n")
	gotLines := strings.Split(string(got), "\n")
	max := len(wantLines)
	if len(gotLines) > max {
		max = len(gotLines)
	}
	for i := 0; i < max; i++ {
		var wantLine, gotLine string
		if i < len(wantLines) {
			wantLine = wantLines[i]
		} else {
			wantLine = "<missing>"
		}
		if i < len(gotLines) {
			gotLine = gotLines[i]
		} else {
			gotLine = "<missing>"
		}
		if wantLine != gotLine {
			return "first differing line:\nwant: " + wantLine + "\n got: " + gotLine
		}
	}
	return "contents differ"
}
