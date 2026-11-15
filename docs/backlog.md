# MVP backlog

This backlog records ownership and handoffs. Each item lists the role responsible for its current step. The PM refines Planned tasks as their dependencies become available.

| ID | Task | Status | Current role | Dependencies |
| --- | --- | --- | --- | --- |
| MA-001 | Confirm local inputs and sample rules | Done | Tester | None |
| MA-002 | Go CLI, configuration, and scraper | Done | Tester | MA-001 |
| MA-003 | Rule evaluator and alert state | Done | Tester | MA-002 |
| MA-004 | Slack webhook sender | Done | Tester | None |
| MA-005 | Integrate primary and secondary watchdog | Done | Tester | MA-002, MA-003, MA-004 |
| MA-006 | Kubernetes deployment and local MVP acceptance | Done | Tester | MA-005 |
| MA-007 | Verify production inputs and deployment | Blocked | PM | MA-006, production inputs |
| MA-008 | Validate primary configuration offline | Done | Tester | MA-002 |

### MA-001 · Confirm local inputs and sample rules

- Verify Prometheus text output from the Node Exporter, Redis Exporter, and RabbitMQ containers in Compose.
- Use one rule per exporter in `config/example.yaml`. The node rule measures available memory as a fraction, the Redis rule observes connected clients, and the RabbitMQ rule sums connections. Demo thresholds make the delivery flow observable.
- Evidence — `./dev/smoke.sh` matched rule notifications from all three exporters. The stack uses standalone Redis as a development fixture.

### MA-002 · Go CLI, configuration, and scraper

- Invalid configuration fails startup with an error identifying the field. A scrape cycle selects configured metrics from HTTP fixtures representing all three exporter groups and tracks each target separately when a timeout or parse error occurs.
- Verify response-size, series, and concurrency limits. Ensure stale samples cannot be evaluated as current data.
- Evidence — `go test ./...` covers example configuration, strict YAML fields, response size, invalid exposition, and isolation of a slow target. The Compose smoke test exercises all three real exporters.

### MA-003 · Rule evaluator and alert state

- Use fixtures to verify calculations, thresholds, `for`, firing, recovery, and stable identities per group or target.
- Verify that missing series, scrape failures, NaN, and division by zero follow the architecture's `unknown` semantics. Cover pending → unknown and firing → unknown → recovery separately.
- Evidence — `TestRatioTransitions` checks aggregation, the pending timer, unknown data, firing deduplication, and recovery. Compose verifies the gauge, sum, and ratio sample rules.

### MA-004 · Slack webhook sender

- Use a local HTTP test server to verify payload, success, HTTP 429 with `Retry-After`, retries after network or 5xx errors, and termination after permanent errors.
- Verify bounded retries and queue capacity, a slow notifier that leaves evaluation running, and webhook URL redaction in errors and logs.
- Evidence — `TestNotifier` checks the payload, limited queue, permanent errors, rate limiting, and retry after 503. Compose and kind received payloads through the HTTP stub. The sender logs status codes or generic error messages.

### MA-005 · Integrate primary and secondary watchdog

- Integrate both modes. Use an end-to-end fixture to trigger an alert, preserve state across cycles, and send one recovery.
- Verify that the secondary sends one outage alert when the primary is unreachable or its progress is stale, followed by a recovery. An individual exporter failure and a single failed poll below the threshold must leave the primary outage alert inactive.
- Evidence — `TestPrimaryHealth` checks startup, fresh and expired progress, and recovery. The Compose smoke test verifies firing and recovery from both Redis and the watchdog, including one watchdog message per transition.

### MA-006 · Kubernetes deployment and local MVP acceptance

- Validate two Deployments, the ClusterIP Service, ConfigMap and Secret references in an isolated local namespace.
- Evidence — kind accepted the manifests with a server dry run and rolled out both modes. The primary reached all three Compose exporters and delivered demo alerts. Restarting the primary rolled out the replacement pod. Scaling from one replica to zero and back produced one outage alert and one recovery through the local webhook receiver.
- Baseline — one `crictl stats` sample with three exporters showed 0.04% CPU and about 9.5 MB of memory for primary, and 0.00% CPU and about 2.9 MB for secondary. Manifest requests and limits are initial values for testing.

### MA-007 · Verify production inputs and deployment

- Inventory the real exporter endpoints, including Redis Cluster nodes, required authentication and TLS, namespace access, and connectivity owners. Collect a priority rule for each exporter group and confirm that the evaluator supports its metric, labels, and calculation.
- Publish the image to the team's registry. Verify exporter connectivity and HTTPS delivery to a Slack test channel from the intended namespace.
- Measure CPU, RSS, and alert latency under a representative target count, series volume, and scrape frequency. Use the results to tune requests, limits, and watchdog thresholds.
- Handoff — local MVP verification is complete. The PM needs real rule examples, the target namespace and registry, and a Slack test webhook supplied through the team's secret-management workflow.
- Unblock — the PM coordinates with infrastructure and exporter owners for endpoints, access requirements, registry, and namespace. Alert owners supply representative rules and provision the test webhook through the secret-management workflow.

### MA-008 · Validate primary configuration offline

- PM scope — add a CLI command to validate a rules file before rollout. Reuse primary startup validation. MA-002 supplied the parser and validator. Developer starts with the CLI dispatch and documents the command for deployment operators.
- With the webhook environment variable unset, `metrics-alert check-config -config config/example.yaml` exits zero and reports success. It opens no listener or outbound connection.
- With an unknown YAML field or a reference to an undefined target, the command exits nonzero and identifies the configuration error. Extra positional arguments also fail.
- Developer handoff — the CLI shares `loadConfig` with primary startup and returns before webhook validation and runtime startup. The README documents the deployment command. Run `go test ./...`, `go vet ./...`, and exercise the built CLI with valid and invalid files.
- Tester verdict — passed. `gofmt`, `go test ./...`, `go vet ./...`, and `git diff --check` completed successfully. Built the CLI with `go build -o /tmp/metrics-alert-check ./cmd/metrics-alert`. With `SLACK_WEBHOOK_URL` unset, the example succeeded and both runtime modes still rejected the missing webhook. Code review confirmed that check-config returns before any runtime or HTTP startup.
- Tester error cases — appended `unexpected_key: true` to a copy of the example, replaced `targets: [redis]` with `targets: [missing]` in a second copy, and passed an extra positional argument with the original example. Each command failed with the expected diagnostic.
- Next step — the PM can use check-config on candidate rule files when MA-007 inputs arrive. Production connectivity and delivery remain part of MA-007.

## Handoff template

```markdown
- Status and current role
- Outcome and related files or commit
- Verification commands and results
- Assumptions, unverified items, or unblock conditions
- Next step and receiving role
```
