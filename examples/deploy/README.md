# Deployment Examples

These examples show ways to run `opstack-doctor export metrics` on a schedule. They are intentionally small templates; adapt paths, image names, labels, and secrets handling to your environment.

## Node Exporter Textfile Collector

Use [prometheus-textfile-cron.sh](prometheus-textfile-cron.sh) on a host that already runs node-exporter with the textfile collector enabled.

Example crontab:

```cron
*/5 * * * * /usr/local/bin/prometheus-textfile-cron.sh
```

This writes `opstack_doctor_*.prom` atomically so Prometheus can scrape the most recent successful run through node-exporter.

## Kubernetes CronJob

Use [kubernetes-cronjob.yaml](kubernetes-cronjob.yaml) when you want Kubernetes to run diagnostics periodically. The example writes Prometheus text output to stdout, which is easy to collect with a log pipeline. In production, common alternatives are:

- Send stdout to a metrics gateway your organization already operates.
- Write into a shared volume consumed by a small HTTP textfile sidecar.
- Run the same command in an existing internal scheduler that exposes job output as scrapeable metrics.

The CronJob references a `doctor.yaml` ConfigMap and the release image pattern `ghcr.io/OWNER/REPO:v0.1.0`. Replace `OWNER/REPO` with the published repository path, or mirror the image into your internal registry. Replace placeholder RPC and metrics URLs with internal read-only endpoints. Do not put private keys in the config; `opstack-doctor` does not need them.

## Prometheus Alert Rules

Generate rules from your real config:

```sh
opstack-doctor generate alerts --config doctor.yaml --out prometheus-rules.yaml
```

The generated `ExecutionCandidateLaggingReference` alert expects the exported `opstack_doctor_execution_candidate_lag_blocks` metric from this scheduled command path.

The generated `DoctorInterop*` alerts expect `opstack_doctor_finding` series from the same scheduled export path. They are useful when doctor can reach dependency, op-supervisor, or op-interop-mon endpoints but Prometheus does not scrape those internal endpoints directly. See [../prometheus-export.interop.example.prom](../prometheus-export.interop.example.prom) for representative exported interop finding series.

See [../../docs/alerts.md](../../docs/alerts.md) for the full alert list, doctor-exported metrics, and label assumptions.
