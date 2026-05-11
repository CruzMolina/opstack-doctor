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

## Adding Checks

- Prefer honest `warn` or `info` findings over false confidence.
- Include actionable recommendations and official docs links when possible.
- Redact URLs before they appear in findings, logs, or errors.
- Keep protocol assumptions explicit in the finding recommendation or docs.
