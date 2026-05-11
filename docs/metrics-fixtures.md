# Metrics Fixtures

`opstack-doctor` uses redacted Prometheus text fixtures to keep metric checks stable across real-world op-node and proxyd label shapes. Fixtures live in [`testdata/metrics`](../testdata/metrics).

Fixtures are for tests only. They must not contain secrets, private hostnames, internal IPs, bearer tokens, customer names, or production-only topology details.

## Capture A Fixture

Capture from a trusted local environment or a temporary scrape target:

```sh
curl -fsS http://127.0.0.1:7300/metrics > op-node.prom
curl -fsS http://127.0.0.1:9761/metrics > proxyd.prom
```

Never add raw production captures directly to git. Redact first.

## Redact

Before contributing, replace sensitive values with stable placeholders:

- Hostnames, instances, jobs, pods, namespaces, regions, and IPs: use values like `source-1`, `source-2`, `fixture-node`, `deriver`, or `example`.
- URLs and token-like labels: remove them or replace with `redacted`.
- Customer, operator, or environment names: replace with generic names such as `op-mainnet`, `chain-a`, or `backend-a`.
- High-cardinality labels that are not needed by a test: remove the label or reduce the fixture to the few series needed.

Keep metric names and useful labels intact when they exercise parser/check behavior. For example, keep variants such as `backend` versus `backend_name`, `code` versus `status_code`, and `ref_name` versus `type` when the fixture is meant to prove label drift behavior.

## Minimize

Small fixtures are better than full scrapes. Include only the metric families needed for the regression:

- `op_node_default_up`
- `op_node_default_refs_number`
- peer count metrics
- derivation and pipeline counters
- RPC latency summaries or histograms
- `proxyd_up`
- proxyd backend health, consensus, error-rate, response-code, CL/source-tier, and backend latency metrics

Each fixture should start with a short comment explaining the scenario.

## Add A Regression

Fixture tests are table-driven in [`internal/checks/metrics_fixture_test.go`](../internal/checks/metrics_fixture_test.go). A good test asserts exact finding IDs and severities for the behavior the fixture protects.

Prefer adding one focused fixture over expanding a large catch-all fixture. The goal is to make label/version drift obvious without making reviews noisy.

## Safety Checklist

- No public RPC or metrics endpoints are queried by tests.
- No private URLs, tokens, internal hostnames, or IPs remain.
- The fixture is small enough to review by eye.
- Expected findings are honest: missing version-specific metrics should usually be `info`, hard health failures such as `proxyd_up != 1` remain `fail`.
