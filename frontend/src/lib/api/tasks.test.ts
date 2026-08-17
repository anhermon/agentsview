import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import {
  createTask,
  createTaskAgent,
  fetchTaskAgents,
  fetchTasks,
  fetchTaskWorkflow,
  updateTask,
} from "./tasks.js";

afterEach(() => vi.unstubAllGlobals());

function json(value: unknown): Response {
  return new Response(JSON.stringify(value), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

describe("task API", () => {
  it("accepts item envelopes for task and agent collections", async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(json({ items: [{ id: "t1", title: "Ship board" }] }))
      .mockResolvedValueOnce(json({ items: [{ id: "a1", name: "Codex" }] }));
    vi.stubGlobal("fetch", fetchMock);

    await expect(fetchTasks()).resolves.toEqual([{ id: "t1", title: "Ship board" }]);
    await expect(fetchTaskAgents()).resolves.toEqual([{ id: "a1", name: "Codex" }]);
    expect(fetchMock.mock.calls.map((call) => call[0])).toEqual([
      "/api/v1/tasks",
      "/api/v1/task-agents",
    ]);
  });

  it("creates and patches tasks with encoded identifiers", async () => {
    const task = {
      id: "task / 1",
      title: "Ship board",
      project: "agentsview",
      type: "feature",
      status: "backlog",
      phase: "plan",
      created_at: "2026-08-17T00:00:00Z",
      updated_at: "2026-08-17T00:00:00Z",
    };
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(json({ task }))
      .mockResolvedValueOnce(json({ task: { ...task, status: "ready" } }));
    vi.stubGlobal("fetch", fetchMock);

    await createTask({
      title: task.title,
      project: task.project,
      type: task.type,
    });
    await updateTask(task.id, { status: "ready", phase: "execute" });

    expect(fetchMock.mock.calls[0]?.[0]).toBe("/api/v1/tasks");
    expect(fetchMock.mock.calls[0]?.[1]).toEqual(expect.objectContaining({ method: "POST" }));
    expect(fetchMock.mock.calls[1]?.[0]).toBe("/api/v1/tasks/task%20%2F%201");
    expect(fetchMock.mock.calls[1]?.[1]).toEqual(expect.objectContaining({
      method: "PATCH",
      body: JSON.stringify({ status: "ready", phase: "execute" }),
    }));
  });

  it("uses the task-agent and project workflow endpoints", async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(json({ agent: { id: "a1", name: "Codex", harness: "codex" } }))
      .mockResolvedValueOnce(json({ project: "repo / app", columns: [] }));
    vi.stubGlobal("fetch", fetchMock);

    await createTaskAgent({ name: "Codex", harness: "codex" });
    await fetchTaskWorkflow("repo / app");

    expect(fetchMock.mock.calls[0]?.[0]).toBe("/api/v1/task-agents");
    expect(fetchMock.mock.calls[1]?.[0]).toBe("/api/v1/task-workflows/repo%20%2F%20app");
  });
});
