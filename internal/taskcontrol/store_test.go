package taskcontrol

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tasks.db")
	store, err := Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	return store, path
}

func TestStoreKeepsTaskControlInSeparateDatabase(t *testing.T) {
	store, path := testStore(t)
	ctx := context.Background()

	workflow, err := store.Workflow(ctx, "demo")
	require.NoError(t, err)
	assert.Equal(t, DefaultStatuses, workflow.Statuses)
	assert.Equal(t, UniversalPhases, workflow.Phases)
	assert.False(t, workflow.InferredTransitionsEnabled)

	var sessionsTable int
	require.NoError(t, store.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='sessions'`,
	).Scan(&sessionsTable))
	assert.Zero(t, sessionsTable)
	assert.Equal(t, "tasks.db", filepath.Base(path))
}

func TestStoreCustomWorkflowAndTaskPlacement(t *testing.T) {
	store, _ := testStore(t)
	ctx := context.Background()

	workflow, err := store.PutWorkflow(ctx, "demo", []string{"Queue", "Doing", "Shipped"}, true)
	require.NoError(t, err)
	assert.True(t, workflow.InferredTransitionsEnabled)

	task, err := store.CreateTask(ctx, Task{Project: "demo", Title: "Add board"})
	require.NoError(t, err)
	assert.Equal(t, "Queue", task.Status)
	assert.Equal(t, "Understand", task.Phase)

	_, err = store.PatchTask(ctx, task.ID, TaskPatch{Status: ptr("Unknown")})
	assert.EqualError(t, err, `status "Unknown" is not in project workflow`)

	_, err = store.PutWorkflow(ctx, "demo", []string{"Doing", "Shipped"}, false)
	assert.ErrorIs(t, err, ErrConflict)
}

func TestStoreCompletionGatesEventsAndSessionLinks(t *testing.T) {
	store, _ := testStore(t)
	ctx := context.Background()

	agent, err := store.CreateAgent(ctx, Agent{Name: "Codex", Harness: "codex", Mode: "managed"})
	require.NoError(t, err)
	task, err := store.CreateTask(ctx, Task{Project: "demo", Title: "Ship API",
		AssigneeID: agent.ID, ActiveRunID: "run-1"})
	require.NoError(t, err)

	gate, err := store.CreateGate(ctx, Gate{TaskID: task.ID, Name: "Tests pass",
		Kind: GateKindDeterministic, Rule: "go_test", Required: true})
	require.NoError(t, err)
	_, err = store.PatchTask(ctx, task.ID, TaskPatch{Status: ptr("Done")})
	assert.ErrorIs(t, err, ErrCompletionBlocked)

	evaluator := GateEvaluatorFunc(func(
		_ context.Context, _ Gate, input GateEvaluationContext,
	) (GateEvaluation, error) {
		assert.Equal(t, task.ID, input.Task.ID)
		return GateEvaluation{Status: GateStatusPassed,
			Evidence: map[string]any{"command": "go test ./..."}}, nil
	})
	gate, err = store.EvaluateGate(ctx, task.ID, gate.ID, evaluator, GateEvaluationContext{})
	require.NoError(t, err)
	assert.Equal(t, GateStatusPassed, gate.Status)

	completed, err := store.PatchTask(ctx, task.ID, TaskPatch{Status: ptr("Done")})
	require.NoError(t, err)
	assert.Equal(t, "Done", completed.Status)
	assert.Equal(t, agent.ID, completed.AssigneeID)
	assert.Equal(t, "run-1", completed.ActiveRunID)

	link, err := store.CreateSessionLink(ctx, SessionLink{TaskID: task.ID,
		SessionID: "session-1", Harness: "codex", Method: "explicit",
		Confidence: 1, Active: true})
	require.NoError(t, err)
	assert.True(t, link.Active)

	event, err := store.AppendEvent(ctx, TaskEvent{TaskID: task.ID,
		Type: "agent.progress", Source: "adapter", Payload: map[string]any{"percent": 50}})
	require.NoError(t, err)
	assert.Positive(t, event.ID)

	events, err := store.ListEvents(ctx, task.ID)
	require.NoError(t, err)
	assert.Len(t, events, 3)
	links, err := store.ListSessionLinks(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, []SessionLink{link}, links)
}

func TestOpenCreatesUsableSQLiteFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "tasks.db")
	store, err := Open(path)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	database, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, database.Close()) })
	var tables int
	require.NoError(t, database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table'`).Scan(&tables))
	assert.Positive(t, tables)
}

func ptr[T any](value T) *T { return &value }
