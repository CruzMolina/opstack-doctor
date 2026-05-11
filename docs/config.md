# Configuration Reference

`opstack-doctor` reads a YAML config, usually named `doctor.yaml`. The config is read-only operational intent: it tells the checker which endpoints to query and how the fleet is expected to be shaped.

Start from [examples/doctor.example.yaml](../examples/doctor.example.yaml).

## Top-Level Fields

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `chain` | object | yes | Chain identity for the local OP Stack chain being checked. |
| `execution` | object | yes | Reference and candidate execution RPC endpoints for migration checks. |
| `op_nodes` | list | no | Configured op-node fleet members and intended topology roles. |
| `proxyd` | object | no | Declared proxyd/routing endpoints and expected backend groups. |
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

## `proxyd`

`proxyd` describes intended RPC routing endpoints. `opstack-doctor` does not read private proxyd TOML and does not claim to verify live flags. It checks externally visible RPC/metrics endpoints, compares proxyd heads against the op-node backends named in config, and reports native proxyd metrics when they are exposed.

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `enabled` | boolean | no | `false` | Enables proxyd/routing topology checks. |
| `endpoints` | list | when enabled | empty | Declared proxyd endpoints to check. |

Each endpoint:

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `name` | string | yes when configured | none | Unique proxyd endpoint name used in findings and alert labels. |
| `role` | string | yes when configured | none | One of `deriver`, `edge`, or `general`. |
| `rpc` | URL string | no | empty | Read-only proxyd RPC URL used for chain ID and head checks. |
| `metrics` | URL string | no | empty | proxyd Prometheus metrics endpoint. |
| `consensus_aware` | boolean | no | `false` | Declared operator intent that this proxyd endpoint uses consensus-aware routing. |
| `expected_backends` | list of strings | no | empty | op-node names this endpoint is expected to route to or front. |

Role meaning:

- `deriver`: proxyd endpoint that fronts source op-nodes for light/sequencer follow-source traffic. This should be `consensus_aware: true` and should declare redundant source-node backends.
- `edge`: user-facing or production read endpoint that fronts light/sequencer nodes.
- `general`: any other proxyd endpoint where basic RPC/metrics reachability is useful.

Validation behavior:

- Unknown proxyd roles are failures.
- Duplicate proxyd endpoint names are failures.
- `expected_backends` entries must point to configured `op_nodes`.
- `deriver` backends must be `source` op-nodes.
- A deriver proxyd with fewer than two expected source backends emits a warning.
- A deriver proxyd without `consensus_aware: true` emits a warning.
- `proxyd_up != 1` emits a failure. Missing consensus-aware count/block gauges remain warnings for consensus-aware endpoints. Missing version-specific proxyd metrics such as backend probe health or backend latency are informational so older or differently configured proxyd deployments do not produce noisy warnings.
- Native proxyd metric checks currently cover process up status, backend probe health, degraded or banned backends, backend sync state, peer-count metric presence, consensus latest/safe/finalized gauges, serving consensus backend counts, CL/source-tier consensus counters, backend error rate, HTTP/backend error-code counters, and backend request latency quantiles.

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

Interop checks are basic readiness checks only. They verify dependency RPC reachability, chain ID, block-number readability, and metrics reachability when provided. They do not validate cross-chain messages, access lists, op-supervisor behavior, or full protocol correctness.

## `thresholds`

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `max_safe_lag_blocks` | integer | no | `20` | Warning threshold for follower safe-head or RPC-head lag behind source. |
| `min_peer_count` | number | no | `1` | Warning threshold for `op_node_default_peer_count` or `op_node_default_p2p_peer_count`. |
| `max_rpc_latency_seconds` | number | no | `2.0` | Warning threshold for observed op-node/proxyd RPC latency metrics and generated latency alert templates. |

## Severity Notes

Configuration failures mean the relevant checks cannot be trusted until config is fixed. Warnings usually mean the tool observed incomplete readiness, missing metrics, weak topology redundancy, or values outside configured thresholds.

Default exit behavior is non-failing unless there is a config/IO error. Use:

```sh
opstack-doctor check --config doctor.yaml --fail-on fail
opstack-doctor check --config doctor.yaml --fail-on warn
```

for CI or automation gates.
