package generate

import (
	"os"
	"testing"

	"gopkg.in/yaml.v3"

	"opstack-doctor/internal/config"
)

func TestAlertsYAMLParsesAndContainsExpectedRules(t *testing.T) {
	cfg := config.Config{
		Chain: config.ChainConfig{Name: "op-mainnet", ChainID: 10},
		Execution: config.ExecutionConfig{
			CompareBlocks:    16,
			MaxHeadLagBlocks: 4,
			ReferenceRPC:     "http://reference.example",
			CandidateRPC:     "http://candidate.example",
		},
		OPNodes: []config.OPNodeConfig{
			{Name: "source-1", Role: "source"},
			{Name: "light-1", Role: "light", Follows: "source-1"},
		},
		Thresholds: config.ThresholdsConfig{MaxSafeLagBlocks: 20, MinPeerCount: 1},
	}

	data, err := Alerts(cfg)
	if err != nil {
		t.Fatalf("Alerts() error = %v", err)
	}
	var parsed RuleFile
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("generated alert YAML does not parse: %v\n%s", err, string(data))
	}
	if len(parsed.Groups) != 1 {
		t.Fatalf("groups = %d, want 1", len(parsed.Groups))
	}

	wantAlerts := []string{
		"OpNodeDown",
		"L2SafeHeadNotAdvancing",
		"OpNodeLowPeerCount",
		"OpNodeDerivationErrors",
		"OpNodePipelineResets",
		"SourceOpNodeDown",
		"ExecutionCandidateLaggingReference",
		"ExecutionBlockComparisonMismatch",
		"ExecutionRPCSurfaceMismatch",
		"LightNodeLaggingSource",
	}
	for _, alert := range wantAlerts {
		rule, ok := findRule(parsed, alert)
		if !ok {
			t.Fatalf("missing alert %s in %+v", alert, parsed.Groups[0].Rules)
		}
		if rule.Expr == "" {
			t.Fatalf("alert %s has empty expr", alert)
		}
		if rule.Labels["severity"] == "" {
			t.Fatalf("alert %s has no severity label", alert)
		}
		if rule.Annotations["summary"] == "" || rule.Annotations["description"] == "" {
			t.Fatalf("alert %s should include summary and description annotations", alert)
		}
	}
}

func TestExampleAlertYAMLParses(t *testing.T) {
	data, err := os.ReadFile("../../examples/prometheus-rules.example.yaml")
	if err != nil {
		t.Fatalf("read example rules: %v", err)
	}
	var parsed RuleFile
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("example alert YAML does not parse: %v", err)
	}
	if _, ok := findRule(parsed, "ExecutionRPCSurfaceMismatch"); !ok {
		t.Fatal("example rules missing ExecutionRPCSurfaceMismatch")
	}
}

func findRule(rules RuleFile, name string) (Rule, bool) {
	for _, group := range rules.Groups {
		for _, rule := range group.Rules {
			if rule.Alert == name {
				return rule, true
			}
		}
	}
	return Rule{}, false
}
