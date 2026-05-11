package checks

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"opstack-doctor/internal/config"
	"opstack-doctor/internal/report"
)

func TestRunnerWithMockedEndpoints(t *testing.T) {
	refRPC := newRPCServer(t, "op-geth/v1.101", 10, 100)
	defer refRPC.Close()
	candRPC := newRPCServer(t, "op-reth/v1.0", 10, 100)
	defer candRPC.Close()
	sourceRPC := newRPCServer(t, "op-node/source", 10, 100)
	defer sourceRPC.Close()
	source2RPC := newRPCServer(t, "op-node/source-2", 10, 100)
	defer source2RPC.Close()
	lightRPC := newRPCServer(t, "op-node/light", 10, 98)
	defer lightRPC.Close()
	proxydRPC := newRPCServer(t, "proxyd/deriver", 10, 100)
	defer proxydRPC.Close()
	depRPC := newRPCServer(t, "reth/base", 8453, 200)
	defer depRPC.Close()

	sourceMetrics := newMetricsServer(t, 100)
	defer sourceMetrics.Close()
	source2Metrics := newMetricsServer(t, 100)
	defer source2Metrics.Close()
	lightMetrics := newMetricsServer(t, 98)
	defer lightMetrics.Close()
	proxydMetrics := newProxydMetricsServer(t)
	defer proxydMetrics.Close()
	depMetrics := newMetricsServer(t, 200)
	defer depMetrics.Close()
	supervisorMetrics := newSupervisorMetricsServer(t)
	defer supervisorMetrics.Close()
	monitorMetrics := newInteropMonitorMetricsServer(t)
	defer monitorMetrics.Close()

	cfg := config.Config{
		Chain: config.ChainConfig{Name: "op-mainnet", ChainID: 10},
		Execution: config.ExecutionConfig{
			ReferenceRPC:     refRPC.URL,
			CandidateRPC:     candRPC.URL,
			CompareBlocks:    4,
			MaxHeadLagBlocks: 4,
		},
		OPNodes: []config.OPNodeConfig{
			{Name: "source-1", Role: "source", RPC: sourceRPC.URL, Metrics: sourceMetrics.URL},
			{Name: "source-2", Role: "source", RPC: source2RPC.URL, Metrics: source2Metrics.URL},
			{Name: "light-1", Role: "light", RPC: lightRPC.URL, Metrics: lightMetrics.URL, Follows: "source-1"},
		},
		Proxyd: config.ProxydConfig{
			Enabled: true,
			Endpoints: []config.ProxydEndpointConfig{
				{Name: "deriver-proxyd", Role: "deriver", RPC: proxydRPC.URL, Metrics: proxydMetrics.URL, ConsensusAware: true, ExpectedBackends: []string{"source-1", "source-2"}},
			},
		},
		Interop: config.InteropConfig{
			Enabled: true,
			Supervisor: config.InteropSupervisorConfig{
				Metrics:        supervisorMetrics.URL,
				ExpectedChains: []uint64{10, 8453},
			},
			Monitor: config.InteropMonitorConfig{Metrics: monitorMetrics.URL},
			Dependencies: []config.DependencyConfig{
				{Name: "base", ChainID: 8453, RPC: depRPC.URL, Metrics: depMetrics.URL},
			},
		},
		Thresholds: config.ThresholdsConfig{MaxSafeLagBlocks: 20, MinPeerCount: 1, MaxRPCLatencySeconds: 2},
	}
	findings := Runner{Timeout: time.Second}.Run(context.Background(), cfg)
	for _, f := range findings {
		if f.Severity == report.SeverityFail {
			t.Fatalf("unexpected fail finding: %+v", f)
		}
	}
	for _, id := range []string{"execution.block_compare.match", "execution.rpc_surface.match", "topology.light-1.safe_head_metrics", "proxyd.deriver-proxyd.head_lag", "interop.scope", "interop.supervisor.expected_chains", "interop.monitor.message_status"} {
		if !hasFinding(findings, id) {
			t.Fatalf("missing finding %s; got ids %s", id, findingIDs(findings))
		}
	}
}

