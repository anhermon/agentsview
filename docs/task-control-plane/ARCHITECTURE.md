# Task Control Plane Architecture

## Shape

The implementation extends the existing AgentsView Go daemon and Svelte UI. It
reuses provider parsers, normalized sessions, usage accounting, REST/SSE
transport, and local operational conventions. Task state lives in a separate
`tasks.db`; `sessions.db` remains the session system of record and its remote
mirror contracts are unchanged.

```text
Svelte Kanban / CLI / MCP
          |
      REST + SSE
          |
 task API + workflow service ---- session query interface
          |                              |
       tasks.db                       sessions.db
          |
  durable task events
          |
 event dispatcher -> run coordinator -> harness adapter -> agent process
```

## Components

- `internal/tasks`: local schema, store, workflow validation, completion gates,
  event log, session links, task metrics, and feature settings.
- `internal/server`: task REST handlers registered alongside existing v1 routes;
  mutations emit a `tasks` SSE scope.
- `internal/taskrun`: capability-based adapters, event triggers, compact context
  envelope, worktree resolution, external JSONL protocol, and active-run lease.
- `cmd/agentsview`: daemon wiring plus thin CLI/MCP entry points. The task store
  is optional so existing read-only/archive modes continue to start.
- `frontend/src/lib/components/tasks`: board, columns, cards, task editor,
  assignment controls, gate summary, activity evidence, and settings.
- `frontend/src/lib/api`: typed task client matching the REST schemas.

## Data model

All IDs are opaque strings. Timestamps are UTC RFC3339 at API boundaries and
integer milliseconds internally where existing conventions require them.

- `projects`: task-local project identity and repository/worktree configuration.
- `workflows`: project revision, ordered states, allowed transitions, and the
  terminal state.
- `tasks`: project, title, description, type, status, phase, priority, assignee,
  classification lock, active run, and timestamps.
- `task_types`: built-in/custom type definitions.
- `task_agents`: name, harness, adapter kind/config, capabilities, enabled state.
- `task_runs`: task, agent, adapter, trigger event, session, worktree, state,
  start/end timestamps, and failure summary.
- `task_events`: append-only event name, source, actor, payload, evidence, and
  idempotency key.
- `task_gates`: scope, kind, required flag, configuration, and order.
- `task_gate_results`: gate, run, status, evidence, evaluator, and timestamp.
- `task_session_links`: task, session ID, source, confidence, evidence, and
  confirmation state.
- `task_settings`: project feature flags and inference thresholds.

Schema upgrades are additive and transactionally versioned. No migration may
delete or recreate user task data.

## REST contract

JSON errors use `{ "error": { "code": string, "message": string,
"details"?: object } }`.

- `GET /api/v1/tasks?project=&status=&type=&agent=` lists board tasks.
- `POST /api/v1/tasks` creates a task and classifies it when no type is given.
- `GET /api/v1/tasks/{id}` returns task, gates, links, runs, and event timeline.
  Session link records contain stable session/run identifiers used to query the
  existing bounded session detail/activity APIs rather than copying transcript
  content into `tasks.db`.
- `PATCH /api/v1/tasks/{id}` updates editable fields or requests a transition;
  stale revisions return `409`.
- `POST /api/v1/tasks/{id}/events` accepts normalized agent events with an
  idempotency key.
- `POST /api/v1/tasks/{id}/complete` evaluates gates and conditionally commits
  the terminal transition.
- `GET|POST /api/v1/task-agents` lists or registers agents.
- `PATCH /api/v1/task-agents/{id}` changes configuration or enabled state.
- `GET|PUT /api/v1/task-workflows/{project}` reads or replaces a workflow with
  optimistic revision checking.
- `POST /api/v1/tasks/{id}/sessions` proposes or confirms a session link.
- `GET /api/v1/task-metrics?project=&status=&phase=&type=&assignee=&harness=&from=&to=`
  returns bounded aggregates, dimension buckets, duration summaries, applied
  filters, and contributing counts.
- `GET|PUT /api/v1/task-settings/{project}` controls experimental inference.

Task mutation responses include the current revision. Invalid transitions or
unmet gates return `409`; malformed input returns `400`; missing entities return
`404`; adapter/runtime failures return `502` without losing the triggering
event.

## Event and run model

Durable task events are the queue. Assignment, dependency clearance, mention,
and retry are start-capable event kinds. A dispatcher claims an unprocessed
event transactionally and asks the run coordinator to acquire the task's single
active-run lease. Replayed events use their idempotency key and become no-ops.

The daemon is a lightweight persistent event listener, not a scheduler. It may
watch configured session roots and task DB changes, but it contains no code path
that wakes an agent based only on elapsed time.

Normalized run events include `run.started`, `phase.changed`,
`progress.evidence`, `agent.blocked`, `gate.reported`, `run.completed`,
`run.failed`, and `run.cancelled`. Explicit agent phase events override inferred
phase until a newer explicit event changes them.

Lifecycle authority is deliberately asymmetric. The implementing agent records
and advances `Understand` to `Plan` to `Execute` to `Verify`, then leaves the
ticket in `Review`/`Verify`. The coordinator evaluates completion gates and is
the only actor permitted to commit `Review`/`Verify` to `Done`/`Deliver`. Actor
identity and the rejected transition are retained when this rule is violated.

## Adapter contract

Each adapter declares `launch`, `resume`, `cancel`, and `observe` capabilities.
Built-in command adapters translate the normalized launch request into harness
arguments without shell interpolation. External adapters exchange one JSON
object per line using protocol version `1`; frame size and event kinds are
bounded and unknown versions fail closed.

The launch envelope contains only task identity, objective, phase, acceptance
criteria, gate summary, project/worktree references, and URLs or tool calls for
fetching more context. Secrets and full transcripts are excluded.

## Intelligence

Session association uses repository, worktree, branch, explicit task ID, and
transcript evidence. Inference writes proposals with confidence and evidence;
the user confirms independently discovered links. Task phase inference may be
displayed automatically. Kanban mutation requires the experimental flag,
minimum confidence, and an allowed workflow transition.

Metrics join confirmed task/session links to existing usage aggregates. Task
event intervals provide lead, active, blocked, and phase time. Expensive Git or
LLM-derived outcomes remain opt-in.

## Security and failure handling

- Bind to loopback and preserve AgentsView host validation.
- Treat adapter output and session content as untrusted input.
- Validate worktree containment using resolved paths, never string prefixes.
- Pass process arguments as arrays; do not invoke through a shell.
- Persist a trigger before launch and a failure event if launch fails.
- Keep the board operable when adapters, inference, or the session archive are
  unavailable.
- LLM completion judges are optional and may never receive secrets or full
  transcripts unless a future explicit policy permits it.

## Validation

- Store/workflow tests use temporary task databases and table-driven testify
  assertions.
- Runtime tests cover duplicate triggers, cancellation, malformed frames,
  unsupported versions, and worktree traversal.
- HTTP tests cover CRUD, conflicts, invalid transitions, gates, and SSE scopes.
- Frontend tests cover board grouping, accessible transitions, assignment,
  empty/error states, and localized strings.
- A cardinality test proves event handling does not scan the full archive.
