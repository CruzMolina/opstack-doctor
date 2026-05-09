# opstack-doctor

`opstack-doctor` is a read-only diagnostic CLI for OP Stack and Superchain node operators. It checks whether a node fleet looks ready for op-geth to op-reth migration, source/light-node topology, basic interop dependency observability, and Prometheus-based alerting.

It is built as a small Go single-binary tool. It never sends transactions, never asks for private keys, and redacts RPC URLs before they appear in findings.

## Why It Exists

Optimism has announced that op-geth and op-program are supported through May 31, 2026, and operators should migrate to op-reth / cannon-kona. The recommended migration pattern is to run op-reth beside the existing node and compare block hashes, state roots, and RPC outputs before moving traffic.

OP Labs also recommends a specialized op-node fleet: a small redundant source tier performs L1 derivation, and light or sequencer nodes follow sources via `--l2.follow.source=<source op-node RPC>`. For interop, nodes need a way to follow every chain in their dependency set, so basic reachability and observability need to be in place before deeper validation is meaningful.

Official references:

- [End of Support for op-geth and op-program](https://docs.optimism.io/notices/op-geth-deprecation)
- [Light Nodes: Specialize Your Op-Node Fleet](https://www.optimism.io/blog/light-nodes-specialize-your-op-node-fleet)
- [OP Stack interoperability explainer](https://docs.optimism.io/op-stack/interop/explainer)
- [Node metrics and monitoring](https://docs.optimism.io/node-operators/guides/monitoring/metrics)

## What It Checks Today

- Config validity, required fields, valid op-node roles, and declared follow-source topology.
- Execution JSON-RPC reachability using `web3_clientVersion`, `eth_chainId`, `eth_blockNumber`, and `eth_getBlockByNumber`.
- Candidate/reference execution head lag and latest common block comparison for hash, parent hash, state root, transactions root, and receipt root when present.
- Conservative client-family heuristics for op-geth and op-reth/reth.
- op-node Prometheus metrics, including `op_node_default_up`, refs, peer counts, derivation errors, pipeline resets, and RPC client latency metric presence.
- Light/sequencer follower lag against configured source nodes using available RPC and parseable safe-head metrics.
- Basic interop dependency RPC, chain ID, block-number, and metrics reachability.
- Prometheus alert-rule and Markdown runbook generation.

## What It Does Not Check Yet

- Full interop protocol correctness or cross-chain message validation.
- op-supervisor-specific behavior or metrics.
- Actual deployed CLI flags unless represented in the config.
- proxyd consensus-aware routing behavior.
- Exhaustive RPC equivalence between op-geth and op-reth.
- Grafana dashboard generation or container packaging.

## Install And Run

```sh
go build ./cmd/opstack-doctor
./opstack-doctor check --config examples/doctor.example.yaml
```

The main command is:

```sh
opstack-doctor check --config doctor.yaml
```

Output modes:

```sh
opstack-doctor check --config doctor.yaml --output human
opstack-doctor check --config doctor.yaml --output json
opstack-doctor check --config doctor.yaml --output prometheus
```

Exit-code policy:

```sh
opstack-doctor check --config doctor.yaml --fail-on fail
opstack-doctor check --config doctor.yaml --fail-on warn
```

By default, findings are printed and the command exits zero unless there is a config or IO error. `--fail-on fail` exits nonzero when any `fail` finding exists. `--fail-on warn` exits nonzero for either `warn` or `fail`.

## Prometheus Export

For scheduled diagnostics, emit scrapeable text metrics:

```sh
opstack-doctor export metrics --config doctor.yaml
```

This is equivalent to `check --output prometheus`. It emits generic finding gauges plus derived metrics such as:

- `opstack_doctor_execution_candidate_lag_blocks`
- `opstack_doctor_execution_block_compare_match`
- `opstack_doctor_topology_follower_lag_blocks`

Run this from a cron job, Kubernetes `CronJob`, or sidecar-style wrapper and expose the output through your normal textfile/scrape path.

## Configuration

Start with [examples/doctor.example.yaml](examples/doctor.example.yaml). The config expresses intended topology; the tool validates behavior through read-only RPC and metrics checks.

For local or mocked endpoints, point `reference_rpc`, `candidate_rpc`, op-node `rpc`, and `metrics` URLs at local `httptest`, anvil-style, or fixture servers that implement the small method set used by the MVP. Tests in this repository do this and never hit public RPC endpoints.

## Local Demo

Try realistic mocked output without any live infrastructure:

```sh
opstack-doctor demo --scenario healthy
opstack-doctor demo --scenario warn --output json
opstack-doctor demo --scenario fail --output prometheus
```

The demo command starts temporary localhost RPC and metrics servers, runs the normal check engine, prints the selected output, and then shuts the servers down. Scenarios are:

- `healthy`: redundant source tier, op-reth candidate, matching execution blocks, healthy metrics.
- `warn`: op-geth reference, one source node, low peer count, derivation errors, follower lag.
- `fail`: op-geth candidate, execution lag/divergence, and an op-node reporting down.

## Generate Alerts And Runbooks

```sh
opstack-doctor generate alerts --config doctor.yaml --out prometheus-rules.yaml
opstack-doctor generate runbook --config doctor.yaml --out runbook.md
```

The generated alert rules are templates. They assume common metric names and scrape labels such as `node`, `role`, `ref`, and `layer`; adjust selectors to match your Prometheus labeling.

The `ExecutionCandidateLaggingReference` alert assumes you run `opstack-doctor export metrics --config doctor.yaml` or `opstack-doctor check --output prometheus` on a schedule and scrape the emitted `opstack_doctor_execution_candidate_lag_blocks` gauge.

## Development

```sh
make fmt
make test
make vet
```

## Roadmap

- Deeper op-reth/op-geth RPC comparison.
- op-supervisor and interop-specific metrics.
- proxyd topology checks.
- Grafana dashboard generation.
- Dependency-set config discovery.
- Optional container image.
- Upstreaming useful checks into Optimism docs/tooling.
