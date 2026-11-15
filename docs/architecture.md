# MVP architecture

## Data flow

```text
Node / Redis / RabbitMQ exporters → Primary (scrape → evaluate → notify) → Slack
                                       ↑
                          ClusterIP /healthz
                                       |
                                  Secondary → Slack
```

Build one Go binary with `primary` and `secondary` modes. Run each mode as an independent Kubernetes Deployment with one replica. Use the `Recreate` strategy for the primary to avoid overlapping evaluators during routine rollouts. The secondary acts as a watchdog. Rule evaluation stops when the primary stops. Simultaneous loss of both workloads or shared Slack egress falls outside the MVP's ability to notify.

## Primary

- Periodically scrape statically configured HTTP(S) endpoints through a shared exporter pipeline. Start with Prometheus text format and confirm real endpoint formats in MA-001. Scrape targets referenced by rules with up to four workers and one shared cycle timeout. Rotate the starting target each cycle so slow targets cannot always crowd out the same endpoints. Limit each response to 2 MiB and 20,000 metric instances and retain only the reductions required by active rules. [Prometheus exposition formats](https://prometheus.io/docs/instrumenting/exposition_formats/)
- Define rules in YAML with an ID, target list, metric name and label selectors, calculation, threshold, `for` duration, and notification content. The MVP supports gauge comparisons, sums of grouped series, and ratios of two sums. Check representative rules before finalizing the schema. Evaluate counter rates, histogram quantiles, full PromQL, and historical queries as follow-up needs. [PromQL](https://prometheus.io/docs/prometheus/latest/querying/basics/)
- Evaluate each rule using valid samples from the current scrape cycle. A timeout, missing series, non-finite value, or zero denominator yields `unknown`. An aggregate rule also yields `unknown` if a required target fails. An `unknown` result resets the pending `for` timer and preserves the state of firing alerts. Send a recovery only when valid data shows the condition has cleared. Track scrape failures separately from rule state.
- Identify each alert by rule ID and either target or group labels. Notify on firing and resolved transitions. Keep alert state and a bounded queue in memory. Restarting loses state and queued notifications. A condition that remains true can fire again after its `for` duration.
- Read the Slack Incoming Webhook URL from a Kubernetes Secret. Use request timeouts, bounded retries for network and 5xx errors, and honor `Retry-After` on HTTP 429. Deliver notifications outside the evaluation loop so delivery failures cannot stop scraping. Track successful delivery, permanent failure, and drops due to retry exhaustion or a full queue. A retry after a network failure can duplicate a message if Slack received the original request. [Slack incoming webhooks](https://docs.slack.dev/messaging/sending-messages-using-incoming-webhooks/) · [Slack rate limits](https://docs.slack.dev/apis/web-api/rate-limits/)

## Secondary

- Poll the primary's `/healthz` endpoint through an internal Service. The primary updates its progress timestamp when a scrape and evaluation cycle finishes, including cycles with handled exporter failures. Report unhealthy before the first completed cycle and when progress becomes stale. Set the freshness threshold above the maximum cycle duration. Account for warm-up and routine rollouts in the watchdog settings.
- Count consecutive failures, including timeouts, connection errors, and unhealthy responses. Notify once when the failure threshold is reached and send a recovery when the primary becomes healthy again. Keep transition state in memory and reuse the primary's Slack delivery mechanism.
- `/healthz` reflects evaluator progress. Track exporter and Slack delivery failures separately in logs so operators can distinguish them from primary failures.

## Deployment prerequisites and resource measurements

- Verify connectivity from the Kubernetes namespace to each exporter and Slack, including DNS, firewall rules, TLS, and authentication. Keep the health Service internal. Verify CNI support for NetworkPolicy if applying ingress or egress restrictions. [Kubernetes NetworkPolicy](https://kubernetes.io/docs/concepts/services-networking/network-policies/)
- Measure CPU, RSS, scrape and evaluation duration, and alert latency using fixtures representative of the target and series counts and scrape frequency. Use the measurements in MA-006 to set resource budgets, requests, and limits for both modes.
