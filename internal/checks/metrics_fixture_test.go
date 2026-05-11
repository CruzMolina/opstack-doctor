package checks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"opstack-doctor/internal/config"
	"opstack-doctor/internal/metrics"
	"opstack-doctor/internal/report"
)

func TestProxydMetricFixtures(t *testing.T) {
	tests := []struct {
		name             string
		fixture          string
		endpoint         config.ProxydEndpointConfig
		want             map[string]report.Severity
		wantNoWarnOrFail bool
	}{
		{
			name:    "healthy consensus-aware metrics",
			fixture: "proxyd-healthy.prom",
			endpoint: config.ProxydEndpointConfig{
				Name:             "fixture-proxyd",
				Role:             "deriver",
				ConsensusAware:   true,
				ExpectedBackends: []string{"source-1", "source-2"},
			},
			wantNoWarnOrFail: true,
			want: map[string]report.Severity{
				"proxyd.fixture-proxyd.up":                    report.SeverityOK,
				"proxyd.fixture-proxyd.backend_probe_healthy": report.SeverityOK,
				"proxyd.fixture-proxyd.backend_degraded":      report.SeverityOK,
				"proxyd.fixture-proxyd.backend_banned":        report.SeverityOK,
				"proxyd.fixture-proxyd.backend_in_sync":       report.SeverityOK,
				"proxyd.fixture-proxyd.consensus_blocks":      report.SeverityOK,
				"proxyd.fixture-proxyd.consensus_count":       report.SeverityOK,
				"proxyd.fixture-proxyd.error_counters":        report.SeverityOK,
				"proxyd.fixture-proxyd.cl_consensus_counters": report.SeverityOK,
				"proxyd.fixture-proxyd.backend_error_rate":    report.SeverityOK,
				"proxyd.fixture-proxyd.backend_latency":       report.SeverityOK,
			},
		},
		{
			name:    "degraded and banned backend metrics",
			fixture: "proxyd-degraded-banned.prom",
			endpoint: config.ProxydEndpointConfig{
				Name:             "fixture-proxyd",
				Role:             "deriver",
				ConsensusAware:   true,
				ExpectedBackends: []string{"source-1", "source-2"},
			},
			want: map[string]report.Severity{
				"proxyd.fixture-proxyd.up":                    report.SeverityOK,
				"proxyd.fixture-proxyd.backend_probe_healthy": report.SeverityWarn,
				"proxyd.fixture-proxyd.backend_degraded":      report.SeverityWarn,
				"proxyd.fixture-proxyd.backend_banned":        report.SeverityWarn,
				"proxyd.fixture-proxyd.backend_in_sync":       report.SeverityWarn,
				"proxyd.fixture-proxyd.consensus_blocks":      report.SeverityOK,
				"proxyd.fixture-proxyd.consensus_count":       report.SeverityOK,
				"proxyd.fixture-proxyd.error_counters":        report.SeverityWarn,
				"proxyd.fixture-proxyd.cl_consensus_counters": report.SeverityWarn,
				"proxyd.fixture-proxyd.http_error_codes":      report.SeverityWarn,
				"proxyd.fixture-proxyd.backend_error_rate":    report.SeverityWarn,
				"proxyd.fixture-proxyd.backend_latency":       report.SeverityWarn,
			},
		},
		{
			name:    "missing version-specific optional metrics",
			fixture: "proxyd-missing-optional.prom",
			endpoint: config.ProxydEndpointConfig{
				Name:             "fixture-proxyd",
				Role:             "deriver",
				ConsensusAware:   true,
				ExpectedBackends: []string{"source-1", "source-2"},
			},
			wantNoWarnOrFail: true,
			want: map[string]report.Severity{
				"proxyd.fixture-proxyd.up":                            report.SeverityOK,
				"proxyd.fixture-proxyd.consensus_blocks":              report.SeverityOK,
				"proxyd.fixture-proxyd.consensus_count":               report.SeverityOK,
				"proxyd.fixture-proxyd.backend_probe_healthy_missing": report.SeverityInfo,
				"proxyd.fixture-proxyd.backend_degraded_missing":      report.SeverityInfo,
				"proxyd.fixture-proxyd.backend_banned_missing":        report.SeverityInfo,
				"proxyd.fixture-proxyd.backend_peer_count_missing":    report.SeverityInfo,
				"proxyd.fixture-proxyd.cl_local_safe_missing":         report.SeverityInfo,
				"proxyd.fixture-proxyd.error_counters_missing":        report.SeverityInfo,
				"proxyd.fixture-proxyd.cl_consensus_counters_missing": report.SeverityInfo,
				"proxyd.fixture-proxyd.backend_error_rate_missing":    report.SeverityInfo,
				"proxyd.fixture-proxyd.backend_latency_missing":       report.SeverityInfo,
			},
		},
		{
			name:    "alternate backend and status-code labels",
			fixture: "proxyd-label-variants.prom",
			endpoint: config.ProxydEndpointConfig{
				Name:             "fixture-proxyd",
				Role:             "deriver",
				ConsensusAware:   true,
				ExpectedBackends: []string{"source-legacy"},
			},
			want: map[string]report.Severity{
				"proxyd.fixture-proxyd.up":                    report.SeverityOK,
				"proxyd.fixture-proxyd.backend_probe_healthy": report.SeverityOK,
				"proxyd.fixture-proxyd.backend_degraded":      report.SeverityOK,
				"proxyd.fixture-proxyd.backend_banned":        report.SeverityOK,
				"proxyd.fixture-proxyd.backend_in_sync":       report.SeverityOK,
				"proxyd.fixture-proxyd.consensus_blocks":      report.SeverityOK,
				"proxyd.fixture-proxyd.consensus_count":       report.SeverityOK,
				"proxyd.fixture-proxyd.http_error_codes":      report.SeverityWarn,
				"proxyd.fixture-proxyd.backend_error_rate":    report.SeverityOK,
				"proxyd.fixture-proxyd.backend_latency":       report.SeverityOK,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings := checkProxydNativeMetrics(tt.endpoint, loadMetricFixture(t, tt.fixture), config.ThresholdsConfig{MaxRPCLatencySeconds: 2})
			assertFindingSeverities(t, findings, tt.want)
			if tt.wantNoWarnOrFail {
				assertNoWarnOrFail(t, findings)
			}
		})
	}
}

