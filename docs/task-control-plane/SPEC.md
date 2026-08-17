# AgentsView Task Control Plane v1

Status: implementation-approved  
Date: 2026-08-17

## Product intent

Add a thin, local-first agent control plane to AgentsView. The board is the
source of work, while existing agent sessions are the evidence of execution.
The product starts agents only in response to explicit events and never wakes
them on a timer.

The primary user is one developer operating multiple coding-agent harnesses on
one machine. The user needs to assign work, see what each agent is doing, and
compare cost and delivery behavior by task type and execution phase without
injecting a large orchestration prompt into every session.

## Product principles

- Local by default: one machine, one user, no account or hosted dependency.
- Event driven: assignment, dependency clearance, mention, and retry may start
  work; periodic agent heartbeats may not.
- Compact context: start with a small task envelope and fetch detail on demand.
- Evidence over self-report: explicit agent events are authoritative; session
  inference fills gaps and always retains supporting evidence.
- Stable analytics: classify a task at creation, lock its type when work starts,
  and retain manual corrections in history.
- Agent-owned execution: the implementing agent advances its own ticket through
  `Understand`, `Plan`, `Execute`, and `Verify`; the coordinator may perform only
  the gated `Verify`/`Review` to `Done` transition.
- Thin control plane: no companies, org charts, budgets, training studio,
  recurring schedules, multi-user governance, or autonomous management chain.

## Default model

Projects receive a default workflow with `Backlog`, `Ready`, `In Progress`,
`Blocked`, `Review`, and `Done`. A project may rename, reorder, add, or remove
columns while retaining exactly one terminal completion state.

Task type defaults are `feature`, `bug`, `research`, `maintenance`, `review`,
and `documentation`. Users may add types. Automatic classification selects one
type at creation and becomes stable when the first run starts.

Execution phase is independent from board status and uses `Understand`, `Plan`,
`Execute`, `Verify`, and `Deliver`. Progress is represented by current phase,
completed gates, recent activity, and cited evidence; v1 does not display an
estimated percentage. An implementing agent must leave completed work in
`Review`/`Verify`. A coordinator changes it to `Done`/`Deliver` only after the
configured completion gates pass.

## Functional requirements

### FR-001 — Kanban task management

The user can create, edit, reorder, filter, and transition tickets on a
project-scoped Kanban board. Activating a ticket opens a stable detail route
with its fields, gates, evidence, activity, session links, and task timings.

Acceptance: Given a project workflow, when a ticket transition is valid, then
the board persists it, records an event, and updates connected clients. When it
is invalid, the API returns a typed conflict without modifying the ticket.
Given a visible card, pointer or keyboard activation navigates to that ticket's
detail route without changing its workflow state.

### FR-002 — Project workflows

Every project starts with the default workflow and may customize its columns
and allowed transitions.

Acceptance: Given a customized workflow, when the board is reloaded, then its
column order and transition rules are unchanged.

### FR-003 — Agent registry and assignment

The user can register an agent with a name, harness, adapter configuration, and
enabled state, then assign one agent to a ticket. V1 permits one assignee and
one active run per ticket.

Acceptance: Given an enabled agent and an unowned ticket, when the agent is
assigned, then a durable assignment event is created exactly once. Duplicate
delivery may not create a second active run.

### FR-004 — Managed and observed agents

Managed agents are launched by the application. Independently launched agents
are discovered through AgentsView session ingestion and may be linked to a
ticket manually or through a confirmed inference.

Acceptance: Given an unlinked discovered session, when association inference
finds a candidate, then the UI shows the candidate and evidence without
silently creating a ticket or changing a task.

### FR-005 — Harness adapters

Adapters declare capabilities for launch, resume, cancel, and observe. V1 ships
command definitions for Claude Code, Codex, Antigravity/agy, Pi, Hermes, and
DSH, and supports external adapters through a versioned JSON-lines protocol.

Acceptance: Given a compatible adapter, when an assignment event is delivered,
then the adapter receives the same normalized task envelope and emits the same
normalized lifecycle events regardless of harness.

### FR-006 — Event-driven execution

The runtime reacts to assignment, dependency-cleared, mention, and retry
events. No timer may invoke an agent merely to check for work.

