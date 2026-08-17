// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";
import { render, screen, waitFor } from "@testing-library/svelte";
import TaskDetailPage from "./TaskDetailPage.svelte";

const mocks = vi.hoisted(() => ({
  fetchTask: vi.fn(),
  fetchTaskSessionPreview: vi.fn(),
  watchEvents: vi.fn(() => ({ close() {} })),
}));

vi.mock("../../api/tasks.js", () => ({
  fetchTask: mocks.fetchTask,
  fetchTaskSessionPreview: mocks.fetchTaskSessionPreview,
}));

vi.mock("../../api/client.js", () => ({ watchEvents: mocks.watchEvents }));

beforeEach(() => {
  vi.clearAllMocks();
  mocks.watchEvents.mockReturnValue({ close() {} });
  mocks.fetchTask.mockResolvedValue({
    id: "task-1",
    title: "Ship ticket views",
    description: "Make task work inspectable.",
    project: "agentsview",
    type: "feature",
    status: "In Progress",
    phase: "Execute",
    assignee_name: "Codex",
    harness: "codex",
    created_at: "2026-08-17T09:00:00Z",
    updated_at: "2026-08-17T10:00:00Z",
    evidence: [{ summary: "Focused tests pass" }],
    gates: [{ id: "g1", name: "Frontend checks", kind: "deterministic", status: "passed", required: true }],
    gate_summary: { total: 1, required: 1, passed: 1, failed: 0, pending: 0, completion_ready: true },
    events: [{ id: 1, task_id: "task-1", type: "agent.progress", source: "codex", payload: { summary: "Implementing detail view" }, created_at: "2026-08-17T10:00:00Z" }],
    timing: { lead_time_ms: 3_600_000, cycle_time_ms: 1_800_000, phase_durations: [{ phase: "Execute", total_ms: 1_800_000 }] },
    session_links: [],
    agent_session: {
      agent_id: "agent-1", agent_name: "Codex", harness: "codex", run_id: "run-1",
      active: true, run_state: "running", phase: "Execute", last_activity_at: "2026-08-17T10:00:00Z",
      recent_activity: [],
      links: [{ session_id: "session-1", harness: "codex", active: true, detail_api_url: "/api/v1/sessions/session-1", activity_api_url: "/api/v1/sessions/session-1/activity", recent_messages_api_url: "/api/v1/sessions/session-1/messages?limit=20&direction=desc", full_session_url: "/sessions/session-1" }],
    },
  });
  mocks.fetchTaskSessionPreview.mockResolvedValue([
    { id: 7, ordinal: 7, role: "assistant", content: "Running the focused frontend checks", timestamp: "2026-08-17T10:00:00Z" },
  ]);
});

describe("TaskDetailPage", () => {
  it("shows ticket evidence, timing, and the bounded live agent session", async () => {
    render(TaskDetailPage, { taskId: "task-1" });

    await waitFor(() => expect(screen.getByRole("heading", { name: "Ship ticket views" })).toBeTruthy());
    expect(screen.getByRole("heading", { name: "Agent session" })).toBeTruthy();
    expect(screen.getByText("Running the focused frontend checks")).toBeTruthy();
    expect(screen.getByRole("link", { name: /Open full session/ }).getAttribute("href")).toBe("/sessions/session-1");
    expect(screen.getByText("Focused tests pass")).toBeTruthy();
    expect(mocks.fetchTaskSessionPreview).toHaveBeenCalledWith("/api/v1/sessions/session-1/messages?limit=20&direction=desc");
  });
});
