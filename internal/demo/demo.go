package demo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

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

type proxydMetricsSpec struct {
	up            float64
	backends      []string
	latest        uint64
	safe          uint64
	finalized     uint64
	probeHealthy  float64
	degraded      float64
	banned        float64
	inSync        float64
	errorCounters float64
	latency       float64
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
	addProxydMetrics := func(spec proxydMetricsSpec) string {
		srv := newProxydMetricsServer(spec)
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
		source1RPC := addRPC(rpcSpec{version: "op-node/source-1", chainID: 10, head: 100, salt: "source1"})
		source2RPC := addRPC(rpcSpec{version: "op-node/source-2", chainID: 10, head: 100, salt: "source2"})
		lightRPC := addRPC(rpcSpec{version: "op-node/light-1", chainID: 10, head: 99, salt: "light"})
		cfg.OPNodes = []config.OPNodeConfig{
			{Name: "source-1", Role: "source", RPC: source1RPC, Metrics: addMetrics(metricsSpec{up: 1, safe: 100, peers: 3})},
			{Name: "source-2", Role: "source", RPC: source2RPC, Metrics: addMetrics(metricsSpec{up: 1, safe: 100, peers: 3})},
			{Name: "light-1", Role: "light", RPC: lightRPC, Metrics: addMetrics(metricsSpec{up: 1, safe: 99, peers: 2}), Follows: "source-1"},
		}
		cfg.Proxyd = config.ProxydConfig{
			Enabled: true,
			Endpoints: []config.ProxydEndpointConfig{
				{Name: "deriver-proxyd", Role: "deriver", RPC: addRPC(rpcSpec{version: "proxyd/deriver", chainID: 10, head: 100, salt: "proxyd"}), Metrics: addProxydMetrics(proxydMetricsSpec{up: 1, backends: []string{"source-1", "source-2"}, latest: 100, safe: 99, finalized: 95, probeHealthy: 1, inSync: 1, latency: 0.2}), ConsensusAware: true, ExpectedBackends: []string{"source-1", "source-2"}},
				{Name: "edge-proxyd", Role: "edge", RPC: addRPC(rpcSpec{version: "proxyd/edge", chainID: 10, head: 99, salt: "edge"}), Metrics: addProxydMetrics(proxydMetricsSpec{up: 1, backends: []string{"light-1"}, latest: 99, safe: 98, finalized: 94, probeHealthy: 1, inSync: 1, latency: 0.2}), ConsensusAware: true, ExpectedBackends: []string{"light-1"}},
			},
		}
	case ScenarioWarn:
		base()
		cfg.Execution.ReferenceRPC = addRPC(rpcSpec{version: "op-geth/v1.101.0-demo", chainID: 10, head: 100, salt: "same"})
		cfg.Execution.CandidateRPC = addRPC(rpcSpec{version: "op-reth/v1.0.0-demo", chainID: 10, head: 98, salt: "same"})
		sourceRPC := addRPC(rpcSpec{version: "op-node/source-1", chainID: 10, head: 100, salt: "source1"})
		lightRPC := addRPC(rpcSpec{version: "op-node/light-1", chainID: 10, head: 70, salt: "light"})
		cfg.OPNodes = []config.OPNodeConfig{
			{Name: "source-1", Role: "source", RPC: sourceRPC, Metrics: addMetrics(metricsSpec{up: 1, safe: 100, peers: 0, derivationErrors: 1})},
			{Name: "light-1", Role: "light", RPC: lightRPC, Metrics: addMetrics(metricsSpec{up: 1, safe: 70, peers: 1}), Follows: "source-1"},
		}
		cfg.Proxyd = config.ProxydConfig{
			Enabled: true,
			Endpoints: []config.ProxydEndpointConfig{
				{Name: "deriver-proxyd", Role: "deriver", RPC: addRPC(rpcSpec{version: "proxyd/deriver", chainID: 10, head: 70, salt: "proxyd"}), Metrics: addProxydMetrics(proxydMetricsSpec{up: 1, backends: []string{"source-1"}, latest: 70, safe: 69, finalized: 65, probeHealthy: 1, inSync: 1, latency: 0.2}), ConsensusAware: false, ExpectedBackends: []string{"source-1"}},
			},
		}
	case ScenarioFail:
		base()
		cfg.Execution.ReferenceRPC = addRPC(rpcSpec{version: "op-geth/v1.101.0-demo", chainID: 10, head: 100, salt: "ref"})
		cfg.Execution.CandidateRPC = addRPC(rpcSpec{version: "op-geth/v1.101.0-demo", chainID: 10, head: 90, salt: "cand"})
		sourceRPC := addRPC(rpcSpec{version: "op-node/source-1", chainID: 10, head: 100, salt: "source1"})
		lightRPC := addRPC(rpcSpec{version: "op-node/light-1", chainID: 10, head: 60, salt: "light"})
		cfg.OPNodes = []config.OPNodeConfig{
			{Name: "source-1", Role: "source", RPC: sourceRPC, Metrics: addMetrics(metricsSpec{up: 0, safe: 100, peers: 0, pipelineResets: 2})},
			{Name: "light-1", Role: "light", RPC: lightRPC, Metrics: addMetrics(metricsSpec{up: 1, safe: 60, peers: 0}), Follows: "source-1"},
		}
		cfg.Proxyd = config.ProxydConfig{
			Enabled: true,
			Endpoints: []config.ProxydEndpointConfig{
				{Name: "deriver-proxyd", Role: "deriver", RPC: addRPC(rpcSpec{version: "proxyd/deriver", chainID: 999, head: 40, salt: "proxyd"}), Metrics: addProxydMetrics(proxydMetricsSpec{up: 0, backends: []string{"source-1"}, latest: 40, safe: 39, finalized: 35, probeHealthy: 0, degraded: 1, banned: 1, errorCounters: 1, latency: 3}), ConsensusAware: false, ExpectedBackends: []string{"source-1"}},
			},
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
		case "eth_getBlockByHash":
			var params []any
			if err := json.Unmarshal(req.Params, &params); err != nil || len(params) == 0 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			hash, _ := params[0].(string)
			if !strings.Contains(hash, "0x"+spec.salt+"_hash_") {
				result = nil
				break
			}
			result = map[string]any{
				"number":           quantity(spec.head),
				"hash":             hash,
				"parentHash":       "0x" + spec.salt + "_parent_by_hash",
				"stateRoot":        "0x" + spec.salt + "_state_by_hash",
				"transactionsRoot": "0x" + spec.salt + "_tx_by_hash",
				"receiptsRoot":     "0x" + spec.salt + "_receipt_by_hash",
			}
		case "eth_getBlockTransactionCountByNumber":
			result = "0x2"
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

func newProxydMetricsServer(spec proxydMetricsSpec) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(spec.backends) == 0 {
			spec.backends = []string{"source-1"}
		}
		fmt.Fprintf(w, "proxyd_up %.0f\n", spec.up)
		fmt.Fprintf(w, "proxyd_group_consensus_latest_block{backend_group_name=\"demo\"} %d\n", spec.latest)
		fmt.Fprintf(w, "proxyd_group_consensus_safe_block{backend_group_name=\"demo\"} %d\n", spec.safe)
		fmt.Fprintf(w, "proxyd_group_consensus_finalized_block{backend_group_name=\"demo\"} %d\n", spec.finalized)
		fmt.Fprintf(w, "proxyd_group_consensus_count{backend_group_name=\"demo\"} %d\n", len(spec.backends))
		fmt.Fprintf(w, "proxyd_group_consensus_total_count{backend_group_name=\"demo\"} %d\n", len(spec.backends))
		fmt.Fprintf(w, "proxyd_consensus_cl_group_local_safe_block{backend_group_name=\"demo\"} %d\n", spec.safe)
		fmt.Fprintf(w, "proxyd_rpc_errors_total{auth=\"none\",backend_name=\"proxyd\",method_name=\"eth_blockNumber\",error_code=\"0\"} %.0f\n", spec.errorCounters)
		fmt.Fprintln(w, "proxyd_rpc_special_errors_total{auth=\"none\",backend_name=\"proxyd\",method_name=\"eth_blockNumber\",error_type=\"none\"} 0")
		fmt.Fprintln(w, "proxyd_unserviceable_requests_total{auth=\"none\",request_source=\"http\"} 0")
		fmt.Fprintln(w, "proxyd_too_many_request_errors_total{backend_name=\"proxyd\"} 0")
		fmt.Fprintln(w, "proxyd_redis_errors_total{source=\"cache\"} 0")
		fmt.Fprintln(w, "proxyd_consensus_cl_ban_not_healthy_total{backend_name=\"source-1\"} 0")
		fmt.Fprintln(w, "proxyd_consensus_cl_output_root_disagreement_total{backend_name=\"source-1\"} 0")
		for _, backend := range spec.backends {
			fmt.Fprintf(w, "proxyd_backend_probe_healthy{backend_name=\"%s\"} %.0f\n", backend, spec.probeHealthy)
			fmt.Fprintf(w, "proxyd_backend_degraded{backend_name=\"%s\"} %.0f\n", backend, spec.degraded)
			fmt.Fprintf(w, "proxyd_consensus_backend_banned{backend_name=\"%s\"} %.0f\n", backend, spec.banned)
			fmt.Fprintf(w, "proxyd_consensus_backend_in_sync{backend_name=\"%s\"} %.0f\n", backend, spec.inSync)
			fmt.Fprintf(w, "proxyd_consensus_backend_peer_count{backend_name=\"%s\"} 3\n", backend)
			fmt.Fprintf(w, "proxyd_backend_error_rate{backend_name=\"%s\"} %.0f\n", backend, spec.errorCounters)
			fmt.Fprintf(w, "proxyd_rpc_backend_request_duration_seconds{backend_name=\"%s\",method_name=\"eth_blockNumber\",batched=\"false\",quantile=\"0.95\"} %.3f\n", backend, spec.latency)
			fmt.Fprintf(w, "proxyd_rpc_backend_request_duration_seconds_count{backend_name=\"%s\",method_name=\"eth_blockNumber\",batched=\"false\"} 12\n", backend)
		}
	}))
}

func quantity(n uint64) string {
	return fmt.Sprintf("0x%x", n)
}
