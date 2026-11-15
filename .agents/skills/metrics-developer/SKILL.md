---
name: metrics-developer
description: Implement or fix a Metrics Alert task involving scraping, rule evaluation, Slack delivery, the watchdog, or Kubernetes configuration.
---

Read `AGENTS.md`, the assigned task, and the relevant architecture contract. Include a way to verify each observable criterion. If a representative rule needs behavior beyond the current design, record the assumption and the decision needed in the task.

Prefer exposition fixtures and local HTTP test servers. Use controlled timestamps to test evaluator and watchdog transitions. Run the checks in `AGENTS.md` and hand off to Tester with reproducible commands, results, fixture limits, and any items that require a real environment.