func TestOPNodeMetricFixtures(t *testing.T) {
	tests := []struct {
		name             string
		fixture          string
		want             map[string]report.Severity
		wantNoWarnOrFail bool
	}{
		{
			name:             "healthy metrics",
			fixture:          "op-node-healthy.prom",
			wantNoWarnOrFail: true,
			want: map[string]report.Severity{
				"op_node.fixture-node.up":                      report.SeverityOK,
				"op_node.fixture-node.refs":                    report.SeverityOK,
				"op_node.fixture-node.refs_safe":               report.SeverityOK,
				"op_node.fixture-node.refs_finalized":          report.SeverityOK,
				"op_node.fixture-node.refs_unsafe":             report.SeverityOK,
				"op_node.fixture-node.peer_count":              report.SeverityOK,
				"op_node.fixture-node.derivation_errors_total": report.SeverityOK,
				"op_node.fixture-node.pipeline_resets_total":   report.SeverityOK,
				"op_node.fixture-node.rpc_latency":             report.SeverityOK,
			},
		},
		{
			name:    "warning metrics",
			fixture: "op-node-warn.prom",
			want: map[string]report.Severity{
				"op_node.fixture-node.up":                      report.SeverityOK,
				"op_node.fixture-node.refs_safe":               report.SeverityOK,
				"op_node.fixture-node.peer_count":              report.SeverityWarn,
				"op_node.fixture-node.derivation_errors_total": report.SeverityWarn,
				"op_node.fixture-node.pipeline_resets_total":   report.SeverityWarn,
				"op_node.fixture-node.rpc_latency_missing":     report.SeverityWarn,
			},
		},
		{
			name:    "failing metrics",
			fixture: "op-node-fail.prom",
			want: map[string]report.Severity{
				"op_node.fixture-node.up":                              report.SeverityFail,
				"op_node.fixture-node.refs_missing":                    report.SeverityWarn,
				"op_node.fixture-node.peer_count_missing":              report.SeverityWarn,
				"op_node.fixture-node.derivation_errors_total_missing": report.SeverityWarn,
				"op_node.fixture-node.pipeline_resets_total_missing":   report.SeverityWarn,
				"op_node.fixture-node.rpc_latency_missing":             report.SeverityWarn,
			},
		},
	}

	node := config.OPNodeConfig{Name: "fixture-node", Role: "source"}
	thresholds := config.ThresholdsConfig{MinPeerCount: 1}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings := checkNodeMetricSamples(node, loadMetricFixture(t, tt.fixture), thresholds)
			assertFindingSeverities(t, findings, tt.want)
			if tt.wantNoWarnOrFail {
				assertNoWarnOrFail(t, findings)
			}
		})
	}
}

