# Managed task runtime

AgentsView can launch a configured task assignee when a task is assigned or an
explicit dependency-cleared, mention, or retry event arrives. The runtime is
event-driven: it has no agent heartbeat, polling loop, or periodic wake-up.

The runtime is disabled by default. Enable it in `config.toml` with absolute,
non-overlapping repository and worktree locations:

```toml
[task_runtime]
enabled = true
repository = "/absolute/path/to/repository"
worktree_root = "/absolute/path/to/task-worktrees"
ref = "HEAD"
```

`worktree_root` defaults to `data_dir/task-worktrees`, and `ref` defaults to
`HEAD`. The repository and worktree root must not contain one another. No
worktree is created and no harness process starts until an assigned task emits
an execution trigger.

For a foreground or one-off background serve, the equivalent flags are:

```text
--task-runtime
--task-runtime-repository=/absolute/path/to/repository
--task-runtime-worktree-root=/absolute/path/to/task-worktrees
--task-runtime-ref=HEAD
```

The daemon registers the built-in `claude`, `codex`, `agy`, `antigravity`,
`pi`, `hermes`, and `dsh` adapters. An agent's `harness` must match one of those
IDs, and its executable must be available on the daemon's `PATH`.

Managed agents receive a compact task envelope rather than full task history.
It instructs them to use the task CLI or MCP tools, record structured progress,
self-transition through Understand, Plan, and Execute, then move to Review with
phase Verify after checks pass. The final transition to Done remains outside
the managed run.

Each dispatch stores a stable `active_run_id`. When a harness exposes a
structured `session_id`, `thread_id`, or `conversation_id`, the runtime also
creates a task session link so the task can open the existing session view.
Normalized phase, progress, activity, and terminal events carry the run ID and
the session ID when known. Activity labels are compact structural summaries;
raw harness output is not copied into task history and remains in the existing
session archive.
