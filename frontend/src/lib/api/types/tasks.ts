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

export interface TaskAgentSessionLink {
  session_id: string;
  harness?: string | null;
  active: boolean;
  detail_api_url: string;
  activity_api_url: string;
  recent_messages_api_url: string;
  full_session_url: string;
}

export interface TaskAgentSession {
  agent_id?: string | null;
  agent_name?: string | null;
  harness?: string | null;
  run_id?: string | null;
  active: boolean;
  run_state: string;
  phase?: string | null;
  last_activity_at?: string | null;
  recent_activity: TaskEvent[];
  links: TaskAgentSessionLink[];
}

export interface TaskSessionPreviewMessage {
  id: number;
  ordinal: number;
  role: string;
  content: string;
  timestamp: string;
  has_tool_use?: boolean;
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

export interface TaskEvent {
  id: number;
  task_id: string;
  type: string;
  source: string;
  payload?: Record<string, unknown>;
  created_at: string;
}

export interface TaskGateSummary {
  total: number;
  required: number;
  passed: number;
  failed: number;
  pending: number;
  completion_ready: boolean;
}

export interface TaskPhaseDuration {
  phase: string;
  total_ms: number;
}

export interface TaskTiming {
  started_at?: string | null;
  completed_at?: string | null;
  lead_time_ms?: number | null;
  cycle_time_ms?: number | null;
  phase_durations: TaskPhaseDuration[];
}

export interface TaskDetail extends Task {
  events: TaskEvent[];
  gate_summary: TaskGateSummary;
  timing: TaskTiming;
  active_run_id?: string | null;
  agent_session?: TaskAgentSession | null;
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

export interface TaskMetricFilters {
  project?: string;
  status?: string;
  phase?: string;
  type?: string;
  assignee?: string;
  from?: string;
  to?: string;
}

export interface TaskTimingMetric {
  samples: number;
  total_ms: number;
  average_ms: number;
  min_ms: number;
  max_ms: number;
}

export interface TaskPhaseMetric extends TaskTimingMetric {
  phase: string;
}

export interface TaskMetrics {
  total_tasks: number;
  counts: {
    by_project: Record<string, number>;
    by_status: Record<string, number>;
    by_phase: Record<string, number>;
    by_type: Record<string, number>;
    by_assignee: Record<string, number>;
  };
  timing: {
    lead_time: TaskTimingMetric;
    cycle_time: TaskTimingMetric;
    phase_time: TaskPhaseMetric[];
  };
  gates: TaskGateSummary & { completion_ready_tasks: number };
}
