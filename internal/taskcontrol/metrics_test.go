package taskcontrol

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskMetricsEmptyData(t *testing.T) {
	store, _ := testStore(t)

	metrics, err := store.TaskMetrics(context.Background(), TaskFilter{})
	require.NoError(t, err)
	assert.Zero(t, metrics.TotalTasks)
	assert.Empty(t, metrics.Counts.ByProject)
	assert.Empty(t, metrics.Counts.ByStatus)
	assert.Empty(t, metrics.Counts.ByPhase)
	assert.Empty(t, metrics.Counts.ByType)
	assert.Empty(t, metrics.Counts.ByAssignee)
	assert.NotNil(t, metrics.Timing.PhaseTime)
	assert.Zero(t, metrics.Timing.LeadTime.Samples)
	assert.Zero(t, metrics.Timing.CycleTime.Samples)
	assert.Zero(t, metrics.Gates.CompletionReadyTasks)
}

func TestTaskMetricsFilters(t *testing.T) {
	store, _ := testStore(t)
	ctx := context.Background()
	agent, err := store.CreateAgent(ctx, Agent{Name: "Codex", Harness: "codex", Mode: "managed"})
	require.NoError(t, err)

	first := createMetricTask(t, store, Task{ID: "first", Project: "alpha", Title: "First",
		TaskType: "feature", Status: "Ready", Phase: "Plan", AssigneeID: agent.ID})
	second := createMetricTask(t, store, Task{ID: "second", Project: "alpha", Title: "Second",
		TaskType: "bug", Status: "Blocked", Phase: "Verify"})
	third := createMetricTask(t, store, Task{ID: "third", Project: "beta", Title: "Third",
		TaskType: "feature", Status: "Done", Phase: "Deliver", AssigneeID: agent.ID})
	firstAt := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	secondAt := firstAt.Add(time.Hour)
	thirdAt := secondAt.Add(time.Hour)
	setTaskCreatedAt(t, store, first.ID, firstAt)
	setTaskCreatedAt(t, store, second.ID, secondAt)
	setTaskCreatedAt(t, store, third.ID, thirdAt)

	tests := []struct {
		name   string
		filter TaskFilter
		want   int
	}{
		{name: "project", filter: TaskFilter{Project: "alpha"}, want: 2},
		{name: "status", filter: TaskFilter{Status: "Done"}, want: 1},
		{name: "phase", filter: TaskFilter{Phase: "Plan"}, want: 1},
		{name: "type", filter: TaskFilter{TaskType: "feature"}, want: 2},
		{name: "assignee", filter: TaskFilter{AssigneeID: agent.ID}, want: 2},
		{name: "from inclusive", filter: TaskFilter{From: &secondAt}, want: 2},
		{name: "to exclusive", filter: TaskFilter{To: &secondAt}, want: 1},
		{name: "range", filter: TaskFilter{From: &secondAt, To: &thirdAt}, want: 1},
		{name: "combined", filter: TaskFilter{Project: "alpha", TaskType: "feature"}, want: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			metrics, err := store.TaskMetrics(ctx, test.filter)
			require.NoError(t, err)
			assert.Equal(t, test.want, metrics.TotalTasks)
		})
	}

	metrics, err := store.TaskMetrics(ctx, TaskFilter{})
	require.NoError(t, err)
	assert.Equal(t, map[string]int{"alpha": 2, "beta": 1}, metrics.Counts.ByProject)
	assert.Equal(t, map[string]int{"bug": 1, "feature": 2}, metrics.Counts.ByType)
	assert.Equal(t, 2, metrics.Counts.ByAssignee[agent.ID])
	assert.Equal(t, 1, metrics.Counts.ByAssignee[""])
	assert.Equal(t, 3, metrics.Gates.CompletionReadyTasks)
}

func TestTaskMetricsDerivesLeadCycleAndPhaseTiming(t *testing.T) {
	store, _ := testStore(t)
	ctx := context.Background()
	task := createMetricTask(t, store, Task{ID: "timed", Project: "demo", Title: "Timed"})
	created := time.Date(2026, 2, 3, 10, 0, 0, 0, time.UTC)
	setTaskCreatedAt(t, store, task.ID, created)

	patchMetricTaskAt(t, store, task.ID, TaskPatch{
		Status: ptr("In Progress"), Phase: ptr("Plan"),
	}, created.Add(10*time.Minute))
	patchMetricTaskAt(t, store, task.ID, TaskPatch{
		Phase: ptr("Execute"),
	}, created.Add(30*time.Minute))
	patchMetricTaskAt(t, store, task.ID, TaskPatch{
		Phase: ptr("Verify"),
	}, created.Add(60*time.Minute))
	patchMetricTaskAt(t, store, task.ID, TaskPatch{
		Status: ptr("Done"), Phase: ptr("Deliver"),
	}, created.Add(90*time.Minute))

	metrics, err := store.TaskMetrics(ctx, TaskFilter{Project: "demo"})
	require.NoError(t, err)
	require.Equal(t, 1, metrics.Timing.LeadTime.Samples)
	assert.EqualValues(t, 5_400_000, metrics.Timing.LeadTime.TotalMS)
	assert.EqualValues(t, 4_800_000, metrics.Timing.CycleTime.TotalMS)
	require.Len(t, metrics.Timing.PhaseTime, 4)
	assert.Equal(t, []string{"Understand", "Plan", "Execute", "Verify"}, []string{
		metrics.Timing.PhaseTime[0].Phase,
		metrics.Timing.PhaseTime[1].Phase,
		metrics.Timing.PhaseTime[2].Phase,
		metrics.Timing.PhaseTime[3].Phase,
	})
	assert.EqualValues(t, 600_000, metrics.Timing.PhaseTime[0].TotalMS)
	assert.EqualValues(t, 1_200_000, metrics.Timing.PhaseTime[1].TotalMS)
	assert.EqualValues(t, 1_800_000, metrics.Timing.PhaseTime[2].TotalMS)
	assert.EqualValues(t, 1_800_000, metrics.Timing.PhaseTime[3].TotalMS)

	detail, err := store.TaskDetail(ctx, task.ID)
	require.NoError(t, err)
	require.NotNil(t, detail.Timing.StartedAt)
	require.NotNil(t, detail.Timing.CompletedAt)
	assert.Equal(t, created.Add(10*time.Minute), *detail.Timing.StartedAt)
	assert.Equal(t, created.Add(90*time.Minute), *detail.Timing.CompletedAt)
	assert.EqualValues(t, 5_400_000, *detail.Timing.LeadTimeMS)
	assert.EqualValues(t, 4_800_000, *detail.Timing.CycleTimeMS)
}

