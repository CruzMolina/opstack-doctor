package checks

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"opstack-doctor/internal/config"
	"opstack-doctor/internal/metrics"
	"opstack-doctor/internal/redact"
	"opstack-doctor/internal/report"
	"opstack-doctor/internal/rpc"
)

const (
	DocOPGethDeprecation = "https://docs.optimism.io/notices/op-geth-deprecation"
	DocLightNodes        = "https://www.optimism.io/blog/light-nodes-specialize-your-op-node-fleet"
	DocInterop           = "https://docs.optimism.io/op-stack/interop/explainer"
	DocMetrics           = "https://docs.optimism.io/node-operators/guides/monitoring/metrics"
	DocProxyd            = "https://docs.optimism.io/operators/chain-operators/tools/proxyd"
	DocOPSupervisor      = "https://github.com/ethereum-optimism/optimism/tree/develop/op-supervisor"
	DocInteropMonitor    = "https://github.com/ethereum-optimism/optimism/tree/develop/op-interop-mon"
)

type Runner struct {
	Timeout time.Duration
}

type BlockDifference struct {
	Field     string
	Reference string
	Candidate string
}

type executionEndpointStatus struct {
	name          string
	rpcURL        string
	clientVersion string
	chainID       uint64
	head          uint64
	reachable     bool
	chainOK       bool
	headOK        bool
}

type nodeMetricsState struct {
	node    config.OPNodeConfig
	samples []metrics.Sample
	ok      bool
	err     error
}

func (r Runner) Run(ctx context.Context, cfg config.Config) []report.Finding {
	if r.Timeout == 0 {
		r.Timeout = 10 * time.Second
	}
	var findings []report.Finding
	findings = append(findings, r.checkConfig(cfg)...)
	_, execFindings := r.checkExecution(ctx, cfg)
	findings = append(findings, execFindings...)
	metricStates, metricFindings := r.checkOPNodeMetrics(ctx, cfg)
	findings = append(findings, metricFindings...)
	findings = append(findings, r.checkTopology(ctx, cfg, metricStates)...)
	findings = append(findings, r.checkProxyd(ctx, cfg)...)
	findings = append(findings, r.checkInterop(ctx, cfg)...)
	return findings
}

func CompareBlockFields(reference, candidate rpc.Block) ([]BlockDifference, []string) {
	fields := []struct {
		name      string
		reference string
		candidate string
		optional  bool
	}{
		{"hash", rpc.StringValue(reference.Hash), rpc.StringValue(candidate.Hash), false},
		{"parentHash", rpc.StringValue(reference.ParentHash), rpc.StringValue(candidate.ParentHash), false},
		{"stateRoot", rpc.StringValue(reference.StateRoot), rpc.StringValue(candidate.StateRoot), false},
		{"transactionsRoot", rpc.StringValue(reference.TransactionsRoot), rpc.StringValue(candidate.TransactionsRoot), false},
		{"receiptsRoot", rpc.StringValue(reference.ReceiptsRoot), rpc.StringValue(candidate.ReceiptsRoot), true},
	}
	var diffs []BlockDifference
	var missing []string
	for _, field := range fields {
		if field.reference == "" && field.candidate == "" && field.optional {
			continue
		}
		if field.reference == "" || field.candidate == "" {
			missing = append(missing, field.name)
			continue
		}
		if !strings.EqualFold(field.reference, field.candidate) {
			diffs = append(diffs, BlockDifference{
				Field:     field.name,
				Reference: field.reference,
				Candidate: field.candidate,
			})
		}
	}
	return diffs, missing
}

func (r Runner) checkConfig(cfg config.Config) []report.Finding {
	issues := cfg.Validate()
	if len(issues) == 0 {
		return []report.Finding{finding("config.valid", "Configuration is valid", report.SeverityOK, "doctor.yaml", "required fields and topology intent are valid", "Keep this file in version control and review it when topology changes.", nil, nil)}
	}
	findings := make([]report.Finding, 0, len(issues))
	for _, issue := range issues {
		sev := report.SeverityWarn
		if issue.Severity == "fail" {
			sev = report.SeverityFail
		}
		findings = append(findings, finding(
			"config."+strings.ReplaceAll(issue.Field, ".", "_"),
			"Configuration issue",
			sev,
			issue.Field,
			issue.Message,
			"Fix the configuration before relying on diagnostic results from affected checks.",
			nil,
			nil,
		))
	}
	return findings
}

func (r Runner) checkExecution(ctx context.Context, cfg config.Config) (map[string]executionEndpointStatus, []report.Finding) {
	statuses := map[string]executionEndpointStatus{}
	var findings []report.Finding
	if strings.TrimSpace(cfg.Execution.ReferenceRPC) == "" || strings.TrimSpace(cfg.Execution.CandidateRPC) == "" {
		findings = append(findings, finding("execution.skipped", "Execution comparison skipped", report.SeverityWarn, "execution", "reference_rpc or candidate_rpc is missing", "Configure both execution.reference_rpc and execution.candidate_rpc to validate op-geth to op-reth migration readiness.", []string{DocOPGethDeprecation}, nil))
		return statuses, findings
	}

	ref := r.probeExecutionEndpoint(ctx, "reference", cfg.Execution.ReferenceRPC, cfg.Chain.ChainID)
	cand := r.probeExecutionEndpoint(ctx, "candidate", cfg.Execution.CandidateRPC, cfg.Chain.ChainID)
	statuses["reference"] = ref.status
	statuses["candidate"] = cand.status
	findings = append(findings, ref.findings...)
	findings = append(findings, cand.findings...)

	if ref.status.headOK && cand.status.headOK {
		commonHead := latestCommonHead(ref.status, cand.status)
		lag := headLag(ref.status, cand.status)
		evidence := map[string]string{
			"reference_head": fmt.Sprintf("%d", ref.status.head),
			"candidate_head": fmt.Sprintf("%d", cand.status.head),
			"lag_blocks":     fmt.Sprintf("%d", lag),
		}
		if lag > cfg.Execution.MaxHeadLagBlocks {
			findings = append(findings, finding("execution.head_lag", "Execution candidate lags reference", report.SeverityFail, "execution.candidate_rpc", fmt.Sprintf("candidate is %d blocks behind reference", lag), "Investigate candidate sync health before migration cutover; keep op-reth running alongside the existing node until heads converge.", []string{DocOPGethDeprecation}, evidence))
		} else {
			findings = append(findings, finding("execution.head_lag", "Execution candidate head is close to reference", report.SeverityOK, "execution", fmt.Sprintf("candidate lag is %d blocks", lag), "Continue comparing blocks and RPC behavior before migration cutover.", []string{DocOPGethDeprecation}, evidence))
		}
		findings = append(findings, r.compareExecutionBlocks(ctx, cfg, ref.status, cand.status, commonHead)...)
		findings = append(findings, r.compareExecutionRPCSurface(ctx, ref.status, cand.status, commonHead)...)
	}
	return statuses, findings
}

func latestCommonHead(ref, cand executionEndpointStatus) uint64 {
	if cand.head < ref.head {
		return cand.head
	}
	return ref.head
}

func headLag(ref, cand executionEndpointStatus) uint64 {
	if ref.head > cand.head {
		return ref.head - cand.head
	}
	return 0
}

type endpointProbe struct {
	status   executionEndpointStatus
	findings []report.Finding
}

func (r Runner) probeExecutionEndpoint(ctx context.Context, name, rawURL string, expectedChainID uint64) endpointProbe {
	client := rpc.NewClient(rawURL, r.Timeout)
	status := executionEndpointStatus{name: name, rpcURL: rawURL}
	target := "execution." + name + "_rpc"
	var findings []report.Finding

	version, err := client.ClientVersion(ctx)
	if err != nil {
		findings = append(findings, finding("execution."+name+".reachable", "Execution RPC is unreachable", report.SeverityFail, target, err.Error(), "Check HTTP reachability, authentication, and JSON-RPC availability for this endpoint.", []string{DocOPGethDeprecation}, map[string]string{"rpc": client.RedactedEndpoint()}))
		return endpointProbe{status: status, findings: findings}
	}
	status.reachable = true
	status.clientVersion = version
	findings = append(findings, finding("execution."+name+".reachable", "Execution RPC is reachable", report.SeverityOK, target, "web3_clientVersion succeeded", "Keep this endpoint read-only for diagnostics; opstack-doctor never sends transactions or uses private keys.", nil, map[string]string{"client_version": version, "rpc": client.RedactedEndpoint()}))
	findings = append(findings, classifyClientFinding(name, target, version))

	chainID, err := client.ChainID(ctx)
	if err != nil {
		findings = append(findings, finding("execution."+name+".chain_id", "Execution chain ID check failed", report.SeverityFail, target, err.Error(), "Verify the endpoint is an execution JSON-RPC endpoint for the configured chain.", []string{DocOPGethDeprecation}, nil))
	} else {
		status.chainOK = chainID == expectedChainID
		status.chainID = chainID
		sev := report.SeverityOK
		title := "Execution chain ID matches config"
		observed := fmt.Sprintf("chain_id=%d", chainID)
		rec := "No action needed."
		if expectedChainID != 0 && chainID != expectedChainID {
			sev = report.SeverityFail
			title = "Execution chain ID does not match config"
			rec = "Point this endpoint at the configured chain before comparing migration readiness."
		}
		findings = append(findings, finding("execution."+name+".chain_id", title, sev, target, observed, rec, nil, map[string]string{"expected_chain_id": fmt.Sprintf("%d", expectedChainID), "observed_chain_id": fmt.Sprintf("%d", chainID)}))
	}

	head, err := client.BlockNumber(ctx)
	if err != nil {
		findings = append(findings, finding("execution."+name+".head", "Execution head check failed", report.SeverityFail, target, err.Error(), "Verify eth_blockNumber is available and the node is synced enough to answer basic RPC calls.", nil, nil))
	} else {
		status.headOK = true
		status.head = head
		findings = append(findings, finding("execution."+name+".head", "Execution head is reachable", report.SeverityOK, target, fmt.Sprintf("head=%d", head), "Track this over time in monitoring; a single read proves reachability, not ongoing advancement.", nil, map[string]string{"head": fmt.Sprintf("%d", head)}))
	}
	return endpointProbe{status: status, findings: findings}
}

func classifyClientFinding(name, target, version string) report.Finding {
	lower := strings.ToLower(version)
	evidence := map[string]string{"client_version": version}
	switch {
	case strings.Contains(lower, "op-reth") || strings.Contains(lower, "reth"):
		sev := report.SeverityInfo
		title := "Execution client family observed"
		rec := "Confirm block hashes, state roots, and RPC outputs against the reference node before cutover."
		if name == "candidate" {
			sev = report.SeverityOK
			title = "Candidate appears to be op-reth/reth"
		}
		return finding("execution."+name+".client_family", title, sev, target, version, rec, []string{DocOPGethDeprecation}, evidence)
	case strings.Contains(lower, "op-geth"):
		sev := report.SeverityWarn
		title := "Reference is still op-geth"
		if name == "candidate" {
			sev = report.SeverityFail
			title = "Candidate is op-geth, not op-reth"
		}
		return finding("execution."+name+".client_family", title, sev, target, version, "Optimism says op-geth and op-program are supported through May 31, 2026; prepare migration to op-reth / cannon-kona and validate by running op-reth alongside the existing node.", []string{DocOPGethDeprecation}, evidence)
	default:
		return finding("execution."+name+".client_family", "Execution client family is unknown", report.SeverityInfo, target, version, "Review the clientVersion manually; opstack-doctor only uses conservative string heuristics for client family detection.", []string{DocOPGethDeprecation}, evidence)
	}
}

