package demo

import (
	"context"
	"testing"
	"time"

	"opstack-doctor/internal/checks"
	"opstack-doctor/internal/report"
)

func TestConfigScenarios(t *testing.T) {
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
			cfg, cleanup, err := Config(scenario)
			if err != nil {
				t.Fatalf("Config() error = %v", err)
			}
			defer cleanup()
			findings := checks.Runner{Timeout: time.Second}.Run(context.Background(), cfg)
			summary := report.Summarize(findings)
			if (summary.Warn > 0) != tt.wantWarn {
				t.Fatalf("warn count = %d, want warn present %v", summary.Warn, tt.wantWarn)
			}
			if (summary.Fail > 0) != tt.wantFail {
				t.Fatalf("fail count = %d, want fail present %v", summary.Fail, tt.wantFail)
			}
		})
	}
}

func TestConfigRejectsUnknownScenario(t *testing.T) {
	if _, _, err := Config("mystery"); err == nil {
		t.Fatal("Config() error = nil, want error")
	}
}
