# Task Control Plane Alignment Report

## Declared intent

Target personas are a developer managing multiple local coding agents and the
agents consuming assigned work. The required surfaces are a live Kanban board,
agent management, a local REST/SSE API, structured CLI and MCP clients, and
harness adapters for Claude Code, Codex, Antigravity/agy, Pi, Hermes, and DSH.

Out-of-box success means a user can create a task, assign an agent, observe its
phase and session evidence, satisfy programmatic completion gates, and complete
the task without a periodic heartbeat or a large injected context. The product
is considered aligned only after it can reliably operate its own implementation
tasks through this path.

## Inventory before iteration 1 — 2026-08-17

- Existing AgentsView: local Go daemon/CLI, SQLite session archive, provider
  parsers, REST/SSE API, Svelte UI, session search, usage, and analytics.
- Planned control plane: separate task store, workflow/gate API, Kanban page,
  event-triggered adapter runtime, compact context envelope, task/session links,
  and task facets.
- Development surface: backend and frontend live-reload processes with the
  frontend proxying API requests to the local daemon.

## Iteration 1 — implementation candidate

### Initial gaps

| # | Gap | Severity | Evidence |
|---|---|---|---|
| 1 | No durable task/workflow API exists in the baseline. | HIGH | `internal/server/` and `internal/db/` inventory before implementation |
| 2 | No Kanban or agent-assignment UI exists in the baseline. | HIGH | `frontend/src/App.svelte` route inventory before implementation |
| 3 | No event-driven harness launch contract exists. | HIGH | No baseline `internal/taskrun` package |
| 4 | No task-aware CLI or MCP surface exists. | HIGH | Baseline `internal/mcp` exposes session retrieval tools only |
| 5 | Session analytics are not attributable by task type or phase. | MEDIUM | Existing analytics filters sessions, projects, agents, and models |

### Wins to preserve

- Local-only operation and a mature provider/session ingestion surface.
- SQLite durability, live SSE refresh, and structured HTTP contracts.
- Dense keyboard-oriented UI and established localization/testing conventions.
- Read-only session intelligence remains useful even when task control is off.

### Actions in progress

- Implement the three HIGH load-bearing slices in isolated worktrees.
- Integrate and run a hot-reload preview.
- Create and complete real implementation tickets using the board.

### Queued evidence

- Record exact dogfood tasks, transitions, agent assignments, gate results, and
  failures after the first integrated build.
- Measure whether idle operation launches any process or model request.
- Verify task-type and phase metrics against linked session usage.

## Iteration 2 — agent-owned lifecycle and metrics — 2026-08-17

### User-directed corrections

- The implementing agent, not the coordinator, owns every transition through
  `Understand`, `Plan`, `Execute`, and `Verify`.
- The coordinator owns only the gated `Review`/`Verify` to `Done` transition.
- Tickets require a navigable detail view, and task metrics require a dedicated
  aggregate dashboard with filtering.
- Ticket detail must show the actual active or linked agent session and recent
  bounded activity so the operator can see what the assignee is doing.

### Evidence and gaps

| # | Observation | Severity | Evidence |
|---|---|---|---|
| 1 | The first CLI/MCP dogfood ticket was bootstrapped after implementation and therefore does not prove an agent-owned lifecycle. | HIGH | Live task `task-cli-mcp`; user correction on 2026-08-17 |
| 2 | Storage, board, CLI/MCP, adapter runtime, and runtime dispatch slices are integrated. | WIN | Integrated commits `710c34ba`, `ae94fdcf`, `cede7194`, `7bb18a3a`, `bfece031` |
| 3 | The daemon has runtime plumbing but does not yet opt in to managed execution from its real startup command. | HIGH | `server.WithTaskRuntime` exists; default daemon wiring remains storage-only |
| 4 | The board cards have no ticket detail route or aggregate metrics route. | HIGH | Live `/tasks` preview and frontend route inventory |

### Active dogfood tickets

- `task-runtime-activation`: assigned at `Ready`/`Understand`; its agent must
  drive the full pre-completion lifecycle and leave it at `Review`/`Verify`.
- `task-metrics-api`: assigned at `Ready`/`Understand` with the same ownership
  rule and API/test evidence requirement.
- `task-ticket-views`: assigned at `Ready`/`Understand` for detail and filtered
  aggregate metrics routes, live agent-session activity, accessibility, and
  localization.

These tickets count toward reliability only if their event timelines show the
implementing agent performing every pre-completion transition. The coordinator
will evaluate evidence and gates before moving them to `Done`.

## Iteration 3 — QA closure and the missing-session root cause — 2026-08-17

### QA evidence

| # | Check | Result |
|---|---|---|
| 1 | Focused Go tests: `internal/taskcontrol`, `internal/taskrun`, `internal/server`, `cmd/agentsview` | 3311 passed, 0 failed |
| 2 | `go vet -tags fts5 ./...` (CGO_ENABLED=1) | Clean |
| 3 | Frontend: svelte-check, kit-ui-check, unit tests, production build | 0/0 errors, 2355/2355 tests, build succeeded |
| 4 | Full integrated Go suite | 10848 passed, 48 packages, 0 failed |
| 5 | `internal/remotesync/TestPreparedHTTPSyncRebuildContributor` flake | Rerun clean; did not recur |

`task-runtime-activation`, `task-metrics-api`, and `task-ticket-views` were
each evaluated against concrete commit/test evidence and moved to
`Done`/`Deliver`.

### Root cause: no task ever shows an agent session

The user reported the ticket-detail Agent Session panel stays empty for
in-progress tasks. This is not a frontend defect. `RunCoordinator.
persistSessionLink` (`internal/taskcontrol/run_coordinator.go:228`) is only
reachable from `persistEvent`, which only fires when the managed runtime
dispatches a real `taskrun.Event` off a live spawned process. The daemon's
`task_runtime.enabled` has never been turned on in this session, no worktree
has ever been created under a `worktree_root`, and no run has ever dispatched
an event — confirmed by inspecting `/api/v1/tasks/task-runtime-integration`,
whose `agent_session.active` is `false` and `session_links` is empty despite
the task showing `In Progress`. Every task's session data is empty because
none has ever been populated, not because it fails to render.

The fix is not a UI patch. It is the same decisive step called out at the top
of this file's prior handoff: enable `[task_runtime]` in the daemon config
and let it spawn one real agent process against an assigned ticket.

### Shipped this iteration

- Kanban `TaskCard` now pulses a blue border/glow when a task has an active
  session link (`activeSessions.length > 0`), respecting
  `prefers-reduced-motion`. Verified live via a temporary DOM class toggle in
  the browser (no real session data was created or touched). It will start
  animating for real once the managed runtime links a live session.

### Still queued

- Enable the managed runtime opt-in config and fire the decisive dogfood run:
  one ticket, one implementing agent, self-driven Understand → Verify, with
  provenance-checked event actors and real `deliverable.attached` evidence.
- `task-runtime-integration` remains stale at `In Progress`/`Execute` with no
  worktree and no run ever created — reconcile once the runtime is live.
- `task-deliverable-attachments` implementation has not started.