func (r Runner) compareExecutionBlocks(ctx context.Context, cfg config.Config, ref, cand executionEndpointStatus, commonHead uint64) []report.Finding {
	if !ref.headOK || !cand.headOK {
		return nil
	}
	compareBlocks := cfg.Execution.CompareBlocks
	if compareBlocks <= 0 {
		compareBlocks = 16
	}
	if uint64(compareBlocks) > commonHead+1 {
		compareBlocks = int(commonHead + 1)
	}
	refClient := rpc.NewClient(ref.rpcURL, r.Timeout)
	candClient := rpc.NewClient(cand.rpcURL, r.Timeout)

	var findings []report.Finding
	var compared int
	var missingFields []string
	for i := 0; i < compareBlocks; i++ {
		number := commonHead - uint64(i)
		refBlock, err := refClient.BlockByNumber(ctx, number)
		if err != nil {
			findings = append(findings, finding("execution.block_compare.fetch_reference", "Reference block fetch failed", report.SeverityFail, "execution.reference_rpc", err.Error(), "Ensure eth_getBlockByNumber is available on the reference endpoint.", []string{DocOPGethDeprecation}, map[string]string{"block_number": fmt.Sprintf("%d", number)}))
			continue
		}
		candBlock, err := candClient.BlockByNumber(ctx, number)
		if err != nil {
			findings = append(findings, finding("execution.block_compare.fetch_candidate", "Candidate block fetch failed", report.SeverityFail, "execution.candidate_rpc", err.Error(), "Ensure eth_getBlockByNumber is available on the candidate endpoint.", []string{DocOPGethDeprecation}, map[string]string{"block_number": fmt.Sprintf("%d", number)}))
			continue
		}
		compared++
		diffs, missing := CompareBlockFields(refBlock, candBlock)
		for _, field := range missing {
			missingFields = append(missingFields, fmt.Sprintf("%d:%s", number, field))
		}
		if len(diffs) > 0 {
			evidence := map[string]string{"block_number": fmt.Sprintf("%d", number)}
			for _, diff := range diffs {
				evidence["reference_"+diff.Field] = diff.Reference
				evidence["candidate_"+diff.Field] = diff.Candidate
			}
			findings = append(findings, finding("execution.block_compare.divergence", "Execution block divergence detected", report.SeverityFail, "execution", fmt.Sprintf("block %d differs across reference and candidate", number), "Do not migrate traffic until block hashes, state roots, transaction roots, and receipt roots match across the comparison window.", []string{DocOPGethDeprecation}, evidence))
		}
	}
	if len(missingFields) > 0 {
		sort.Strings(missingFields)
		if len(missingFields) > 12 {
			missingFields = append(missingFields[:12], "...")
		}
		findings = append(findings, finding("execution.block_compare.missing_fields", "Some block fields were missing", report.SeverityWarn, "execution", "one or more compared blocks lacked optional or expected fields", "Inspect endpoint compatibility; opstack-doctor skipped field equality checks when either side omitted a field.", []string{DocOPGethDeprecation}, map[string]string{"missing": strings.Join(missingFields, ",")}))
	}
	hasDivergence := false
	for _, f := range findings {
		if f.ID == "execution.block_compare.divergence" && f.Severity == report.SeverityFail {
			hasDivergence = true
			break
		}
	}
	if compared > 0 && !hasDivergence {
		findings = append(findings, finding("execution.block_compare.match", "Execution block comparison matched", report.SeverityOK, "execution", fmt.Sprintf("%d latest common blocks matched", compared), "Continue running op-reth alongside the existing node and expand comparison depth for higher confidence before migration.", []string{DocOPGethDeprecation}, map[string]string{"compared_blocks": fmt.Sprintf("%d", compared), "common_head": fmt.Sprintf("%d", commonHead)}))
	}
	return findings
}

func (r Runner) compareExecutionRPCSurface(ctx context.Context, ref, cand executionEndpointStatus, commonHead uint64) []report.Finding {
	refClient := rpc.NewClient(ref.rpcURL, r.Timeout)
	candClient := rpc.NewClient(cand.rpcURL, r.Timeout)
	var findings []report.Finding
	compared := 0

	refTxCount, err := refClient.BlockTransactionCountByNumber(ctx, commonHead)
	if err != nil {
		findings = append(findings, finding("execution.rpc_surface.fetch_reference", "Reference RPC surface check failed", report.SeverityFail, "execution.reference_rpc", err.Error(), "Ensure eth_getBlockTransactionCountByNumber is available on the reference endpoint.", []string{DocOPGethDeprecation}, map[string]string{"method": "eth_getBlockTransactionCountByNumber", "block_number": fmt.Sprintf("%d", commonHead)}))
	} else {
		candTxCount, err := candClient.BlockTransactionCountByNumber(ctx, commonHead)
		if err != nil {
			findings = append(findings, finding("execution.rpc_surface.fetch_candidate", "Candidate RPC surface check failed", report.SeverityFail, "execution.candidate_rpc", err.Error(), "Ensure eth_getBlockTransactionCountByNumber is available on the candidate endpoint.", []string{DocOPGethDeprecation}, map[string]string{"method": "eth_getBlockTransactionCountByNumber", "block_number": fmt.Sprintf("%d", commonHead)}))
		} else {
			compared++
			if refTxCount != candTxCount {
				findings = append(findings, finding("execution.rpc_surface.transaction_count_mismatch", "Execution RPC transaction count differs", report.SeverityFail, "execution", fmt.Sprintf("eth_getBlockTransactionCountByNumber differs at block %d", commonHead), "Do not migrate traffic until read-only RPC outputs match for deterministic block-derived methods.", []string{DocOPGethDeprecation}, map[string]string{"block_number": fmt.Sprintf("%d", commonHead), "reference_count": fmt.Sprintf("%d", refTxCount), "candidate_count": fmt.Sprintf("%d", candTxCount)}))
			}
		}
	}

	refBlock, err := refClient.BlockByNumber(ctx, commonHead)
	if err != nil {
		findings = append(findings, finding("execution.rpc_surface.fetch_reference", "Reference RPC surface check failed", report.SeverityFail, "execution.reference_rpc", err.Error(), "Ensure eth_getBlockByNumber remains available during RPC surface comparison.", []string{DocOPGethDeprecation}, map[string]string{"method": "eth_getBlockByNumber", "block_number": fmt.Sprintf("%d", commonHead)}))
	} else if rpc.StringValue(refBlock.Hash) == "" {
		findings = append(findings, finding("execution.rpc_surface.block_hash_missing", "Reference block hash missing", report.SeverityWarn, "execution.reference_rpc", fmt.Sprintf("block %d did not include a hash", commonHead), "Cannot run eth_getBlockByHash comparison without a reference block hash.", []string{DocOPGethDeprecation}, map[string]string{"block_number": fmt.Sprintf("%d", commonHead)}))
	} else {
		hash := rpc.StringValue(refBlock.Hash)
		refByHash, err := refClient.BlockByHash(ctx, hash)
		if err != nil {
			findings = append(findings, finding("execution.rpc_surface.fetch_reference", "Reference RPC surface check failed", report.SeverityFail, "execution.reference_rpc", err.Error(), "Ensure eth_getBlockByHash is available on the reference endpoint.", []string{DocOPGethDeprecation}, map[string]string{"method": "eth_getBlockByHash", "block_hash": hash}))
		} else {
			candByHash, err := candClient.BlockByHash(ctx, hash)
			if err != nil {
				findings = append(findings, finding("execution.rpc_surface.fetch_candidate", "Candidate RPC surface check failed", report.SeverityFail, "execution.candidate_rpc", err.Error(), "Ensure eth_getBlockByHash can retrieve canonical blocks by hash on the candidate endpoint.", []string{DocOPGethDeprecation}, map[string]string{"method": "eth_getBlockByHash", "block_hash": hash}))
			} else {
				compared++
				diffs, missing := CompareBlockFields(refByHash, candByHash)
				if len(missing) > 0 {
					findings = append(findings, finding("execution.rpc_surface.block_by_hash_missing_fields", "eth_getBlockByHash omitted compared fields", report.SeverityWarn, "execution", "one or more block-by-hash fields were missing", "Inspect endpoint compatibility; opstack-doctor skipped field equality checks when either side omitted a field.", []string{DocOPGethDeprecation}, map[string]string{"block_hash": hash, "missing": strings.Join(missing, ",")}))
				}
				if len(diffs) > 0 {
					evidence := map[string]string{"block_hash": hash}
					for _, diff := range diffs {
						evidence["reference_"+diff.Field] = diff.Reference
						evidence["candidate_"+diff.Field] = diff.Candidate
					}
					findings = append(findings, finding("execution.rpc_surface.block_by_hash_mismatch", "eth_getBlockByHash output differs", report.SeverityFail, "execution", "block-by-hash output differs across reference and candidate", "Do not migrate traffic until block-by-hash output agrees for the same canonical hash.", []string{DocOPGethDeprecation}, evidence))
				}
			}
		}
	}

	hasFailure := false
	for _, f := range findings {
		if f.Severity == report.SeverityFail {
			hasFailure = true
			break
		}
	}
	if compared > 0 && !hasFailure {
		findings = append(findings, finding("execution.rpc_surface.match", "Read-only execution RPC surface matched", report.SeverityOK, "execution", fmt.Sprintf("%d deterministic RPC surface checks matched", compared), "This is still a sample, not exhaustive RPC equivalence; increase coverage before high-risk migrations.", []string{DocOPGethDeprecation}, map[string]string{"block_number": fmt.Sprintf("%d", commonHead), "compared_methods": fmt.Sprintf("%d", compared)}))
	}
	return findings
}

func (r Runner) checkOPNodeMetrics(ctx context.Context, cfg config.Config) (map[string]nodeMetricsState, []report.Finding) {
	states := map[string]nodeMetricsState{}
	var findings []report.Finding
	for _, node := range cfg.OPNodes {
		target := "op_nodes." + node.Name + ".metrics"
		state := nodeMetricsState{node: node}
		if strings.TrimSpace(node.Metrics) == "" {
			findings = append(findings, finding("op_node."+node.Name+".metrics_endpoint", "op-node metrics endpoint is not configured", report.SeverityWarn, target, "metrics URL is empty", "Enable and configure op-node Prometheus metrics, commonly exposed at http://localhost:7300/metrics.", []string{DocMetrics}, nil))
			states[node.Name] = state
			continue
		}
		samples, err := r.fetchMetrics(ctx, node.Metrics)
		if err != nil {
			state.err = err
			findings = append(findings, finding("op_node."+node.Name+".metrics_fetch", "op-node metrics fetch failed", report.SeverityWarn, target, err.Error(), "Check the metrics URL, firewall rules, and whether op-node Prometheus metrics are enabled.", []string{DocMetrics}, map[string]string{"metrics": redact.URL(node.Metrics)}))
			states[node.Name] = state
			continue
		}
		state.samples = samples
		state.ok = true
		states[node.Name] = state
		findings = append(findings, finding("op_node."+node.Name+".metrics_fetch", "op-node metrics fetched", report.SeverityOK, target, fmt.Sprintf("%d metric samples parsed", len(samples)), "Use these metrics in Prometheus dashboards and alerts; source-tier metrics should be treated as a hard dependency.", []string{DocMetrics, DocLightNodes}, map[string]string{"metrics": redact.URL(node.Metrics)}))
		findings = append(findings, checkNodeMetricSamples(node, samples, cfg.Thresholds)...)
	}
	return states, findings
}

func checkNodeMetricSamples(node config.OPNodeConfig, samples []metrics.Sample, thresholds config.ThresholdsConfig) []report.Finding {
	var findings []report.Finding
	target := "op_nodes." + node.Name + ".metrics"

	up := metrics.Find(samples, "op_node_default_up")
	if len(up) == 0 {
		findings = append(findings, finding("op_node."+node.Name+".up_missing", "op-node up metric is missing", report.SeverityWarn, target, "op_node_default_up was not present", "Confirm the metrics endpoint belongs to op-node and exposes the default metric namespace.", []string{DocMetrics}, nil))
	} else if anyValueNot(up, 1) {
		findings = append(findings, finding("op_node."+node.Name+".up", "op-node reports not up", report.SeverityFail, target, "op_node_default_up is not 1 for every series", "Investigate op-node process health immediately.", []string{DocMetrics}, map[string]string{"series": formatSeries(up)}))
	} else {
		findings = append(findings, finding("op_node."+node.Name+".up", "op-node reports up", report.SeverityOK, target, "op_node_default_up=1", "No action needed.", []string{DocMetrics}, map[string]string{"series": formatSeries(up)}))
	}

	refs := metrics.Find(samples, "op_node_default_refs_number")
	if len(refs) == 0 {
		findings = append(findings, finding("op_node."+node.Name+".refs_missing", "op-node refs metric is missing", report.SeverityWarn, target, "op_node_default_refs_number was not present", "Expose refs metrics so safe, finalized, and unsafe heads can be monitored.", []string{DocMetrics}, nil))
	} else {
		findings = append(findings, finding("op_node."+node.Name+".refs", "op-node refs metric present", report.SeverityOK, target, "op_node_default_refs_number was present", "Review the series labels and wire safe/finalized/unsafe refs into dashboards.", []string{DocMetrics}, map[string]string{"series": formatSeries(refs)}))
		for _, refName := range []string{"safe", "finalized", "unsafe"} {
			if value, ok := refValue(samples, refName); ok {
				findings = append(findings, finding("op_node."+node.Name+".refs_"+refName, "op-node "+refName+" ref is parseable", report.SeverityOK, target, fmt.Sprintf("%s ref %.0f", refName, value), "Use parseable safe/finalized/unsafe refs to monitor sync progress and reorg behavior.", []string{DocMetrics}, nil))
			} else {
				findings = append(findings, finding("op_node."+node.Name+".refs_"+refName+"_missing", "op-node "+refName+" ref was not parseable", report.SeverityInfo, target, "no refs series label contained "+refName, "This is informational because op-node labels can vary; verify refs manually if your metrics use different labels.", []string{DocMetrics}, nil))
			}
		}
	}

	peers := append(metrics.Find(samples, "op_node_default_peer_count"), metrics.Find(samples, "op_node_default_p2p_peer_count")...)
	if len(peers) == 0 {
		findings = append(findings, finding("op_node."+node.Name+".peer_count_missing", "op-node peer count metric is missing", report.SeverityWarn, target, "peer count metric was not present", "Expose peer count metrics and alert when P2P connectivity falls below the configured threshold.", []string{DocMetrics}, nil))
	} else if min, _ := metrics.MinValue(peers); min < thresholds.MinPeerCount {
		findings = append(findings, finding("op_node."+node.Name+".peer_count", "op-node peer count is low", report.SeverityWarn, target, fmt.Sprintf("minimum peer count %.0f is below threshold %.0f", min, thresholds.MinPeerCount), "Investigate P2P connectivity, bootnodes, firewalling, and peer limits.", []string{DocMetrics}, map[string]string{"series": formatSeries(peers)}))
	} else {
		findings = append(findings, finding("op_node."+node.Name+".peer_count", "op-node peer count meets threshold", report.SeverityOK, target, "peer count is at or above threshold", "No action needed.", []string{DocMetrics}, map[string]string{"series": formatSeries(peers)}))
	}

	for _, metricName := range []string{"op_node_default_derivation_errors_total", "op_node_default_pipeline_resets_total"} {
		series := metrics.Find(samples, metricName)
		idSuffix := strings.TrimPrefix(metricName, "op_node_default_")
		if len(series) == 0 {
			findings = append(findings, finding("op_node."+node.Name+"."+idSuffix+"_missing", metricName+" is missing", report.SeverityWarn, target, metricName+" was not present", "Expose derivation and pipeline reset counters so operators can detect derivation instability.", []string{DocMetrics}, nil))
			continue
		}
		max, _ := metrics.MaxValue(series)
		if max > 0 {
			findings = append(findings, finding("op_node."+node.Name+"."+idSuffix, metricName+" is nonzero", report.SeverityWarn, target, fmt.Sprintf("max observed value %.0f", max), "Investigate derivation errors, L1 RPC reliability, and pipeline reset causes.", []string{DocMetrics}, map[string]string{"series": formatSeries(series)}))
		} else {
			findings = append(findings, finding("op_node."+node.Name+"."+idSuffix, metricName+" is zero", report.SeverityOK, target, "counter is zero", "No action needed.", []string{DocMetrics}, map[string]string{"series": formatSeries(series)}))
		}
	}

	latency := metrics.FindPrefix(samples, "op_node_default_rpc_client_request_duration_seconds")
	if len(latency) == 0 {
		findings = append(findings, finding("op_node."+node.Name+".rpc_latency_missing", "op-node RPC latency metric is missing", report.SeverityWarn, target, "op_node_default_rpc_client_request_duration_seconds was not present", "Expose RPC client latency metrics and alert on sustained L1/L2 RPC slowness.", []string{DocMetrics}, nil))
	} else {
		findings = append(findings, finding("op_node."+node.Name+".rpc_latency", "op-node RPC latency metric present", report.SeverityOK, target, "RPC client latency metric was present", "Use histogram or summary series to alert on sustained latency above threshold.", []string{DocMetrics}, map[string]string{"series": formatSeries(latency)}))
	}
	return findings
}

