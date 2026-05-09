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
  - name: source-2
    role: source
    rpc: http://source2.example
    metrics: http://source2.example:7300/metrics
  - name: light-1
    role: light
    rpc: http://light.example
    metrics: http://light.example:7300/metrics
    follows: source-1
proxyd:
  enabled: true
  endpoints:
    - name: deriver-proxyd
      role: deriver
      rpc: http://deriver.example
      metrics: http://deriver.example:9761/metrics
      consensus_aware: true
      expected_backends:
        - source-1
        - source-2
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
	if hasSeverity(issues, "warn") {
		t.Fatalf("Validate() got warn issues: %+v", issues)
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

func TestValidateProxydTopology(t *testing.T) {
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
  - name: light-1
    role: light
    follows: source-1
proxyd:
  enabled: true
  endpoints:
    - name: bad-deriver
      role: deriver
      expected_backends:
        - light-1
    - name: bad-edge
      role: edge
      expected_backends:
        - source-1
    - name: bad-role
      role: mystery
      expected_backends:
        - missing
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	issues := cfg.Validate()
	if !hasField(issues, "proxyd.endpoints[0].consensus_aware") {
		t.Fatalf("Validate() should warn about deriver consensus awareness, got %+v", issues)
	}
	if !hasField(issues, "proxyd.endpoints[0].expected_backends[0]") {
		t.Fatalf("Validate() should flag deriver backend role, got %+v", issues)
	}
	if !hasField(issues, "proxyd.endpoints[1].expected_backends[0]") {
		t.Fatalf("Validate() should warn about edge source backend, got %+v", issues)
	}
	if !hasField(issues, "proxyd.endpoints[2].role") {
		t.Fatalf("Validate() should reject invalid proxyd role, got %+v", issues)
	}
	if !hasField(issues, "proxyd.endpoints[2].expected_backends[0]") {
		t.Fatalf("Validate() should reject unknown proxyd backend, got %+v", issues)
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
