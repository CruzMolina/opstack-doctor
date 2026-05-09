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
		lag := uint64(0)
		if ref.status.head > cand.status.head {
			lag = ref.status.head - cand.status.head
		}
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
		findings = append(findings, r.compareExecutionBlocks(ctx, cfg, ref.status, cand.status)...)
	}
	return statuses, findings
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

func (r Runner) compareExecutionBlocks(ctx context.Context, cfg config.Config, ref, cand executionEndpointStatus) []report.Finding {
	if !ref.headOK || !cand.headOK {
		return nil
	}
	commonHead := ref.head
	if cand.head < commonHead {
		commonHead = cand.head
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

func (r Runner) checkInterop(ctx context.Context, cfg config.Config) []report.Finding {
	if !cfg.Interop.Enabled {
		return []report.Finding{finding("interop.disabled", "Interop readiness checks disabled", report.SeverityInfo, "interop", "interop.enabled=false", "Enable interop checks when this node participates in an interop dependency set.", []string{DocInterop}, nil)}
	}
	findings := []report.Finding{
		finding("interop.scope", "Interop check scope is basic", report.SeverityInfo, "interop", "checking dependency endpoint reachability, chain ID, block number, and metrics only", "This MVP does not validate cross-chain message execution, op-supervisor behavior, or full interop protocol correctness.", []string{DocInterop}, nil),
	}
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
		if strings.Contains(strings.ToLower(value), name) {
			return true
		}
	}
	return false
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
