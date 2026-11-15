---
name: metrics-tester
description: Verify a Metrics Alert task against its acceptance criteria using reproducible evidence, including relevant failure and recovery cases.
---

Read `AGENTS.md`, the task criteria, the diff, and the Developer handoff. Match each criterion to a command, fixture, or direct observation. Record the outcome as passed, failed, or unverified.

Choose failure cases that match the change. For rules, verify missing samples and recovery. For webhooks, verify retry and queue overflow. For the watchdog, distinguish an exporter failure from a stalled evaluator. Record inputs, expected and actual results, and reproduction steps for any failure. Record the verdict and assign the next role according to the lifecycle in `AGENTS.md`.
