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
	running, err := store.Task(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, "run-1", running.ActiveRunID)
	request := adapter.lastRequest()
	require.Len(t, request.Envelope.Instructions, 3)
	assert.Contains(t, request.Envelope.Instructions[0], "CLI or task MCP")
	assert.Contains(t, request.Envelope.Instructions[2], "Review with phase Verify")

	adapter.emit(taskrun.Event{Type: taskrun.EventPhaseChanged, Phase: "Execute"})
	adapter.emit(taskrun.Event{Type: taskrun.EventProgress, Data: map[string]any{"completed": 2, "total": 3}})
	adapter.emit(taskrun.Event{Type: taskrun.EventOutput, Message: "raw transcript must not persist"})
	adapter.emit(taskrun.Event{Type: taskrun.EventActivity, SessionID: "session-1", Message: "item.started: command_execution"})
	adapter.emit(taskrun.Event{Type: taskrun.EventProgress, Data: map[string]any{"oversized": string(make([]byte, 5000))}})
	adapter.emit(taskrun.Event{Type: taskrun.EventCompleted})
	adapter.closeEvents()

	require.Eventually(t, func() bool {
		updated, taskErr := store.Task(ctx, task.ID)
		return taskErr == nil && updated.Status == "Review" && updated.ActiveRunID == ""
	}, 5*time.Second, 10*time.Millisecond)
	updated, err := store.Task(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, "Verify", updated.Phase)
	links, err := store.ListSessionLinks(ctx, task.ID)
	require.NoError(t, err)
	require.Len(t, links, 1)
	assert.Equal(t, "session-1", links[0].SessionID)
	assert.Equal(t, "fake", links[0].Harness)
	assert.Equal(t, "runtime", links[0].Method)
	assert.False(t, links[0].Active, "session link must deactivate once the run completes")
	events, err := store.ListEvents(ctx, task.ID)
	require.NoError(t, err)
	types := make([]string, 0, len(events))
	for _, event := range events {
		types = append(types, event.Type)
	}
	assert.Contains(t, types, "agent.run.dispatched")
	assert.Contains(t, types, "agent.run.phase-changed")
	assert.Contains(t, types, "agent.run.progress")
	assert.Contains(t, types, "agent.run.activity")
	assert.Contains(t, types, "agent.run.completed")
	sawTruncatedData := false
	for _, event := range events {
		assert.NotContains(t, fmt.Sprint(event.Payload), "raw transcript must not persist")
		if event.Type == "agent.run.activity" {
			assert.Equal(t, "run-1", event.Payload["run_id"])
			assert.Equal(t, "session-1", event.Payload["session_id"])
		}
		if event.Type == "agent.run.progress" {
			if data, ok := event.Payload["data"].(map[string]any); ok && data["truncated"] == true {
				assert.Len(t, data, 1)
				sawTruncatedData = true
			}
		}
	}
	assert.True(t, sawTruncatedData)
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
	requests []taskrun.LaunchRequest
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

func (a *coordinatorFakeAdapter) Launch(_ context.Context, request taskrun.LaunchRequest) (taskrun.RunRef, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.launches++
	a.requests = append(a.requests, request)
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

func (a *coordinatorFakeAdapter) lastRequest() taskrun.LaunchRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.requests[len(a.requests)-1]
}
