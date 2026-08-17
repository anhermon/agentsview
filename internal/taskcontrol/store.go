package taskcontrol

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const schema = `
PRAGMA foreign_keys = ON;
CREATE TABLE IF NOT EXISTS workflows (
    project TEXT PRIMARY KEY,
    statuses_json TEXT NOT NULL,
    inferred_transitions_enabled INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS agents (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    harness TEXT NOT NULL,
    mode TEXT NOT NULL CHECK (mode IN ('managed', 'external')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS tasks (
    id TEXT PRIMARY KEY,
    project TEXT NOT NULL,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    task_type TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    phase TEXT NOT NULL,
    priority INTEGER NOT NULL DEFAULT 0,
    assignee_id TEXT REFERENCES agents(id) ON DELETE SET NULL,
    active_run_id TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_tasks_board ON tasks(project, status, priority, updated_at);
CREATE TABLE IF NOT EXISTS gates (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('deterministic', 'human', 'llm')),
    rule TEXT NOT NULL DEFAULT '',
    config_json TEXT NOT NULL DEFAULT '{}',
    status TEXT NOT NULL CHECK (status IN ('pending', 'passed', 'failed')),
    evidence_json TEXT NOT NULL DEFAULT '{}',
    required INTEGER NOT NULL DEFAULT 1,
    sort_order INTEGER NOT NULL DEFAULT 0,
    evaluated_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_gates_task ON gates(task_id, sort_order, created_at);
CREATE TABLE IF NOT EXISTS task_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    type TEXT NOT NULL,
    source TEXT NOT NULL,
    payload_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_task_events_task ON task_events(task_id, id);
CREATE TABLE IF NOT EXISTS session_links (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    session_id TEXT NOT NULL,
    harness TEXT NOT NULL DEFAULT '',
    method TEXT NOT NULL,
    confidence REAL NOT NULL DEFAULT 1.0 CHECK (confidence >= 0 AND confidence <= 1),
    active INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    UNIQUE(task_id, session_id)
);
CREATE INDEX IF NOT EXISTS idx_session_links_session ON session_links(session_id);
`

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("tasks database path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create tasks database directory: %w", err)
	}
	dsn := (&url.URL{Scheme: "file", Path: path, RawQuery: url.Values{
		"_busy_timeout": []string{"5000"},
		"_foreign_keys": []string{"on"},
		"_journal_mode": []string{"WAL"},
	}.Encode()}).String()
	database, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open tasks database: %w", err)
	}
	database.SetMaxOpenConns(1)
	if _, err := database.Exec(schema); err != nil {
		database.Close()
		return nil, fmt.Errorf("initialize tasks database: %w", err)
	}
	return &Store{db: database}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func nowUTC() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

func newID(prefix string) string {
	var buf [12]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return prefix + "_" + hex.EncodeToString(buf[:])
}

