package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"opstack-doctor/internal/report"
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

func TestRunValidateJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"validate", "--config", "../../examples/doctor.example.yaml", "--output", "json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("validate code = %d, stderr = %s", code, stderr.String())
	}
	var parsed report.JSONReport
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		t.Fatalf("validate json did not parse: %v\n%s", err, stdout.String())
	}
	if parsed.Summary.OK != 1 || parsed.Summary.Fail != 0 || parsed.Findings[0].ID != "config.valid" {
		t.Fatalf("unexpected validate json: %+v", parsed)
	}
}

func TestRunValidateFailsOnConfigFailure(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(configPath, []byte("chain: {}\nexecution: {}\n"), 0o644); err != nil {
		t.Fatalf("write bad config: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"validate", "--config", configPath, "--output", "human"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("validate code = %d, want 1; stderr = %s; stdout = %s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "[FAIL] Configuration issue") {
		t.Fatalf("validate human output missing fail finding:\n%s", stdout.String())
	}
}

func TestRunValidateFailOnWarn(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "warn.yaml")
	if err := os.WriteFile(configPath, []byte(`
chain:
  name: op-mainnet
  chain_id: 10
execution:
  reference_rpc: http://reference.example
  candidate_rpc: http://candidate.example
op_nodes:
  - name: source-1
    role: source
  - name: light-1
    role: light
`), 0o644); err != nil {
		t.Fatalf("write warn config: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"validate", "--config", configPath, "--fail-on", "warn"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("validate --fail-on warn code = %d, want 1; stderr = %s; stdout = %s", code, stderr.String(), stdout.String())
	}
}

func TestRunCompletionBash(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"completion", "bash"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("completion bash code = %d, stderr = %s", code, stderr.String())
	}
	for _, want := range []string{"complete -F _opstack_doctor_completion opstack-doctor", "validate check export demo generate completion version help"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("completion bash output missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestRunCompletionRejectsUnsupportedShell(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"completion", "powershell"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("completion unsupported code = %d, want 2; stdout = %s", code, stdout.String())
	}
	if !strings.Contains(stderr.String(), "unsupported shell") {
		t.Fatalf("completion unsupported stderr = %s", stderr.String())
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