func (r Runner) checkTopology(ctx context.Context, cfg config.Config, metricStates map[string]nodeMetricsState) []report.Finding {
	var findings []report.Finding
	nodes := map[string]config.OPNodeConfig{}
	var sources []config.OPNodeConfig
	for _, node := range cfg.OPNodes {
		nodes[node.Name] = node
		if node.Role == "source" {
			sources = append(sources, node)
		}
	}
	switch len(sources) {
	case 0:
		findings = append(findings, finding("topology.source_tier", "No source op-node tier configured", report.SeverityWarn, "op_nodes", "zero source nodes configured", "Configure a small redundant source tier for L1 derivation, then have light and sequencer op-nodes follow it.", []string{DocLightNodes}, nil))
	case 1:
		findings = append(findings, finding("topology.source_tier", "Only one source op-node configured", report.SeverityWarn, "op_nodes", "one source node configured", "Add source-node redundancy; OP Labs treats the source tier as a hard production dependency.", []string{DocLightNodes}, map[string]string{"source": sources[0].Name}))
	default:
		findings = append(findings, finding("topology.source_tier", "Source op-node tier has redundancy", report.SeverityOK, "op_nodes", fmt.Sprintf("%d source nodes configured", len(sources)), "Keep source-tier dashboards and alerts separate from light-node capacity dashboards.", []string{DocLightNodes}, nil))
	}

	for _, node := range cfg.OPNodes {
		if node.Role != "light" && node.Role != "sequencer" {
			continue
		}
		target := "op_nodes." + node.Name
		if strings.TrimSpace(node.Follows) == "" {
			findings = append(findings, finding("topology."+node.Name+".follows", "Node does not declare a follow source", report.SeverityWarn, target, "follows is empty", "Set follows to the configured source op-node this node is intended to track; production nodes should use --l2.follow.source=<source op-node RPC>.", []string{DocLightNodes}, nil))
			continue
		}
		source, ok := nodes[node.Follows]
		if !ok || source.Role != "source" {
			findings = append(findings, finding("topology."+node.Name+".follows", "Node follows an invalid source", report.SeverityFail, target, "follows does not point to a configured source node", "Point follows at an op-node configured with role: source.", []string{DocLightNodes}, map[string]string{"follows": node.Follows}))
			continue
		}
		findings = append(findings, finding("topology."+node.Name+".follows", "Node declares a source follow target", report.SeverityOK, target, "follows="+node.Follows, "Validate the actual deployed op-node flag separately; this config expresses intended topology.", []string{DocLightNodes}, nil))
		findings = append(findings, r.compareTopologyRPCHeads(ctx, cfg, source, node)...)
		findings = append(findings, compareTopologyMetricSafeHeads(cfg, metricStates[source.Name], metricStates[node.Name])...)
	}
	return findings
}

func (r Runner) compareTopologyRPCHeads(ctx context.Context, cfg config.Config, source, node config.OPNodeConfig) []report.Finding {
	if source.RPC == "" || node.RPC == "" {
		return []report.Finding{finding("topology."+node.Name+".rpc_head", "Topology RPC head comparison skipped", report.SeverityWarn, "op_nodes."+node.Name+".rpc", "source or follower RPC URL is missing", "Configure op-node RPC endpoints if you want RPC-based head comparisons.", []string{DocLightNodes}, nil)}
	}
	sourceClient := rpc.NewClient(source.RPC, r.Timeout)
	nodeClient := rpc.NewClient(node.RPC, r.Timeout)
	sourceHead, err := sourceClient.BlockNumber(ctx)
	if err != nil {
		return []report.Finding{finding("topology."+node.Name+".rpc_head", "Source RPC head read failed", report.SeverityWarn, "op_nodes."+source.Name+".rpc", err.Error(), "Ensure the configured source RPC exposes a read-only block-number method for this MVP check, or rely on metrics safe-head comparison.", []string{DocLightNodes}, map[string]string{"source_rpc": sourceClient.RedactedEndpoint()})}
	}
	nodeHead, err := nodeClient.BlockNumber(ctx)
	if err != nil {
		return []report.Finding{finding("topology."+node.Name+".rpc_head", "Follower RPC head read failed", report.SeverityWarn, "op_nodes."+node.Name+".rpc", err.Error(), "Ensure the configured follower RPC exposes a read-only block-number method for this MVP check, or rely on metrics safe-head comparison.", []string{DocLightNodes}, map[string]string{"node_rpc": nodeClient.RedactedEndpoint()})}
	}
	lag := uint64(0)
	if sourceHead > nodeHead {
		lag = sourceHead - nodeHead
	}
	evidence := map[string]string{"source_head": fmt.Sprintf("%d", sourceHead), "node_head": fmt.Sprintf("%d", nodeHead), "lag_blocks": fmt.Sprintf("%d", lag), "source": source.Name}
	if lag > cfg.Thresholds.MaxSafeLagBlocks {
		return []report.Finding{finding("topology."+node.Name+".rpc_head", "Follower RPC head lags source", report.SeverityWarn, "op_nodes."+node.Name, fmt.Sprintf("%s is %d blocks behind %s", node.Name, lag, source.Name), "Investigate follow-source health, source availability, and follower sync state.", []string{DocLightNodes}, evidence)}
	}
	return []report.Finding{finding("topology."+node.Name+".rpc_head", "Follower RPC head tracks source", report.SeverityOK, "op_nodes."+node.Name, fmt.Sprintf("lag is %d blocks", lag), "No action needed.", []string{DocLightNodes}, evidence)}
}

func compareTopologyMetricSafeHeads(cfg config.Config, sourceState, nodeState nodeMetricsState) []report.Finding {
	target := "op_nodes." + nodeState.node.Name + ".metrics"
	sourceSafe, sourceOK := safeRef(sourceState.samples)
	nodeSafe, nodeOK := safeRef(nodeState.samples)
	if !sourceOK || !nodeOK {
		return []report.Finding{finding("topology."+nodeState.node.Name+".safe_head_metrics", "Safe-head metric comparison unavailable", report.SeverityInfo, target, "source or follower did not expose parseable safe refs", "Expose labeled op_node_default_refs_number series for safe refs to validate light-node tracking through metrics.", []string{DocMetrics, DocLightNodes}, map[string]string{"source": sourceState.node.Name})}
	}
	lag := float64(0)
	if sourceSafe > nodeSafe {
		lag = sourceSafe - nodeSafe
	}
	evidence := map[string]string{"source_safe": fmt.Sprintf("%.0f", sourceSafe), "node_safe": fmt.Sprintf("%.0f", nodeSafe), "lag_blocks": fmt.Sprintf("%.0f", lag), "source": sourceState.node.Name}
	if lag > float64(cfg.Thresholds.MaxSafeLagBlocks) {
		return []report.Finding{finding("topology."+nodeState.node.Name+".safe_head_metrics", "Follower safe head lags source", report.SeverityWarn, target, fmt.Sprintf("safe-head lag is %.0f blocks", lag), "Treat source-tier and follower-safe-head lag as production alerts.", []string{DocMetrics, DocLightNodes}, evidence)}
	}
	return []report.Finding{finding("topology."+nodeState.node.Name+".safe_head_metrics", "Follower safe head tracks source", report.SeverityOK, target, fmt.Sprintf("safe-head lag is %.0f blocks", lag), "No action needed.", []string{DocMetrics, DocLightNodes}, evidence)}
}

func (r Runner) checkProxyd(ctx context.Context, cfg config.Config) []report.Finding {
	if !cfg.Proxyd.Enabled {
		if specializedTopologyConfigured(cfg) {
			return []report.Finding{finding("proxyd.disabled", "proxyd checks are not configured", report.SeverityWarn, "proxyd", "proxyd.enabled=false", "For production source/light-node topology, put the deriver tier behind consensus-aware proxyd and configure proxyd checks here.", []string{DocLightNodes, DocProxyd}, nil)}
		}
		return []report.Finding{finding("proxyd.disabled", "proxyd checks are disabled", report.SeverityInfo, "proxyd", "proxyd.enabled=false", "Enable proxyd checks when production RPC routing depends on proxyd or another consensus-aware routing layer.", []string{DocProxyd}, nil)}
	}
	if len(cfg.Proxyd.Endpoints) == 0 {
		return []report.Finding{finding("proxyd.endpoints", "No proxyd endpoints configured", report.SeverityWarn, "proxyd.endpoints", "proxyd.enabled=true but endpoint list is empty", "Add deriver and/or edge proxyd endpoints so routing readiness can be checked.", []string{DocLightNodes, DocProxyd}, nil)}
	}

	nodes := make(map[string]config.OPNodeConfig, len(cfg.OPNodes))
	for _, node := range cfg.OPNodes {
		nodes[node.Name] = node
	}
	var findings []report.Finding
	for _, endpoint := range cfg.Proxyd.Endpoints {
		findings = append(findings, r.checkProxydEndpoint(ctx, cfg, endpoint, nodes)...)
	}
	return findings
}

func specializedTopologyConfigured(cfg config.Config) bool {
	hasSource := false
	hasFollower := false
	for _, node := range cfg.OPNodes {
		switch node.Role {
		case "source":
			hasSource = true
		case "light", "sequencer":
			hasFollower = true
		}
	}
	return hasSource && hasFollower
}

type proxydEndpointProbe struct {
	head   uint64
	headOK bool
}

func (r Runner) checkProxydEndpoint(ctx context.Context, cfg config.Config, endpoint config.ProxydEndpointConfig, nodes map[string]config.OPNodeConfig) []report.Finding {
	var findings []report.Finding
	target := proxydTarget(endpoint)

	findings = append(findings, checkProxydConsensusIntent(endpoint))
	probe, probeFindings := r.probeProxydRPC(ctx, cfg, endpoint)
	findings = append(findings, probeFindings...)
	findings = append(findings, r.checkProxydMetrics(ctx, endpoint, cfg.Thresholds)...)
	findings = append(findings, r.checkProxydBackends(ctx, cfg, endpoint, nodes, probe)...)
	if len(endpoint.ExpectedBackends) > 0 {
		findings = append(findings, finding("proxyd."+endpoint.Name+".backends", "proxyd expected backends are declared", report.SeverityOK, target, fmt.Sprintf("%d expected backends configured", len(endpoint.ExpectedBackends)), "Keep this list aligned with proxyd backend groups and service discovery.", []string{DocLightNodes, DocProxyd}, map[string]string{"backend_count": fmt.Sprintf("%d", len(endpoint.ExpectedBackends)), "role": endpoint.Role}))
	}
	return findings
}

