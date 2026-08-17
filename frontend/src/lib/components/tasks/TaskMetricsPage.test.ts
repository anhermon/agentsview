// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";
import { render, screen, waitFor } from "@testing-library/svelte";
import TaskMetricsPage from "./TaskMetricsPage.svelte";

const mocks = vi.hoisted(() => ({ fetchTaskMetrics: vi.fn() }));
vi.mock("../../api/tasks.js", () => ({ fetchTaskMetrics: mocks.fetchTaskMetrics }));

class ResizeObserverMock { observe() {} disconnect() {} }

beforeEach(() => {
  Object.defineProperty(globalThis, "ResizeObserver", { configurable: true, value: ResizeObserverMock });
  window.history.replaceState(null, "", "/tasks/metrics?project=agentsview&phase=Verify");
  window.dispatchEvent(new PopStateEvent("popstate"));
  mocks.fetchTaskMetrics.mockResolvedValue({
    total_tasks: 4,
    counts: {
      by_project: { agentsview: 4 }, by_status: { Review: 3, Done: 1 },
      by_phase: { Verify: 3, Deliver: 1 }, by_type: { feature: 4 }, by_assignee: { Codex: 4 },
    },
    timing: {
      lead_time: { samples: 2, total_ms: 7_200_000, average_ms: 3_600_000, min_ms: 1_800_000, max_ms: 5_400_000 },
      cycle_time: { samples: 2, total_ms: 3_600_000, average_ms: 1_800_000, min_ms: 900_000, max_ms: 2_700_000 },
      phase_time: [{ phase: "Verify", samples: 2, total_ms: 1_800_000, average_ms: 900_000, min_ms: 600_000, max_ms: 1_200_000 }],
    },
    gates: { total: 4, required: 4, passed: 3, failed: 0, pending: 1, completion_ready_tasks: 3 },
  });
});

describe("TaskMetricsPage", () => {
  it("loads URL-persisted filters and renders distributions and timing", async () => {
    render(TaskMetricsPage);
    await waitFor(() => expect(screen.getByRole("heading", { name: "Task metrics" })).toBeTruthy());
    await waitFor(() => expect(screen.getByText("By status")).toBeTruthy());
    expect(screen.getByText("Phase timing")).toBeTruthy();
    expect(screen.getAllByText("Verify").length).toBeGreaterThan(0);
    expect(mocks.fetchTaskMetrics).toHaveBeenCalledWith(expect.objectContaining({ project: "agentsview", phase: "Verify" }));
    expect(window.location.pathname).toBe("/tasks/metrics");
  });
});
