import {
  ApiError,
  authHeaders,
  getBase,
  responseErrorMessage,
} from "./runtime.js";
import type {
  CreateTaskAgentRequest,
  CreateTaskRequest,
  Task,
  TaskAgent,
  TaskWorkflow,
  UpdateTaskRequest,
} from "./types/tasks.js";

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(
    `${getBase()}${path}`,
    authHeaders({
      ...init,
      headers: {
        "Content-Type": "application/json",
        ...init?.headers,
      },
    }),
  );
  if (!res.ok) {
    throw new ApiError(res.status, await responseErrorMessage(res));
  }
  return (await res.json()) as T;
}

function collection<T>(value: T[] | { items?: T[]; tasks?: T[]; agents?: T[] }): T[] {
  if (Array.isArray(value)) return value;
  return value.items ?? value.tasks ?? value.agents ?? [];
}

function entity<T>(value: T | { task?: T; agent?: T }): T {
  if (value && typeof value === "object") {
    if ("task" in value && value.task) return value.task;
    if ("agent" in value && value.agent) return value.agent;
  }
  return value as T;
}

export async function fetchTasks(): Promise<Task[]> {
  return collection(await request<Task[] | { items?: Task[]; tasks?: Task[] }>("/tasks"));
}

export async function createTask(input: CreateTaskRequest): Promise<Task> {
  return entity(await request<Task | { task: Task }>("/tasks", {
    method: "POST",
    body: JSON.stringify(input),
  }));
}

export async function updateTask(id: string, input: UpdateTaskRequest): Promise<Task> {
  return entity(await request<Task | { task: Task }>(`/tasks/${encodeURIComponent(id)}`, {
    method: "PATCH",
    body: JSON.stringify(input),
  }));
}

export async function fetchTaskAgents(): Promise<TaskAgent[]> {
  return collection(
    await request<TaskAgent[] | { items?: TaskAgent[]; agents?: TaskAgent[] }>("/task-agents"),
  );
}

export async function createTaskAgent(input: CreateTaskAgentRequest): Promise<TaskAgent> {
  return entity(await request<TaskAgent | { agent: TaskAgent }>("/task-agents", {
    method: "POST",
    body: JSON.stringify(input),
  }));
}

export function fetchTaskWorkflow(project: string): Promise<TaskWorkflow> {
  return request(`/task-workflows/${encodeURIComponent(project)}`);
}

export function putTaskWorkflow(
  project: string,
  workflow: TaskWorkflow,
): Promise<TaskWorkflow> {
  return request(`/task-workflows/${encodeURIComponent(project)}`, {
    method: "PUT",
    body: JSON.stringify(workflow),
  });
}