func TestTaskDetailIncludesGateSummaryEventsAndSessionLinks(t *testing.T) {
	store, _ := testStore(t)
	ctx := context.Background()
	task := createMetricTask(t, store, Task{ID: "detail", Project: "demo", Title: "Detail"})
	_, err := store.CreateGate(ctx, Gate{TaskID: task.ID, Name: "Required",
		Kind: GateKindHuman, Required: true})
	require.NoError(t, err)
	_, err = store.CreateGate(ctx, Gate{TaskID: task.ID, Name: "Optional",
		Kind: GateKindLLM, Required: false, Status: GateStatusFailed})
	require.NoError(t, err)
	link, err := store.CreateSessionLink(ctx, SessionLink{TaskID: task.ID,
		SessionID: "session-1", Harness: "codex", Method: "explicit", Confidence: 1, Active: true})
	require.NoError(t, err)
	_, err = store.AppendEvent(ctx, TaskEvent{TaskID: task.ID,
		Type: "agent.progress", Source: "codex", Payload: map[string]any{"phase": "Execute"}})
	require.NoError(t, err)

	detail, err := store.TaskDetail(ctx, task.ID)
	require.NoError(t, err)
	assert.Len(t, detail.Gates, 2)
	assert.Len(t, detail.Events, 2)
	assert.Equal(t, []SessionLink{link}, detail.SessionLinks)
	assert.Equal(t, GateSummary{Total: 2, Required: 1, Failed: 1,
		Pending: 1, CompletionReady: false}, detail.GateSummary)
	assert.False(t, detail.EventsTruncated)
}

func TestTaskDetailBoundsRecentEvents(t *testing.T) {
	store, _ := testStore(t)
	ctx := context.Background()
	task := createMetricTask(t, store, Task{ID: "bounded", Project: "demo", Title: "Bounded"})
	tx, err := store.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	for index := 0; index < MaxDetailEvents; index++ {
		_, err = tx.ExecContext(ctx, `INSERT INTO task_events(
			task_id, type, source, payload_json, created_at) VALUES (?, ?, ?, '{}', ?)`,
			task.ID, "agent.progress", "test", task.CreatedAt.Add(time.Duration(index+1)*time.Second).Format(time.RFC3339Nano))
		require.NoError(t, err)
	}
	require.NoError(t, tx.Commit())

	detail, err := store.TaskDetail(ctx, task.ID)
	require.NoError(t, err)
	assert.Len(t, detail.Events, MaxDetailEvents)
	assert.True(t, detail.EventsTruncated)
	assert.Equal(t, "agent.progress", detail.Events[0].Type)
}

func createMetricTask(t *testing.T, store *Store, task Task) Task {
	t.Helper()
	created, err := store.CreateTask(context.Background(), task)
	require.NoError(t, err)
	return created
}

func setTaskCreatedAt(t *testing.T, store *Store, taskID string, created time.Time) {
	t.Helper()
	formatted := created.UTC().Format(time.RFC3339Nano)
	_, err := store.db.ExecContext(context.Background(),
		`UPDATE tasks SET created_at=? WHERE id=?`, formatted, taskID)
	require.NoError(t, err)
	_, err = store.db.ExecContext(context.Background(),
		`UPDATE task_events SET created_at=? WHERE task_id=? AND type='task.created'`, formatted, taskID)
	require.NoError(t, err)
}

func patchMetricTaskAt(t *testing.T, store *Store, taskID string, patch TaskPatch, at time.Time) {
	t.Helper()
	_, err := store.PatchTask(context.Background(), taskID, patch)
	require.NoError(t, err)
	_, err = store.db.ExecContext(context.Background(), `UPDATE task_events SET created_at=?
		WHERE id=(SELECT MAX(id) FROM task_events WHERE task_id=?)`,
		at.UTC().Format(time.RFC3339Nano), taskID)
	require.NoError(t, err)
}
