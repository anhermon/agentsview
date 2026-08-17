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

**Correction:** "evaluated against concrete evidence" overstates what the gate
mechanism actually did. `defaultTaskGateEvaluator`
(`internal/server/huma_routes_tasks.go:694`) never reads `gate.Rule` or
`gate.Config` — it only trusts a caller-supplied `evidence.passed` /
`approved` boolean, and `WithTaskGateEvaluator`
(`internal/server/server.go:320`) is never called from the real daemon
startup path, only from tests. So every gate on the live daemon, `deterministic`
label notwithstanding, passes on self-attestation with no independent re-check.
The QA fork's test runs were real, but the gate pass itself proves only that
the fork asserted `passed: true` after running them — the same honor-system
path this project already rejected for `task-cli-mcp`. See Iteration 4 for the
full finding and the gate-enforcement ticket it produced.

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

## Iteration 4 — gates are self-attested, not deterministic — 2026-08-17

### Finding

The user asked whether `Verify` actually works, or whether a human still has
to find bugs in closed tickets. Traced the live gate-evaluate path:
`defaultTaskGateEvaluator` (`internal/server/huma_routes_tasks.go:694`) reads
only `input.Evidence["passed"]` / `input.Approved` — it never reads
`gate.Rule` or `gate.Config`. `WithTaskGateEvaluator`
(`internal/server/server.go:320`) exists as an extension point but is never
called from the real daemon startup, only from tests. So a gate labeled
`kind: "deterministic"` enforces nothing: it passes because whoever calls
`POST /tasks/{id}/gates/{gateId}/evaluate` asserts a boolean. Existing gates
do store a runnable-looking `rule` (e.g. `go test ./... && go vet ...`), so
the schema anticipated real enforcement — it was never implemented.

This means the three tickets closed in Iteration 3 — and `task-cli-mcp`
before them — all passed gates on self-attestation. The underlying test runs
were real and are recorded in each gate's `evidence` field, but the gate
mechanism itself did not independently verify them. Corrected the Iteration 3
claim above rather than leaving a false green in the record.

### Shipped this iteration

- Ticket-detail completion gates now render `rule`, raw `evidence`, and
  `evaluated_at` per gate (previously only name/kind/status showed), plus a
  per-ticket count of `Verify → Execute` phase reworks derived from existing
  `task.updated` events. This makes the self-attestation gap visible in the
  UI instead of hidden behind a plain "passed" chip.
- Task metrics: phase-timing rows now carry the same relative bars as the
  other distribution panels; the date-range filter no longer defaults to a
  meaningless rolling 30-day window on a same-day dataset (defaults to
  "All time").

### New ticket: gate enforcement (S11)

Filed `task_41ed1720fc3a1e59d11bc2c3` — "Make deterministic gates actually
deterministic," priority 0, outranking `task-deliverable-attachments` (an
evidence *delivery* mechanism is pointless in front of a gate system that
doesn't check evidence). Contains an explicit, unresolved design question for
the user: execute `gate.Rule` as a server-side shell command (real but a
command-injection surface in a daemon that has none today), or require a
verifiable artifact reference the evaluator checks without executing
anything (safer, but depends on `task-deliverable-attachments` landing first
for durable evidence storage). Not implemented pending that answer.

## Iteration 5 — deterministic gates now execute their rule — 2026-08-17

### User decision

Given both options, the user chose (1): execute `gate.Rule` as a server-side
shell command and gate on its exit code.

### Shipped

- `internal/server/task_gate_evaluator.go`: `ruleGateEvaluator` runs a
  deterministic gate's `Rule` via `sh -c` with a 5-minute timeout, in the
  task's managed-runtime worktree if one has been prepared
  (`taskrun.ResolveWorktreePath`), else the configured repository root.
  Status is set from the real exit code; evidence records `rule`, `exit_code`,
  `duration_ms`, `dir_source`, and captured (truncated) `output`. Non-
  deterministic gate kinds (`human`, `llm`) and deterministic gates with no
  `rule` fall back to the prior caller-asserted behavior, since the daemon
  cannot execute human or model judgment.
- `cmd/agentsview/task_runtime.go`: split repository/worktree validation out
  of `resolveManagedTaskRuntimeConfig` into `validateTaskRuntimeConfig`, and
  added `resolveGateRuleRepository` / `taskGateEvaluatorOption`, which wire
  the real evaluator whenever `task_runtime.repository` is configured --
  independent of `task_runtime.enabled`, since gate evaluation happens
  through the CLI/API whether or not the daemon spawns agents. An
  unconfigured daemon keeps the prior trust-based fallback.
- Wired in `cmd/agentsview/main.go` next to the existing managed-runtime
  option. Set `[task_runtime] repository` in the local `config.toml`
  (`~/.agentsview/config.toml`, not tracked in the repo) to this checkout;
  left `enabled` unset (default `false`) so the agent-spawning runtime stays
  inert, per the standing "default-off must remain inert" rule.
- Tests: `internal/server/task_gate_evaluator_test.go` (execution result,
  output capture, worktree-vs-repository preference, timeout, non-
  deterministic/ruleless fallback -- 6 cases) and additions to
  `cmd/agentsview/task_runtime_test.go` for the new resolve/option functions.
  Focused packages: 3325 passed. `go vet -tags fts5 ./...`: clean.

### Live end-to-end verification (not just tests)

Against the actual running daemon (first a throwaway instance, then the
persistent hot-reload one after restarting it with the new config), created
two gates on `task-release-validation` and evaluated each with a dishonest
caller assertion:

- Rule `exit 1`, caller asserted `approved: true` / `evidence.passed: true`
  -> API returned `status: "failed"`, `evidence.exit_code: 1`.
- Rule `echo verify-proof && exit 0`, caller asserted `approved: false` /
  `evidence.passed: false` -> API returned `status: "passed"`,
  `evidence.output: "verify-proof\n"`.

The real exit code won in both directions; the caller's claim had no effect.
Left the three verification gates (`gate-eval-verify-should-fail`,
`gate-eval-verify-should-pass`, `gate-eval-verify-persistent-daemon`) on
`task-release-validation` as durable proof rather than deleting them --
there is currently no gate-delete endpoint, and manually editing `tasks.db`
while the daemon is live was judged riskier than leaving three harmless,
clearly-named, non-required gates in place.

Filed this against `task_41ed1720fc3a1e59d11bc2c3` (S11) and moved it to
`Done`/`Deliver` with an `agent.evidence` event recording the above.

### Still queued

- Gates evaluated for `task-runtime-activation`, `task-metrics-api`, and
  `task-ticket-views` in Iteration 3 were never re-evaluated under the real
  evaluator; those tickets remain `Done` on their original self-attested
  evidence and are not retroactively re-verified.
- No gate-delete endpoint exists; the three verification gates remain
  visible on `task-release-validation`.
- Enabling the managed runtime (agent-spawning) and the deliverable-
  attachments feature remain unstarted, as recorded in prior iterations.
