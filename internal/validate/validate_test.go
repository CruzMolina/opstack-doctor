package validate

import (
	"testing"

	"opstack-doctor/internal/config"
	"opstack-doctor/internal/report"
)

func TestFindingsOK(t *testing.T) {
	findings := Findings(config.Config{
		Chain: config.ChainConfig{Name: "op-mainnet", ChainID: 10},
		Execution: config.ExecutionConfig{
			ReferenceRPC:     "http://reference.example",
			CandidateRPC:     "http://candidate.example",
			CompareBlocks:    16,
			MaxHeadLagBlocks: 4,
		},
		OPNodes: []config.OPNodeConfig{
			{Name: "source-1", Role: "source"},
			{Name: "source-2", Role: "source"},
		},
	}, "fixture.yaml")
	if len(findings) != 1 {
		t.Fatalf("Findings() len = %d, want 1", len(findings))
	}
	if findings[0].ID != "config.valid" || findings[0].Severity != report.SeverityOK || findings[0].Target != "fixture.yaml" {
		t.Fatalf("Findings()[0] = %+v", findings[0])
	}
}

func TestFindingsIssues(t *testing.T) {
	findings := Findings(config.Config{}, "bad.yaml")
	if len(findings) == 0 {
		t.Fatalf("Findings() should report issues")
	}
	summary := report.Summarize(findings)
	if summary.Fail == 0 {
		t.Fatalf("Findings() should include fail findings: %+v", findings)
	}
	for _, finding := range findings {
		if finding.ID == "" || finding.Target == "" || finding.Observed == "" {
			t.Fatalf("finding should be structured: %+v", finding)
		}
	}
}
