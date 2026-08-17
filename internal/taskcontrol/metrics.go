package taskcontrol

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	MaxDetailEvents  = 500
	MaxMetricsTasks  = 500
	MaxMetricsEvents = 10_000
	MaxMetricsGates  = 5_000
)

func (s *Store) TaskDetail(ctx context.Context, id string) (TaskDetail, error) {
	task, err := s.Task(ctx, id)
	if err != nil {
		return TaskDetail{}, err
	}
	gates, err := s.ListGates(ctx, id)
	if err != nil {
		return TaskDetail{}, err
	}
	links, err := s.ListSessionLinks(ctx, id)
	if err != nil {
		return TaskDetail{}, err
	}
	events, truncated, err := s.listRecentEvents(ctx, id, MaxDetailEvents)
	if err != nil {
		return TaskDetail{}, err
	}
	timingEvents := events
	if truncated {
		var truncatedTiming bool
		timingEvents, truncatedTiming, err = s.listRecentEvents(ctx, id, MaxMetricsEvents)
		if err != nil {
			return TaskDetail{}, err
		}
		if truncatedTiming {
			return TaskDetail{}, fmt.Errorf("%w: task has too many events for timing", ErrQueryLimit)
		}
	}
	workflow, err := s.Workflow(ctx, task.Project)
	if err != nil {
		return TaskDetail{}, err
	}
	return TaskDetail{
		Task:            task,
		Gates:           gates,
		Events:          events,
		SessionLinks:    links,
		GateSummary:     summarizeGates(gates),
		Timing:          deriveTaskTiming(task, timingEvents, terminalStatus(workflow)),
		EventsTruncated: truncated,
	}, nil
}