func encodeJSON(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func decodeMap(value string) map[string]any {
	result := map[string]any{}
	_ = json.Unmarshal([]byte(value), &result)
	return result
}

func normalizeProject(project string) (string, error) {
	project = strings.TrimSpace(project)
	if project == "" {
		return "", errors.New("project is required")
	}
	return project, nil
}

func validateStrings(values []string, label string) ([]string, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("%s must not be empty", label)
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("%s must not contain empty values", label)
		}
		if _, exists := seen[value]; exists {
			return nil, fmt.Errorf("%s must not contain duplicates", label)
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

func (s *Store) Workflow(ctx context.Context, project string) (Workflow, error) {
	project, err := normalizeProject(project)
	if err != nil {
		return Workflow{}, err
	}
	var statusesJSON, updated string
	var inferred bool
	err = s.db.QueryRowContext(ctx, `
		SELECT statuses_json, inferred_transitions_enabled, updated_at
		FROM workflows WHERE project = ?`, project,
	).Scan(&statusesJSON, &inferred, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Workflow{
			Project: project, Statuses: slices.Clone(DefaultStatuses),
			Phases:                     slices.Clone(UniversalPhases),
			InferredTransitionsEnabled: false,
		}, nil
	}
	if err != nil {
		return Workflow{}, err
	}
	var statuses []string
	if err := json.Unmarshal([]byte(statusesJSON), &statuses); err != nil {
		return Workflow{}, fmt.Errorf("decode workflow statuses: %w", err)
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, updated)
	if err != nil {
		return Workflow{}, err
	}
	return Workflow{Project: project, Statuses: statuses,
		Phases:                     slices.Clone(UniversalPhases),
		InferredTransitionsEnabled: inferred, UpdatedAt: updatedAt}, nil
}

func (s *Store) PutWorkflow(
	ctx context.Context, project string, statuses []string, inferred bool,
) (Workflow, error) {
	project, err := normalizeProject(project)
	if err != nil {
		return Workflow{}, err
	}
	statuses, err = validateStrings(statuses, "statuses")
	if err != nil {
		return Workflow{}, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT status FROM tasks WHERE project = ?`, project)
	if err != nil {
		return Workflow{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		if err := rows.Scan(&status); err != nil {
			return Workflow{}, err
		}
		if !slices.Contains(statuses, status) {
			return Workflow{}, fmt.Errorf("%w: status %q is used by existing tasks", ErrConflict, status)
		}
	}
	if err := rows.Err(); err != nil {
		return Workflow{}, err
	}
	now := nowUTC()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO workflows(project, statuses_json, inferred_transitions_enabled, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(project) DO UPDATE SET statuses_json=excluded.statuses_json,
		inferred_transitions_enabled=excluded.inferred_transitions_enabled,
		updated_at=excluded.updated_at`,
		project, encodeJSON(statuses), inferred, now.Format(time.RFC3339Nano))
	if err != nil {
		return Workflow{}, err
	}
	return Workflow{Project: project, Statuses: statuses,
		Phases:                     slices.Clone(UniversalPhases),
		InferredTransitionsEnabled: inferred, UpdatedAt: now}, nil
}

func (s *Store) CreateAgent(ctx context.Context, agent Agent) (Agent, error) {
	agent.ID = strings.TrimSpace(agent.ID)
	if agent.ID == "" {
		agent.ID = newID("agent")
	}
	agent.Name = strings.TrimSpace(agent.Name)
	agent.Harness = strings.TrimSpace(agent.Harness)
	if agent.Name == "" || agent.Harness == "" {
		return Agent{}, errors.New("agent name and harness are required")
	}
	if agent.Mode == "" {
		agent.Mode = "managed"
	}
	if agent.Mode != "managed" && agent.Mode != "external" {
		return Agent{}, errors.New("agent mode must be managed or external")
	}
	agent.CreatedAt, agent.UpdatedAt = nowUTC(), nowUTC()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agents(id, name, harness, mode, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`, agent.ID, agent.Name, agent.Harness,
		agent.Mode, agent.CreatedAt.Format(time.RFC3339Nano),
		agent.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return Agent{}, fmt.Errorf("%w: create agent: %v", ErrConflict, err)
	}
	return agent, nil
}

func (s *Store) ListAgents(ctx context.Context) ([]Agent, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, harness, mode, created_at, updated_at
		FROM agents ORDER BY name, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Agent{}
	for rows.Next() {
		var agent Agent
		var created, updated string
		if err := rows.Scan(&agent.ID, &agent.Name, &agent.Harness, &agent.Mode,
			&created, &updated); err != nil {
			return nil, err
		}
		agent.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		agent.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		result = append(result, agent)
	}
	return result, rows.Err()
}

func (s *Store) CreateTask(ctx context.Context, task Task) (Task, error) {
	task.Project = strings.TrimSpace(task.Project)
	task.Title = strings.TrimSpace(task.Title)
	if task.Project == "" || task.Title == "" {
		return Task{}, errors.New("project and title are required")
	}
	workflow, err := s.Workflow(ctx, task.Project)
	if err != nil {
		return Task{}, err
	}
	if task.Status == "" {
		task.Status = workflow.Statuses[0]
	}
	if task.Phase == "" {
		task.Phase = UniversalPhases[0]
	}
	if err := validateTaskPlacement(workflow, task.Status, task.Phase); err != nil {
		return Task{}, err
	}
	if task.ID == "" {
		task.ID = newID("task")
	}
	now := nowUTC()
	task.CreatedAt, task.UpdatedAt = now, now
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO tasks(id, project, title, description, task_type, status, phase,
		priority, assignee_id, active_run_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?)`,
		task.ID, task.Project, task.Title, task.Description, task.TaskType,
		task.Status, task.Phase, task.Priority, task.AssigneeID, task.ActiveRunID,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return Task{}, fmt.Errorf("%w: create task: %v", ErrConflict, err)
	}
	if _, err = appendEventTx(ctx, tx, TaskEvent{TaskID: task.ID,
		Type: "task.created", Source: "api", Payload: map[string]any{
			"status": task.Status, "phase": task.Phase,
		}}); err != nil {
		return Task{}, err
	}
	if err := tx.Commit(); err != nil {
		return Task{}, err
	}
	return task, nil
}

func validateTaskPlacement(workflow Workflow, status, phase string) error {
	if !slices.Contains(workflow.Statuses, status) {
		return fmt.Errorf("status %q is not in project workflow", status)
	}
	if !slices.Contains(UniversalPhases, phase) {
		return fmt.Errorf("phase %q is not a universal phase", phase)
	}
	return nil
}

func (s *Store) Task(ctx context.Context, id string) (Task, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, project, title, description, task_type, status, phase, priority,
		COALESCE(assignee_id, ''), COALESCE(active_run_id, ''), created_at, updated_at
		FROM tasks WHERE id = ?`, id)
	return scanTask(row)
}

type scanner interface{ Scan(...any) error }

func scanTask(row scanner) (Task, error) {
	var task Task
	var created, updated string
	if err := row.Scan(&task.ID, &task.Project, &task.Title, &task.Description,
		&task.TaskType, &task.Status, &task.Phase, &task.Priority, &task.AssigneeID,
		&task.ActiveRunID, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Task{}, ErrNotFound
		}
		return Task{}, err
	}
	task.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	task.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return task, nil
}

func (s *Store) ListTasks(ctx context.Context, project string) ([]Task, error) {
	query := `SELECT id, project, title, description, task_type, status, phase,
		priority, COALESCE(assignee_id, ''), COALESCE(active_run_id, ''),
		created_at, updated_at FROM tasks`
	args := []any{}
	if strings.TrimSpace(project) != "" {
		query += " WHERE project = ?"
		args = append(args, strings.TrimSpace(project))
	}
	query += " ORDER BY priority DESC, updated_at DESC, id"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Task{}
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, task)
	}
	return result, rows.Err()
}

func (s *Store) PatchTask(ctx context.Context, id string, patch TaskPatch) (Task, error) {
	current, err := s.Task(ctx, id)
	if err != nil {
		return Task{}, err
	}
	before := current
	if patch.Title != nil {
		current.Title = strings.TrimSpace(*patch.Title)
	}
	if patch.Description != nil {
		current.Description = *patch.Description
	}
	if patch.TaskType != nil {
		current.TaskType = strings.TrimSpace(*patch.TaskType)
	}
	if patch.Status != nil {
		current.Status = strings.TrimSpace(*patch.Status)
	}
	if patch.Phase != nil {
		current.Phase = strings.TrimSpace(*patch.Phase)
	}
	if patch.Priority != nil {
		current.Priority = *patch.Priority
	}
	if patch.AssigneeID != nil {
		current.AssigneeID = strings.TrimSpace(*patch.AssigneeID)
	}
	if patch.ActiveRunID != nil {
		current.ActiveRunID = strings.TrimSpace(*patch.ActiveRunID)
	}
	if current.Title == "" {
		return Task{}, errors.New("title must not be empty")
	}
	workflow, err := s.Workflow(ctx, current.Project)
	if err != nil {
		return Task{}, err
	}
	if err := validateTaskPlacement(workflow, current.Status, current.Phase); err != nil {
		return Task{}, err
	}
	if current.Status == workflow.Statuses[len(workflow.Statuses)-1] {
		gates, err := s.ListGates(ctx, id)
		if err != nil {
			return Task{}, err
		}
		for _, gate := range gates {
			if gate.Required && gate.Status != GateStatusPassed {
				return Task{}, fmt.Errorf("%w: gate %q is %s", ErrCompletionBlocked, gate.Name, gate.Status)
			}
		}
	}
	current.UpdatedAt = nowUTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `UPDATE tasks SET title=?, description=?, task_type=?,
		status=?, phase=?, priority=?, assignee_id=NULLIF(?, ''),
		active_run_id=NULLIF(?, ''), updated_at=? WHERE id=?`,
		current.Title, current.Description, current.TaskType, current.Status,
		current.Phase, current.Priority, current.AssigneeID, current.ActiveRunID,
		current.UpdatedAt.Format(time.RFC3339Nano), id)
	if err != nil {
		return Task{}, fmt.Errorf("%w: update task: %v", ErrConflict, err)
	}
	payload := map[string]any{"before": before, "after": current}
	if _, err := appendEventTx(ctx, tx, TaskEvent{TaskID: id,
		Type: "task.updated", Source: "api", Payload: payload}); err != nil {
		return Task{}, err
	}
	if err := tx.Commit(); err != nil {
		return Task{}, err
	}
	return current, nil
}

func (s *Store) CreateGate(ctx context.Context, gate Gate) (Gate, error) {
	if _, err := s.Task(ctx, gate.TaskID); err != nil {
		return Gate{}, err
	}
	gate.Name, gate.Rule = strings.TrimSpace(gate.Name), strings.TrimSpace(gate.Rule)
	if gate.Name == "" {
		return Gate{}, errors.New("gate name is required")
	}
	if gate.Kind != GateKindDeterministic && gate.Kind != GateKindHuman && gate.Kind != GateKindLLM {
		return Gate{}, errors.New("invalid gate kind")
	}
	if gate.Kind == GateKindDeterministic && gate.Rule == "" {
		return Gate{}, errors.New("deterministic gates require a rule")
	}
	if gate.ID == "" {
		gate.ID = newID("gate")
	}
	if gate.Status == "" {
		gate.Status = GateStatusPending
	}
	if gate.Config == nil {
		gate.Config = map[string]any{}
	}
	if gate.Evidence == nil {
		gate.Evidence = map[string]any{}
	}
	now := nowUTC()
	gate.CreatedAt, gate.UpdatedAt = now, now
	_, err := s.db.ExecContext(ctx, `INSERT INTO gates(id, task_id, name, kind, rule,
		config_json, status, evidence_json, required, sort_order, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, gate.ID, gate.TaskID,
		gate.Name, gate.Kind, gate.Rule, encodeJSON(gate.Config), gate.Status,
		encodeJSON(gate.Evidence), gate.Required, gate.SortOrder,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return Gate{}, fmt.Errorf("%w: create gate: %v", ErrConflict, err)
	}
	return gate, nil
}

func (s *Store) ListGates(ctx context.Context, taskID string) ([]Gate, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, task_id, name, kind, rule,
		config_json, status, evidence_json, required, sort_order, evaluated_at,
		created_at, updated_at FROM gates WHERE task_id=? ORDER BY sort_order, created_at, id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Gate{}
	for rows.Next() {
		var gate Gate
		var config, evidence, created, updated string
		var evaluated sql.NullString
		if err := rows.Scan(&gate.ID, &gate.TaskID, &gate.Name, &gate.Kind, &gate.Rule,
			&config, &gate.Status, &evidence, &gate.Required, &gate.SortOrder,
			&evaluated, &created, &updated); err != nil {
			return nil, err
		}
		gate.Config, gate.Evidence = decodeMap(config), decodeMap(evidence)
		gate.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		gate.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		if evaluated.Valid {
			value, _ := time.Parse(time.RFC3339Nano, evaluated.String)
			gate.EvaluatedAt = &value
		}
		result = append(result, gate)
	}
	return result, rows.Err()
}

func (s *Store) EvaluateGate(ctx context.Context, taskID, gateID string,
	evaluator GateEvaluator, input GateEvaluationContext) (Gate, error) {
	gates, err := s.ListGates(ctx, taskID)
	if err != nil {
		return Gate{}, err
	}
	index := slices.IndexFunc(gates, func(g Gate) bool { return g.ID == gateID })
	if index < 0 {
		return Gate{}, ErrNotFound
	}
	gate := gates[index]
	if evaluator == nil {
		return Gate{}, errors.New("gate evaluator is not configured")
	}
	input.Task, err = s.Task(ctx, taskID)
	if err != nil {
		return Gate{}, err
	}
	result, err := evaluator.Evaluate(ctx, gate, input)
	if err != nil {
		return Gate{}, err
	}
	if result.Status != GateStatusPending && result.Status != GateStatusPassed && result.Status != GateStatusFailed {
		return Gate{}, errors.New("gate evaluator returned invalid status")
	}
	if result.Evidence == nil {
		result.Evidence = map[string]any{}
	}
	now := nowUTC()
	_, err = s.db.ExecContext(ctx, `UPDATE gates SET status=?, evidence_json=?,
		evaluated_at=?, updated_at=? WHERE id=? AND task_id=?`, result.Status,
		encodeJSON(result.Evidence), now.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano), gateID, taskID)
	if err != nil {
		return Gate{}, err
	}
	gate.Status, gate.Evidence, gate.EvaluatedAt, gate.UpdatedAt = result.Status, result.Evidence, &now, now
	return gate, nil
}

func (s *Store) AppendEvent(ctx context.Context, event TaskEvent) (TaskEvent, error) {
	if _, err := s.Task(ctx, event.TaskID); err != nil {
		return TaskEvent{}, err
	}
	return appendEventTx(ctx, s.db, event)
}

type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func appendEventTx(ctx context.Context, exec execer, event TaskEvent) (TaskEvent, error) {
	event.Type, event.Source = strings.TrimSpace(event.Type), strings.TrimSpace(event.Source)
	if event.Type == "" || event.Source == "" {
		return TaskEvent{}, errors.New("event type and source are required")
	}
	if event.Payload == nil {
		event.Payload = map[string]any{}
	}
	event.CreatedAt = nowUTC()
	result, err := exec.ExecContext(ctx, `INSERT INTO task_events(task_id, type, source,
		payload_json, created_at) VALUES (?, ?, ?, ?, ?)`, event.TaskID, event.Type,
		event.Source, encodeJSON(event.Payload), event.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return TaskEvent{}, err
	}
	event.ID, _ = result.LastInsertId()
	return event, nil
}

func (s *Store) ListEvents(ctx context.Context, taskID string) ([]TaskEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, task_id, type, source, payload_json,
		created_at FROM task_events WHERE task_id=? ORDER BY id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []TaskEvent{}
	for rows.Next() {
		var event TaskEvent
		var payload, created string
		if err := rows.Scan(&event.ID, &event.TaskID, &event.Type, &event.Source,
			&payload, &created); err != nil {
			return nil, err
		}
		event.Payload = decodeMap(payload)
		event.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		result = append(result, event)
	}
	return result, rows.Err()
}

func (s *Store) CreateSessionLink(ctx context.Context, link SessionLink) (SessionLink, error) {
	if _, err := s.Task(ctx, link.TaskID); err != nil {
		return SessionLink{}, err
	}
	link.SessionID, link.Method = strings.TrimSpace(link.SessionID), strings.TrimSpace(link.Method)
	if link.SessionID == "" || link.Method == "" {
		return SessionLink{}, errors.New("session_id and method are required")
	}
	if link.Confidence < 0 || link.Confidence > 1 {
		return SessionLink{}, errors.New("confidence must be between 0 and 1")
	}
	if link.ID == "" {
		link.ID = newID("link")
	}
	link.CreatedAt = nowUTC()
	_, err := s.db.ExecContext(ctx, `INSERT INTO session_links(id, task_id, session_id,
		harness, method, confidence, active, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		link.ID, link.TaskID, link.SessionID, link.Harness, link.Method,
		link.Confidence, link.Active, link.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return SessionLink{}, fmt.Errorf("%w: create session link: %v", ErrConflict, err)
	}
	return link, nil
}

func (s *Store) ListSessionLinks(ctx context.Context, taskID string) ([]SessionLink, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, task_id, session_id, harness,
		method, confidence, active, created_at FROM session_links WHERE task_id=? ORDER BY created_at, id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []SessionLink{}
	for rows.Next() {
		var link SessionLink
		var created string
		if err := rows.Scan(&link.ID, &link.TaskID, &link.SessionID, &link.Harness,
			&link.Method, &link.Confidence, &link.Active, &created); err != nil {
			return nil, err
		}
		link.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		result = append(result, link)
	}
	return result, rows.Err()
}