func checkProxydConsensusIntent(endpoint config.ProxydEndpointConfig) report.Finding {
	target := proxydTarget(endpoint)
	evidence := map[string]string{"role": endpoint.Role, "consensus_aware": fmt.Sprintf("%t", endpoint.ConsensusAware)}
	switch {
	case endpoint.Role == "deriver" && endpoint.ConsensusAware:
		return finding("proxyd."+endpoint.Name+".consensus_aware", "Deriver proxyd is declared consensus aware", report.SeverityOK, target, "consensus_aware=true", "Validate the actual proxyd TOML separately; this check records intended routing behavior from doctor config.", []string{DocLightNodes, DocProxyd}, evidence)
	case endpoint.Role == "deriver":
		return finding("proxyd."+endpoint.Name+".consensus_aware", "Deriver proxyd is not declared consensus aware", report.SeverityWarn, target, "consensus_aware=false", "OP Labs recommends the deriver tier sit behind consensus-aware proxyd so downstream light nodes follow a stable source endpoint.", []string{DocLightNodes, DocProxyd}, evidence)
	case endpoint.ConsensusAware:
		return finding("proxyd."+endpoint.Name+".consensus_aware", "proxyd is declared consensus aware", report.SeverityOK, target, "consensus_aware=true", "Confirm the deployed proxyd config enables the matching routing strategy.", []string{DocProxyd}, evidence)
	default:
		return finding("proxyd."+endpoint.Name+".consensus_aware", "proxyd consensus awareness not declared", report.SeverityInfo, target, "consensus_aware=false", "This is informational for non-deriver endpoints; use consensus-aware routing where production RPC correctness depends on backend consensus.", []string{DocProxyd}, evidence)
	}
}

func (r Runner) probeProxydRPC(ctx context.Context, cfg config.Config, endpoint config.ProxydEndpointConfig) (proxydEndpointProbe, []report.Finding) {
	target := proxydTarget(endpoint)
	if strings.TrimSpace(endpoint.RPC) == "" {
		return proxydEndpointProbe{}, []report.Finding{finding("proxyd."+endpoint.Name+".rpc", "proxyd RPC endpoint is not configured", report.SeverityWarn, target+".rpc", "rpc URL is empty", "Configure the externally used proxyd RPC URL to validate routing reachability and head lag.", []string{DocProxyd}, nil)}
	}
	client := rpc.NewClient(endpoint.RPC, r.Timeout)
	var findings []report.Finding

	chainID, err := client.ChainID(ctx)
	if err != nil {
		findings = append(findings, finding("proxyd."+endpoint.Name+".rpc", "proxyd RPC is unreachable", report.SeverityFail, target+".rpc", err.Error(), "Check proxyd process health, routing config, backend availability, and network access.", []string{DocProxyd}, map[string]string{"rpc": client.RedactedEndpoint(), "role": endpoint.Role}))
		return proxydEndpointProbe{}, findings
	}
	if cfg.Chain.ChainID != 0 && chainID != cfg.Chain.ChainID {
		findings = append(findings, finding("proxyd."+endpoint.Name+".chain_id", "proxyd chain ID does not match config", report.SeverityFail, target+".rpc", fmt.Sprintf("chain_id=%d", chainID), "Point proxyd at backends for the configured chain before routing production traffic.", []string{DocProxyd}, map[string]string{"expected_chain_id": fmt.Sprintf("%d", cfg.Chain.ChainID), "observed_chain_id": fmt.Sprintf("%d", chainID), "rpc": client.RedactedEndpoint(), "role": endpoint.Role}))
	} else {
		findings = append(findings, finding("proxyd."+endpoint.Name+".chain_id", "proxyd chain ID matches config", report.SeverityOK, target+".rpc", fmt.Sprintf("chain_id=%d", chainID), "No action needed.", []string{DocProxyd}, map[string]string{"observed_chain_id": fmt.Sprintf("%d", chainID), "rpc": client.RedactedEndpoint(), "role": endpoint.Role}))
	}

	head, err := client.BlockNumber(ctx)
	if err != nil {
		findings = append(findings, finding("proxyd."+endpoint.Name+".head", "proxyd head read failed", report.SeverityFail, target+".rpc", err.Error(), "Verify proxyd can serve eth_blockNumber from healthy backends.", []string{DocProxyd}, map[string]string{"rpc": client.RedactedEndpoint(), "role": endpoint.Role}))
		return proxydEndpointProbe{}, findings
	}
	findings = append(findings, finding("proxyd."+endpoint.Name+".head", "proxyd head is reachable", report.SeverityOK, target+".rpc", fmt.Sprintf("head=%d", head), "Track this over time through opstack-doctor exported metrics or native proxyd metrics.", []string{DocProxyd}, map[string]string{"head": fmt.Sprintf("%d", head), "rpc": client.RedactedEndpoint(), "role": endpoint.Role}))
	return proxydEndpointProbe{head: head, headOK: true}, findings
}

func (r Runner) checkProxydMetrics(ctx context.Context, endpoint config.ProxydEndpointConfig, thresholds config.ThresholdsConfig) []report.Finding {
	target := proxydTarget(endpoint) + ".metrics"
	if strings.TrimSpace(endpoint.Metrics) == "" {
		return []report.Finding{finding("proxyd."+endpoint.Name+".metrics", "proxyd metrics endpoint is not configured", report.SeverityWarn, target, "metrics URL is empty", "Configure proxyd Prometheus metrics so request latency, error rates, backend health, and consensus routing can be observed.", []string{DocProxyd, DocMetrics}, nil)}
	}
	samples, err := r.fetchMetrics(ctx, endpoint.Metrics)
	if err != nil {
		return []report.Finding{finding("proxyd."+endpoint.Name+".metrics", "proxyd metrics fetch failed", report.SeverityWarn, target, err.Error(), "Check the proxyd metrics listener, scrape path, and network policy.", []string{DocProxyd, DocMetrics}, map[string]string{"metrics": redact.URL(endpoint.Metrics), "role": endpoint.Role})}
	}
	findings := []report.Finding{finding("proxyd."+endpoint.Name+".metrics", "proxyd metrics are reachable", report.SeverityOK, target, fmt.Sprintf("%d metric samples parsed", len(samples)), "Wire proxyd latency, error-rate, backend-health, and consensus-routing metrics into production dashboards.", []string{DocProxyd, DocMetrics}, map[string]string{"metrics": redact.URL(endpoint.Metrics), "samples": fmt.Sprintf("%d", len(samples)), "role": endpoint.Role})}
	proxydSeries := countMetricPrefix(samples, "proxyd")
	if proxydSeries == 0 {
		findings = append(findings, finding("proxyd."+endpoint.Name+".metrics_names", "proxyd metric names were not detected", report.SeverityInfo, target, "no parsed metric name started with proxyd", "This may be normal if metrics are renamed or relabeled; manually confirm the endpoint exposes proxyd backend health and routing metrics.", []string{DocProxyd, DocMetrics}, map[string]string{"samples": fmt.Sprintf("%d", len(samples)), "role": endpoint.Role}))
	} else {
		findings = append(findings, finding("proxyd."+endpoint.Name+".metrics_names", "proxyd metric names are present", report.SeverityOK, target, fmt.Sprintf("%d proxyd metric samples parsed", proxydSeries), "No action needed.", []string{DocProxyd, DocMetrics}, map[string]string{"proxyd_samples": fmt.Sprintf("%d", proxydSeries), "role": endpoint.Role}))
		findings = append(findings, checkProxydNativeMetrics(endpoint, samples, thresholds)...)
	}
	return findings
}

func checkProxydNativeMetrics(endpoint config.ProxydEndpointConfig, samples []metrics.Sample, thresholds config.ThresholdsConfig) []report.Finding {
	var findings []report.Finding
	target := proxydTarget(endpoint) + ".metrics"

	up := metrics.Find(samples, "proxyd_up")
	switch {
	case len(up) == 0:
		findings = append(findings, finding("proxyd."+endpoint.Name+".up_missing", "proxyd up metric is missing", report.SeverityWarn, target, "proxyd_up was not present", "Expose proxyd_up so Prometheus can distinguish scrape reachability from proxyd process health.", []string{DocProxyd, DocMetrics}, nil))
	case anyValueNot(up, 1):
		findings = append(findings, finding("proxyd."+endpoint.Name+".up", "proxyd reports not up", report.SeverityFail, target, "proxyd_up is not 1 for every series", "Investigate proxyd process health and routing availability immediately.", []string{DocProxyd, DocMetrics}, map[string]string{"series": formatSeries(up), "role": endpoint.Role}))
	default:
		findings = append(findings, finding("proxyd."+endpoint.Name+".up", "proxyd reports up", report.SeverityOK, target, "proxyd_up=1", "No action needed.", []string{DocProxyd, DocMetrics}, map[string]string{"series": formatSeries(up), "role": endpoint.Role}))
	}

	findings = append(findings, checkProxydBackendHealthMetrics(endpoint, samples)...)
	findings = append(findings, checkProxydConsensusMetrics(endpoint, samples)...)
	findings = append(findings, checkProxydErrorMetrics(endpoint, samples)...)
	findings = append(findings, checkProxydLatencyMetrics(endpoint, samples, thresholds)...)
	return findings
}

func checkProxydBackendHealthMetrics(endpoint config.ProxydEndpointConfig, samples []metrics.Sample) []report.Finding {
	target := proxydTarget(endpoint) + ".metrics"
	var findings []report.Finding

	probeHealthy := metrics.Find(samples, "proxyd_backend_probe_healthy")
	if len(probeHealthy) == 0 {
		findings = append(findings, finding("proxyd."+endpoint.Name+".backend_probe_healthy_missing", "proxyd backend probe health metric is missing", report.SeverityInfo, target, "proxyd_backend_probe_healthy was not present", "This can be normal when backend probes or newer proxyd metrics are not enabled; use this metric where available to show whether each backend probe is currently healthy.", []string{DocProxyd, DocMetrics}, nil))
	} else if bad := samplesByBackendValue(probeHealthy, endpoint.ExpectedBackends, func(v float64) bool { return math.Abs(v-1) > 0.000001 }); len(bad) > 0 {
		findings = append(findings, finding("proxyd."+endpoint.Name+".backend_probe_healthy", "proxyd backend probes report unhealthy backends", report.SeverityWarn, target, "one or more backend probe health series is not 1", "Investigate backend probe targets, source/light-node health, and proxyd backend removal behavior.", []string{DocProxyd, DocMetrics}, map[string]string{"series": formatSeries(bad), "role": endpoint.Role}))
	} else {
		findings = append(findings, finding("proxyd."+endpoint.Name+".backend_probe_healthy", "proxyd backend probes are healthy", report.SeverityOK, target, "backend probe health is 1", "No action needed.", []string{DocProxyd, DocMetrics}, map[string]string{"series": formatSeries(filterBackendSamples(probeHealthy, endpoint.ExpectedBackends)), "role": endpoint.Role}))
	}

	for _, spec := range []struct {
		name           string
		id             string
		titleWarn      string
		titleOK        string
		recommendation string
	}{
		{"proxyd_backend_degraded", "backend_degraded", "proxyd reports degraded backends", "proxyd reports no degraded backends", "Investigate backend latency, error rate, peer count, sync state, and latest-block lag."},
		{"proxyd_consensus_backend_banned", "backend_banned", "proxyd reports banned consensus backends", "proxyd reports no banned consensus backends", "Inspect consensus-aware routing state, backend divergence, latency, error rates, and ban thresholds."},
	} {
		series := metrics.Find(samples, spec.name)
		if len(series) == 0 {
			findings = append(findings, finding("proxyd."+endpoint.Name+"."+spec.id+"_missing", spec.name+" is missing", report.SeverityInfo, target, spec.name+" was not present", "This can be normal for older proxyd versions or non-consensus-aware endpoints; verify manually if this endpoint is production critical.", []string{DocProxyd, DocMetrics}, nil))
			continue
		}
		bad := samplesByBackendValue(series, endpoint.ExpectedBackends, func(v float64) bool { return v > 0 })
		if len(bad) > 0 {
			findings = append(findings, finding("proxyd."+endpoint.Name+"."+spec.id, spec.titleWarn, report.SeverityWarn, target, "one or more backend health gauge series is nonzero", spec.recommendation, []string{DocProxyd, DocMetrics}, map[string]string{"series": formatSeries(bad), "role": endpoint.Role}))
		} else {
			findings = append(findings, finding("proxyd."+endpoint.Name+"."+spec.id, spec.titleOK, report.SeverityOK, target, "all observed backend health gauges are zero", "No action needed.", []string{DocProxyd, DocMetrics}, map[string]string{"series": formatSeries(filterBackendSamples(series, endpoint.ExpectedBackends)), "role": endpoint.Role}))
		}
	}

	inSync := metrics.Find(samples, "proxyd_consensus_backend_in_sync")
	if len(inSync) > 0 {
		bad := samplesByBackendValue(inSync, endpoint.ExpectedBackends, func(v float64) bool { return math.Abs(v-1) > 0.000001 })
		if len(bad) > 0 {
			findings = append(findings, finding("proxyd."+endpoint.Name+".backend_in_sync", "proxyd reports consensus backends out of sync", report.SeverityWarn, target, "one or more proxyd_consensus_backend_in_sync series is not 1", "Investigate backend sync state before depending on this consensus-aware routing endpoint.", []string{DocProxyd, DocMetrics}, map[string]string{"series": formatSeries(bad), "role": endpoint.Role}))
		} else {
			findings = append(findings, finding("proxyd."+endpoint.Name+".backend_in_sync", "proxyd reports consensus backends in sync", report.SeverityOK, target, "observed consensus backend in-sync gauges are 1", "No action needed.", []string{DocProxyd, DocMetrics}, map[string]string{"series": formatSeries(filterBackendSamples(inSync, endpoint.ExpectedBackends)), "role": endpoint.Role}))
		}
	}

	peerCount := metrics.Find(samples, "proxyd_consensus_backend_peer_count")
	if endpoint.ConsensusAware && len(peerCount) == 0 {
		findings = append(findings, finding("proxyd."+endpoint.Name+".backend_peer_count_missing", "proxyd consensus peer count metric is missing", report.SeverityInfo, target, "proxyd_consensus_backend_peer_count was not present", "Consensus-aware proxyd can use backend peer count as a health input; confirm metric availability for production dashboards.", []string{DocProxyd, DocMetrics}, nil))
	} else if len(peerCount) > 0 {
		findings = append(findings, finding("proxyd."+endpoint.Name+".backend_peer_count", "proxyd consensus peer count metric present", report.SeverityOK, target, "proxyd_consensus_backend_peer_count was present", "Use this metric to correlate backend health decisions with P2P state.", []string{DocProxyd, DocMetrics}, map[string]string{"series": formatSeries(filterBackendSamples(peerCount, endpoint.ExpectedBackends)), "role": endpoint.Role}))
	}
	return findings
}

