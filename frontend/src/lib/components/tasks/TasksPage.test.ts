// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";
import { render, screen, waitFor } from "@testing-library/svelte";
import TasksPage from "./TasksPage.svelte";

const mocks = vi.hoisted(() => ({
  fetchTasks: vi.fn(), fetchTaskAgents: vi.fn(), fetchTaskWorkflow: vi.fn(),
  createTask: vi.fn(), createTaskAgent: vi.fn(), putTaskWorkflow: vi.fn(), updateTask: vi.fn(),
  listener: null as null | ((event: { scope: string }) => void),
  watchEvents: vi.fn((listener: (event: { scope: string }) => void) => {
    mocks.listener = listener;
    return { close() {} };
  }),
}));

vi.mock("../../api/tasks.js", () => mocks);
vi.mock("../../api/client.js", () => ({ watchEvents: mocks.watchEvents }));

class ResizeObserverMock { observe() {} disconnect() {} }
const workflow = {
  project: "default", automatic_transitions_enabled: false,
  columns: [
    { id: "Backlog", label: "Backlog", position: 0 }, { id: "Ready", label: "Ready", position: 1 },
    { id: "In Progress", label: "In Progress", position: 2 }, { id: "Blocked", label: "Blocked", position: 3 },
    { id: "Review", label: "Review", position: 4 }, { id: "Done", label: "Done", position: 5 },
  ],
};
function task(summary: string, lastActivity: string) {
  return {
    id: "task-1", title: "Live task", project: "default", type: "feature", status: "In Progress", phase: "execute",
    session_summary: summary, last_activity_at: lastActivity, evidence: [], gates: [], session_links: [],
    created_at: "2026-08-17T09:00:00Z", updated_at: lastActivity,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.listener = null;
  Object.defineProperty(globalThis, "ResizeObserver", { configurable: true, value: ResizeObserverMock });
  window.history.replaceState(null, "", "/tasks");
  window.dispatchEvent(new PopStateEvent("popstate"));
  mocks.fetchTaskAgents.mockResolvedValue([]);
  mocks.fetchTaskWorkflow.mockResolvedValue(workflow);
  mocks.fetchTasks
    .mockResolvedValueOnce([task("Understanding the task", "2026-08-17T09:00:00Z")])
    .mockResolvedValueOnce([task("Running focused checks", "2026-08-17T10:00:00Z")]);
});

describe("TasksPage live updates", () => {
  it("refreshes card progress when the tasks SSE scope fires", async () => {
    render(TasksPage);
    await waitFor(() => expect(screen.getByText("Understanding the task")).toBeTruthy());
    mocks.listener?.({ scope: "tasks" });
    await waitFor(() => expect(screen.getByText("Running focused checks")).toBeTruthy());
    expect(screen.queryByText("Understanding the task")).toBeNull();
    expect(mocks.fetchTasks).toHaveBeenCalledTimes(2);
  });
});