func (s *Store) listRecentEvents(
	ctx context.Context, taskID string, limit int,
) ([]TaskEvent, bool, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, task_id, type, source, payload_json, created_at
		FROM (
			SELECT id, task_id, type, source, payload_json, created_at
			FROM task_events WHERE task_id=? ORDER BY id DESC LIMIT ?
		) ORDER BY created_at, id`, taskID, limit+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	events, err := scanEvents(rows)
	if err != nil {
		return nil, false, err
	}
	truncated := len(events) > limit
	if truncated {
		events = events[len(events)-limit:]
	}
	return events, truncated, nil
}

func (s *Store) TaskMetrics(ctx context.Context, filter TaskFilter) (TaskMetrics, error) {
	tasks, err := s.filteredMetricTasks(ctx, filter)
	if err != nil {
		return TaskMetrics{}, err
	}
	metrics := emptyTaskMetrics()
	metrics.TotalTasks = len(tasks)
	if len(tasks) == 0 {
		return metrics, nil
	}

	ids := make([]string, 0, len(tasks))
	projects := make(map[string]struct{})
	for _, task := range tasks {
		ids = append(ids, task.ID)
		projects[task.Project] = struct{}{}
		metrics.Counts.ByProject[task.Project]++
		metrics.Counts.ByStatus[task.Status]++
		metrics.Counts.ByPhase[task.Phase]++
		metrics.Counts.ByType[task.TaskType]++
		metrics.Counts.ByAssignee[task.AssigneeID]++
	}
	events, err := s.metricEvents(ctx, ids)
	if err != nil {
		return TaskMetrics{}, err
	}
	gates, err := s.metricGates(ctx, ids)
	if err != nil {
		return TaskMetrics{}, err
	}
	terminalByProject := make(map[string]string, len(projects))
	for project := range projects {
		workflow, err := s.Workflow(ctx, project)
		if err != nil {
			return TaskMetrics{}, err
		}
		terminalByProject[project] = terminalStatus(workflow)
	}

	eventsByTask := make(map[string][]TaskEvent, len(tasks))
	for _, event := range events {
		eventsByTask[event.TaskID] = append(eventsByTask[event.TaskID], event)
	}
	gatesByTask := make(map[string][]Gate, len(tasks))
	for _, gate := range gates {
		gatesByTask[gate.TaskID] = append(gatesByTask[gate.TaskID], gate)
	}

	lead := durationAccumulator{}
	cycle := durationAccumulator{}
	phase := make(map[string]*durationAccumulator)
	for _, task := range tasks {
		timing := deriveTaskTiming(task, eventsByTask[task.ID], terminalByProject[task.Project])
		if timing.LeadTimeMS != nil {
			lead.add(*timing.LeadTimeMS)
		}
		if timing.CycleTimeMS != nil {
			cycle.add(*timing.CycleTimeMS)
		}
		for _, duration := range timing.PhaseDurations {
			accumulator := phase[duration.Phase]
			if accumulator == nil {
				accumulator = &durationAccumulator{}
				phase[duration.Phase] = accumulator
			}
			accumulator.add(duration.TotalMS)
		}

		summary := summarizeGates(gatesByTask[task.ID])
		metrics.Gates.Total += summary.Total
		metrics.Gates.Required += summary.Required
		metrics.Gates.Passed += summary.Passed
		metrics.Gates.Failed += summary.Failed
		metrics.Gates.Pending += summary.Pending
		if summary.CompletionReady {
			metrics.Gates.CompletionReadyTasks++
		}
	}
	metrics.Timing.LeadTime = lead.stats()
	metrics.Timing.CycleTime = cycle.stats()
	metrics.Timing.PhaseTime = phaseStats(phase)
	return metrics, nil
}

func emptyTaskMetrics() TaskMetrics {
	return TaskMetrics{
		Counts: CountBreakdown{
			ByProject:  map[string]int{},
			ByStatus:   map[string]int{},
			ByPhase:    map[string]int{},
			ByType:     map[string]int{},
			ByAssignee: map[string]int{},
		},
		Timing: MetricsTiming{PhaseTime: []PhaseDurationStats{}},
	}
}

func (s *Store) filteredMetricTasks(ctx context.Context, filter TaskFilter) ([]Task, error) {
	query := `SELECT id, project, title, description, task_type, status, phase,
		priority, COALESCE(assignee_id, ''), COALESCE(active_run_id, ''),
		created_at, updated_at FROM tasks`
	conditions := []string{}
	args := []any{}
	add := func(column, value string) {
		value = strings.TrimSpace(value)
		if value != "" {
			conditions = append(conditions, column+" = ?")
			args = append(args, value)
		}
	}
	add("project", filter.Project)
	add("status", filter.Status)
	add("phase", filter.Phase)
	add("task_type", filter.TaskType)
	add("assignee_id", filter.AssigneeID)
	if filter.From != nil {
		conditions = append(conditions, "created_at >= ?")
		args = append(args, filter.From.UTC().Format(time.RFC3339Nano))
	}
	if filter.To != nil {
		conditions = append(conditions, "created_at < ?")
		args = append(args, filter.To.UTC().Format(time.RFC3339Nano))
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY created_at, id LIMIT ?"
	args = append(args, MaxMetricsTasks+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks := []Task{}
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(tasks) > MaxMetricsTasks {
		return nil, fmt.Errorf("%w: narrow the metrics filters", ErrQueryLimit)
	}
	return tasks, nil
}

func (s *Store) metricEvents(ctx context.Context, taskIDs []string) ([]TaskEvent, error) {
	query, args := inQuery(`SELECT id, task_id, type, source, payload_json, created_at
		FROM task_events WHERE task_id IN (%s) ORDER BY task_id, created_at, id LIMIT ?`, taskIDs)
	args = append(args, MaxMetricsEvents+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events, err := scanEvents(rows)
	if err != nil {
		return nil, err
	}
	if len(events) > MaxMetricsEvents {
		return nil, fmt.Errorf("%w: selected tasks have too many events", ErrQueryLimit)
	}
	return events, nil
}

func scanEvents(rows *sql.Rows) ([]TaskEvent, error) {
	events := []TaskEvent{}
	for rows.Next() {
		var event TaskEvent
		var payload, created string
		if err := rows.Scan(&event.ID, &event.TaskID, &event.Type, &event.Source,
			&payload, &created); err != nil {
			return nil, err
		}
		event.Payload = decodeMap(payload)
		event.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *Store) metricGates(ctx context.Context, taskIDs []string) ([]Gate, error) {
	query, args := inQuery(`SELECT id, task_id, name, kind, rule, config_json,
		status, evidence_json, required, sort_order, evaluated_at, created_at, updated_at
		FROM gates WHERE task_id IN (%s) ORDER BY task_id, sort_order, created_at, id LIMIT ?`, taskIDs)
	args = append(args, MaxMetricsGates+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	gates := []Gate{}
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
		gates = append(gates, gate)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(gates) > MaxMetricsGates {
		return nil, fmt.Errorf("%w: selected tasks have too many gates", ErrQueryLimit)
	}
	return gates, nil
}

func inQuery(template string, values []string) (string, []any) {
	placeholders := make([]string, len(values))
	args := make([]any, len(values))
	for index, value := range values {
		placeholders[index] = "?"
		args[index] = value
	}
	return fmt.Sprintf(template, strings.Join(placeholders, ",")), args
}

func terminalStatus(workflow Workflow) string {
	if len(workflow.Statuses) == 0 {
		return "Done"
	}
	return workflow.Statuses[len(workflow.Statuses)-1]
}

func summarizeGates(gates []Gate) GateSummary {
	summary := GateSummary{Total: len(gates), CompletionReady: true}
	for _, gate := range gates {
		if gate.Required {
			summary.Required++
			if gate.Status != GateStatusPassed {
				summary.CompletionReady = false
			}
		}
		switch gate.Status {
		case GateStatusPassed:
			summary.Passed++
		case GateStatusFailed:
			summary.Failed++
		default:
			summary.Pending++
		}
	}
	return summary
}

type taskState struct {
	status string
	phase  string
}

func deriveTaskTiming(task Task, events []TaskEvent, terminal string) TaskTiming {
	result := TaskTiming{PhaseDurations: []PhaseDuration{}}
	createdAt := task.CreatedAt
	current := taskState{phase: task.Phase, status: task.Status}
	phaseStarted := createdAt
	phaseTotals := map[string]int64{}
	initialized := false

	for _, event := range events {
		state, ok := eventTaskState(event)
		if !ok {
			continue
		}
		if !initialized {
			current = state
			phaseStarted = createdAt
			initialized = true
			if current.status == "In Progress" {
				started := createdAt
				result.StartedAt = &started
			}
			if current.status == terminal {
				completed := createdAt
				result.CompletedAt = &completed
			}
			if event.Type == "task.created" {
				continue
			}
		}
		if state.phase != "" && state.phase != current.phase {
			addPhaseDuration(phaseTotals, current.phase, phaseStarted, event.CreatedAt)
			current.phase = state.phase
			phaseStarted = event.CreatedAt
		}
		current.status = state.status
		if result.StartedAt == nil && state.status == "In Progress" {
			started := event.CreatedAt
			result.StartedAt = &started
		}
		if result.CompletedAt == nil && state.status == terminal {
			completed := event.CreatedAt
			result.CompletedAt = &completed
			addPhaseDuration(phaseTotals, current.phase, phaseStarted, completed)
			break
		}
	}

	if result.CompletedAt != nil {
		lead := durationMS(createdAt, *result.CompletedAt)
		result.LeadTimeMS = &lead
		if result.StartedAt != nil {
			cycle := durationMS(*result.StartedAt, *result.CompletedAt)
			result.CycleTimeMS = &cycle
		}
	}
	result.PhaseDurations = orderedPhaseDurations(phaseTotals)
	return result
}

func eventTaskState(event TaskEvent) (taskState, bool) {
	source := event.Payload
	if event.Type == "task.updated" {
		after, ok := source["after"].(map[string]any)
		if !ok {
			return taskState{}, false
		}
		source = after
	} else if event.Type != "task.created" {
		return taskState{}, false
	}
	status, statusOK := source["status"].(string)
	phase, phaseOK := source["phase"].(string)
	return taskState{status: status, phase: phase}, statusOK && phaseOK
}

func addPhaseDuration(totals map[string]int64, phase string, from, to time.Time) {
	if phase == "" || !to.After(from) {
		return
	}
	totals[phase] += durationMS(from, to)
}

func durationMS(from, to time.Time) int64 {
	if !to.After(from) {
		return 0
	}
	return to.Sub(from).Milliseconds()
}

func orderedPhaseDurations(totals map[string]int64) []PhaseDuration {
	result := make([]PhaseDuration, 0, len(totals))
	for phase, total := range totals {
		result = append(result, PhaseDuration{Phase: phase, TotalMS: total})
	}
	sort.Slice(result, func(i, j int) bool {
		return phaseLess(result[i].Phase, result[j].Phase)
	})
	return result
}

type durationAccumulator struct {
	samples int
	total   int64
	min     int64
	max     int64
}

func (a *durationAccumulator) add(value int64) {
	if a.samples == 0 || value < a.min {
		a.min = value
	}
	if a.samples == 0 || value > a.max {
		a.max = value
	}
	a.samples++
	a.total += value
}

func (a durationAccumulator) stats() DurationStats {
	stats := DurationStats{Samples: a.samples, TotalMS: a.total, MinMS: a.min, MaxMS: a.max}
	if a.samples > 0 {
		stats.AverageMS = float64(a.total) / float64(a.samples)
	}
	return stats
}

func phaseStats(accumulators map[string]*durationAccumulator) []PhaseDurationStats {
	result := make([]PhaseDurationStats, 0, len(accumulators))
	for phase, accumulator := range accumulators {
		result = append(result, PhaseDurationStats{Phase: phase, DurationStats: accumulator.stats()})
	}
	sort.Slice(result, func(i, j int) bool { return phaseLess(result[i].Phase, result[j].Phase) })
	return result
}

func phaseLess(left, right string) bool {
	leftIndex, rightIndex := phaseOrder(left), phaseOrder(right)
	if leftIndex != rightIndex {
		return leftIndex < rightIndex
	}
	return left < right
}

func phaseOrder(phase string) int {
	for index, known := range UniversalPhases {
		if phase == known {
			return index
		}
	}
	return len(UniversalPhases)
}
