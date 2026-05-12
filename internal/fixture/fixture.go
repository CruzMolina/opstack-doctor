package fixture

import (
	"fmt"
	"strings"

	"opstack-doctor/internal/checks"
	"opstack-doctor/internal/report"
)

const (
	ScenarioHealthy = "healthy"
	ScenarioWarn    = "warn"
	ScenarioFail    = "fail"
)

type Report struct {
	Name     string
	Chain    string
	Findings []report.Finding
}

func Names() []string {
	return []string{ScenarioHealthy, ScenarioWarn, ScenarioFail}
}

func Get(name string) (Report, error) {
	switch normalize(name) {
	case ScenarioHealthy:
		return Report{Name: ScenarioHealthy, Chain: "op-mainnet-fixture", Findings: clone(healthyFindings())}, nil
	case ScenarioWarn:
		return Report{Name: ScenarioWarn, Chain: "op-mainnet-fixture", Findings: clone(warnFindings())}, nil
	case ScenarioFail:
		return Report{Name: ScenarioFail, Chain: "op-mainnet-fixture", Findings: clone(failFindings())}, nil
	default:
		return Report{}, fmt.Errorf("unknown fixture scenario %q: expected healthy, warn, or fail", name)
	}
}

func normalize(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func clone(findings []report.Finding) []report.Finding {
	out := make([]report.Finding, len(findings))
	for i, finding := range findings {
		out[i] = finding
		if len(finding.Docs) > 0 {
			out[i].Docs = append([]string(nil), finding.Docs...)
		}
		if len(finding.Evidence) > 0 {
			out[i].Evidence = make(map[string]string, len(finding.Evidence))
			for key, value := range finding.Evidence {
				out[i].Evidence[key] = value
			}
		}
	}
	return out
}

func healthyFindings() []report.Finding {
	return []report.Finding{
		finding("config.valid", "Configuration is valid", report.SeverityOK, "config", "fixture topology includes redundant sources, followers, proxyd, and interop dependency intent", "Use this as a deterministic example report; live validation still requires opstack-doctor check against operator-owned endpoints.", nil, nil),
		finding("execution.candidate.client_family", "Execution candidate is op-reth/reth", report.SeverityOK, "execution.candidate_rpc", "clientVersion=op-reth/v1.4.2 fixture", "Continue running the candidate beside the reference and comparing hashes, state roots, and RPC outputs before migration cutover.", []string{checks.DocOPGethDeprecation}, map[string]string{"client_version": "op-reth/v1.4.2 fixture"}),
		finding("execution.head_lag", "Execution candidate head is close to reference", report.SeverityOK, "execution", "candidate lag is 1 blocks", "Keep monitoring candidate lag until cutover; this fixture represents a healthy steady state.", []string{checks.DocOPGethDeprecation}, map[string]string{"candidate_head": "17000009", "lag_blocks": "1", "reference_head": "17000010"}),
		finding("execution.block_compare.match", "Execution blocks match", report.SeverityOK, "execution", "compared 16 common blocks with matching hashes and roots", "Keep this comparison in pre-cutover automation so op-reth divergence is caught before serving traffic.", []string{checks.DocOPGethDeprecation}, map[string]string{"common_head": "17000009", "compared_blocks": "16"}),
		finding("execution.rpc_surface.match", "Execution RPC samples match", report.SeverityOK, "execution", "sampled read-only RPC outputs matched between reference and candidate", "Expand sampled methods cautiously as operator confidence and endpoint compatibility requirements grow.", []string{checks.DocOPGethDeprecation}, map[string]string{"sampled_methods": "eth_getBlockByNumber,eth_getBlockByHash,eth_getBlockTransactionCountByNumber"}),
		finding("op_node.source-1.up", "op-node reports up", report.SeverityOK, "op_nodes.source-1", "op_node_default_up=1", "Keep source node availability as a hard dependency in dashboards and alerting.", []string{checks.DocMetrics, checks.DocLightNodes}, map[string]string{"value": "1"}),
		finding("op_node.source-1.peer_count", "op-node peer count is above threshold", report.SeverityOK, "op_nodes.source-1", "peer count 8 >= threshold 1", "Keep P2P peer count alerting enabled for source nodes.", []string{checks.DocMetrics}, map[string]string{"peer_count": "8", "threshold": "1"}),
		finding("topology.light-1.rpc_head", "Follower RPC head tracks source", report.SeverityOK, "op_nodes.light-1", "light-1 is 1 blocks behind source-1", "Use source nodes as the dependency tier for light and sequencer op-nodes; keep follower lag alerting in place.", []string{checks.DocLightNodes}, map[string]string{"follower_head": "17000008", "lag_blocks": "1", "source": "source-1", "source_head": "17000009", "threshold": "20"}),
		finding("topology.light-1.safe_head_metrics", "Follower safe head tracks source", report.SeverityOK, "op_nodes.light-1", "safe head lag is 2 blocks", "Treat source safe-head freshness as a dependency for follower health.", []string{checks.DocLightNodes, checks.DocMetrics}, map[string]string{"follower_safe_head": "16999998", "lag_blocks": "2", "source": "source-1", "source_safe_head": "17000000", "threshold": "20"}),
		finding("proxyd.deriver.consensus_aware", "Deriver proxyd is consensus-aware", report.SeverityOK, "proxyd.deriver", "consensus-aware routing intent declared with 2 source backends", "Keep consensus-aware proxyd in front of production consumers that need protection from divergent backends.", []string{checks.DocProxyd, checks.DocLightNodes}, map[string]string{"backend_count": "2", "consensus_aware": "true"}),
		finding("interop.scope", "Interop fixture scope", report.SeverityInfo, "interop", "basic dependency readiness only; no cross-chain message validation is implied", "Use this fixture to understand output shape. Live interop readiness still requires following every dependency chain and protocol-specific validation.", []string{checks.DocInterop}, nil),
		finding("interop.base.reachable", "Interop dependency RPC is reachable", report.SeverityOK, "interop.dependencies.base", "chain_id=8453 head=24000000", "Keep dependency-set RPC and metrics endpoints observable before enabling deeper interop validation.", []string{checks.DocInterop, checks.DocMetrics}, map[string]string{"chain_id": "8453", "head": "24000000"}),
	}
}

func warnFindings() []report.Finding {
	return []report.Finding{
		finding("config.valid", "Configuration is valid", report.SeverityOK, "config", "fixture config parses, but operational warnings remain", "Use warnings as follow-up work before treating the fleet as production-ready.", nil, nil),
		finding("execution.reference.client_family", "Execution reference is op-geth", report.SeverityWarn, "execution.reference_rpc", "clientVersion=op-geth/v1.101.4 fixture", "op-geth and op-program support ends May 31, 2026. Keep op-geth as a temporary reference only while validating an op-reth candidate.", []string{checks.DocOPGethDeprecation}, map[string]string{"client_version": "op-geth/v1.101.4 fixture"}),
		finding("execution.candidate.client_family", "Execution candidate is op-reth/reth", report.SeverityOK, "execution.candidate_rpc", "clientVersion=op-reth/v1.4.2 fixture", "Continue validating candidate behavior against the reference until cutover.", []string{checks.DocOPGethDeprecation}, map[string]string{"client_version": "op-reth/v1.4.2 fixture"}),
		finding("execution.head_lag", "Execution candidate head is close to reference", report.SeverityOK, "execution", "candidate lag is 2 blocks", "Keep monitoring lag while block and RPC comparisons continue.", []string{checks.DocOPGethDeprecation}, map[string]string{"candidate_head": "17000008", "lag_blocks": "2", "reference_head": "17000010"}),
		finding("execution.block_compare.match", "Execution blocks match", report.SeverityOK, "execution", "compared 16 common blocks with matching hashes and roots", "Continue comparing common blocks during migration rehearsal.", []string{checks.DocOPGethDeprecation}, map[string]string{"common_head": "17000008", "compared_blocks": "16"}),
		finding("topology.source_redundancy", "Only one source op-node is configured", report.SeverityWarn, "op_nodes", "source_count=1", "Add source-node redundancy before many light or sequencer nodes depend on the source tier.", []string{checks.DocLightNodes}, map[string]string{"source_count": "1"}),
		finding("op_node.source-1.peer_count", "op-node peer count is below threshold", report.SeverityWarn, "op_nodes.source-1", "peer count 0 < threshold 1", "Investigate P2P connectivity and peer discovery; source nodes should not silently lose peer coverage.", []string{checks.DocMetrics}, map[string]string{"peer_count": "0", "threshold": "1"}),
		finding("op_node.source-1.derivation_errors_total", "Derivation errors observed", report.SeverityWarn, "op_nodes.source-1", "op_node_default_derivation_errors_total=3", "Investigate derivation errors and correlate with L1 RPC, resets, and source-node load.", []string{checks.DocMetrics}, map[string]string{"errors_total": "3"}),
		finding("topology.light-1.safe_head_metrics", "Follower safe head lags source", report.SeverityWarn, "op_nodes.light-1", "safe head lag is 34 blocks, above threshold 20", "Check follow-source reachability, source-node health, and follower safe-chain tracking.", []string{checks.DocLightNodes, checks.DocMetrics}, map[string]string{"follower_safe_head": "16999966", "lag_blocks": "34", "source": "source-1", "source_safe_head": "17000000", "threshold": "20"}),
		finding("proxyd.deriver.consensus_aware", "Deriver proxyd consensus awareness is not declared", report.SeverityWarn, "proxyd.deriver", "consensus_aware=false", "Use consensus-aware proxyd for production deriver routing when source-node backends can disagree.", []string{checks.DocProxyd, checks.DocLightNodes}, map[string]string{"consensus_aware": "false"}),
		finding("interop.scope", "Interop fixture scope", report.SeverityInfo, "interop", "basic dependency readiness only; no cross-chain message validation is implied", "Use this as a dependency observability warning example, not as proof of protocol readiness.", []string{checks.DocInterop}, nil),
		finding("interop.base.metrics", "Interop dependency metrics are absent", report.SeverityWarn, "interop.dependencies.base", "metrics endpoint not configured or unreachable", "Add metrics for each dependency chain so dependency-set health is visible before interop workloads rely on it.", []string{checks.DocInterop, checks.DocMetrics}, nil),
	}
}

func failFindings() []report.Finding {
	return []report.Finding{
		finding("config.valid", "Configuration is valid", report.SeverityOK, "config", "fixture config parses, but live readiness failures are represented", "Fix fail findings before migration or topology cutover.", nil, nil),
		finding("execution.candidate.client_family", "Execution candidate is op-geth", report.SeverityFail, "execution.candidate_rpc", "clientVersion=op-geth/v1.101.4 fixture", "Candidate migration endpoints should be op-reth/reth. op-geth and op-program support ends May 31, 2026.", []string{checks.DocOPGethDeprecation}, map[string]string{"client_version": "op-geth/v1.101.4 fixture"}),
		finding("execution.head_lag", "Execution candidate lags reference", report.SeverityFail, "execution.candidate_rpc", "candidate is 12 blocks behind reference", "Investigate candidate sync health before migration cutover; keep op-reth running alongside the existing node until heads converge.", []string{checks.DocOPGethDeprecation}, map[string]string{"candidate_head": "16999998", "lag_blocks": "12", "reference_head": "17000010"}),
		finding("execution.block_compare.divergence", "Execution block comparison diverged", report.SeverityFail, "execution", "block 16999998 field stateRoot differs", "Do not cut over traffic until block hash, state root, transaction root, and receipt root comparisons match across the sampled window.", []string{checks.DocOPGethDeprecation}, map[string]string{"block_number": "16999998", "candidate": "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "field": "stateRoot", "reference": "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}),
		finding("execution.rpc_surface.block_by_hash", "Execution RPC sample mismatch", report.SeverityFail, "execution", "eth_getBlockByHash returned different output for the sampled common head", "Treat RPC output mismatch as a cutover blocker until the candidate behavior is understood.", []string{checks.DocOPGethDeprecation}, map[string]string{"method": "eth_getBlockByHash"}),
		finding("op_node.source-1.up", "op-node reports down", report.SeverityFail, "op_nodes.source-1", "op_node_default_up=0", "Restore source-node health before allowing light or sequencer op-nodes to depend on it.", []string{checks.DocMetrics, checks.DocLightNodes}, map[string]string{"value": "0"}),
		finding("topology.light-1.rpc_head", "Follower RPC head lags source", report.SeverityWarn, "op_nodes.light-1", "light-1 is 45 blocks behind source-1", "Check follow-source connectivity and source-node availability before using this follower for production traffic.", []string{checks.DocLightNodes}, map[string]string{"follower_head": "16999955", "lag_blocks": "45", "source": "source-1", "source_head": "17000000", "threshold": "20"}),
		finding("proxyd.deriver.rpc", "Deriver proxyd RPC is unreachable", report.SeverityFail, "proxyd.deriver", "HTTP request failed in fixture", "Restore proxyd routing before using this endpoint as a production dependency for derivation or edge traffic.", []string{checks.DocProxyd, checks.DocLightNodes}, nil),
		finding("interop.scope", "Interop fixture scope", report.SeverityInfo, "interop", "basic dependency readiness only; no cross-chain message validation is implied", "This fail fixture still represents endpoint readiness only, not full interop protocol validation.", []string{checks.DocInterop}, nil),
		finding("interop.base.chain_id", "Interop dependency chain ID mismatch", report.SeverityWarn, "interop.dependencies.base", "expected chain_id=8453 observed chain_id=10", "Point every dependency RPC at the configured chain before relying on dependency-set readiness checks.", []string{checks.DocInterop}, map[string]string{"expected_chain_id": "8453", "observed_chain_id": "10"}),
	}
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