func checkProxydConsensusMetrics(endpoint config.ProxydEndpointConfig, samples []metrics.Sample) []report.Finding {
	target := proxydTarget(endpoint) + ".metrics"
	var findings []report.Finding
	latest, latestOK := maxMetricValue(samples, "proxyd_group_consensus_latest_block")
	safe, safeOK := maxMetricValue(samples, "proxyd_group_consensus_safe_block")
	finalized, finalizedOK := maxMetricValue(samples, "proxyd_group_consensus_finalized_block")

	if endpoint.ConsensusAware && (!latestOK || !safeOK || !finalizedOK) {
		findings = append(findings, finding("proxyd."+endpoint.Name+".consensus_blocks", "proxyd consensus block metrics are incomplete", report.SeverityWarn, target, "latest, safe, or finalized consensus block metric was missing", "Expose proxyd group consensus block gauges to prove consensus-aware routing is tracking latest, safe, and finalized heads.", []string{DocProxyd, DocMetrics}, map[string]string{"latest_present": fmt.Sprintf("%t", latestOK), "safe_present": fmt.Sprintf("%t", safeOK), "finalized_present": fmt.Sprintf("%t", finalizedOK), "role": endpoint.Role}))
	} else if latestOK && safeOK && finalizedOK {
		evidence := map[string]string{"latest": fmt.Sprintf("%.0f", latest), "safe": fmt.Sprintf("%.0f", safe), "finalized": fmt.Sprintf("%.0f", finalized), "role": endpoint.Role}
		if latest < safe || safe < finalized {
			findings = append(findings, finding("proxyd."+endpoint.Name+".consensus_blocks", "proxyd consensus block ordering looks invalid", report.SeverityWarn, target, "expected latest >= safe >= finalized", "Investigate consensus tracker state and backend consistency.", []string{DocProxyd, DocMetrics}, evidence))
		} else {
			findings = append(findings, finding("proxyd."+endpoint.Name+".consensus_blocks", "proxyd consensus block metrics are present", report.SeverityOK, target, "latest, safe, and finalized consensus gauges are parseable", "Use these metrics to alert on stale or divergent consensus-aware routing.", []string{DocProxyd, DocMetrics}, evidence))
		}
	}

	count, countOK := maxMetricValue(samples, "proxyd_group_consensus_count")
	total, totalOK := maxMetricValue(samples, "proxyd_group_consensus_total_count")
	if endpoint.ConsensusAware && !countOK {
		findings = append(findings, finding("proxyd."+endpoint.Name+".consensus_count_missing", "proxyd consensus group count metric is missing", report.SeverityWarn, target, "proxyd_group_consensus_count was not present", "Expose consensus group count so dashboards can detect zero serving consensus candidates.", []string{DocProxyd, DocMetrics}, nil))
	} else if countOK && count <= 0 {
		findings = append(findings, finding("proxyd."+endpoint.Name+".consensus_count", "proxyd has no serving consensus backends", report.SeverityWarn, target, "proxyd_group_consensus_count <= 0", "Investigate backend bans, probe health, sync state, and consensus-group filtering.", []string{DocProxyd, DocMetrics}, map[string]string{"consensus_count": fmt.Sprintf("%.0f", count), "role": endpoint.Role}))
	} else if countOK {
		evidence := map[string]string{"consensus_count": fmt.Sprintf("%.0f", count), "role": endpoint.Role}
		if totalOK {
			evidence["consensus_total_count"] = fmt.Sprintf("%.0f", total)
		}
		findings = append(findings, finding("proxyd."+endpoint.Name+".consensus_count", "proxyd has serving consensus backends", report.SeverityOK, target, fmt.Sprintf("consensus_count=%.0f", count), "No action needed.", []string{DocProxyd, DocMetrics}, evidence))
	}

	clLocalSafe := metrics.Find(samples, "proxyd_consensus_cl_group_local_safe_block")
	if endpoint.Role == "deriver" && endpoint.ConsensusAware && len(clLocalSafe) == 0 {
		findings = append(findings, finding("proxyd."+endpoint.Name+".cl_local_safe_missing", "proxyd CL local-safe consensus metric is missing", report.SeverityInfo, target, "proxyd_consensus_cl_group_local_safe_block was not present", "For op-node/source routing, use CL-specific proxyd metrics when available to track local-safe L2 consensus.", []string{DocProxyd, DocMetrics}, nil))
	} else if len(clLocalSafe) > 0 {
		max, _ := metrics.MaxValue(clLocalSafe)
		findings = append(findings, finding("proxyd."+endpoint.Name+".cl_local_safe", "proxyd CL local-safe consensus metric present", report.SeverityOK, target, fmt.Sprintf("max local-safe %.0f", max), "Use this metric for op-node source-tier routing dashboards.", []string{DocProxyd, DocMetrics}, map[string]string{"series": formatSeries(clLocalSafe), "role": endpoint.Role}))
	}
	return findings
}

func checkProxydErrorMetrics(endpoint config.ProxydEndpointConfig, samples []metrics.Sample) []report.Finding {
	target := proxydTarget(endpoint) + ".metrics"
	var findings []report.Finding
	for _, spec := range []struct {
		names          []string
		id             string
		titleWarn      string
		titleOK        string
		recommendation string
	}{
		{[]string{"proxyd_rpc_errors_total", "proxyd_rpc_special_errors_total", "proxyd_unserviceable_requests_total", "proxyd_too_many_request_errors_total", "proxyd_redis_errors_total", "proxyd_group_consensus_ha_error"}, "error_counters", "proxyd error counters are nonzero", "proxyd error counters are zero", "Review rates over a recent window; lifetime counters can remain nonzero after old incidents."},
		{[]string{"proxyd_consensus_cl_ban_not_healthy_total", "proxyd_consensus_cl_ban_unexpected_block_tags_total", "proxyd_consensus_cl_ban_interop_safe_gt_local_safe_total", "proxyd_consensus_cl_ban_output_root_mismatch_total", "proxyd_consensus_cl_ban_output_root_timeout_total", "proxyd_consensus_cl_output_root_disagreement_total", "proxyd_consensus_cl_no_pin_candidate_total"}, "cl_consensus_counters", "proxyd CL consensus counters are nonzero", "proxyd CL consensus counters are zero", "Investigate source-tier op-node health, output-root agreement, local-safe behavior, and pin-candidate selection."},
	} {
		series := findAnyMetric(samples, spec.names...)
		if len(series) == 0 {
			findings = append(findings, finding("proxyd."+endpoint.Name+"."+spec.id+"_missing", "proxyd "+spec.id+" metrics are missing", report.SeverityInfo, target, "none of "+strings.Join(spec.names, ", ")+" were present", "This may be normal depending on proxyd version and enabled features; confirm dashboards cover request errors and consensus bans where applicable.", []string{DocProxyd, DocMetrics}, nil))
			continue
		}
		max, _ := metrics.MaxValue(series)
		if max > 0 {
			findings = append(findings, finding("proxyd."+endpoint.Name+"."+spec.id, spec.titleWarn, report.SeverityWarn, target, fmt.Sprintf("max observed counter %.0f", max), spec.recommendation, []string{DocProxyd, DocMetrics}, map[string]string{"series": formatSeries(series), "role": endpoint.Role}))
		} else {
			findings = append(findings, finding("proxyd."+endpoint.Name+"."+spec.id, spec.titleOK, report.SeverityOK, target, "observed counters are zero", "No action needed.", []string{DocProxyd, DocMetrics}, map[string]string{"series": formatSeries(series), "role": endpoint.Role}))
		}
	}

	httpErrors := httpErrorSamples(samples)
	if len(httpErrors) > 0 {
		findings = append(findings, finding("proxyd."+endpoint.Name+".http_error_codes", "proxyd HTTP/backend error codes are nonzero", report.SeverityWarn, target, "5xx or 429 response-code counters were observed", "Review rate increases for backend/server errors and throttling; lifetime counters can remain nonzero after old incidents.", []string{DocProxyd, DocMetrics}, map[string]string{"series": formatSeries(httpErrors), "role": endpoint.Role}))
	}

	errorRate := metrics.Find(samples, "proxyd_backend_error_rate")
	if len(errorRate) == 0 {
		findings = append(findings, finding("proxyd."+endpoint.Name+".backend_error_rate_missing", "proxyd backend error-rate metric is missing", report.SeverityInfo, target, "proxyd_backend_error_rate was not present", "This can be normal for older proxyd versions; where available, scrape backend error rate to identify degraded backends before they affect routing.", []string{DocProxyd, DocMetrics}, nil))
	} else if bad := samplesByBackendValue(errorRate, endpoint.ExpectedBackends, func(v float64) bool { return v > 0 }); len(bad) > 0 {
		findings = append(findings, finding("proxyd."+endpoint.Name+".backend_error_rate", "proxyd backend error rate is nonzero", report.SeverityWarn, target, "one or more backend error-rate gauges is nonzero", "Identify the affected backend and method, then check backend logs, request latency, and upstream RPC health.", []string{DocProxyd, DocMetrics}, map[string]string{"series": formatSeries(bad), "role": endpoint.Role}))
	} else {
		findings = append(findings, finding("proxyd."+endpoint.Name+".backend_error_rate", "proxyd backend error rate is zero", report.SeverityOK, target, "observed backend error-rate gauges are zero", "No action needed.", []string{DocProxyd, DocMetrics}, map[string]string{"series": formatSeries(filterBackendSamples(errorRate, endpoint.ExpectedBackends)), "role": endpoint.Role}))
	}
	return findings
}

func checkProxydLatencyMetrics(endpoint config.ProxydEndpointConfig, samples []metrics.Sample, thresholds config.ThresholdsConfig) []report.Finding {
	target := proxydTarget(endpoint) + ".metrics"
	series := metrics.FindPrefix(samples, "proxyd_rpc_backend_request_duration_seconds")
	if len(series) == 0 {
		return []report.Finding{finding("proxyd."+endpoint.Name+".backend_latency_missing", "proxyd backend request latency metric is missing", report.SeverityInfo, target, "proxyd_rpc_backend_request_duration_seconds was not present", "This can be normal for older proxyd deployments; expose backend request duration metrics where available so operators can detect slow backends and correlate consensus bans with latency.", []string{DocProxyd, DocMetrics}, nil)}
	}
	quantiles := quantileSamples(series)
	max, ok := metrics.MaxValue(quantiles)
	if thresholds.MaxRPCLatencySeconds == 0 {
		thresholds.MaxRPCLatencySeconds = 2
	}
	if ok && max > thresholds.MaxRPCLatencySeconds {
		return []report.Finding{finding("proxyd."+endpoint.Name+".backend_latency", "proxyd backend request latency is high", report.SeverityWarn, target, fmt.Sprintf("max observed backend latency quantile %.3fs", max), "Investigate slow backend nodes, network latency, and proxyd backend timeout settings.", []string{DocProxyd, DocMetrics}, map[string]string{"series": formatSeries(quantiles), "threshold_seconds": fmt.Sprintf("%.3f", thresholds.MaxRPCLatencySeconds), "role": endpoint.Role})}
	}
	return []report.Finding{finding("proxyd."+endpoint.Name+".backend_latency", "proxyd backend request latency metric present", report.SeverityOK, target, "backend request duration series are present", "Alert on recent latency quantiles using deployment-specific SLOs.", []string{DocProxyd, DocMetrics}, map[string]string{"series": formatSeries(limitSamples(series, 8)), "role": endpoint.Role})}
}

