# opstack-doctor Runbook: op-mainnet

This runbook is generated from the operator config. It is intentionally read-only: no transaction sending, no private keys, and no mutation steps are required.

## op-reth Migration Validation

- Run op-reth alongside the existing execution node before migration cutover.
- Compare head block, block hashes, parent hashes, state roots, transaction roots, and receipt roots across at least `16` latest common blocks.
- Keep candidate lag under `4` blocks before moving traffic.
- Treat op-geth as a migration risk: Optimism says op-geth and op-program are supported through May 31, 2026, and operators should migrate to op-reth / cannon-kona.
- After cutover, keep a rollback window and continue comparing sampled RPC outputs.

## Source/Light-Node Topology

- Maintain a small redundant source tier that performs L1 derivation.
- Configure light and sequencer op-nodes to follow source nodes with `--l2.follow.source=<source op-node RPC>`.
- Production proxyd should be consensus-aware, and dashboards should treat the source tier as a hard dependency.
- Alert when follower safe-head lag exceeds `20` blocks.

Configured op-nodes:

- `source-1`: role=source follows=(none)
- `source-2`: role=source follows=(none)
- `light-1`: role=light follows=source-1
- `sequencer-1`: role=sequencer follows=source-1

## proxyd / Routing Topology

- Confirm deriver-tier proxyd is consensus-aware and fronts redundant source nodes.
- Confirm edge proxyd fronts light/sequencer nodes for read scaling where applicable.
- Compare proxyd RPC heads with declared backend heads; alert when lag exceeds `20` blocks.

Configured proxyd endpoints:

- `deriver-proxyd`: role=deriver consensus_aware=true expected_backends=source-1, source-2
- `edge-proxyd`: role=edge consensus_aware=true expected_backends=light-1, sequencer-1

## Interop Dependency Checklist

- Verify every dependency chain has reachable RPC and metrics.
- Monitor dependency heads over time; a single diagnostic run only proves reachability.
- Scrape op-supervisor metrics if this operator runs a supervisor; confirm local/cross safety heads cover the configured dependency set.
- Scrape op-interop-mon metrics if this operator runs the optional monitor; alert on invalid/missing messages and terminal status changes.
- This MVP does not validate cross-chain messages or op-supervisor protocol behavior.

Configured dependencies:

- `base`: chain_id=8453

## Monitoring Checklist

- Scrape `op_node_default_up`, `op_node_default_refs_number`, peer count metrics, derivation errors, pipeline resets, and RPC client latency metrics.
- Scrape proxyd metrics such as `proxyd_up`, `proxyd_backend_probe_healthy`, `proxyd_group_consensus_count`, `proxyd_backend_degraded`, `proxyd_consensus_backend_banned`, `proxyd_backend_error_rate`, and `proxyd_rpc_backend_request_duration_seconds`.
- Scrape interop metrics such as `op_supervisor_*_up`, `op_supervisor_*_refs_number`, `op_supervisor_*_access_list_verify_failure`, `op_interop_mon_*_up`, `op_interop_mon_*_message_status`, and `op_interop_mon_*_terminal_status_changes` when those services are deployed.
- Use the generated Prometheus rules as templates; adjust selectors to your actual scrape labels.
- Keep source-tier alerts distinct from light-node capacity alerts.
- Track execution candidate lag via a scheduled doctor run or an equivalent recording rule.

## Incident Response

### OpNodeDown / SourceOpNodeDown

1. Confirm the process and host are alive.
2. Check the metrics scrape target and op-node logs.
3. For source nodes, verify dependent light/sequencer nodes have another healthy source to follow.

### L2SafeHeadNotAdvancing / LightNodeLaggingSource

1. Compare source and follower safe-head metrics.
2. Check L1 RPC availability and derivation errors.
3. Verify follow-source configuration and source-node RPC reachability.

### ProxydEndpointUnhealthy / ProxydHeadLaggingBackends

1. Compare proxyd `eth_blockNumber` with each declared backend.
2. Check proxyd backend health, consensus-aware routing state, and backend group config.
3. For deriver proxyd, verify at least two healthy source-node backends are available.

### DeriverProxydNotConsensusAware / ProxydMetricsUnavailable

1. Inspect proxyd deployment config and confirm consensus-aware routing is enabled where production deriver traffic depends on proxyd.
2. Verify proxyd Prometheus listener, scrape labels, and dashboard coverage.
3. Keep doctor config aligned with actual proxyd backend groups; this tool does not introspect private proxyd TOML.

### ProxydDown / ProxydBackendProbeUnhealthy

1. Confirm proxyd is running and serving both RPC and metrics listeners.
2. Check backend probe URLs, backend process health, and network policy.
3. If source-node backends are unhealthy, move dependent light/sequencer nodes to a healthy source-tier endpoint.

### ProxydBackendDegradedOrBanned / ProxydNoConsensusBackends

1. Inspect proxyd consensus-aware backend state and ban reasons.
2. Compare backend latest, safe, finalized, peer count, sync state, latency, and error-rate metrics.
3. Restore at least one healthy backend immediately; for deriver/source tiers, restore redundancy before considering the incident closed.

### ProxydBackendRequestLatencyHigh / ProxydBackendErrorRate / ProxydCLConsensusIssues

1. Use labels such as `backend_name`, `method_name`, and `backend_group_name` to identify the affected backend and method.
2. Check backend node logs, disk/network saturation, L1 RPC health, and output-root/local-safe agreement.
3. Treat increases in CL ban and output-root disagreement counters as source-tier correctness incidents, not just capacity incidents.

### OpSupervisorDown / OpSupervisorRefsMissing / OpSupervisorAccessListVerifyFailures

1. Confirm op-supervisor is running, metrics are scraped, and the dependency-set config is loaded.
2. Compare `local_unsafe`, `cross_unsafe`, `local_safe`, and `cross_safe` refs for every expected chain.
3. Investigate access-list verification failures as cross-chain message correctness incidents.

### OpInteropMonitorDown / OpInteropMonitorRiskyMessages / OpInteropMonitorTerminalStatusChanges

1. Confirm op-interop-mon is running and connected to each configured chain RPC.
2. Inspect message status labels for the executing and initiating chain IDs involved.
3. Treat invalid/missing message statuses or terminal status flips as high-signal interop incidents.

### OpNodeLowPeerCount

1. Check P2P listen address, advertised address, firewall, and bootnodes.
2. Confirm peer limits and discovery settings.
3. Correlate with unsafe-head advancement and derivation health.

### OpNodeDerivationErrors / OpNodePipelineResets

1. Inspect op-node logs around the alert window.
2. Check L1 RPC latency/errors and rollup config consistency.
3. Compare source nodes to distinguish local faults from upstream L1/RPC issues.

### ExecutionCandidateLaggingReference

1. Compare `eth_blockNumber` on reference and candidate.
2. Inspect candidate execution logs and disk/network saturation.
3. Do not cut over until lag and block comparison findings are healthy.

## Official References

- https://docs.optimism.io/node-operators/guides/monitoring/metrics
- https://docs.optimism.io/notices/op-geth-deprecation
- https://docs.optimism.io/op-stack/interop/explainer
- https://docs.optimism.io/operators/chain-operators/tools/proxyd
- https://github.com/ethereum-optimism/optimism/tree/develop/op-interop-mon
- https://github.com/ethereum-optimism/optimism/tree/develop/op-supervisor
- https://www.optimism.io/blog/light-nodes-specialize-your-op-node-fleet
