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
  aggregate metrics routes, including accessibility and localization.

These tickets count toward reliability only if their event timelines show the
implementing agent performing every pre-completion transition. The coordinator
will evaluate evidence and gates before moving them to `Done`.
