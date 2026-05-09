package report

import (
	"strings"
	"testing"
)

func TestRenderPrometheus(t *testing.T) {
	findings := []Finding{
		{
			ID:       "execution.head_lag",
			Severity: SeverityOK,
			Target:   "execution",
			Evidence: map[string]string{"lag_blocks": "3"},
		},
		{
			ID:       "execution.block_compare.match",
			Severity: SeverityOK,
			Target:   "execution",
		},
		{
			ID:       "topology.light-1.rpc_head",
			Severity: SeverityOK,
			Target:   "op_nodes.light-1",
			Evidence: map[string]string{"lag_blocks": "2", "source": "source-1"},
		},
	}
	var out strings.Builder
	if err := RenderPrometheus(&out, findings, PrometheusOptions{Chain: "op-mainnet"}); err != nil {
		t.Fatalf("RenderPrometheus() error = %v", err)
	}
	got := out.String()
	for _, want := range []string{
		`opstack_doctor_findings{severity="ok"} 3`,
		`opstack_doctor_execution_candidate_lag_blocks{chain="op-mainnet"} 3`,
		`opstack_doctor_execution_block_compare_match{chain="op-mainnet"} 1`,
		`opstack_doctor_topology_follower_lag_blocks{chain="op-mainnet",kind="rpc_head",node="light-1",source="source-1"} 2`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("Prometheus output missing %q:\n%s", want, got)
		}
	}
}
