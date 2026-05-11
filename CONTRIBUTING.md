# Contributing

Thanks for helping make `opstack-doctor` useful for real OP Stack operators.

## Development

```sh
go test ./...
go vet ./...
```

Keep checks read-only. New diagnostics must not send transactions, require private keys, or mutate node state.

## Testing

Tests should use `httptest` or local fixtures. Do not add tests that hit public RPC endpoints, public metrics endpoints, or production infrastructure.

Metric regression fixtures live in [`testdata/metrics`](testdata/metrics). See [docs/metrics-fixtures.md](docs/metrics-fixtures.md) before contributing captured op-node or proxyd metrics; fixtures must be minimized and redacted before they are committed.

Generated alert rules have a golden fixture in [`internal/generate/testdata`](internal/generate/testdata). If an alert generator change intentionally updates emitted YAML, refresh the fixture with:

```sh
UPDATE_GOLDEN=1 go test ./internal/generate
make promtool-check
```

Review the golden YAML diff carefully so expression, threshold, label, duration, and annotation changes are explicit in the PR.

The checked-in example rules in [`examples/prometheus-rules.example.yaml`](examples/prometheus-rules.example.yaml) should remain generated from [`examples/doctor.example.yaml`](examples/doctor.example.yaml). Refresh them with:

```sh
go run ./cmd/opstack-doctor generate alerts --config examples/doctor.example.yaml --out examples/prometheus-rules.example.yaml
```

Generated runbooks have the same regression pattern. Refresh the golden fixture and public example with:

```sh
UPDATE_GOLDEN=1 go test ./internal/generate
go run ./cmd/opstack-doctor generate runbook --config examples/doctor.example.yaml --out examples/runbook.example.md
```

The checked-in JSON Schema should match the static schema generator. Refresh it after config-shape changes with:

```sh
go run ./cmd/opstack-doctor generate schema --out examples/doctor.schema.json
```

Schema contract tests validate the public example config and intentionally invalid fixtures against the checked-in schema. When adding or changing config fields, update the schema, examples, docs, and fixtures together, then run:

```sh
make schema-check
```

## Adding Checks

- Prefer honest `warn` or `info` findings over false confidence.
- Include actionable recommendations and official docs links when possible.
- Redact URLs before they appear in findings, logs, or errors.
- Keep protocol assumptions explicit in the finding recommendation or docs.
