package fixture

import (
	"strings"
	"testing"

	"opstack-doctor/internal/report"
)

func TestGetScenariosSummaries(t *testing.T) {
	tests := map[string]struct {
		wantWarn bool
		wantFail bool
	}{
		ScenarioHealthy: {wantWarn: false, wantFail: false},
		ScenarioWarn:    {wantWarn: true, wantFail: false},
		ScenarioFail:    {wantWarn: true, wantFail: true},
	}

	for scenario, tt := range tests {
		t.Run(scenario, func(t *testing.T) {
			got, err := Get(scenario)
			if err != nil {
				t.Fatalf("Get() error = %v", err)
			}
			summary := report.Summarize(got.Findings)
			if summary.Total == 0 {
				t.Fatal("Get() returned no findings")
			}
			if (summary.Warn > 0) != tt.wantWarn {
				t.Fatalf("warn count = %d, want warning presence %v", summary.Warn, tt.wantWarn)
			}
			if (summary.Fail > 0) != tt.wantFail {
				t.Fatalf("fail count = %d, want failure presence %v", summary.Fail, tt.wantFail)
			}
		})
	}
}

func TestGetRejectsUnknownScenario(t *testing.T) {
	if _, err := Get("mystery"); err == nil {
		t.Fatal("Get() error = nil, want error")
	}
}

func TestGetReturnsDefensiveCopy(t *testing.T) {
	first, err := Get(ScenarioHealthy)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	first.Findings[0].ID = "mutated"
	first.Findings[1].Evidence["client_version"] = "mutated"

	second, err := Get(ScenarioHealthy)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if second.Findings[0].ID == "mutated" {
		t.Fatal("Get() did not copy findings")
	}
	if second.Findings[1].Evidence["client_version"] == "mutated" {
		t.Fatal("Get() did not copy evidence")
	}
}

func TestFixtureFindingsAvoidLiveEndpoints(t *testing.T) {
	for _, name := range Names() {
		t.Run(name, func(t *testing.T) {
			got, err := Get(name)
			if err != nil {
				t.Fatalf("Get() error = %v", err)
			}
			for _, finding := range got.Findings {
				text := finding.Target + " " + finding.Observed + " " + finding.Recommendation
				if strings.Contains(text, "localhost") || strings.Contains(text, "example.com") {
					t.Fatalf("fixture finding appears to contain a live endpoint: %+v", finding)
				}
			}
		})
	}
}
