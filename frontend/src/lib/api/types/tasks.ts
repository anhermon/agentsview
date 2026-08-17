export type TaskStatus =
  | "backlog"
  | "ready"
  | "in_progress"
  | "blocked"
  | "review"
  | "done"
  | string;

export type TaskPhase =
  | "understand"
  | "plan"
  | "execute"
  | "verify"
  | "deliver"
  | string;

export type TaskGateStatus = "pending" | "passed" | "failed" | "waived";

export interface TaskGate {
  id: string;
  task_id?: string;
  name: string;
  kind?: "deterministic" | "human" | "llm";
  rule?: string | null;
  config?: Record<string, unknown> | null;
  status: TaskGateStatus;
  evidence?: unknown;
  required?: boolean;
  sort_order?: number;
  evaluated_at?: string | null;
}

export interface TaskSessionLink {
  id: string;
  task_id: string;
  session_id: string;
  harness?: string | null;
  method: string;
  confidence: number;
  active: boolean;
  created_at: string;
}

export interface Task {
  id: string;
  title: string;
  description?: string | null;
  project: string;
  type: string;
  status: TaskStatus;
  phase: TaskPhase;
  assignee_id?: string | null;
  assignee_name?: string | null;
  harness?: string | null;
  last_activity_at?: string | null;
  session_summary?: string | null;
  evidence?: unknown[];
  gates?: TaskGate[];
  session_links?: TaskSessionLink[];
  created_at: string;
  updated_at: string;
}

export interface CreateTaskRequest {
  title: string;
  description?: string;
  project: string;
  type: string;
  status?: TaskStatus;
}

export type UpdateTaskRequest = Partial<
  Pick<
    Task,
    | "title"
    | "description"
    | "project"
    | "type"
    | "status"
    | "phase"
    | "assignee_id"
  >
>;

export type TaskAgentStatus = "available" | "working" | "offline" | string;

export interface TaskAgent {
  id: string;
  name: string;
  harness: string;
  status: TaskAgentStatus;
  session_id?: string | null;
  current_task_id?: string | null;
}

export interface CreateTaskAgentRequest {
  name: string;
  harness: string;
}

export interface TaskWorkflowColumn {
  id: string;
  label: string;
  position: number;
  wip_limit?: number | null;
}

export interface TaskWorkflow {
  project: string;
  columns: TaskWorkflowColumn[];
  automatic_transitions_enabled: boolean;
  phases?: string[];
}