func TestRunnerFlagsProxydRoutingRisks(t *testing.T) {
	sourceRPC := newRPCServer(t, "op-node/source", 10, 100)
	defer sourceRPC.Close()
	proxydRPC := newRPCServer(t, "proxyd/deriver", 10, 70)
	defer proxydRPC.Close()
	refRPC := newRPCServer(t, "op-reth/ref", 10, 100)
	defer refRPC.Close()
	candRPC := newRPCServer(t, "op-reth/cand", 10, 100)
	defer candRPC.Close()

	cfg := config.Config{
		Chain: config.ChainConfig{Name: "op-mainnet", ChainID: 10},
		Execution: config.ExecutionConfig{
			ReferenceRPC:     refRPC.URL,
			CandidateRPC:     candRPC.URL,
			CompareBlocks:    1,
			MaxHeadLagBlocks: 4,
		},
		OPNodes: []config.OPNodeConfig{
			{Name: "source-1", Role: "source", RPC: sourceRPC.URL},
			{Name: "light-1", Role: "light", Follows: "source-1"},
		},
		Proxyd: config.ProxydConfig{
			Enabled: true,
			Endpoints: []config.ProxydEndpointConfig{
				{Name: "deriver-proxyd", Role: "deriver", RPC: proxydRPC.URL, ConsensusAware: false, ExpectedBackends: []string{"source-1"}},
			},
		},
		Thresholds: config.ThresholdsConfig{MaxSafeLagBlocks: 20, MinPeerCount: 1},
	}
	cfg.ApplyDefaults()
	findings := Runner{Timeout: time.Second}.Run(context.Background(), cfg)
	for _, id := range []string{"proxyd.deriver-proxyd.consensus_aware", "proxyd.deriver-proxyd.head_lag", "proxyd.deriver-proxyd.metrics"} {
		if !hasFinding(findings, id) {
			t.Fatalf("missing finding %s; got ids %s", id, findingIDs(findings))
		}
	}
	if !hasWarnFinding(findings, "proxyd.deriver-proxyd.head_lag") {
		t.Fatalf("expected proxyd head lag warning; got %+v", findings)
	}
}

func TestRunnerFlagsProxydNativeMetricRisks(t *testing.T) {
	sourceRPC := newRPCServer(t, "op-node/source", 10, 100)
	defer sourceRPC.Close()
	proxydRPC := newRPCServer(t, "proxyd/deriver", 10, 100)
	defer proxydRPC.Close()
	refRPC := newRPCServer(t, "op-reth/ref", 10, 100)
	defer refRPC.Close()
	candRPC := newRPCServer(t, "op-reth/cand", 10, 100)
	defer candRPC.Close()
	proxydMetrics := newProxydRiskMetricsServer(t)
	defer proxydMetrics.Close()

	cfg := config.Config{
		Chain: config.ChainConfig{Name: "op-mainnet", ChainID: 10},
		Execution: config.ExecutionConfig{
			ReferenceRPC:     refRPC.URL,
			CandidateRPC:     candRPC.URL,
			CompareBlocks:    1,
			MaxHeadLagBlocks: 4,
		},
		OPNodes: []config.OPNodeConfig{
			{Name: "source-1", Role: "source", RPC: sourceRPC.URL},
			{Name: "light-1", Role: "light", Follows: "source-1"},
		},
		Proxyd: config.ProxydConfig{
			Enabled: true,
			Endpoints: []config.ProxydEndpointConfig{
				{Name: "deriver-proxyd", Role: "deriver", RPC: proxydRPC.URL, Metrics: proxydMetrics.URL, ConsensusAware: true, ExpectedBackends: []string{"source-1"}},
			},
		},
		Thresholds: config.ThresholdsConfig{MaxSafeLagBlocks: 20, MinPeerCount: 1, MaxRPCLatencySeconds: 2},
	}
	cfg.ApplyDefaults()
	findings := Runner{Timeout: time.Second}.Run(context.Background(), cfg)
	for _, id := range []string{
		"proxyd.deriver-proxyd.up",
		"proxyd.deriver-proxyd.backend_probe_healthy",
		"proxyd.deriver-proxyd.backend_degraded",
		"proxyd.deriver-proxyd.backend_banned",
		"proxyd.deriver-proxyd.backend_in_sync",
		"proxyd.deriver-proxyd.error_counters",
		"proxyd.deriver-proxyd.cl_consensus_counters",
		"proxyd.deriver-proxyd.backend_error_rate",
		"proxyd.deriver-proxyd.backend_latency",
		"proxyd.deriver-proxyd.consensus_count",
	} {
		if !hasFinding(findings, id) {
			t.Fatalf("missing finding %s; got ids %s", id, findingIDs(findings))
		}
	}
	if !hasFailFinding(findings, "proxyd.deriver-proxyd.up") {
		t.Fatalf("expected proxyd up failure; got %+v", findings)
	}
	if !hasWarnFinding(findings, "proxyd.deriver-proxyd.backend_latency") {
		t.Fatalf("expected proxyd latency warning; got %+v", findings)
	}
}

