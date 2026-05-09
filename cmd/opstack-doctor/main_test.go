package main

import (
	"bytes"
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
