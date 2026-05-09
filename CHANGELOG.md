# Changelog

All notable changes to `opstack-doctor` will be documented here.

The project uses semantic versioning. Release tags are prefixed with `v`, for example `v0.1.0`.

## 0.1.1 - Unreleased

### Added

- proxyd/routing topology readiness checks for declared deriver and edge RPC routing endpoints.

### Changed

- Add Apache-2.0 license to release-ready source.
- Update GitHub Actions to current major versions with Node 24-compatible releases.
- Add Dependabot coverage for Go modules, Docker, and GitHub Actions.

## 0.1.0 - 2026-05-08

Initial MVP for OP Stack / Superchain node and chain operators.

### Added

- Read-only `opstack-doctor check --config doctor.yaml` CLI.
- Human, JSON, and Prometheus output formats.
- Config validation for chain, execution endpoints, op-node topology, interop dependencies, and thresholds.
- op-geth to op-reth migration readiness checks:
  - client version heuristics,
  - chain ID validation,
  - head lag comparison,
  - latest common block field comparison,
  - sampled read-only RPC surface comparison.
- op-node Prometheus metrics checks for up status, refs, peer counts, derivation errors, pipeline resets, and RPC latency metric presence.
- Source/light-node topology checks for source redundancy, declared follow-source intent, RPC-head lag, and safe-head lag when metrics are parseable.
- Basic interop dependency reachability and readiness checks.
- Alert and runbook generation.
- Prometheus exporter metrics for scheduled diagnostics.
- Mocked `demo` scenarios for healthy, warning, and failing fleets.
- GitHub Actions CI, release binaries, SHA256 checksums, and GHCR container image publishing.
- Dockerfile and Kubernetes/node-exporter deployment examples.
- Configuration, alerting, and release documentation.

### Limitations

- Interop checks are basic endpoint readiness checks only.
- RPC comparison is sampled and read-only, not exhaustive op-geth/op-reth equivalence.
- Generated Prometheus alert rules are templates and may need label-selector edits for each deployment.
