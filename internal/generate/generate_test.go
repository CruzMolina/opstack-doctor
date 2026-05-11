package generate

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"opstack-doctor/internal/config"
)

const (
	generatorGoldenConfig = "testdata/alerts.golden.config.yaml"
	alertGoldenFile       = "testdata/alerts.golden.yaml"
	runbookGoldenFile     = "testdata/runbook.golden.md"
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
		Proxyd: config.ProxydConfig{
			Enabled: true,
			Endpoints: []config.ProxydEndpointConfig{
				{Name: "deriver-proxyd", Role: "deriver", ConsensusAware: true, ExpectedBackends: []string{"source-1"}},
			},
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
		"ProxydEndpointUnhealthy",
		"DeriverProxydNotConsensusAware",
		"ProxydMetricsUnavailable",
		"ProxydDown",
		"ProxydBackendProbeUnhealthy",
		"ProxydBackendDegradedOrBanned",
		"ProxydNoConsensusBackends",
		"ProxydBackendRequestLatencyHigh",
		"ProxydBackendErrorRate",
		"ProxydCLConsensusIssues",
		"OpSupervisorDown",
		"OpSupervisorRefsMissing",
		"OpSupervisorAccessListVerifyFailures",
		"OpInteropMonitorDown",
		"OpInteropMonitorRiskyMessages",
		"OpInteropMonitorTerminalStatusChanges",
		"DoctorInteropDependencyReadinessWarning",
		"DoctorInteropSupervisorReadinessFailed",
		"DoctorInteropSupervisorReadinessWarning",
		"DoctorInteropMonitorReadinessFailed",
		"DoctorInteropMonitorReadinessWarning",
		"ProxydHeadLaggingBackends",
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
	latencyRule, ok := findRule(parsed, "ProxydBackendRequestLatencyHigh")
	if !ok {
		t.Fatalf("missing proxyd latency alert")
	}
	if !strings.Contains(latencyRule.Expr, "> 2.000") {
		t.Fatalf("proxyd latency alert should use default latency threshold, got %q", latencyRule.Expr)
	}
}

func TestAlertsGolden(t *testing.T) {
	cfg, err := config.Load(generatorGoldenConfig)
	if err != nil {
		t.Fatalf("load golden config: %v", err)
	}
	got, err := Alerts(cfg)
	if err != nil {
		t.Fatalf("Alerts() error = %v", err)
	}
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(alertGoldenFile, got, 0o644); err != nil {
			t.Fatalf("update golden file: %v", err)
		}
	}
	want, err := os.ReadFile(alertGoldenFile)
	if err != nil {
		t.Fatalf("read golden file: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("generated alert YAML differs from %s; run UPDATE_GOLDEN=1 go test ./internal/generate\n%s", alertGoldenFile, firstDifference(want, got))
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
	for _, alert := range []string{"ExecutionRPCSurfaceMismatch", "ProxydHeadLaggingBackends"} {
		if _, ok := findRule(parsed, alert); !ok {
			t.Fatalf("example rules missing %s", alert)
		}
	}
}

func TestSchemaJSONParsesAndContainsExpectedFields(t *testing.T) {
	data, err := Schema()
	if err != nil {
		t.Fatalf("Schema() error = %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("generated schema JSON does not parse: %v\n%s", err, string(data))
	}
	if parsed["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
		t.Fatalf("unexpected schema draft: %v", parsed["$schema"])
	}
	props := parsed["properties"].(map[string]any)
	for _, field := range []string{"chain", "execution", "op_nodes", "proxyd", "interop", "thresholds"} {
		if _, ok := props[field]; !ok {
			t.Fatalf("schema missing top-level field %q", field)
		}
	}
	execution := props["execution"].(map[string]any)
	executionProps := execution["properties"].(map[string]any)
	if _, ok := executionProps["reference_rpc"]; !ok {
		t.Fatalf("schema missing execution.reference_rpc")
	}
	opNodes := props["op_nodes"].(map[string]any)
	opNodeItems := opNodes["items"].(map[string]any)
	opNodeProps := opNodeItems["properties"].(map[string]any)
	role := opNodeProps["role"].(map[string]any)
	if !containsJSONValue(role["enum"].([]any), "source") || !containsJSONValue(role["enum"].([]any), "sequencer") {
		t.Fatalf("op-node role enum missing expected values: %v", role["enum"])
	}
}

func TestExampleSchemaJSONMatchesGenerator(t *testing.T) {
	got, err := Schema()
	if err != nil {
		t.Fatalf("Schema() error = %v", err)
	}
	want, err := os.ReadFile("../../examples/doctor.schema.json")
	if err != nil {
		t.Fatalf("read example schema: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("generated schema differs from examples/doctor.schema.json; run go run ./cmd/opstack-doctor generate schema --out examples/doctor.schema.json\n%s", firstDifference(want, got))
	}
}

func TestRunbookGolden(t *testing.T) {
	cfg, err := config.Load(generatorGoldenConfig)
	if err != nil {
		t.Fatalf("load golden config: %v", err)
	}
	got := Runbook(cfg)
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(runbookGoldenFile, got, 0o644); err != nil {
			t.Fatalf("update runbook golden file: %v", err)
		}
	}
	want, err := os.ReadFile(runbookGoldenFile)
	if err != nil {
		t.Fatalf("read runbook golden file: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("generated runbook differs from %s; run UPDATE_GOLDEN=1 go test ./internal/generate\n%s", runbookGoldenFile, firstDifference(want, got))
	}
}

func containsJSONValue(values []any, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
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