func TestInteropSupervisorMetricFixtures(t *testing.T) {
	cfg := config.Config{
		Chain: config.ChainConfig{Name: "op-mainnet", ChainID: 10},
		Interop: config.InteropConfig{
			Enabled: true,
			Supervisor: config.InteropSupervisorConfig{
				ExpectedChains: []uint64{10, 8453},
			},
			Dependencies: []config.DependencyConfig{{Name: "base", ChainID: 8453}},
		},
	}
	tests := []struct {
		name             string
		fixture          string
		want             map[string]report.Severity
		wantNoWarnOrFail bool
	}{
		{
			name:             "healthy supervisor",
			fixture:          "op-supervisor-healthy.prom",
			wantNoWarnOrFail: true,
			want: map[string]report.Severity{
				"interop.supervisor.up":                         report.SeverityOK,
				"interop.supervisor.info":                       report.SeverityOK,
				"interop.supervisor.refs":                       report.SeverityOK,
				"interop.supervisor.expected_chains":            report.SeverityOK,
				"interop.supervisor.ref_types":                  report.SeverityOK,
				"interop.supervisor.access_list_verify_failure": report.SeverityOK,
				"interop.supervisor.logdb_entries":              report.SeverityOK,
				"interop.supervisor.rpc_metrics":                report.SeverityOK,
			},
		},
		{
			name:    "risky supervisor",
			fixture: "op-supervisor-risk.prom",
			want: map[string]report.Severity{
				"interop.supervisor.up":                         report.SeverityFail,
				"interop.supervisor.refs":                       report.SeverityOK,
				"interop.supervisor.expected_chains":            report.SeverityWarn,
				"interop.supervisor.ref_types":                  report.SeverityWarn,
				"interop.supervisor.access_list_verify_failure": report.SeverityWarn,
				"interop.supervisor.logdb_entries_missing":      report.SeverityInfo,
				"interop.supervisor.rpc_metrics_missing":        report.SeverityInfo,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings := checkSupervisorMetricSamples(cfg, loadMetricFixture(t, tt.fixture))
			assertFindingSeverities(t, findings, tt.want)
			if tt.wantNoWarnOrFail {
				assertNoWarnOrFail(t, findings)
			}
		})
	}
}

func TestInteropMonitorMetricFixtures(t *testing.T) {
	tests := []struct {
		name             string
		fixture          string
		want             map[string]report.Severity
		wantNoWarnOrFail bool
	}{
		{
			name:             "healthy monitor",
			fixture:          "op-interop-mon-healthy.prom",
			wantNoWarnOrFail: true,
			want: map[string]report.Severity{
				"interop.monitor.up":                      report.SeverityOK,
				"interop.monitor.message_status":          report.SeverityOK,
				"interop.monitor.terminal_status_changes": report.SeverityOK,
				"interop.monitor.block_ranges":            report.SeverityOK,
			},
		},
		{
			name:    "risky monitor",
			fixture: "op-interop-mon-risk.prom",
			want: map[string]report.Severity{
				"interop.monitor.up":                      report.SeverityFail,
				"interop.monitor.message_status":          report.SeverityWarn,
				"interop.monitor.terminal_status_changes": report.SeverityWarn,
				"interop.monitor.block_ranges_missing":    report.SeverityInfo,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings := checkInteropMonitorMetricSamples(loadMetricFixture(t, tt.fixture))
			assertFindingSeverities(t, findings, tt.want)
			if tt.wantNoWarnOrFail {
				assertNoWarnOrFail(t, findings)
			}
		})
	}
}

func loadMetricFixture(t *testing.T, name string) []metrics.Sample {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "metrics", name)
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open fixture %s: %v", name, err)
	}
	defer f.Close()
	samples, err := metrics.ParseText(f)
	if err != nil {
		t.Fatalf("parse fixture %s: %v", name, err)
	}
	return samples
}

func assertFindingSeverities(t *testing.T, findings []report.Finding, want map[string]report.Severity) {
	t.Helper()
	got := map[string]report.Severity{}
	for _, finding := range findings {
		got[finding.ID] = finding.Severity
	}
	for id, severity := range want {
		if got[id] != severity {
			t.Fatalf("finding %s severity = %q, want %q; findings: %s", id, got[id], severity, fixtureFindingSummary(findings))
		}
	}
}

func assertNoWarnOrFail(t *testing.T, findings []report.Finding) {
	t.Helper()
	for _, finding := range findings {
		if finding.Severity == report.SeverityWarn || finding.Severity == report.SeverityFail {
			t.Fatalf("unexpected %s finding %s: %s", finding.Severity, finding.ID, fixtureFindingSummary(findings))
		}
	}
}

func fixtureFindingSummary(findings []report.Finding) string {
	parts := make([]string, 0, len(findings))
	for _, finding := range findings {
		parts = append(parts, finding.ID+"="+string(finding.Severity))
	}
	return strings.Join(parts, ", ")
}
