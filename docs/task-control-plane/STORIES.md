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

## S7 — Activate managed runtime from the daemon (M)

As a local operator, I want an explicit daemon option for managed execution so
that assignments can launch agents while default idle operation remains inert.

Given the option is disabled, no runtime, worktree, or harness process is
created. Given valid repository/worktree configuration and an assignment event,
the selected built-in adapter starts with compact lifecycle instructions.

Dependencies: S3, S5.

## S8 — Expose task detail and aggregate metrics (M)

As a local operator, I want bounded detail and metrics queries so that ticket
and portfolio views are derived from durable task evidence.

Given task filters, aggregate counts and timings identify applied filters and
contributing tasks. Given a ticket, its detail supplies gates, events, session
links, run state, and timing without copying unbounded session transcripts.

Dependencies: S1, S2.

## S9 — Add ticket detail, live session, and metrics dashboard views (M)

As a local operator, I want to open a ticket, inspect the working agent session,
and filter overall task metrics so that I can understand both current execution
and delivery performance.

Given a card, pointer or keyboard activation opens `/tasks/{id}`. Given an
active or linked session, the detail view shows bounded live activity and links
to the full session. Given `/tasks/metrics`, filters update the aggregates and
can be cleared, including valid empty/loading/error states.

Dependencies: S4, S8.
