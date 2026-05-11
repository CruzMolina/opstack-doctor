# Changelog

All notable changes to `opstack-doctor` will be documented here.

The project uses semantic versioning. Release tags are prefixed with `v`, for example `v0.1.0`.

## 0.1.12 - 2026-05-11

### Added

- JSON Schema generation with `opstack-doctor generate schema --out doctor.schema.json`, including a checked-in schema example and parity tests.
- Schema contract validation for the checked-in example config and intentionally invalid fixtures, wired into release checks with `make schema-check`.

## 0.1.11 - 2026-05-11

### Added

- Shell completion generation with `opstack-doctor completion bash|zsh|fish`.

## 0.1.10 - 2026-05-11

### Added

- Offline `opstack-doctor validate --config doctor.yaml` command for config/topology linting without RPC or metrics endpoint access, with human and JSON output.

## 0.1.9 - 2026-05-11

### Added

- Golden-file and command-level parity coverage for generated runbooks, including a checked-in sample runbook generated from `examples/doctor.example.yaml`.

## 0.1.8 - 2026-05-11

### Added

- Golden-file regression coverage for generated alert YAML, including an `UPDATE_GOLDEN=1` refresh workflow and `promtool` validation of the golden output.
- Command-level parity coverage proving `examples/prometheus-rules.example.yaml` matches `opstack-doctor generate alerts --config examples/doctor.example.yaml`.
- Negative `promtool test rules` cases proving representative generated alerts stay quiet for healthy doctor-exported and native interop inputs.

## 0.1.7 - 2026-05-11

### Added

- `promtool test rules` coverage for native op-supervisor and op-interop-mon alert templates.

## 0.1.6 - 2026-05-11

### Added

- `promtool test rules` fixtures for representative generated alerts, including doctor-exported interop readiness, execution lag, and proxyd readiness alerts.

## 0.1.5 - 2026-05-11

### Added

- Doctor-exported Prometheus alert templates and examples for interop dependency, op-supervisor, and op-interop-mon readiness findings.
- `promtool` validation for checked-in generated alert rules in CI and release checks.

## 0.1.4 - 2026-05-11

### Added

- Optional op-supervisor metrics readiness checks for up status, local/cross safety refs, expected chain coverage, access-list verification failures, log DB metrics, and RPC metric presence.
- Optional op-interop-mon metrics readiness checks for up status, message status, terminal status changes, and block range metric presence.
- Prometheus alert templates, runbook content, docs, sample config, and fixtures for interop-specific metrics.

## 0.1.3 - 2026-05-10

### Added

- Redacted Prometheus metric fixtures for proxyd and op-node healthy, warning, failure, missing-optional, and label-variant cases.
- Regression tests that assert stable finding IDs and severities from metric fixtures without hitting live endpoints.

### Changed

- Treat missing version-specific proxyd backend probe and backend latency metrics as informational findings instead of warnings.

## 0.1.2 - 2026-05-09

### Added

- proxyd-native metric checks for process up status, backend probes, degraded or banned backends, consensus counts, consensus block gauges, CL/source-tier consensus counters, backend error counters, and backend request latency.
- Prometheus alert templates for proxyd-native process, backend health, consensus, latency, and CL consensus signals.

## 0.1.1 - 2026-05-09

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
