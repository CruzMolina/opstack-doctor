# opstack-doctor

`opstack-doctor` is a read-only diagnostic CLI for OP Stack and Superchain node operators. It checks whether a node fleet looks ready for op-geth to op-reth migration, source/light-node topology, basic interop dependency observability, and Prometheus-based alerting.

It is built as a small Go single-binary tool. It never sends transactions, never asks for private keys, and redacts RPC URLs before they appear in findings.

## Why It Exists

Optimism has announced that op-geth and op-program are supported through May 31, 2026, and operators should migrate to op-reth / cannon-kona. The recommended migration pattern is to run op-reth beside the existing node and compare block hashes, state roots, and RPC outputs before moving traffic.

OP Labs also recommends a specialized op-node fleet: a small redundant source tier performs L1 derivation, and light or sequencer nodes follow sources via `--l2.follow.source=<source op-node RPC>`. For interop, nodes need a way to follow every chain in their dependency set, so basic reachability and observability need to be in place before deeper validation is meaningful.

For production routing, proxyd can provide backend routing, retries, consensus tracking, response rewriting, load balancing, caching, and metrics. `opstack-doctor` checks declared proxyd endpoints from the outside; it does not read private proxyd TOML.

Official references:

- [End of Support for op-geth and op-program](https://docs.optimism.io/notices/op-geth-deprecation)
- [Light Nodes: Specialize Your Op-Node Fleet](https://www.optimism.io/blog/light-nodes-specialize-your-op-node-fleet)
- [proxyd](https://docs.optimism.io/operators/chain-operators/tools/proxyd)
- [OP Stack interoperability explainer](https://docs.optimism.io/op-stack/interop/explainer)
- [Node metrics and monitoring](https://docs.optimism.io/node-operators/guides/monitoring/metrics)

## What It Checks Today

- Offline config validity, required fields, URL shape, valid topology roles, follow-source intent, proxyd backend intent, and basic interop declarations.
- Execution JSON-RPC reachability using `web3_clientVersion`, `eth_chainId`, `eth_blockNumber`, and block-read methods.
- Candidate/reference execution head lag, latest common block comparison, and sampled read-only RPC output comparison using `eth_getBlockByNumber`, `eth_getBlockByHash`, and `eth_getBlockTransactionCountByNumber`.
- Conservative client-family heuristics for op-geth and op-reth/reth.
- op-node Prometheus metrics, including `op_node_default_up`, refs, peer counts, derivation errors, pipeline resets, and RPC client latency metric presence.
- Light/sequencer follower lag against configured source nodes using available RPC and parseable safe-head metrics.
- proxyd/routing readiness for declared deriver and edge endpoints: consensus-aware intent, RPC/metrics reachability, expected backend roles, head lag against readable backends, and native proxyd health metrics such as `proxyd_up`, backend probes, consensus counts, degraded/banned backends, backend error rate, CL consensus counters, and backend latency.
- Basic interop dependency RPC, chain ID, block-number, dependency metrics reachability, optional op-supervisor metrics readiness, and optional op-interop-mon message-monitor metrics readiness.
- Prometheus alert-rule and Markdown runbook generation.

## What It Does Not Check Yet

- Full interop protocol correctness or cross-chain message validation.
- op-supervisor protocol correctness or private dependency-set discovery.
- Actual deployed CLI flags unless represented in the config.
- Private proxyd TOML introspection or proof that a live proxyd process is using every declared backend.
- Every proxyd metric variant across every deployed version; missing version-specific proxyd metrics are reported conservatively.
- Exhaustive RPC equivalence between op-geth and op-reth; current RPC comparison is sampled and read-only.
- Grafana dashboard generation.

## Install And Run

Download a release archive and verify its checksum:

```sh
VERSION=0.1.11
OS=linux
ARCH=amd64
curl -L -O "https://github.com/CruzMolina/opstack-doctor/releases/download/v${VERSION}/opstack-doctor_${VERSION}_${OS}_${ARCH}.tar.gz"
curl -L -O "https://github.com/CruzMolina/opstack-doctor/releases/download/v${VERSION}/SHA256SUMS"
sha256sum -c SHA256SUMS --ignore-missing
tar -xzf "opstack-doctor_${VERSION}_${OS}_${ARCH}.tar.gz"
./opstack-doctor version
```

Or build from source:

```sh
go build ./cmd/opstack-doctor
./opstack-doctor check --config examples/doctor.example.yaml
```

Container images are published for tagged releases:

```sh
docker run --rm ghcr.io/cruzmolina/opstack-doctor:v0.1.11 version
docker run --rm -v "$PWD/examples/doctor.example.yaml:/config/doctor.yaml:ro" ghcr.io/cruzmolina/opstack-doctor:v0.1.11 check --config /config/doctor.yaml
```

The main command is:

```sh
opstack-doctor check --config doctor.yaml
```

Validate config and topology intent without contacting RPC or metrics endpoints:

```sh
opstack-doctor validate --config doctor.yaml
opstack-doctor validate --config doctor.yaml --output json
opstack-doctor validate --config doctor.yaml --fail-on warn
```

Generate shell completion scripts:

```sh
opstack-doctor completion bash
opstack-doctor completion zsh
opstack-doctor completion fish
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

By default, `check` findings are printed and the command exits zero unless there is a config or IO error. `--fail-on fail` exits nonzero when any `fail` finding exists. `--fail-on warn` exits nonzero for either `warn` or `fail`.

`validate` is meant for local linting and CI. It exits nonzero on `fail` findings by default, supports `--fail-on warn` for stricter gates, and supports `--fail-on none` when only report rendering is desired.

## Prometheus Export

For scheduled diagnostics, emit scrapeable text metrics:

```sh
opstack-doctor export metrics --config doctor.yaml
```

This is equivalent to `check --output prometheus`. It emits generic finding gauges plus derived metrics such as:

- `opstack_doctor_execution_candidate_lag_blocks`
- `opstack_doctor_execution_block_compare_match`
- `opstack_doctor_execution_rpc_surface_match`
- `opstack_doctor_topology_follower_lag_blocks`
- `opstack_doctor_proxyd_head_lag_blocks`

Interop readiness alerts can also use exported `opstack_doctor_finding` series from scheduled runs. See [examples/prometheus-export.interop.example.prom](examples/prometheus-export.interop.example.prom) for representative dependency, op-supervisor, and op-interop-mon finding labels.

Run this from a cron job, Kubernetes `CronJob`, or sidecar-style wrapper and expose the output through your normal textfile/scrape path.

See [examples/deploy](examples/deploy) for a node-exporter textfile script and a Kubernetes CronJob template.

## Configuration

Start with [examples/doctor.example.yaml](examples/doctor.example.yaml). The config expresses intended topology; `validate` checks the shape and topology intent offline, while `check` validates live behavior through read-only RPC and metrics checks.

See [docs/config.md](docs/config.md) for a field-by-field schema reference. A generated JSON Schema is available at [examples/doctor.schema.json](examples/doctor.schema.json) and can be regenerated with `opstack-doctor generate schema --out doctor.schema.json`.

For local or mocked endpoints, point `reference_rpc`, `candidate_rpc`, op-node `rpc`, and `metrics` URLs at local `httptest`, anvil-style, or fixture servers that implement the small method set used by the MVP. Tests in this repository do this and never hit public RPC endpoints.

Metric regression fixtures live in [testdata/metrics](testdata/metrics). See [docs/metrics-fixtures.md](docs/metrics-fixtures.md) for how to contribute redacted op-node or proxyd captures safely.

## Local Demo

Try realistic mocked output without any live infrastructure:

```sh
opstack-doctor demo --scenario healthy
opstack-doctor demo --scenario warn --output json
opstack-doctor demo --scenario fail --output prometheus
```

The demo command starts temporary localhost RPC and metrics servers, runs the normal check engine, prints the selected output, and then shuts the servers down. Scenarios are:

- `healthy`: redundant source tier, consensus-aware deriver proxyd, op-reth candidate, matching execution blocks, healthy metrics.
- `warn`: op-geth reference, one source node, non-consensus-aware deriver proxyd, low peer count, derivation errors, follower lag.
- `fail`: op-geth candidate, execution lag/divergence, unreachable proxyd routing, and an op-node reporting down.

## Generate Artifacts

```sh
opstack-doctor generate alerts --config doctor.yaml --out prometheus-rules.yaml
opstack-doctor generate runbook --config doctor.yaml --out runbook.md
opstack-doctor generate schema --out doctor.schema.json
```

The generated alert rules are templates. They assume common metric names and scrape labels such as `node`, `role`, `ref`, and `layer`; adjust selectors to match your Prometheus labeling.

The `ExecutionCandidateLaggingReference` alert assumes you run `opstack-doctor export metrics --config doctor.yaml` or `opstack-doctor check --output prometheus` on a schedule and scrape the emitted `opstack_doctor_execution_candidate_lag_blocks` gauge.

See [docs/alerts.md](docs/alerts.md) for alert assumptions, doctor-exported metrics, and validation notes.

See [examples/runbook.example.md](examples/runbook.example.md) for a generated runbook from the sample config.

The generated schema is static and describes the supported `doctor.yaml` shape for editor validation and config review. It does not contact endpoints and does not replace `opstack-doctor validate`, which applies the same topology checks the CLI uses.

## Shell Completion

Generate a completion script for your shell and install it using your normal shell completion path:

```sh
opstack-doctor completion bash > opstack-doctor.bash
opstack-doctor completion zsh > _opstack-doctor
opstack-doctor completion fish > opstack-doctor.fish
```

The scripts complete top-level commands, common subcommands, and supported flag values.

## Development

```sh
make fmt
make test
make vet
```

Maintainers cutting a release should follow [docs/release.md](docs/release.md).

Useful release-prep targets:

```sh
make release-check
make docker-build
make docker-smoke
```

## License

Apache-2.0. See [LICENSE](LICENSE).

## Roadmap

- Deeper op-reth/op-geth RPC comparison.
- Deeper op-supervisor and interop-specific metrics.
- proxyd metric version matrix and richer consensus-aware routing diagnostics.
- Grafana dashboard generation.
- Dependency-set config discovery.
- Upstreaming useful checks into Optimism docs/tooling.
