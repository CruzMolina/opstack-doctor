package config

import (
	"strings"
	"testing"
)

func TestParseAppliesDefaultsAndValidatesTopology(t *testing.T) {
	cfg, err := Parse(strings.NewReader(`
chain:
  name: op-mainnet
  chain_id: 10
execution:
  reference_rpc: http://reference.example
  candidate_rpc: http://candidate.example
op_nodes:
  - name: source-1
    role: source
    rpc: http://source.example
    metrics: http://source.example:7300/metrics
  - name: light-1
    role: light
    rpc: http://light.example
    metrics: http://light.example:7300/metrics
    follows: source-1
thresholds: {}
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.Execution.CompareBlocks != 16 {
		t.Fatalf("CompareBlocks default = %d, want 16", cfg.Execution.CompareBlocks)
	}
	if cfg.Thresholds.MaxSafeLagBlocks != 20 {
		t.Fatalf("MaxSafeLagBlocks default = %d, want 20", cfg.Thresholds.MaxSafeLagBlocks)
	}
	issues := cfg.Validate()
	if hasSeverity(issues, "fail") {
		t.Fatalf("Validate() got fail issues: %+v", issues)
	}
	if !hasField(issues, "op_nodes") {
		t.Fatalf("Validate() should warn about one source node, got %+v", issues)
	}
}

func TestValidateRejectsInvalidRoleAndFollowTarget(t *testing.T) {
	cfg, err := Parse(strings.NewReader(`
chain:
  name: bad
  chain_id: 10
execution:
  reference_rpc: http://reference.example
  candidate_rpc: http://candidate.example
op_nodes:
  - name: source-1
    role: standalone
  - name: light-1
    role: light
    follows: source-1
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	issues := cfg.Validate()
	if !hasSeverity(issues, "fail") {
		t.Fatalf("Validate() should fail, got %+v", issues)
	}
	if !hasField(issues, "op_nodes[1].follows") {
		t.Fatalf("Validate() should flag follows target, got %+v", issues)
	}
}

func hasSeverity(issues []ValidationIssue, severity string) bool {
	for _, issue := range issues {
		if issue.Severity == severity {
			return true
		}
	}
	return false
}

func hasField(issues []ValidationIssue, field string) bool {
	for _, issue := range issues {
		if issue.Field == field {
			return true
		}
	}
	return false
}
