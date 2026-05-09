# Alerting Guide

`opstack-doctor generate alerts` writes plain Prometheus alert-rule YAML. The generated rules are useful starting points, not a complete site-specific monitoring stack.

## Generate Rules

```sh
opstack-doctor generate alerts --config doctor.yaml --out prometheus-rules.yaml
```

Load the generated file into Prometheus, or adapt it into a `PrometheusRule` object if you use the Prometheus Operator.

## Export Doctor Metrics

Some generated alerts depend on metrics emitted by a scheduled doctor run:

```sh
opstack-doctor export metrics --config doctor.yaml
```

The deployment examples show two common patterns:

- [node-exporter textfile collector](../examples/deploy/prometheus-textfile-cron.sh)
- [Kubernetes CronJob template](../examples/deploy/kubernetes-cronjob.yaml)

Doctor-exported metrics used by generated rules:

| Metric | Meaning |
| --- | --- |
| `opstack_doctor_finding` | Generic finding presence by id, target, and severity; proxyd readiness alerts use this for RPC/metrics/config-intent failures. |
| `opstack_doctor_execution_candidate_lag_blocks` | Candidate execution head lag behind the reference endpoint. |
| `opstack_doctor_execution_block_compare_match` | `1` when sampled block field comparison matched, `0` when divergence was observed. |
| `opstack_doctor_execution_rpc_surface_match` | `1` when sampled read-only RPC outputs matched, `0` when mismatch or fetch failure was observed. |
| `opstack_doctor_topology_follower_lag_blocks` | Follower lag behind its configured source from RPC heads or safe-head metrics. |
| `opstack_doctor_proxyd_head_lag_blocks` | proxyd RPC head lag behind the latest readable declared backend. |

## Label Assumptions

op-node metric labels vary by version and scrape configuration. Generated rules assume common labels such as:

- `node`: logical node name, matching config names such as `source-1` or `light-1`.
- `role`: logical role such as `source`, when source nodes are selected by role.
- `ref`: ref type containing values like `safe`, `finalized`, or `unsafe`.
- `layer`: layer label containing values like `l2`.

Before production use, compare generated selectors against your real Prometheus series:

```promql
op_node_default_up
op_node_default_refs_number
op_node_default_peer_count
op_node_default_p2p_peer_count
op_node_default_derivation_errors_total
op_node_default_pipeline_resets_total
```

Adjust selectors if your scrape labels use names such as `instance`, `job`, `chain`, `ref_name`, or `type` instead.

## Generated Alerts

| Alert | Source metric family | Purpose |
| --- | --- | --- |
| `OpNodeDown` | `op_node_default_up` | Detects any op-node reporting not up. |
| `L2SafeHeadNotAdvancing` | `op_node_default_refs_number` | Detects safe-head refs that stop increasing. |
| `OpNodeLowPeerCount` | peer count metrics | Detects low P2P peer count. |
| `OpNodeDerivationErrors` | `op_node_default_derivation_errors_total` | Detects derivation errors. |
| `OpNodePipelineResets` | `op_node_default_pipeline_resets_total` | Detects pipeline resets. |
| `SourceOpNodeDown` | `op_node_default_up` | Treats the source tier as a hard dependency. |
| `ProxydEndpointUnhealthy` | doctor export | Detects configured proxyd RPC, head, or chain ID failures. |
| `DeriverProxydNotConsensusAware` | doctor export | Detects deriver proxyd endpoints not declared consensus-aware in doctor config. |
| `ProxydMetricsUnavailable` | doctor export | Detects missing or unreachable proxyd metrics endpoints. |
| `ProxydHeadLaggingBackends` | doctor export | Detects proxyd RPC head lag behind declared backends. |
| `LightNodeLaggingSource` | `op_node_default_refs_number` | Detects follower safe-head lag behind configured source. |
| `ExecutionCandidateLaggingReference` | doctor export | Detects candidate execution lag. |
| `ExecutionBlockComparisonMismatch` | doctor export | Detects sampled block field mismatch. |
| `ExecutionRPCSurfaceMismatch` | doctor export | Detects sampled read-only RPC output mismatch or fetch failure. |

## Validation

The Go test suite parses generated alert YAML and the checked-in example file:

```sh
go test ./internal/generate
```

This proves the YAML shape is valid for the local structs. It does not prove your Prometheus server accepts every expression after local label edits, so validate customized rules with your Prometheus tooling before deploying them.