func maxMetricValue(samples []metrics.Sample, name string) (float64, bool) {
	return metrics.MaxValue(metrics.Find(samples, name))
}

func findAnyMetric(samples []metrics.Sample, names ...string) []metrics.Sample {
	var out []metrics.Sample
	for _, name := range names {
		out = append(out, metrics.Find(samples, name)...)
	}
	return out
}

func samplesByBackendValue(samples []metrics.Sample, expectedBackends []string, match func(float64) bool) []metrics.Sample {
	candidates := filterBackendSamples(samples, expectedBackends)
	out := make([]metrics.Sample, 0, len(candidates))
	for _, sample := range candidates {
		if match(sample.Value) {
			out = append(out, sample)
		}
	}
	return out
}

func filterBackendSamples(samples []metrics.Sample, expectedBackends []string) []metrics.Sample {
	if len(expectedBackends) == 0 {
		return samples
	}
	expected := map[string]struct{}{}
	for _, backend := range expectedBackends {
		expected[backend] = struct{}{}
	}
	out := make([]metrics.Sample, 0, len(samples))
	for _, sample := range samples {
		if _, ok := expected[backendLabel(sample)]; ok {
			out = append(out, sample)
		}
	}
	if len(out) == 0 {
		return samples
	}
	return out
}

func backendLabel(sample metrics.Sample) string {
	return metrics.LabelValue(sample, "backend_name", "backend")
}

func httpErrorSamples(samples []metrics.Sample) []metrics.Sample {
	var out []metrics.Sample
	for _, sample := range findAnyMetric(samples, "proxyd_http_response_codes_total", "proxyd_rpc_backend_http_response_codes_total") {
		statusCode := metrics.LabelValue(sample, "status_code", "code")
		if sample.Value <= 0 {
			continue
		}
		if strings.HasPrefix(statusCode, "5") || statusCode == "429" {
			out = append(out, sample)
		}
	}
	return out
}

func quantileSamples(samples []metrics.Sample) []metrics.Sample {
	out := make([]metrics.Sample, 0, len(samples))
	for _, sample := range samples {
		if metrics.LabelValue(sample, "quantile") != "" {
			out = append(out, sample)
		}
	}
	return out
}

func limitSamples(samples []metrics.Sample, n int) []metrics.Sample {
	if len(samples) <= n {
		return samples
	}
	return samples[:n]
}

func findMetricSuffix(samples []metrics.Sample, prefix, suffix string) []metrics.Sample {
	var out []metrics.Sample
	for _, sample := range samples {
		if strings.HasPrefix(sample.Name, prefix+"_") && strings.HasSuffix(sample.Name, suffix) {
			out = append(out, sample)
		}
	}
	return out
}

func findMetricContains(samples []metrics.Sample, prefix string, parts ...string) []metrics.Sample {
	var out []metrics.Sample
	for _, sample := range samples {
		if !strings.HasPrefix(sample.Name, prefix+"_") {
			continue
		}
		for _, part := range parts {
			if strings.Contains(sample.Name, part) {
				out = append(out, sample)
				break
			}
		}
	}
	return out
}

func samplesIncludeLabelValue(samples []metrics.Sample, value string, keys ...string) bool {
	for _, sample := range samples {
		if metrics.LabelValue(sample, keys...) == value {
			return true
		}
	}
	return false
}

func maxRefSampleValue(samples []metrics.Sample, refType string) (float64, bool) {
	var matched []metrics.Sample
	for _, sample := range samples {
		if metrics.LabelValue(sample, "type", "ref", "ref_name") == refType {
			matched = append(matched, sample)
		}
	}
	return metrics.MaxValue(matched)
}

func messageStatusRiskSamples(samples []metrics.Sample) []metrics.Sample {
	var out []metrics.Sample
	riskTokens := []string{"invalid", "missing", "failed", "failure", "error", "unknown"}
	for _, sample := range samples {
		if sample.Value <= 0 {
			continue
		}
		status := strings.ToLower(metrics.LabelValue(sample, "status"))
		for _, token := range riskTokens {
			if strings.Contains(status, token) {
				out = append(out, sample)
				break
			}
		}
	}
	return out
}

func expectedInteropChains(cfg config.Config) []uint64 {
	seen := map[uint64]struct{}{}
	var out []uint64
	add := func(chainID uint64) {
		if chainID == 0 {
			return
		}
		if _, ok := seen[chainID]; ok {
			return
		}
		seen[chainID] = struct{}{}
		out = append(out, chainID)
	}
	add(cfg.Chain.ChainID)
	for _, dep := range cfg.Interop.Dependencies {
		add(dep.ChainID)
	}
	return out
}

func joinUint64s(values []uint64) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprintf("%d", value))
	}
	return strings.Join(parts, ",")
}

func (r Runner) checkProxydBackends(ctx context.Context, cfg config.Config, endpoint config.ProxydEndpointConfig, nodes map[string]config.OPNodeConfig, probe proxydEndpointProbe) []report.Finding {
	target := proxydTarget(endpoint)
	if len(endpoint.ExpectedBackends) == 0 {
		return []report.Finding{finding("proxyd."+endpoint.Name+".backends", "proxyd expected backends are not declared", report.SeverityWarn, target, "expected_backends is empty", "Declare the op-node backends this proxyd endpoint should route to so doctor can compare heads and topology intent.", []string{DocLightNodes, DocProxyd}, map[string]string{"role": endpoint.Role})}
	}

	var findings []report.Finding
	readableHeads := map[string]uint64{}
	roleCounts := map[string]int{}
	for _, backendName := range endpoint.ExpectedBackends {
		node, ok := nodes[backendName]
		if !ok {
			findings = append(findings, finding("proxyd."+endpoint.Name+".backend."+backendName, "proxyd backend is not configured as an op-node", report.SeverityFail, target, "unknown backend "+backendName, "Fix expected_backends so each name points at a configured op-node.", []string{DocLightNodes, DocProxyd}, map[string]string{"backend": backendName, "role": endpoint.Role}))
			continue
		}
		roleCounts[node.Role]++
		if strings.TrimSpace(node.RPC) == "" {
			findings = append(findings, finding("proxyd."+endpoint.Name+".backend."+node.Name+".head", "proxyd backend RPC is not configured", report.SeverityWarn, "op_nodes."+node.Name+".rpc", "rpc URL is empty", "Configure backend op-node RPC if you want doctor to compare proxyd routing head against this backend.", []string{DocLightNodes, DocProxyd}, map[string]string{"backend": node.Name, "backend_role": node.Role, "role": endpoint.Role}))
			continue
		}
		client := rpc.NewClient(node.RPC, r.Timeout)
		head, err := client.BlockNumber(ctx)
		if err != nil {
			findings = append(findings, finding("proxyd."+endpoint.Name+".backend."+node.Name+".head", "proxyd backend head read failed", report.SeverityWarn, "op_nodes."+node.Name+".rpc", err.Error(), "Check backend op-node RPC reachability; this does not prove proxyd is using the backend, only that the declared backend can be compared.", []string{DocLightNodes, DocProxyd}, map[string]string{"backend": node.Name, "backend_role": node.Role, "backend_rpc": client.RedactedEndpoint(), "role": endpoint.Role}))
			continue
		}
		readableHeads[node.Name] = head
		findings = append(findings, finding("proxyd."+endpoint.Name+".backend."+node.Name+".head", "proxyd backend head is reachable", report.SeverityOK, "op_nodes."+node.Name+".rpc", fmt.Sprintf("head=%d", head), "No action needed.", []string{DocLightNodes, DocProxyd}, map[string]string{"backend": node.Name, "backend_role": node.Role, "head": fmt.Sprintf("%d", head), "backend_rpc": client.RedactedEndpoint(), "role": endpoint.Role}))
	}

	findings = append(findings, checkProxydBackendRoles(endpoint, roleCounts)...)
	if probe.headOK {
		findings = append(findings, compareProxydBackendHeads(cfg, endpoint, probe.head, readableHeads)...)
	}
	return findings
}

func checkProxydBackendRoles(endpoint config.ProxydEndpointConfig, roleCounts map[string]int) []report.Finding {
	target := proxydTarget(endpoint)
	switch endpoint.Role {
	case "deriver":
		sources := roleCounts["source"]
		if sources >= 2 {
			return []report.Finding{finding("proxyd."+endpoint.Name+".backend_roles", "Deriver proxyd fronts redundant source nodes", report.SeverityOK, target, fmt.Sprintf("%d source backends declared", sources), "Keep this deriver tier sized for derivation throughput and failover.", []string{DocLightNodes, DocProxyd}, map[string]string{"source_backends": fmt.Sprintf("%d", sources), "role": endpoint.Role})}
		}
		return []report.Finding{finding("proxyd."+endpoint.Name+".backend_roles", "Deriver proxyd lacks source redundancy", report.SeverityWarn, target, fmt.Sprintf("%d source backends declared", sources), "Add at least two source-node backends behind the deriver-tier proxyd.", []string{DocLightNodes, DocProxyd}, map[string]string{"source_backends": fmt.Sprintf("%d", sources), "role": endpoint.Role})}
	case "edge":
		followers := roleCounts["light"] + roleCounts["sequencer"]
		if followers > 0 {
			return []report.Finding{finding("proxyd."+endpoint.Name+".backend_roles", "Edge proxyd fronts follower tier nodes", report.SeverityOK, target, fmt.Sprintf("%d light/sequencer backends declared", followers), "Scale this tier with read demand while keeping derivation concentrated on source nodes.", []string{DocLightNodes, DocProxyd}, map[string]string{"follower_backends": fmt.Sprintf("%d", followers), "role": endpoint.Role})}
		}
		return []report.Finding{finding("proxyd."+endpoint.Name+".backend_roles", "Edge proxyd does not declare follower backends", report.SeverityInfo, target, "no light or sequencer backends declared", "The OP Labs reference architecture puts a light-node tier in front of the deriver tier for production read capacity.", []string{DocLightNodes, DocProxyd}, map[string]string{"role": endpoint.Role})}
	default:
		return nil
	}
}

func compareProxydBackendHeads(cfg config.Config, endpoint config.ProxydEndpointConfig, proxydHead uint64, backendHeads map[string]uint64) []report.Finding {
	target := proxydTarget(endpoint)
	if len(backendHeads) == 0 {
		return []report.Finding{finding("proxyd."+endpoint.Name+".head_lag", "proxyd head comparison unavailable", report.SeverityInfo, target, "no declared backend heads were readable", "Make backend RPCs reachable to compare proxyd routing head against the declared backend tier.", []string{DocLightNodes, DocProxyd}, map[string]string{"proxyd_head": fmt.Sprintf("%d", proxydHead), "role": endpoint.Role})}
	}
	var backendMax uint64
	for _, head := range backendHeads {
		if head > backendMax {
			backendMax = head
		}
	}
	lag := uint64(0)
	if backendMax > proxydHead {
		lag = backendMax - proxydHead
	}
	evidence := map[string]string{
		"proxyd_head":      fmt.Sprintf("%d", proxydHead),
		"backend_max_head": fmt.Sprintf("%d", backendMax),
		"lag_blocks":       fmt.Sprintf("%d", lag),
		"backend_count":    fmt.Sprintf("%d", len(backendHeads)),
		"role":             endpoint.Role,
	}
	if lag > cfg.Thresholds.MaxSafeLagBlocks {
		return []report.Finding{finding("proxyd."+endpoint.Name+".head_lag", "proxyd head lags declared backends", report.SeverityWarn, target, fmt.Sprintf("proxyd is %d blocks behind the latest readable backend", lag), "Investigate proxyd backend health, routing strategy, and consensus tracker state before relying on this routing endpoint.", []string{DocLightNodes, DocProxyd}, evidence)}
	}
	return []report.Finding{finding("proxyd."+endpoint.Name+".head_lag", "proxyd head tracks declared backends", report.SeverityOK, target, fmt.Sprintf("lag is %d blocks", lag), "No action needed.", []string{DocLightNodes, DocProxyd}, evidence)}
}

func countMetricPrefix(samples []metrics.Sample, prefix string) int {
	count := 0
	for _, sample := range samples {
		if sample.Name == prefix || strings.HasPrefix(sample.Name, prefix+"_") {
			count++
		}
	}
	return count
}

func proxydTarget(endpoint config.ProxydEndpointConfig) string {
	if endpoint.Name == "" {
		return "proxyd.endpoints"
	}
	return "proxyd." + endpoint.Name
}