func newRPCServer(t *testing.T, version string, chainID, head uint64) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		var result any
		switch req.Method {
		case "web3_clientVersion":
			result = version
		case "eth_chainId":
			result = fmt.Sprintf("0x%x", chainID)
		case "eth_blockNumber":
			result = fmt.Sprintf("0x%x", head)
		case "eth_getBlockByNumber":
			var params []any
			if err := json.Unmarshal(req.Params, &params); err != nil {
				t.Fatalf("decode params: %v", err)
			}
			n, ok := params[0].(string)
			if !ok {
				t.Fatalf("block number param = %#v", params[0])
			}
			result = map[string]any{
				"number":           n,
				"hash":             "0xhash" + n,
				"parentHash":       "0xparent" + n,
				"stateRoot":        "0xstate" + n,
				"transactionsRoot": "0xtx" + n,
				"receiptsRoot":     "0xreceipt" + n,
			}
		case "eth_getBlockByHash":
			var params []any
			if err := json.Unmarshal(req.Params, &params); err != nil {
				t.Fatalf("decode params: %v", err)
			}
			hash, ok := params[0].(string)
			if !ok {
				t.Fatalf("block hash param = %#v", params[0])
			}
			result = map[string]any{
				"number":           fmt.Sprintf("0x%x", head),
				"hash":             hash,
				"parentHash":       "0xparent_by_hash",
				"stateRoot":        "0xstate_by_hash",
				"transactionsRoot": "0xtx_by_hash",
				"receiptsRoot":     "0xreceipt_by_hash",
			}
		case "eth_getBlockTransactionCountByNumber":
			result = "0x2"
		default:
			result = nil
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
}

func newMetricsServer(t *testing.T, safe int) *httptest.Server {
	t.Helper()
	body := fmt.Sprintf(`
op_node_default_up 1
op_node_default_refs_number{ref="l2_safe"} %d
op_node_default_refs_number{ref="l2_finalized"} %d
op_node_default_peer_count 2
op_node_default_derivation_errors_total 0
op_node_default_pipeline_resets_total 0
op_node_default_rpc_client_request_duration_seconds_count{method="eth_getBlockByNumber"} 10
`, safe, safe-5)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
}

func newProxydMetricsServer(t *testing.T) *httptest.Server {
	t.Helper()
	body := `
proxyd_up 1
proxyd_group_consensus_latest_block{backend_group_name="deriver"} 100
proxyd_group_consensus_safe_block{backend_group_name="deriver"} 99
proxyd_group_consensus_finalized_block{backend_group_name="deriver"} 95
proxyd_group_consensus_count{backend_group_name="deriver"} 2
proxyd_group_consensus_total_count{backend_group_name="deriver"} 2
proxyd_consensus_cl_group_local_safe_block{backend_group_name="deriver"} 99
proxyd_backend_probe_healthy{backend_name="source-1"} 1
proxyd_backend_probe_healthy{backend_name="source-2"} 1
proxyd_backend_degraded{backend_name="source-1"} 0
proxyd_backend_degraded{backend_name="source-2"} 0
proxyd_consensus_backend_banned{backend_name="source-1"} 0
proxyd_consensus_backend_banned{backend_name="source-2"} 0
proxyd_consensus_backend_in_sync{backend_name="source-1"} 1
proxyd_consensus_backend_in_sync{backend_name="source-2"} 1
proxyd_consensus_backend_peer_count{backend_name="source-1"} 3
proxyd_consensus_backend_peer_count{backend_name="source-2"} 3
proxyd_backend_error_rate{backend_name="source-1"} 0
proxyd_backend_error_rate{backend_name="source-2"} 0
proxyd_rpc_errors_total{auth="none",backend_name="source-1",method_name="eth_blockNumber",error_code="0"} 0
proxyd_rpc_special_errors_total{auth="none",backend_name="source-1",method_name="eth_blockNumber",error_type="none"} 0
proxyd_unserviceable_requests_total{auth="none",request_source="http"} 0
proxyd_too_many_request_errors_total{backend_name="source-1"} 0
proxyd_redis_errors_total{source="cache"} 0
proxyd_consensus_cl_ban_not_healthy_total{backend_name="source-1"} 0
proxyd_consensus_cl_output_root_disagreement_total{backend_name="source-1"} 0
proxyd_http_response_codes_total{status_code="200"} 1
proxyd_rpc_backend_request_duration_seconds{backend_name="source-1",method_name="eth_blockNumber",batched="false",quantile="0.95"} 0.2
proxyd_rpc_backend_request_duration_seconds_count{backend_name="source-1",method_name="eth_blockNumber",batched="false"} 10
`
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
}

