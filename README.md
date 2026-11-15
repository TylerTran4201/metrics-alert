# Metrics Alert

Metrics Alert gives DevOps control over Slack alerts from Node Exporter, Redis Exporter, and RabbitMQ metrics. Two Go workloads run on the existing Kubernetes cluster without requiring a new VM for Prometheus, Alertmanager, or Grafana. Infra continues to use Kibana for its existing monitoring.

- **Primary** scrapes configured endpoints, evaluates rules, and sends Slack alerts.
- **Secondary** checks whether the primary continues to complete processing cycles and sends Slack notifications when the signal is lost or restored.

The MVP runs locally with Docker Compose and has been verified on a local Kubernetes cluster. See the [MVP architecture](docs/architecture.md), [working agreements](AGENTS.md), and [backlog](docs/backlog.md).

## Start a Codex session

Use one of the prompts below. Tasks and handoffs live in the backlog so later sessions can resume from the repository state.

```text
$metrics-pm Review the backlog and prepare MA-001 for handoff.
$metrics-developer Implement MA-002 once it is Ready.
$metrics-tester Verify MA-002 against its acceptance criteria and record the results in the backlog.
```

## Run locally with Docker Compose

```sh
docker compose up --build -d
docker compose logs -f webhook
```

Compose starts Node Exporter, a standalone Redis with Redis Exporter, RabbitMQ with its Prometheus plugin, both Metrics Alert modes, and a local webhook receiver. The receiver logs Slack payloads for local verification. The exporter ports bind to loopback on `19100`, `19121`, and `19292`. Primary health is available at `http://127.0.0.1:19080/healthz`.

Run `./dev/smoke.sh` to check all three sample rules, Redis alert recovery, and one outage and recovery notification after stopping and restarting the primary. The script recreates the local primary and secondary and leaves Compose running. Stop the environment with `docker compose down`.

## Configure rules

Start from [the Compose example](config/example.yaml) and replace its endpoints and demo thresholds before deploying. Each selector matches a metric name and optional exact labels. `gauge` expects one matching series per target and tracks each target independently. `sum` totals matches across the listed targets. `ratio` divides that total by the total from a second selector. The threshold compares the raw result, so use `0.85` for an 85% ratio. See the [architecture](docs/architecture.md) for missing-data and retry behavior.

## Deploy to Kubernetes

Build and publish the Docker image to your registry, then replace both `metrics-alert:dev` references in [the workload manifest](deploy/workloads.yaml) with that image. Use exporter URLs reachable from the namespace. The local Redis example is standalone. For a Redis Cluster, configure the exporter endpoints and rules for the nodes you want to monitor. Exporter URLs must be reachable directly over HTTP or HTTPS. Endpoint-specific authentication and custom CA configuration remain deployment requirements to confirm for your environment.

Create `metrics-alert-slack` in the target namespace with the HTTPS Slack webhook URL under the `webhook-url` key using your secret-management workflow. With the Secret in place, apply the config and workloads.

```sh
kubectl -n "$NAMESPACE" create configmap metrics-alert-rules \
  --from-file=rules.yaml=path/to/rules.yaml --dry-run=client -o yaml |
  kubectl -n "$NAMESPACE" apply -f -
kubectl -n "$NAMESPACE" apply -f deploy/workloads.yaml
kubectl -n "$NAMESPACE" rollout status deployment/metrics-alert-primary
kubectl -n "$NAMESPACE" rollout status deployment/metrics-alert-secondary
```

Restart the primary Deployment after config changes. Restart both Deployments after rotating the webhook Secret. Tune resource settings against the deployment's target count, metric volume, and alert latency requirements.