func (r Runner) checkInterop(ctx context.Context, cfg config.Config) []report.Finding {
	if !cfg.Interop.Enabled {
		return []report.Finding{finding("interop.disabled", "Interop readiness checks disabled", report.SeverityInfo, "interop", "interop.enabled=false", "Enable interop checks when this node participates in an interop dependency set.", []string{DocInterop}, nil)}
	}
	findings := []report.Finding{
		finding("interop.scope", "Interop check scope is basic", report.SeverityInfo, "interop", "checking dependency endpoint reachability, chain ID, block number, and metrics only", "This MVP does not validate cross-chain message execution, op-supervisor behavior, or full interop protocol correctness.", []string{DocInterop, DocOPSupervisor, DocInteropMonitor}, nil),
	}
	findings = append(findings, r.checkInteropSupervisorMetrics(ctx, cfg)...)
	findings = append(findings, r.checkInteropMonitorMetrics(ctx, cfg)...)
	if len(cfg.Interop.Dependencies) == 0 {
		findings = append(findings, finding("interop.dependencies", "No interop dependencies configured", report.SeverityWarn, "interop.dependencies", "dependency set is empty", "Configure every chain in the dependency set so operators can observe and follow each dependency.", []string{DocInterop}, nil))
		return findings
	}
	for _, dep := range cfg.Interop.Dependencies {
		target := "interop.dependencies." + dep.Name
		if dep.RPC == "" {
			findings = append(findings, finding("interop."+dep.Name+".rpc", "Dependency RPC is missing", report.SeverityWarn, target+".rpc", "rpc URL is empty", "Configure a read-only RPC endpoint for every dependency chain.", []string{DocInterop}, nil))
			continue
		}
		client := rpc.NewClient(dep.RPC, r.Timeout)
		chainID, err := client.ChainID(ctx)
		if err != nil {
			findings = append(findings, finding("interop."+dep.Name+".rpc", "Dependency RPC is unreachable", report.SeverityWarn, target+".rpc", err.Error(), "Nodes participating in interop need a way to follow every chain in the dependency set; fix RPC reachability for this dependency.", []string{DocInterop}, map[string]string{"rpc": client.RedactedEndpoint()}))
		} else if chainID != dep.ChainID {
			findings = append(findings, finding("interop."+dep.Name+".chain_id", "Dependency chain ID mismatch", report.SeverityWarn, target+".rpc", fmt.Sprintf("observed chain_id=%d", chainID), "Point this dependency at the configured chain before relying on interop readiness.", []string{DocInterop}, map[string]string{"expected_chain_id": fmt.Sprintf("%d", dep.ChainID), "observed_chain_id": fmt.Sprintf("%d", chainID)}))
		} else {
			findings = append(findings, finding("interop."+dep.Name+".chain_id", "Dependency chain ID matches", report.SeverityOK, target+".rpc", fmt.Sprintf("chain_id=%d", chainID), "No action needed.", []string{DocInterop}, map[string]string{"rpc": client.RedactedEndpoint()}))
		}
		head, err := client.BlockNumber(ctx)
		if err != nil {
			findings = append(findings, finding("interop."+dep.Name+".head", "Dependency block number read failed", report.SeverityWarn, target+".rpc", err.Error(), "Verify eth_blockNumber is available; a single run checks reachability, while Prometheus should check ongoing advancement.", []string{DocInterop}, nil))
		} else {
			findings = append(findings, finding("interop."+dep.Name+".head", "Dependency block number is reachable", report.SeverityOK, target+".rpc", fmt.Sprintf("head=%d", head), "Monitor this over time to prove the dependency is advancing.", []string{DocInterop}, map[string]string{"head": fmt.Sprintf("%d", head)}))
		}
		if dep.Metrics == "" {
			findings = append(findings, finding("interop."+dep.Name+".metrics", "Dependency metrics endpoint is absent", report.SeverityWarn, target+".metrics", "metrics URL is empty", "Configure dependency-chain metrics so dependency-set health is observable.", []string{DocInterop, DocMetrics}, nil))
			continue
		}
		samples, err := r.fetchMetrics(ctx, dep.Metrics)
		if err != nil {
			findings = append(findings, finding("interop."+dep.Name+".metrics", "Dependency metrics fetch failed", report.SeverityWarn, target+".metrics", err.Error(), "Fix metrics reachability for this dependency chain.", []string{DocInterop, DocMetrics}, map[string]string{"metrics": redact.URL(dep.Metrics)}))
		} else {
			findings = append(findings, finding("interop."+dep.Name+".metrics", "Dependency metrics are reachable", report.SeverityOK, target+".metrics", fmt.Sprintf("%d metric samples parsed", len(samples)), "Add dependency-set dashboards and alerts before enabling production interop flows.", []string{DocInterop, DocMetrics}, map[string]string{"metrics": redact.URL(dep.Metrics)}))
		}
	}
	return findings
}

func (r Runner) checkInteropSupervisorMetrics(ctx context.Context, cfg config.Config) []report.Finding {
	target := "interop.supervisor.metrics"
	if strings.TrimSpace(cfg.Interop.Supervisor.Metrics) == "" {
		return []report.Finding{finding("interop.supervisor.metrics_endpoint", "op-supervisor metrics endpoint is not configured", report.SeverityInfo, target, "metrics URL is empty", "Configure op-supervisor Prometheus metrics when this operator runs op-supervisor; this check is optional because interop rollout patterns are still evolving.", []string{DocInterop, DocOPSupervisor, DocMetrics}, nil)}
	}
	samples, err := r.fetchMetrics(ctx, cfg.Interop.Supervisor.Metrics)
	if err != nil {
		return []report.Finding{finding("interop.supervisor.metrics_fetch", "op-supervisor metrics fetch failed", report.SeverityWarn, target, err.Error(), "Check the op-supervisor metrics listener, scrape path, and network policy.", []string{DocOPSupervisor, DocMetrics}, map[string]string{"metrics": redact.URL(cfg.Interop.Supervisor.Metrics)})}
	}
	findings := []report.Finding{finding("interop.supervisor.metrics_fetch", "op-supervisor metrics fetched", report.SeverityOK, target, fmt.Sprintf("%d metric samples parsed", len(samples)), "Use these metrics to observe interop safety heads and supervisor health.", []string{DocOPSupervisor, DocMetrics}, map[string]string{"metrics": redact.URL(cfg.Interop.Supervisor.Metrics)})}
	supervisorSeries := countMetricPrefix(samples, "op_supervisor")
	if supervisorSeries == 0 {
		findings = append(findings, finding("interop.supervisor.metrics_names", "op-supervisor metric names were not detected", report.SeverityWarn, target, "no parsed metric name started with op_supervisor", "Confirm the endpoint belongs to op-supervisor and that metrics are enabled.", []string{DocOPSupervisor, DocMetrics}, map[string]string{"samples": fmt.Sprintf("%d", len(samples))}))
		return findings
	}
	findings = append(findings, finding("interop.supervisor.metrics_names", "op-supervisor metric names are present", report.SeverityOK, target, fmt.Sprintf("%d op-supervisor metric samples parsed", supervisorSeries), "No action needed.", []string{DocOPSupervisor, DocMetrics}, map[string]string{"op_supervisor_samples": fmt.Sprintf("%d", supervisorSeries)}))
	findings = append(findings, checkSupervisorMetricSamples(cfg, samples)...)
	return findings
}

func checkSupervisorMetricSamples(cfg config.Config, samples []metrics.Sample) []report.Finding {
	target := "interop.supervisor.metrics"
	var findings []report.Finding

	up := findMetricSuffix(samples, "op_supervisor", "_up")
	switch {
	case len(up) == 0:
		findings = append(findings, finding("interop.supervisor.up_missing", "op-supervisor up metric is missing", report.SeverityWarn, target, "op_supervisor_*_up was not present", "Expose the op-supervisor up metric so scrape reachability can be distinguished from process readiness.", []string{DocOPSupervisor, DocMetrics}, nil))
	case anyValueNot(up, 1):
		findings = append(findings, finding("interop.supervisor.up", "op-supervisor reports not up", report.SeverityFail, target, "op_supervisor_*_up is not 1 for every series", "Investigate op-supervisor process health and metrics wiring before depending on interop safety signals.", []string{DocOPSupervisor, DocMetrics}, map[string]string{"series": formatSeries(up)}))
	default:
		findings = append(findings, finding("interop.supervisor.up", "op-supervisor reports up", report.SeverityOK, target, "op_supervisor_*_up=1", "No action needed.", []string{DocOPSupervisor, DocMetrics}, map[string]string{"series": formatSeries(up)}))
	}

	info := findMetricSuffix(samples, "op_supervisor", "_info")
	if len(info) == 0 {
		findings = append(findings, finding("interop.supervisor.info_missing", "op-supervisor info metric is missing", report.SeverityInfo, target, "op_supervisor_*_info was not present", "Expose the info pseudo-metric where available to identify deployed versions in dashboards.", []string{DocOPSupervisor, DocMetrics}, nil))
	} else {
		findings = append(findings, finding("interop.supervisor.info", "op-supervisor info metric present", report.SeverityOK, target, "op_supervisor_*_info was present", "Use this to correlate interop metrics with deployed versions.", []string{DocOPSupervisor, DocMetrics}, map[string]string{"series": formatSeries(limitSamples(info, 4))}))
	}

	refs := findMetricSuffix(samples, "op_supervisor", "_refs_number")
	if len(refs) == 0 {
		findings = append(findings, finding("interop.supervisor.refs_missing", "op-supervisor refs metric is missing", report.SeverityWarn, target, "op_supervisor_*_refs_number was not present", "Expose supervisor refs to observe local/cross unsafe and safe heads for every dependency-set chain.", []string{DocInterop, DocOPSupervisor, DocMetrics}, nil))
	} else {
		findings = append(findings, finding("interop.supervisor.refs", "op-supervisor refs metric present", report.SeverityOK, target, "op_supervisor_*_refs_number was present", "Track local and cross safety heads per chain.", []string{DocInterop, DocOPSupervisor, DocMetrics}, map[string]string{"series": formatSeries(limitSamples(refs, 8))}))
		findings = append(findings, checkSupervisorExpectedChains(cfg, refs)...)
		findings = append(findings, checkSupervisorRefTypes(refs)...)
	}

	accessFailures := findMetricSuffix(samples, "op_supervisor", "_access_list_verify_failure")
	if len(accessFailures) == 0 {
		findings = append(findings, finding("interop.supervisor.access_list_verify_failure_missing", "op-supervisor access-list failure metric is missing", report.SeverityInfo, target, "op_supervisor_*_access_list_verify_failure was not present", "This metric is expected only when access-list verification warning metrics are enabled; use it to alert on failed message-access checks.", []string{DocInterop, DocOPSupervisor, DocMetrics}, nil))
	} else if max, _ := metrics.MaxValue(accessFailures); max > 0 {
		findings = append(findings, finding("interop.supervisor.access_list_verify_failure", "op-supervisor access-list verification failures observed", report.SeverityWarn, target, fmt.Sprintf("max observed counter %.0f", max), "Investigate cross-chain message access-list verification failures before enabling or expanding interop traffic.", []string{DocInterop, DocOPSupervisor, DocMetrics}, map[string]string{"series": formatSeries(accessFailures)}))
	} else {
		findings = append(findings, finding("interop.supervisor.access_list_verify_failure", "op-supervisor access-list verification failures are zero", report.SeverityOK, target, "observed counter is zero", "No action needed.", []string{DocInterop, DocOPSupervisor, DocMetrics}, map[string]string{"series": formatSeries(accessFailures)}))
	}

	dbEntries := findMetricSuffix(samples, "op_supervisor", "_logdb_entries_current")
	if len(dbEntries) == 0 {
		findings = append(findings, finding("interop.supervisor.logdb_entries_missing", "op-supervisor log DB entry metric is missing", report.SeverityInfo, target, "op_supervisor_*_logdb_entries_current was not present", "Track log DB entries where available to confirm supervisor indexing state per chain and DB kind.", []string{DocOPSupervisor, DocMetrics}, nil))
	} else {
		findings = append(findings, finding("interop.supervisor.logdb_entries", "op-supervisor log DB entry metric present", report.SeverityOK, target, "op_supervisor_*_logdb_entries_current was present", "Use this to detect stalled or empty indexing databases.", []string{DocOPSupervisor, DocMetrics}, map[string]string{"series": formatSeries(limitSamples(dbEntries, 8))}))
	}

	rpcMetrics := findMetricContains(samples, "op_supervisor", "_rpc_client_", "_rpc_server_")
	if len(rpcMetrics) == 0 {
		findings = append(findings, finding("interop.supervisor.rpc_metrics_missing", "op-supervisor RPC metrics are missing", report.SeverityInfo, target, "no op_supervisor RPC client/server metrics were present", "Expose RPC metrics where available to monitor L1/L2 RPC and supervisor API health.", []string{DocOPSupervisor, DocMetrics}, nil))
	} else {
		findings = append(findings, finding("interop.supervisor.rpc_metrics", "op-supervisor RPC metrics present", report.SeverityOK, target, "op-supervisor RPC client/server metrics were present", "Alert on RPC error and latency rates using deployment-specific labels.", []string{DocOPSupervisor, DocMetrics}, map[string]string{"series": formatSeries(limitSamples(rpcMetrics, 8))}))
	}
	return findings
}

