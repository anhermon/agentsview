// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";
import { fireEvent, render, screen } from "@testing-library/svelte";
import { tick } from "svelte";
import TaskCard from "./TaskCard.svelte";
import type { Task, TaskAgent } from "../../api/types/tasks.js";

class ResizeObserverMock {
  observe = vi.fn();
  disconnect = vi.fn();
}

const task: Task = {
  id: "task-1",
  title: "Ship event-driven board",
  project: "agentsview",
  type: "feature",
  status: "in_progress",
  phase: "execute",
  assignee_id: "agent-1",
  assignee_name: "Codex",
  harness: "codex",
  last_activity_at: "2026-08-17T10:00:00Z",
  evidence: [{ summary: "Focused checks pass" }],
  gates: [
    { id: "gate-1", name: "Frontend tests", status: "passed" },
    { id: "gate-2", name: "Live dogfood", status: "failed" },
  ],
  session_links: [{
    id: "link-1",
    task_id: "task-1",
    session_id: "session-1",
    harness: "codex",
    method: "explicit",
    confidence: 1,
    active: true,
    created_at: "2026-08-17T10:00:00Z",
  }],
  created_at: "2026-08-17T09:00:00Z",
  updated_at: "2026-08-17T10:00:00Z",
};

const agents: TaskAgent[] = [{
  id: "agent-1",
  name: "Codex",
  harness: "codex",
  status: "working",
  current_task_id: "task-1",
}];

beforeEach(() => {
  Object.defineProperty(globalThis, "ResizeObserver", {
    configurable: true,
    value: ResizeObserverMock,
  });
});

describe("TaskCard", () => {
  it("shows task intelligence and exposes an accessible status transition", async () => {
    const onmove = vi.fn();
    render(TaskCard, {
      task,
      agents,
      previousStatus: "ready",
      nextStatus: "review",
      onmove,
      onphase: vi.fn(),
      onassign: vi.fn(),
      ondragstart: vi.fn(),
    });

    expect(screen.getByText("Ship event-driven board")).toBeTruthy();
    expect(screen.getAllByText("Execute").length).toBeGreaterThan(0);
    expect(screen.getByText("Focused checks pass")).toBeTruthy();
    expect(screen.getByText(/Live dogfood/)).toBeTruthy();
    expect(screen.getByText("1 active session")).toBeTruthy();

    await fireEvent.click(screen.getByRole("button", { name: "Move task to next column" }));
    expect(onmove).toHaveBeenCalledWith("review");
  });

  it("lets the operator change the universal task phase", async () => {
    const onphase = vi.fn();
    render(TaskCard, {
      task,
      agents,
      nextStatus: "review",
      onmove: vi.fn(),
      onphase,
      onassign: vi.fn(),
      ondragstart: vi.fn(),
    });

    await fireEvent.click(screen.getByRole("button", { name: "Select phase" }));
    await tick();
    await fireEvent.mouseDown(screen.getByRole("option", { name: "Verify" }));
    expect(onphase).toHaveBeenCalledWith("verify");
  });
});