Acceptance: Given no task event, then an idle daemon performs no model request
and starts no agent process.

### FR-007 — Task worktrees

Every managed run uses a task-specific Git worktree. The path is validated to
remain under the configured worktree root.

Acceptance: Given two managed tasks for the same repository, when both run,
then they have different worktree paths and cannot claim the same active task
run lease.

### FR-008 — Compact agent surface

The canonical local daemon exposes REST and SSE. A structured CLI and MCP
surface act as thin clients. Agents initially receive task identity, objective,
current phase, acceptance criteria, gate summary, and references—not the full
ticket or session history.

Acceptance: Given a task with a long history, when a run starts, then history is
absent from the initial envelope and can be fetched explicitly by reference.

### FR-009 — Progress intelligence

Explicit phase and progress events win over inferred state. When explicit
events are absent, the system may infer task phase, progress evidence, and
session association from transcript, tool, repository, branch, and working
directory signals, retaining a confidence score and evidence references.

Acceptance: Given conflicting explicit and inferred phases, then the explicit
phase is displayed as current and the inference remains available as evidence.

### FR-010 — Experimental inferred transitions

Inference-driven Kanban transitions are controlled by a feature flag that is
off by default. When enabled, both a workflow permission and a configured
confidence threshold are required.

Acceptance: Given the flag is off, no inference may mutate task status. Given
the flag is on but the workflow disallows the transition, status remains
unchanged.

### FR-011 — Completion gates

An implementing agent advances and records its own execution phases through
`Verify`, but it does not bypass the final workflow authority. Only the
coordinator may transition `Review`/`Verify` to `Done`/`Deliver`, and only when
all configured deterministic, human-approval, and LLM-judge gates pass. LLM
gates use an optional adapter and retain their verdict evidence.

Acceptance: Given one failed or pending required gate, when final completion is
requested, then the task remains in `Review`/`Verify` and the unmet gates are
returned. Given an implementing agent, any direct request for `Done` is denied.
With all gates passed, the coordinator's transition succeeds atomically.

### FR-012 — Task analytics

Metrics are filterable by project, task type, phase, agent, and harness. V1
reports throughput, lead time, active time, blocked time, phase duration,
completion rate, retry count, input/output/cache tokens, estimated cost, peak
context, and linked-session count. The UI exposes an overall metrics route with
filters for project, status, phase, task type, assignee, harness, and time range;
the active filter state is visible and resettable.

Acceptance: Given linked sessions and task events, when metrics are queried by
task type and phase, then every aggregate is derived from durable timestamps or
existing usage records and exposes the contributing task count. Empty and
filtered result sets remain valid responses and identify the applied filters.

### FR-013 — Live audit trail

Every assignment, transition, phase change, gate result, run lifecycle change,
and session link is append-only and visible on the ticket.

Acceptance: Given a task mutation, when the ticket is fetched, then the actor,
source, timestamp, previous value, new value, and evidence reference are
available when applicable.

## Non-functional requirements

- Idle operation starts zero model calls and zero harness processes.
- REST list and board queries complete within 200 ms at p95 for 10,000 tasks on
  a developer laptop, verified by a local benchmark.
- A filesystem/session event performs work proportional to the changed batch,
  not the full session or task archive.
- Duplicate trigger delivery is idempotent and produces at most one active run
  per task.
- All process arguments, worktree paths, task IDs, and external adapter frames
  are validated before use.
- Existing AgentsView session ingestion and analytics remain functional and do
  not require a task database.
- Task data survives daemon restarts through a separate SQLite database.

## Explicitly out of scope for v1

- Multiple simultaneous assignees or cooperating agent teams.
- Multiple users, remote machines, RBAC, authentication, or hosted control.
- Recurring schedules, cron, periodic heartbeats, or autonomous managers.
- Budgets, org charts, goals hierarchy, secrets management, and approvals
  beyond task completion gates.
- Automatic inferred ticket creation.
- Inference-driven status transitions enabled by default.

## Initial success criteria

- A user can create, assign, observe, gate, and complete a managed ticket from
  the board using at least Claude Code and Codex adapters.
- The same board can link and explain one independently launched session.
- A 24-hour idle run launches no agents and makes no model requests.
- Task-type and task-phase analytics reconcile to linked session usage totals.
