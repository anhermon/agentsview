# Task Control Plane Stories

All design gates are auto-approved from the user's instruction to infer
remaining details and implement.

## S1 — Persist tasks and workflows (M)

As a local operator, I want durable tasks, agents, workflows, gates, and events
so that the board survives restarts.

Given a fresh task database, when it opens, then the default workflow and task
types exist. Given valid CRUD operations, then they persist and record audit
events. Given a terminal transition, then required gates are enforced.

Files: `internal/tasks/**`. Tests: store, workflow, migration, and gate unit
tests. Dependencies: none.

## S2 — Expose the task REST API (M)

As a local operator, I want task APIs and live update scopes so that every
client uses one contract.

Given valid REST requests, then task/agent/workflow resources round-trip. Given
invalid input or revisions, then typed 400/409 responses are returned. Given a
mutation, then the `tasks` SSE scope emits.

Files: `internal/server/**`, daemon wiring. Tests: handler and route tests.
Dependencies: S1.

## S3 — Add the event-driven harness runtime (M)

As a local operator, I want assignments to launch any supported harness without
heartbeats so that tokens are consumed only for real work.

Given a supported trigger, then exactly one run starts with a compact envelope
and isolated worktree. Given no trigger, no agent starts. Given an external
adapter, malformed or unsupported frames fail closed.

Files: `internal/taskrun/**`. Tests: adapters, protocol, leases, context, and
worktree validation. Dependencies: none.

## S4 — Add the Kanban and agent UI (M)

As a local operator, I want a compact board showing task state, phase, agent,
gates, and evidence so that I can manage work at a glance.

Given tasks, then they group in project workflow order. Given keyboard, pointer,
or touch interaction, then valid transitions and assignment are accessible.
Given live updates, then the board refreshes without a page reload.

Files: `frontend/src/**`, `frontend/messages/**`. Tests: typed client, board,
interactions, and localization. Dependencies: S2 API contract.

## S5 — Connect tasks, sessions, intelligence, and runtime (L)

As a local operator, I want session evidence and usage attributed to tasks and
assignments delivered to adapters so that the board reflects real execution.

Given an assignment event, then the dispatcher starts the configured adapter.
Given an existing session, then a link proposal carries confidence and evidence.
Given the experimental transition flag is disabled, inference cannot move the
ticket. Given linked sessions, metrics reconcile to existing usage totals.

Files: task/server/run integration, CLI/MCP surface, task metrics, inference.
Tests: integration and idempotency. Dependencies: S1, S2, S3.

## S6 — Validate and harden v1 (M)

As a maintainer, I want the complete control plane tested and documented so
that existing AgentsView behavior remains stable.

Given the integrated feature, focused Go tests, `go vet`, frontend check/tests,
and build pass. Given a 24-hour-equivalent idle test, then no launch or model
request occurs. Given a large archive, task event work remains batch-bounded.

Dependencies: S1-S5.

