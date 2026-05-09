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
	lightRPC := newRPCServer(t, "op-node/light", 10, 98)
	defer lightRPC.Close()
	depRPC := newRPCServer(t, "reth/base", 8453, 200)
	defer depRPC.Close()

	sourceMetrics := newMetricsServer(t, 100)
	defer sourceMetrics.Close()
	lightMetrics := newMetricsServer(t, 98)
	defer lightMetrics.Close()
	depMetrics := newMetricsServer(t, 200)
	defer depMetrics.Close()

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
			{Name: "light-1", Role: "light", RPC: lightRPC.URL, Metrics: lightMetrics.URL, Follows: "source-1"},
		},
		Interop: config.InteropConfig{
			Enabled: true,
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
	for _, id := range []string{"execution.block_compare.match", "execution.rpc_surface.match", "topology.light-1.safe_head_metrics", "interop.scope"} {
		if !hasFinding(findings, id) {
			t.Fatalf("missing finding %s; got ids %s", id, findingIDs(findings))
		}
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

func hasFinding(findings []report.Finding, id string) bool {
	for _, f := range findings {
		if f.ID == id {
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