func newProxydRiskMetricsServer(t *testing.T) *httptest.Server {
	t.Helper()
	body := `
proxyd_up 0
proxyd_group_consensus_latest_block{backend_group_name="deriver"} 100
proxyd_group_consensus_safe_block{backend_group_name="deriver"} 99
proxyd_group_consensus_finalized_block{backend_group_name="deriver"} 95
proxyd_group_consensus_count{backend_group_name="deriver"} 0
proxyd_group_consensus_total_count{backend_group_name="deriver"} 1
proxyd_consensus_cl_group_local_safe_block{backend_group_name="deriver"} 99
proxyd_backend_probe_healthy{backend_name="source-1"} 0
proxyd_backend_degraded{backend_name="source-1"} 1
proxyd_consensus_backend_banned{backend_name="source-1"} 1
proxyd_consensus_backend_in_sync{backend_name="source-1"} 0
proxyd_consensus_backend_peer_count{backend_name="source-1"} 0
proxyd_backend_error_rate{backend_name="source-1"} 1
proxyd_rpc_errors_total{auth="none",backend_name="source-1",method_name="eth_blockNumber",error_code="-32000"} 4
proxyd_rpc_special_errors_total{auth="none",backend_name="source-1",method_name="eth_blockNumber",error_type="timeout"} 1
proxyd_unserviceable_requests_total{auth="none",request_source="http"} 2
proxyd_consensus_cl_ban_not_healthy_total{backend_name="source-1"} 1
proxyd_consensus_cl_output_root_disagreement_total{backend_name="source-1"} 1
proxyd_http_response_codes_total{status_code="500"} 1
proxyd_rpc_backend_request_duration_seconds{backend_name="source-1",method_name="eth_blockNumber",batched="false",quantile="0.95"} 3.5
proxyd_rpc_backend_request_duration_seconds_count{backend_name="source-1",method_name="eth_blockNumber",batched="false"} 10
`
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
}

func newSupervisorMetricsServer(t *testing.T) *httptest.Server {
	t.Helper()
	body := `
op_supervisor_default_up 1
op_supervisor_default_info{version="test"} 1
op_supervisor_default_refs_number{layer="l2",type="local_unsafe",chain="10"} 100
op_supervisor_default_refs_number{layer="l2",type="cross_unsafe",chain="10"} 99
op_supervisor_default_refs_number{layer="l2",type="local_safe",chain="10"} 90
op_supervisor_default_refs_number{layer="l2",type="cross_safe",chain="10"} 89
op_supervisor_default_refs_number{layer="l2",type="local_unsafe",chain="8453"} 200
op_supervisor_default_refs_number{layer="l2",type="cross_unsafe",chain="8453"} 199
op_supervisor_default_refs_number{layer="l2",type="local_safe",chain="8453"} 190
op_supervisor_default_refs_number{layer="l2",type="cross_safe",chain="8453"} 189
op_supervisor_default_access_list_verify_failure{chain="10"} 0
op_supervisor_default_logdb_entries_current{chain="10",kind="events"} 10
op_supervisor_default_rpc_client_requests_total{rpc="l2",method="eth_blockNumber"} 1
`
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
}

func newInteropMonitorMetricsServer(t *testing.T) *httptest.Server {
	t.Helper()
	body := `
op_interop_mon_default_up 1
op_interop_mon_default_message_status{executing_chain_id="10",initiating_chain_id="8453",status="valid"} 1
op_interop_mon_default_message_status{executing_chain_id="10",initiating_chain_id="8453",status="invalid"} 0
op_interop_mon_default_terminal_status_changes{executing_chain_id="10",initiating_chain_id="8453"} 0
op_interop_mon_default_executing_block_range{chain_id="10",range_type="min"} 100
op_interop_mon_default_executing_block_range{chain_id="10",range_type="max"} 101
`
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
}

func hasFinding(findings []report.Finding, id string) bool {
	for _, f := range findings {
		if f.ID == id {
			return true
		}
	}
	return false
}

func hasWarnFinding(findings []report.Finding, id string) bool {
	for _, f := range findings {
		if f.ID == id && f.Severity == report.SeverityWarn {
			return true
		}
	}
	return false
}

func hasFailFinding(findings []report.Finding, id string) bool {
	for _, f := range findings {
		if f.ID == id && f.Severity == report.SeverityFail {
			return true
		}
	}
	return false
}

func findingIDs(findings []report.Finding) string {
	ids := make([]string, 0, len(findings))
	for _, f := range findings {
		ids = append(ids, f.ID)
	}
	return strings.Join(ids, ",")
}
