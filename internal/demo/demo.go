package demo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	"opstack-doctor/internal/config"
)

const (
	ScenarioHealthy = "healthy"
	ScenarioWarn    = "warn"
	ScenarioFail    = "fail"
)

type rpcSpec struct {
	version string
	chainID uint64
	head    uint64
	salt    string
}

type metricsSpec struct {
	up               float64
	safe             uint64
	peers            float64
	derivationErrors float64
	pipelineResets   float64
}

func Config(scenario string) (config.Config, func(), error) {
	if scenario == "" {
		scenario = ScenarioHealthy
	}

	var cfg config.Config
	var servers []*httptest.Server
	addRPC := func(spec rpcSpec) string {
		srv := newRPCServer(spec)
		servers = append(servers, srv)
		return srv.URL
	}
	addMetrics := func(spec metricsSpec) string {
		srv := newMetricsServer(spec)
		servers = append(servers, srv)
		return srv.URL
	}
	cleanup := func() {
		for _, srv := range servers {
			srv.Close()
		}
	}

	base := func() {
		cfg = config.Config{
			Chain: config.ChainConfig{Name: "op-mainnet-demo", ChainID: 10},
			Execution: config.ExecutionConfig{
				CompareBlocks:    4,
				MaxHeadLagBlocks: 4,
				ReferenceRPC:     addRPC(rpcSpec{version: "op-reth/v1.0.0-demo", chainID: 10, head: 100, salt: "same"}),
				CandidateRPC:     addRPC(rpcSpec{version: "op-reth/v1.0.0-demo", chainID: 10, head: 100, salt: "same"}),
			},
			Interop: config.InteropConfig{
				Enabled: true,
				Dependencies: []config.DependencyConfig{
					{
						Name:    "base-demo",
						ChainID: 8453,
						RPC:     addRPC(rpcSpec{version: "op-reth/base-demo", chainID: 8453, head: 200, salt: "base"}),
						Metrics: addMetrics(metricsSpec{up: 1, safe: 200, peers: 3}),
					},
				},
			},
			Thresholds: config.ThresholdsConfig{
				MaxSafeLagBlocks:     20,
				MinPeerCount:         1,
				MaxRPCLatencySeconds: 2,
			},
		}
	}

	switch scenario {
	case ScenarioHealthy:
		base()
		cfg.OPNodes = []config.OPNodeConfig{
			{Name: "source-1", Role: "source", RPC: addRPC(rpcSpec{version: "op-node/source-1", chainID: 10, head: 100, salt: "source1"}), Metrics: addMetrics(metricsSpec{up: 1, safe: 100, peers: 3})},
			{Name: "source-2", Role: "source", RPC: addRPC(rpcSpec{version: "op-node/source-2", chainID: 10, head: 100, salt: "source2"}), Metrics: addMetrics(metricsSpec{up: 1, safe: 100, peers: 3})},
			{Name: "light-1", Role: "light", RPC: addRPC(rpcSpec{version: "op-node/light-1", chainID: 10, head: 99, salt: "light"}), Metrics: addMetrics(metricsSpec{up: 1, safe: 99, peers: 2}), Follows: "source-1"},
		}
	case ScenarioWarn:
		base()
		cfg.Execution.ReferenceRPC = addRPC(rpcSpec{version: "op-geth/v1.101.0-demo", chainID: 10, head: 100, salt: "same"})
		cfg.Execution.CandidateRPC = addRPC(rpcSpec{version: "op-reth/v1.0.0-demo", chainID: 10, head: 98, salt: "same"})
		cfg.OPNodes = []config.OPNodeConfig{
			{Name: "source-1", Role: "source", RPC: addRPC(rpcSpec{version: "op-node/source-1", chainID: 10, head: 100, salt: "source1"}), Metrics: addMetrics(metricsSpec{up: 1, safe: 100, peers: 0, derivationErrors: 1})},
			{Name: "light-1", Role: "light", RPC: addRPC(rpcSpec{version: "op-node/light-1", chainID: 10, head: 70, salt: "light"}), Metrics: addMetrics(metricsSpec{up: 1, safe: 70, peers: 1}), Follows: "source-1"},
		}
	case ScenarioFail:
		base()
		cfg.Execution.ReferenceRPC = addRPC(rpcSpec{version: "op-geth/v1.101.0-demo", chainID: 10, head: 100, salt: "ref"})
		cfg.Execution.CandidateRPC = addRPC(rpcSpec{version: "op-geth/v1.101.0-demo", chainID: 10, head: 90, salt: "cand"})
		cfg.OPNodes = []config.OPNodeConfig{
			{Name: "source-1", Role: "source", RPC: addRPC(rpcSpec{version: "op-node/source-1", chainID: 10, head: 100, salt: "source1"}), Metrics: addMetrics(metricsSpec{up: 0, safe: 100, peers: 0, pipelineResets: 2})},
			{Name: "light-1", Role: "light", RPC: addRPC(rpcSpec{version: "op-node/light-1", chainID: 10, head: 60, salt: "light"}), Metrics: addMetrics(metricsSpec{up: 1, safe: 60, peers: 0}), Follows: "source-1"},
		}
	default:
		cleanup()
		return config.Config{}, nil, fmt.Errorf("unknown demo scenario %q: expected healthy, warn, or fail", scenario)
	}
	cfg.ApplyDefaults()
	return cfg, cleanup, nil
}

func newRPCServer(spec rpcSpec) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var result any
		switch req.Method {
		case "web3_clientVersion":
			result = spec.version
		case "eth_chainId":
			result = quantity(spec.chainID)
		case "eth_blockNumber":
			result = quantity(spec.head)
		case "eth_getBlockByNumber":
			var params []any
			if err := json.Unmarshal(req.Params, &params); err != nil || len(params) == 0 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			number, _ := params[0].(string)
			result = map[string]any{
				"number":           number,
				"hash":             "0x" + spec.salt + "_hash_" + number,
				"parentHash":       "0x" + spec.salt + "_parent_" + number,
				"stateRoot":        "0x" + spec.salt + "_state_" + number,
				"transactionsRoot": "0x" + spec.salt + "_tx_" + number,
				"receiptsRoot":     "0x" + spec.salt + "_receipt_" + number,
			}
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"error":   map[string]any{"code": -32601, "message": "method not found"},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
}

func newMetricsServer(spec metricsSpec) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		finalized := uint64(0)
		if spec.safe > 5 {
			finalized = spec.safe - 5
		}
		unsafe := spec.safe + 1
		fmt.Fprintf(w, "op_node_default_up %.0f\n", spec.up)
		fmt.Fprintf(w, "op_node_default_refs_number{ref=\"l2_safe\"} %d\n", spec.safe)
		fmt.Fprintf(w, "op_node_default_refs_number{ref=\"l2_finalized\"} %d\n", finalized)
		fmt.Fprintf(w, "op_node_default_refs_number{ref=\"l2_unsafe\"} %d\n", unsafe)
		fmt.Fprintf(w, "op_node_default_peer_count %.0f\n", spec.peers)
		fmt.Fprintf(w, "op_node_default_derivation_errors_total %.0f\n", spec.derivationErrors)
		fmt.Fprintf(w, "op_node_default_pipeline_resets_total %.0f\n", spec.pipelineResets)
		fmt.Fprintln(w, "op_node_default_rpc_client_request_duration_seconds_count{method=\"eth_getBlockByNumber\"} 10")
	}))
}

func quantity(n uint64) string {
	return fmt.Sprintf("0x%x", n)
}