func checkSupervisorExpectedChains(cfg config.Config, refs []metrics.Sample) []report.Finding {
	target := "interop.supervisor.metrics"
	chains := cfg.Interop.Supervisor.ExpectedChains
	if len(chains) == 0 {
		chains = expectedInteropChains(cfg)
	}
	if len(chains) == 0 {
		return []report.Finding{finding("interop.supervisor.expected_chains", "op-supervisor expected chains are not configured", report.SeverityInfo, target, "no expected chain IDs available", "Set interop.supervisor.expected_chains or configure interop dependencies so doctor can confirm supervisor refs cover the dependency set.", []string{DocInterop, DocOPSupervisor}, nil)}
	}
	var missing []string
	for _, chainID := range chains {
		if !samplesIncludeLabelValue(refs, fmt.Sprintf("%d", chainID), "chain", "chain_id") {
			missing = append(missing, fmt.Sprintf("%d", chainID))
		}
	}
	if len(missing) > 0 {
		return []report.Finding{finding("interop.supervisor.expected_chains", "op-supervisor refs do not cover every expected chain", report.SeverityWarn, target, "missing refs for chain IDs "+strings.Join(missing, ", "), "Confirm the supervisor dependency set and L2 RPC/indexing configuration include every chain this operator expects to follow.", []string{DocInterop, DocOPSupervisor}, map[string]string{"missing_chain_ids": strings.Join(missing, ","), "expected_chain_ids": joinUint64s(chains)})}
	}
	return []report.Finding{finding("interop.supervisor.expected_chains", "op-supervisor refs cover expected chains", report.SeverityOK, target, fmt.Sprintf("%d expected chains observed", len(chains)), "No action needed.", []string{DocInterop, DocOPSupervisor}, map[string]string{"expected_chain_ids": joinUint64s(chains)})}
}

func checkSupervisorRefTypes(refs []metrics.Sample) []report.Finding {
	target := "interop.supervisor.metrics"
	required := []string{"local_unsafe", "local_safe", "cross_unsafe", "cross_safe"}
	missing := make([]string, 0, len(required))
	for _, refType := range required {
		if _, ok := maxRefSampleValue(refs, refType); !ok {
			missing = append(missing, refType)
		}
	}
	if len(missing) > 0 {
		return []report.Finding{finding("interop.supervisor.ref_types", "op-supervisor refs are missing expected safety types", report.SeverityInfo, target, "missing ref types "+strings.Join(missing, ", "), "This can be normal before all safety levels have advanced; dashboards should eventually track local/cross unsafe and safe heads.", []string{DocInterop, DocOPSupervisor, DocMetrics}, map[string]string{"missing_ref_types": strings.Join(missing, ",")})}
	}
	localUnsafe, _ := maxRefSampleValue(refs, "local_unsafe")
	crossUnsafe, _ := maxRefSampleValue(refs, "cross_unsafe")
	localSafe, _ := maxRefSampleValue(refs, "local_safe")
	crossSafe, _ := maxRefSampleValue(refs, "cross_safe")
	evidence := map[string]string{
		"local_unsafe": fmt.Sprintf("%.0f", localUnsafe),
		"cross_unsafe": fmt.Sprintf("%.0f", crossUnsafe),
		"local_safe":   fmt.Sprintf("%.0f", localSafe),
		"cross_safe":   fmt.Sprintf("%.0f", crossSafe),
	}
	if crossUnsafe > localUnsafe || crossSafe > localSafe {
		return []report.Finding{finding("interop.supervisor.ref_types", "op-supervisor safety head ordering looks invalid", report.SeverityWarn, target, "cross safety heads exceed corresponding local heads", "Investigate supervisor indexing state and dependency-set configuration; cross heads should not exceed corresponding local heads.", []string{DocInterop, DocOPSupervisor, DocMetrics}, evidence)}
	}
	return []report.Finding{finding("interop.supervisor.ref_types", "op-supervisor safety refs are parseable", report.SeverityOK, target, "local/cross unsafe and safe refs were observed", "No action needed.", []string{DocInterop, DocOPSupervisor, DocMetrics}, evidence)}
}

func (r Runner) checkInteropMonitorMetrics(ctx context.Context, cfg config.Config) []report.Finding {
	target := "interop.monitor.metrics"
	if strings.TrimSpace(cfg.Interop.Monitor.Metrics) == "" {
		return []report.Finding{finding("interop.monitor.metrics_endpoint", "op-interop-mon metrics endpoint is not configured", report.SeverityInfo, target, "metrics URL is empty", "Configure op-interop-mon metrics if this operator runs the optional interop monitor for executing-message observability.", []string{DocInterop, DocInteropMonitor, DocMetrics}, nil)}
	}
	samples, err := r.fetchMetrics(ctx, cfg.Interop.Monitor.Metrics)
	if err != nil {
		return []report.Finding{finding("interop.monitor.metrics_fetch", "op-interop-mon metrics fetch failed", report.SeverityWarn, target, err.Error(), "Check the op-interop-mon metrics listener, scrape path, and network policy.", []string{DocInteropMonitor, DocMetrics}, map[string]string{"metrics": redact.URL(cfg.Interop.Monitor.Metrics)})}
	}
	findings := []report.Finding{finding("interop.monitor.metrics_fetch", "op-interop-mon metrics fetched", report.SeverityOK, target, fmt.Sprintf("%d metric samples parsed", len(samples)), "Use these metrics to alert on interop message status and monitor health.", []string{DocInteropMonitor, DocMetrics}, map[string]string{"metrics": redact.URL(cfg.Interop.Monitor.Metrics)})}
	monitorSeries := countMetricPrefix(samples, "op_interop_mon")
	if monitorSeries == 0 {
		findings = append(findings, finding("interop.monitor.metrics_names", "op-interop-mon metric names were not detected", report.SeverityWarn, target, "no parsed metric name started with op_interop_mon", "Confirm the endpoint belongs to op-interop-mon and that metrics are enabled.", []string{DocInteropMonitor, DocMetrics}, map[string]string{"samples": fmt.Sprintf("%d", len(samples))}))
		return findings
	}
	findings = append(findings, finding("interop.monitor.metrics_names", "op-interop-mon metric names are present", report.SeverityOK, target, fmt.Sprintf("%d op-interop-mon metric samples parsed", monitorSeries), "No action needed.", []string{DocInteropMonitor, DocMetrics}, map[string]string{"op_interop_mon_samples": fmt.Sprintf("%d", monitorSeries)}))
	findings = append(findings, checkInteropMonitorMetricSamples(samples)...)
	return findings
}

func checkInteropMonitorMetricSamples(samples []metrics.Sample) []report.Finding {
	target := "interop.monitor.metrics"
	var findings []report.Finding

	up := findMetricSuffix(samples, "op_interop_mon", "_up")
	switch {
	case len(up) == 0:
		findings = append(findings, finding("interop.monitor.up_missing", "op-interop-mon up metric is missing", report.SeverityWarn, target, "op_interop_mon_*_up was not present", "Expose the op-interop-mon up metric so scrape reachability can be distinguished from process readiness.", []string{DocInteropMonitor, DocMetrics}, nil))
	case anyValueNot(up, 1):
		findings = append(findings, finding("interop.monitor.up", "op-interop-mon reports not up", report.SeverityFail, target, "op_interop_mon_*_up is not 1 for every series", "Investigate op-interop-mon process health before depending on executing-message observability.", []string{DocInteropMonitor, DocMetrics}, map[string]string{"series": formatSeries(up)}))
	default:
		findings = append(findings, finding("interop.monitor.up", "op-interop-mon reports up", report.SeverityOK, target, "op_interop_mon_*_up=1", "No action needed.", []string{DocInteropMonitor, DocMetrics}, map[string]string{"series": formatSeries(up)}))
	}

	messageStatus := findMetricSuffix(samples, "op_interop_mon", "_message_status")
	if len(messageStatus) == 0 {
		findings = append(findings, finding("interop.monitor.message_status_missing", "op-interop-mon message status metric is missing", report.SeverityWarn, target, "op_interop_mon_*_message_status was not present", "Expose message status gauges to alert on invalid, missing, or unknown executing messages.", []string{DocInterop, DocInteropMonitor, DocMetrics}, nil))
	} else if bad := messageStatusRiskSamples(messageStatus); len(bad) > 0 {
		findings = append(findings, finding("interop.monitor.message_status", "op-interop-mon message status reports risky messages", report.SeverityWarn, target, "invalid, missing, failed, error, or unknown message statuses were nonzero", "Investigate executing messages and dependency-chain RPC health before expanding interop traffic.", []string{DocInterop, DocInteropMonitor, DocMetrics}, map[string]string{"series": formatSeries(bad)}))
	} else {
		findings = append(findings, finding("interop.monitor.message_status", "op-interop-mon message status metrics are present", report.SeverityOK, target, "no risky message status series were nonzero", "No action needed.", []string{DocInterop, DocInteropMonitor, DocMetrics}, map[string]string{"series": formatSeries(limitSamples(messageStatus, 8))}))
	}

	terminalChanges := findMetricSuffix(samples, "op_interop_mon", "_terminal_status_changes")
	if len(terminalChanges) == 0 {
		findings = append(findings, finding("interop.monitor.terminal_status_changes_missing", "op-interop-mon terminal status change metric is missing", report.SeverityInfo, target, "op_interop_mon_*_terminal_status_changes was not present", "Track terminal status changes where available to detect valid/invalid flips.", []string{DocInteropMonitor, DocMetrics}, nil))
	} else if max, _ := metrics.MaxValue(terminalChanges); max > 0 {
		findings = append(findings, finding("interop.monitor.terminal_status_changes", "op-interop-mon terminal status changes observed", report.SeverityWarn, target, fmt.Sprintf("max observed value %.0f", max), "Investigate message status transitions; terminal valid/invalid flips are high-signal interop incidents.", []string{DocInterop, DocInteropMonitor, DocMetrics}, map[string]string{"series": formatSeries(terminalChanges)}))
	} else {
		findings = append(findings, finding("interop.monitor.terminal_status_changes", "op-interop-mon terminal status changes are zero", report.SeverityOK, target, "observed value is zero", "No action needed.", []string{DocInteropMonitor, DocMetrics}, map[string]string{"series": formatSeries(terminalChanges)}))
	}

	blockRanges := append(findMetricSuffix(samples, "op_interop_mon", "_executing_block_range"), findMetricSuffix(samples, "op_interop_mon", "_initiating_block_range")...)
	if len(blockRanges) == 0 {
		findings = append(findings, finding("interop.monitor.block_ranges_missing", "op-interop-mon block range metrics are missing", report.SeverityInfo, target, "executing/initiating block range metrics were not present", "Track executing and initiating block ranges where available to understand monitor coverage windows.", []string{DocInteropMonitor, DocMetrics}, nil))
	} else {
		findings = append(findings, finding("interop.monitor.block_ranges", "op-interop-mon block range metrics present", report.SeverityOK, target, "executing or initiating block range metrics were present", "Use these gauges to confirm the monitor is scanning the expected chain ranges.", []string{DocInteropMonitor, DocMetrics}, map[string]string{"series": formatSeries(limitSamples(blockRanges, 8))}))
	}
	return findings
}

func (r Runner) fetchMetrics(ctx context.Context, rawURL string) ([]metrics.Sample, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build metrics request for %s: %s", redact.URL(rawURL), redact.String(err.Error(), rawURL))
	}
	client := &http.Client{Timeout: r.Timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("metrics request to %s failed: %s", redact.URL(rawURL), redact.String(err.Error(), rawURL))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("metrics request to %s returned HTTP %d: %s", redact.URL(rawURL), resp.StatusCode, redact.String(string(msg), rawURL))
	}
	samples, err := metrics.ParseText(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parse metrics from %s: %w", redact.URL(rawURL), err)
	}
	return samples, nil
}

func anyValueNot(samples []metrics.Sample, expected float64) bool {
	for _, sample := range samples {
		if math.Abs(sample.Value-expected) > 0.000001 {
			return true
		}
	}
	return false
}

func safeRef(samples []metrics.Sample) (float64, bool) {
	return refValue(samples, "safe")
}

func refValue(samples []metrics.Sample, name string) (float64, bool) {
	var values []metrics.Sample
	for _, sample := range metrics.Find(samples, "op_node_default_refs_number") {
		if sampleLooksLikeRef(sample, name) {
			values = append(values, sample)
		}
	}
	return metrics.MaxValue(values)
}

func sampleLooksLikeRef(sample metrics.Sample, name string) bool {
	for _, value := range sample.Labels {
		if containsLabelToken(strings.ToLower(value), name) {
			return true
		}
	}
	return false
}

func containsLabelToken(value, token string) bool {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	})
	for _, part := range parts {
		if part == token {
			return true
		}
	}
	return value == token
}

func formatSeries(samples []metrics.Sample) string {
	if len(samples) == 0 {
		return ""
	}
	parts := make([]string, 0, len(samples))
	for _, sample := range samples {
		parts = append(parts, fmt.Sprintf("%s%s=%s", sample.Name, formatLabels(sample.Labels), formatFloat(sample.Value)))
	}
	return strings.Join(parts, "; ")
}

func formatLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+labels[key])
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func formatFloat(v float64) string {
	if math.Abs(v-math.Round(v)) < 0.000001 {
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%.6g", v)
}

func finding(id, title string, severity report.Severity, target, observed, recommendation string, docs []string, evidence map[string]string) report.Finding {
	return report.Finding{
		ID:             id,
		Title:          title,
		Severity:       severity,
		Target:         target,
		Observed:       observed,
		Recommendation: recommendation,
		Docs:           docs,
		Evidence:       evidence,
	}
}
