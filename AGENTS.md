# Metrics Alert working agreements

## Project language

Use English for all project work, including source code identifiers, comments, logs, error messages, configuration examples, tests, documentation, skills, tasks, handoffs, commit messages, and pull requests. This applies even when a request arrives in another language. Preserve external names and literal values required by integrations, such as exporter metric names and labels.

## Start of session

1. Check the branch, `git status --short`, and the relevant diff. Preserve the user's work in progress.
2. Read the [backlog](docs/backlog.md), the active task, and the relevant part of the [architecture](docs/architecture.md). The task record is the handoff between sessions.
3. Read the skill for the assigned role. When a session covers multiple roles, record the outcome of each step.

| Role | Handoff | Skill |
| --- | --- | --- |
| PM | Task with scope, dependencies, and acceptance criteria | [metrics-pm](.agents/skills/metrics-pm/SKILL.md) |
| Developer | Reviewable diff and verification results | [metrics-developer](.agents/skills/metrics-developer/SKILL.md) |
| Tester | Verdict and evidence for each acceptance criterion | [metrics-tester](.agents/skills/metrics-tester/SKILL.md) |

## Task lifecycle

- PM moves a task from `Planned` to `Ready` when it is actionable and its dependencies are available.
- Developer takes the task through `Ready → In progress → In review` and records verification evidence.
- Tester moves a task from `In review` to `Done` when evidence supports every criterion. Return a failing task to `In progress` with reproduction steps. Keep it `In review` and document any criteria that could not be verified.
- Use `Blocked` when a dependency prevents further work. Record what would unblock the task and which role or team owns that dependency. Once unblocked, return the task to the status that reflects the remaining work.
- Before ending a session, update the task with its status, changes, verification commands and results, outstanding items, and next step. Include the commit SHA if available. Update the architecture when a technical decision changes.

## Code changes

- Prefer one Go binary with two independent runtime modes. Use the standard library where sufficient and reuse an existing exposition parser for scraping.
- Set timeouts on all I/O. Bound concurrency, response size, tracked series, and queued notifications. Keep webhook URLs, tokens, and raw metric payloads out of logs and the repository.
- Use sanitized fixtures and local HTTP test servers. Once the Go module exists, run `gofmt` on changed Go files, `go test ./...`, and `go vet ./...`. Record any task-specific checks.
- Document changes to rule behavior, missing-data semantics, alert identity, retry policy, or the health contract between workloads in the task.

## Git delivery

After the full feature flow passes its acceptance checks, update the handoff, commit the feature, and push its branch to the configured Git remote. Verify the remote branch points at the delivered commit. If validation or the push is blocked, record the specific blocker and the checks already completed.
