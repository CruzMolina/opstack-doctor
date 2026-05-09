# Configuration Reference

`opstack-doctor` reads a YAML config, usually named `doctor.yaml`. The config is read-only operational intent: it tells the checker which endpoints to query and how the fleet is expected to be shaped.

Start from [examples/doctor.example.yaml](../examples/doctor.example.yaml).

## Top-Level Fields

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `chain` | object | yes | Chain identity for the local OP Stack chain being checked. |
| `execution` | object | yes | Reference and candidate execution RPC endpoints for migration checks. |
| `op_nodes` | list | no | Configured op-node fleet members and intended topology roles. |
| `interop` | object | no | Basic dependency-set readiness checks. |
| `thresholds` | object | no | Operator thresholds used by warning/failure checks. |

## `chain`

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `name` | string | yes | none | Human-readable chain name. Used in reports and Prometheus labels. |
| `chain_id` | integer | yes | none | Expected L2 chain ID. Compared against `eth_chainId` on execution endpoints. |

Example:

```yaml
chain:
  name: op-mainnet
  chain_id: 10
```

## `execution`

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `reference_rpc` | URL string | yes | none | Existing execution client endpoint, typically op-geth during migration validation. |
| `candidate_rpc` | URL string | yes | none | Candidate execution client endpoint, typically op-reth during migration validation. |
| `compare_blocks` | integer | no | `16` | Number of latest common blocks to compare. Must be greater than zero. |
| `max_head_lag_blocks` | integer | no | `4` | Maximum allowed candidate lag behind reference before a failure finding. |

Execution checks are read-only and use JSON-RPC over HTTP:

- `web3_clientVersion`
- `eth_chainId`
- `eth_blockNumber`
- `eth_getBlockByNumber`
- `eth_getBlockByHash`
- `eth_getBlockTransactionCountByNumber`

URLs must use `http` or `https`. Avoid embedding long-lived credentials in URLs where possible; if a URL does contain credentials or tokens, findings redact them before rendering.

## `op_nodes`

Each entry describes one intended op-node role.

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `name` | string | yes | none | Unique node name used in findings and follow-source references. |
| `role` | string | yes | none | One of `source`, `light`, `sequencer`, or `standalone`. |
| `rpc` | URL string | no | empty | Read-only RPC endpoint used for basic head comparison when available. |
| `metrics` | URL string | no | empty | Prometheus text endpoint, commonly `http://host:7300/metrics`. |
| `follows` | string | for `light`/`sequencer`, recommended | empty | Name of the configured source node this node is intended to follow. |

Role meaning:

- `source`: op-node that performs L1 derivation and serves as a follow-source target.
- `light`: op-node intended to follow a source node via `--l2.follow.source=<source op-node RPC>`.
- `sequencer`: sequencer op-node intended to follow a source node.
- `standalone`: traditional non-specialized op-node.

Validation behavior:

- Unknown roles are failures.
- Duplicate names are failures.
- If any op-nodes are configured and none are `source`, the config emits a warning.
- A single source node emits a warning because source-tier redundancy is recommended.
- `light` and `sequencer` nodes should set `follows`.
- If `follows` is set, it must point to a configured node with `role: source`.

## `interop`

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `enabled` | boolean | no | `false` | Enables basic dependency endpoint checks. |
| `dependencies` | list | when enabled | empty | Chains in the configured dependency set. |

Each dependency:

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `name` | string | yes when interop enabled | none | Human-readable dependency name. |
| `chain_id` | integer | yes when interop enabled | none | Expected chain ID for the dependency. |
| `rpc` | URL string | yes when interop enabled | none | Read-only dependency execution RPC endpoint. |
| `metrics` | URL string | no | empty | Dependency metrics endpoint, if available. |

Interop checks in v0.1.0 are basic readiness checks only. They verify dependency RPC reachability, chain ID, block-number readability, and metrics reachability when provided. They do not validate cross-chain messages, access lists, op-supervisor behavior, or full protocol correctness.

## `thresholds`

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `max_safe_lag_blocks` | integer | no | `20` | Warning threshold for follower safe-head or RPC-head lag behind source. |
| `min_peer_count` | number | no | `1` | Warning threshold for `op_node_default_peer_count` or `op_node_default_p2p_peer_count`. |
| `max_rpc_latency_seconds` | number | no | `2.0` | Reserved for latency-oriented alert templates and future checks. |

## Severity Notes

Configuration failures mean the relevant checks cannot be trusted until config is fixed. Warnings usually mean the tool observed incomplete readiness, missing metrics, weak topology redundancy, or values outside configured thresholds.

Default exit behavior is non-failing unless there is a config/IO error. Use:

```sh
opstack-doctor check --config doctor.yaml --fail-on fail
opstack-doctor check --config doctor.yaml --fail-on warn
```

for CI or automation gates.
