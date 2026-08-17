package taskcontrol

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound          = errors.New("task control record not found")
	ErrConflict          = errors.New("task control conflict")
	ErrCompletionBlocked = errors.New("task completion gates are not satisfied")
	ErrQueryLimit        = errors.New("task metrics query exceeds the bounded result limit")
)

var DefaultStatuses = []string{
	"Backlog", "Ready", "In Progress", "Blocked", "Review", "Done",
}

var UniversalPhases = []string{
	"Understand", "Plan", "Execute", "Verify", "Deliver",
}

type Workflow struct {
	Project                    string    `json:"project"`
	Statuses                   []string  `json:"statuses"`
	Phases                     []string  `json:"phases"`
	InferredTransitionsEnabled bool      `json:"inferred_transitions_enabled"`
	UpdatedAt                  time.Time `json:"updated_at"`
}

type Agent struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Harness   string    `json:"harness"`
	Mode      string    `json:"mode"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Task struct {
	ID          string    `json:"id"`
	Project     string    `json:"project"`
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	TaskType    string    `json:"type,omitempty"`
	Status      string    `json:"status"`
	Phase       string    `json:"phase"`
	Priority    int       `json:"priority"`
	AssigneeID  string    `json:"assignee_id,omitempty"`
	ActiveRunID string    `json:"active_run_id,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type TaskPatch struct {
	Title       *string
	Description *string
	TaskType    *string
	Status      *string
	Phase       *string
	Priority    *int
	AssigneeID  *string
	ActiveRunID *string
}

type GateKind string

const (
	GateKindDeterministic GateKind = "deterministic"
	GateKindHuman         GateKind = "human"
	GateKindLLM           GateKind = "llm"
)

type GateStatus string

const (
	GateStatusPending GateStatus = "pending"
	GateStatusPassed  GateStatus = "passed"
	GateStatusFailed  GateStatus = "failed"
)

type Gate struct {
	ID          string         `json:"id"`
	TaskID      string         `json:"task_id"`
	Name        string         `json:"name"`
	Kind        GateKind       `json:"kind"`
	Rule        string         `json:"rule,omitempty"`
	Config      map[string]any `json:"config,omitempty"`
	Status      GateStatus     `json:"status"`
	Evidence    map[string]any `json:"evidence,omitempty"`
	Required    bool           `json:"required"`
	SortOrder   int            `json:"sort_order"`
	EvaluatedAt *time.Time     `json:"evaluated_at,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

type GateEvaluation struct {
	Status   GateStatus     `json:"status"`
	Evidence map[string]any `json:"evidence,omitempty"`
}

type GateEvaluationContext struct {
	Task     Task
	Evidence map[string]any
	Approved *bool
}

// GateEvaluator is the extension point for deterministic and model-backed
// completion criteria. Implementations must not mutate storage themselves.
type GateEvaluator interface {
	Evaluate(context.Context, Gate, GateEvaluationContext) (GateEvaluation, error)
}

type GateEvaluatorFunc func(
	context.Context, Gate, GateEvaluationContext,
) (GateEvaluation, error)

func (f GateEvaluatorFunc) Evaluate(
	ctx context.Context, gate Gate, input GateEvaluationContext,
) (GateEvaluation, error) {
	return f(ctx, gate, input)
}

type TaskEvent struct {
	ID        int64          `json:"id"`
	TaskID    string         `json:"task_id"`
	Type      string         `json:"type"`
	Source    string         `json:"source"`
	Payload   map[string]any `json:"payload,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

type SessionLink struct {
	ID         string    `json:"id"`
	TaskID     string    `json:"task_id"`
	SessionID  string    `json:"session_id"`
	Harness    string    `json:"harness,omitempty"`
	Method     string    `json:"method"`
	Confidence float64   `json:"confidence"`
	Active     bool      `json:"active"`
	CreatedAt  time.Time `json:"created_at"`
}

type TaskFilter struct {
	Project    string
	Status     string
	Phase      string
	TaskType   string
	AssigneeID string
	From       *time.Time
	To         *time.Time
}

type GateSummary struct {
	Total           int  `json:"total"`
	Required        int  `json:"required"`
	Passed          int  `json:"passed"`
	Failed          int  `json:"failed"`
	Pending         int  `json:"pending"`
	CompletionReady bool `json:"completion_ready"`
}

type PhaseDuration struct {
	Phase   string `json:"phase"`
	TotalMS int64  `json:"total_ms"`
}

type TaskTiming struct {
	StartedAt      *time.Time      `json:"started_at,omitempty"`
	CompletedAt    *time.Time      `json:"completed_at,omitempty"`
	LeadTimeMS     *int64          `json:"lead_time_ms,omitempty"`
	CycleTimeMS    *int64          `json:"cycle_time_ms,omitempty"`
	PhaseDurations []PhaseDuration `json:"phase_durations"`
}

type TaskDetail struct {
	Task            Task          `json:"task"`
	Gates           []Gate        `json:"gates"`
	Events          []TaskEvent   `json:"events"`
	SessionLinks    []SessionLink `json:"session_links"`
	GateSummary     GateSummary   `json:"gate_summary"`
	Timing          TaskTiming    `json:"timing"`
	EventsTruncated bool          `json:"events_truncated"`
}

type CountBreakdown struct {
	ByProject  map[string]int `json:"by_project"`
	ByStatus   map[string]int `json:"by_status"`
	ByPhase    map[string]int `json:"by_phase"`
	ByType     map[string]int `json:"by_type"`
	ByAssignee map[string]int `json:"by_assignee"`
}

type DurationStats struct {
	Samples   int     `json:"samples"`
	TotalMS   int64   `json:"total_ms"`
	AverageMS float64 `json:"average_ms"`
	MinMS     int64   `json:"min_ms"`
	MaxMS     int64   `json:"max_ms"`
}

type PhaseDurationStats struct {
	Phase string `json:"phase"`
	DurationStats
}

type MetricsTiming struct {
	LeadTime  DurationStats        `json:"lead_time"`
	CycleTime DurationStats        `json:"cycle_time"`
	PhaseTime []PhaseDurationStats `json:"phase_time"`
}

type MetricsGateSummary struct {
	Total                int `json:"total"`
	Required             int `json:"required"`
	Passed               int `json:"passed"`
	Failed               int `json:"failed"`
	Pending              int `json:"pending"`
	CompletionReadyTasks int `json:"completion_ready_tasks"`
}

type TaskMetrics struct {
	TotalTasks int                `json:"total_tasks"`
	Counts     CountBreakdown     `json:"counts"`
	Timing     MetricsTiming      `json:"timing"`
	Gates      MetricsGateSummary `json:"gates"`
}
