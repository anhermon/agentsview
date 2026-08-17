package server

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.kenn.io/agentsview/internal/taskcontrol"
	"go.kenn.io/agentsview/internal/taskrun"
)

func TestTaskRouteAssignmentStartsOneRunWhileIdleStartsNone(t *testing.T) {
	store, err := taskcontrol.Open(filepath.Join(t.TempDir(), "tasks.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	adapter := newServerRuntimeFakeAdapter()
	runtime, err := taskrun.NewRuntime(t.TempDir(), adapter)
	require.NoError(t, err)
	srv := testServer(t, 30*time.Second, WithTaskStore(store), WithTaskRuntime(runtime))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, srv.Shutdown(ctx))
	})

	agentResponse := taskRouteRequest(t, srv, http.MethodPost, "/api/v1/task-agents", map[string]any{
		"name": "Local fake", "harness": adapter.ID(), "mode": "managed",
	})
	require.Equal(t, http.StatusCreated, agentResponse.Code, agentResponse.Body.String())
	agent := decodeTaskRouteBody[taskAgentView](t, agentResponse)

	idleResponse := taskRouteRequest(t, srv, http.MethodPost, "/api/v1/tasks", map[string]any{
		"project": "demo", "title": "Idle until assigned",
	})
	require.Equal(t, http.StatusCreated, idleResponse.Code, idleResponse.Body.String())
	idleTask := decodeTaskRouteBody[taskView](t, idleResponse)
	assert.Equal(t, 0, adapter.launchCount())

	assignedResponse := taskRouteRequest(t, srv, http.MethodPatch, "/api/v1/tasks/"+idleTask.ID, map[string]any{
		"assignee_id": agent.ID,
	})
	require.Equal(t, http.StatusOK, assignedResponse.Code, assignedResponse.Body.String())
	assigned := decodeTaskRouteBody[taskView](t, assignedResponse)
	assert.Equal(t, "In Progress", assigned.Status)
	assert.Equal(t, "run-1", assigned.ActiveRunID)
	assert.Equal(t, 1, adapter.launchCount())

	repeatedResponse := taskRouteRequest(t, srv, http.MethodPatch, "/api/v1/tasks/"+idleTask.ID, map[string]any{
		"assignee_id": agent.ID,
	})
	require.Equal(t, http.StatusOK, repeatedResponse.Code, repeatedResponse.Body.String())
	assert.Equal(t, 1, adapter.launchCount())

	adapter.complete("run-1")
	waitForTaskRunToFinish(t, srv, store, idleTask.ID)
	for index, eventType := range []string{"task.dependency-cleared", "task.mentioned", "task.retry"} {
		eventResponse := taskRouteRequest(t, srv, http.MethodPost,
			"/api/v1/tasks/"+idleTask.ID+"/events", map[string]any{
				"type": eventType, "source": "test",
			})
		require.Equal(t, http.StatusCreated, eventResponse.Code, eventResponse.Body.String())
		expectedRuns := index + 2
		assert.Equal(t, expectedRuns, adapter.launchCount())
		runID := fmt.Sprintf("run-%d", expectedRuns)
		adapter.complete(runID)
		waitForTaskRunToFinish(t, srv, store, idleTask.ID)
	}
}

func waitForTaskRunToFinish(t *testing.T, srv *Server, store *taskcontrol.Store, taskID string) {
	t.Helper()
	require.Eventually(t, func() bool {
		task, err := store.Task(context.Background(), taskID)
		srv.mu.RLock()
		runner := srv.taskRunner
		srv.mu.RUnlock()
		_, active := runner.ActiveRun(taskID)
		return err == nil && task.ActiveRunID == "" && !active
	}, 5*time.Second, 10*time.Millisecond)
}

type serverRuntimeFakeAdapter struct {
	mu       sync.Mutex
	launches int
	runs     map[string]chan taskrun.Event
}

func newServerRuntimeFakeAdapter() *serverRuntimeFakeAdapter {
	return &serverRuntimeFakeAdapter{runs: make(map[string]chan taskrun.Event)}
}

func (a *serverRuntimeFakeAdapter) ID() string { return "route-fake" }

func (a *serverRuntimeFakeAdapter) Capabilities() taskrun.Capabilities {
	return taskrun.Capabilities{
		taskrun.CapabilityLaunch: true, taskrun.CapabilityCancel: true, taskrun.CapabilityObserve: true,
	}
}

func (a *serverRuntimeFakeAdapter) Launch(_ context.Context, _ taskrun.LaunchRequest) (taskrun.RunRef, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.launches++
	runID := fmt.Sprintf("run-%d", a.launches)
	a.runs[runID] = make(chan taskrun.Event, 4)
	return taskrun.RunRef{ID: runID}, nil
}

func (a *serverRuntimeFakeAdapter) Observe(_ context.Context, runID string) (<-chan taskrun.Event, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.runs[runID], nil
}

func (a *serverRuntimeFakeAdapter) Cancel(_ context.Context, runID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if events, ok := a.runs[runID]; ok {
		events <- taskrun.Event{Type: taskrun.EventCancelled}
		close(events)
		delete(a.runs, runID)
	}
	return nil
}

func (a *serverRuntimeFakeAdapter) complete(runID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if events, ok := a.runs[runID]; ok {
		events <- taskrun.Event{Type: taskrun.EventCompleted}
		close(events)
		delete(a.runs, runID)
	}
}

func (a *serverRuntimeFakeAdapter) launchCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.launches
}
