package taskcontrol

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.kenn.io/agentsview/internal/taskrun"
)

func TestRunCoordinatorAssignmentStartsOneAdapterAndPersistsEvents(t *testing.T) {
	t.Parallel()

	store := openRunCoordinatorStore(t)
	ctx := context.Background()
	agent, err := store.CreateAgent(ctx, Agent{ID: "agent-1", Name: "Test agent", Harness: "fake"})
	require.NoError(t, err)
	task, err := store.CreateTask(ctx, Task{
		ID: "task-1", Project: "demo", Title: "Implement event bridge", AssigneeID: agent.ID,
	})
	require.NoError(t, err)
	adapter := newCoordinatorFakeAdapter()
	runtime, err := taskrun.NewRuntime(t.TempDir(), adapter)
	require.NoError(t, err)
	coordinator := NewRunCoordinator(ctx, store, runtime, nil)
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, coordinator.Close(closeCtx))
	})

	require.NoError(t, coordinator.Trigger(task.ID, taskrun.TriggerAssignment))
	require.ErrorIs(t, coordinator.Trigger(task.ID, taskrun.TriggerRetry), taskrun.ErrActiveRun)
	assert.Equal(t, 1, adapter.launchCount())

	adapter.emit(taskrun.Event{Type: taskrun.EventPhaseChanged, Phase: "Execute"})
	adapter.emit(taskrun.Event{Type: taskrun.EventProgress, Data: map[string]any{"completed": 2, "total": 3}})
	adapter.emit(taskrun.Event{Type: taskrun.EventCompleted})
	adapter.closeEvents()

	require.Eventually(t, func() bool {
		updated, taskErr := store.Task(ctx, task.ID)
		return taskErr == nil && updated.Status == "Done" && updated.ActiveRunID == ""
	}, 5*time.Second, 10*time.Millisecond)
	updated, err := store.Task(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, "Deliver", updated.Phase)
	events, err := store.ListEvents(ctx, task.ID)
	require.NoError(t, err)
	types := make([]string, 0, len(events))
	for _, event := range events {
		types = append(types, event.Type)
	}
	assert.Contains(t, types, "agent.run.dispatched")
	assert.Contains(t, types, "agent.run.phase-changed")
	assert.Contains(t, types, "agent.run.progress")
	assert.Contains(t, types, "agent.run.completed")
}

func TestRunCoordinatorIdleTaskStartsNoAdapter(t *testing.T) {
	t.Parallel()

	store := openRunCoordinatorStore(t)
	task, err := store.CreateTask(context.Background(), Task{
		ID: "task-idle", Project: "demo", Title: "Wait for assignment",
	})
	require.NoError(t, err)
	adapter := newCoordinatorFakeAdapter()
	runtime, err := taskrun.NewRuntime(t.TempDir(), adapter)
	require.NoError(t, err)
	coordinator := NewRunCoordinator(context.Background(), store, runtime, nil)

	err = coordinator.Trigger(task.ID, taskrun.TriggerAssignment)
	require.ErrorIs(t, err, ErrTaskHasNoAssignee)
	assert.Equal(t, 0, adapter.launchCount())
}

func TestTriggerTypeForEvent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		event   string
		trigger taskrun.TriggerType
		ok      bool
	}{
		{event: "task.assigned", trigger: taskrun.TriggerAssignment, ok: true},
		{event: "task.dependency-cleared", trigger: taskrun.TriggerDependencyCleared, ok: true},
		{event: "task.mentioned", trigger: taskrun.TriggerMention, ok: true},
		{event: "task.retry", trigger: taskrun.TriggerRetry, ok: true},
		{event: "agent.progress", ok: false},
	}
	for _, test := range tests {
		t.Run(test.event, func(t *testing.T) {
			t.Parallel()
			trigger, ok := TriggerTypeForEvent(test.event)
			assert.Equal(t, test.ok, ok)
			assert.Equal(t, test.trigger, trigger)
		})
	}
}

func openRunCoordinatorStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "tasks.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	return store
}

type coordinatorFakeAdapter struct {
	mu       sync.Mutex
	launches int
	events   chan taskrun.Event
	closed   bool
}

func newCoordinatorFakeAdapter() *coordinatorFakeAdapter {
	return &coordinatorFakeAdapter{events: make(chan taskrun.Event, 16)}
}

func (a *coordinatorFakeAdapter) ID() string { return "fake" }

func (a *coordinatorFakeAdapter) Capabilities() taskrun.Capabilities {
	return taskrun.Capabilities{
		taskrun.CapabilityLaunch: true, taskrun.CapabilityCancel: true, taskrun.CapabilityObserve: true,
	}
}

func (a *coordinatorFakeAdapter) Launch(_ context.Context, _ taskrun.LaunchRequest) (taskrun.RunRef, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.launches++
	return taskrun.RunRef{ID: fmt.Sprintf("run-%d", a.launches)}, nil
}

func (a *coordinatorFakeAdapter) Observe(_ context.Context, _ string) (<-chan taskrun.Event, error) {
	return a.events, nil
}

func (a *coordinatorFakeAdapter) Cancel(_ context.Context, _ string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.closed {
		a.events <- taskrun.Event{Type: taskrun.EventCancelled}
		close(a.events)
		a.closed = true
	}
	return nil
}

func (a *coordinatorFakeAdapter) emit(event taskrun.Event) { a.events <- event }

func (a *coordinatorFakeAdapter) closeEvents() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.closed {
		close(a.events)
		a.closed = true
	}
}

func (a *coordinatorFakeAdapter) launchCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.launches
}
