package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.kenn.io/agentsview/internal/taskcontrol"
)

func taskRouteServer(t *testing.T) (*Server, *Broadcaster) {
	t.Helper()
	store, err := taskcontrol.Open(filepath.Join(t.TempDir(), "tasks.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	broadcaster := NewBroadcaster(0)
	return testServer(t, 30*time.Second, WithTaskStore(store), WithBroadcaster(broadcaster)), broadcaster
}

func taskRouteRequest(t *testing.T, srv *Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		require.NoError(t, err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(data))
	req.Host = "127.0.0.1:0"
	req.RemoteAddr = "127.0.0.1:1234"
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet {
		req.Header.Set("Origin", "http://127.0.0.1:0")
	}
	recorder := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recorder, req)
	return recorder
}

func decodeTaskRouteBody[T any](t *testing.T, recorder *httptest.ResponseRecorder) T {
	t.Helper()
	var result T
	require.NoError(t, json.NewDecoder(recorder.Body).Decode(&result), recorder.Body.String())
	return result
}

func TestTaskRoutesCompletionWorkflowAndBroadcast(t *testing.T) {
	srv, broadcaster := taskRouteServer(t)
	events, unsubscribe := broadcaster.Subscribe()
	t.Cleanup(unsubscribe)

	agentResponse := taskRouteRequest(t, srv, http.MethodPost, "/api/v1/task-agents", map[string]any{
		"name": "Codex", "harness": "codex", "mode": "managed",
	})
	require.Equal(t, http.StatusCreated, agentResponse.Code, agentResponse.Body.String())
	agent := decodeTaskRouteBody[taskAgentView](t, agentResponse)

	taskResponse := taskRouteRequest(t, srv, http.MethodPost, "/api/v1/tasks", map[string]any{
		"project": "demo", "title": "Build board", "type": "feature",
		"assignee_id": agent.ID, "active_run_id": "run-1",
	})
	require.Equal(t, http.StatusCreated, taskResponse.Code, taskResponse.Body.String())
	task := decodeTaskRouteBody[taskView](t, taskResponse)
	assert.Equal(t, "Backlog", task.Status)
	assert.Equal(t, "Understand", task.Phase)
	assert.Equal(t, agent.Name, task.AssigneeName)
	assert.Empty(t, task.Gates)

	linkResponse := taskRouteRequest(t, srv, http.MethodPost,
		"/api/v1/tasks/"+task.ID+"/session-links", map[string]any{
			"session_id": "codex:session/1", "harness": "codex", "method": "explicit",
		})
	require.Equal(t, http.StatusCreated, linkResponse.Code, linkResponse.Body.String())
	activityResponse := taskRouteRequest(t, srv, http.MethodPost,
		"/api/v1/tasks/"+task.ID+"/events", map[string]any{
			"type": "agent.run.progress", "source": "codex",
			"payload": map[string]any{"run_id": "run-1", "phase": "Verify", "message": "Running tests"},
		})
	require.Equal(t, http.StatusCreated, activityResponse.Code, activityResponse.Body.String())

	select {
	case event := <-events:
		assert.Equal(t, "tasks", event.Scope)
	case <-time.After(time.Second):
		t.Fatal("expected task refresh event")
	}

	gateResponse := taskRouteRequest(t, srv, http.MethodPost,
		"/api/v1/tasks/"+task.ID+"/gates", map[string]any{
			"name": "Tests pass", "kind": "deterministic", "rule": "go_test",
		})
	require.Equal(t, http.StatusCreated, gateResponse.Code, gateResponse.Body.String())
	gate := decodeTaskRouteBody[taskcontrol.Gate](t, gateResponse)
	assert.True(t, gate.Required)

	blocked := taskRouteRequest(t, srv, http.MethodPatch, "/api/v1/tasks/"+task.ID,
		map[string]any{"status": "Done"})
	assert.Equal(t, http.StatusConflict, blocked.Code, blocked.Body.String())

	evaluated := taskRouteRequest(t, srv, http.MethodPost,
		"/api/v1/tasks/"+task.ID+"/gates/"+gate.ID+"/evaluate",
		map[string]any{"evidence": map[string]any{"passed": true, "command": "go test ./..."}})
	require.Equal(t, http.StatusOK, evaluated.Code, evaluated.Body.String())

	completed := taskRouteRequest(t, srv, http.MethodPatch, "/api/v1/tasks/"+task.ID,
		map[string]any{"status": "Done", "phase": "Deliver"})
	require.Equal(t, http.StatusOK, completed.Code, completed.Body.String())
	completedTask := decodeTaskRouteBody[taskView](t, completed)
	assert.Equal(t, "Done", completedTask.Status)
	assert.Equal(t, "Deliver", completedTask.Phase)
	assert.Len(t, completedTask.Evidence, 1)

	detailResponse := taskRouteRequest(t, srv, http.MethodGet, "/api/v1/tasks/"+task.ID, nil)
	require.Equal(t, http.StatusOK, detailResponse.Code, detailResponse.Body.String())
	detail := decodeTaskRouteBody[taskDetailView](t, detailResponse)
	require.NotNil(t, detail.AgentSession)
	assert.Equal(t, agent.ID, detail.AgentSession.AgentID)
	assert.Equal(t, "run-1", detail.AgentSession.RunID)
	assert.Equal(t, "running", detail.AgentSession.RunState)
	assert.Equal(t, "Verify", detail.AgentSession.Phase)
	require.Len(t, detail.AgentSession.RecentActivity, 1)
	require.Len(t, detail.AgentSession.Links, 1)
	assert.Equal(t, "/api/v1/sessions/codex:session%2F1", detail.AgentSession.Links[0].DetailAPIURL)
	assert.Equal(t, "/api/v1/sessions/codex:session%2F1/messages?limit=20&direction=desc",
		detail.AgentSession.Links[0].RecentMessagesAPIURL)
	assert.Equal(t, "/sessions/codex:session%2F1", detail.AgentSession.Links[0].FullSessionURL)

	listResponse := taskRouteRequest(t, srv, http.MethodGet, "/api/v1/tasks?project=demo", nil)
	require.Equal(t, http.StatusOK, listResponse.Code, listResponse.Body.String())
	list := decodeTaskRouteBody[itemsBody[taskView]](t, listResponse)
	require.Len(t, list.Items, 1)
	assert.Equal(t, task.ID, list.Items[0].ID)
}

func TestTaskRoutesWorkflowSessionLinksAndEvents(t *testing.T) {
	srv, _ := taskRouteServer(t)

	workflowResponse := taskRouteRequest(t, srv, http.MethodGet,
		"/api/v1/task-workflows/demo", nil)
	require.Equal(t, http.StatusOK, workflowResponse.Code, workflowResponse.Body.String())
	workflow := decodeTaskRouteBody[workflowView](t, workflowResponse)
	assert.False(t, workflow.AutomaticTransitionsEnabled)
	assert.Len(t, workflow.Columns, 6)

	updatedResponse := taskRouteRequest(t, srv, http.MethodPut,
		"/api/v1/task-workflows/demo", map[string]any{
			"columns": []map[string]any{{"id": "Queue", "label": "Queue", "position": 0},
				{"id": "Shipped", "label": "Shipped", "position": 1}},
			"automatic_transitions_enabled": true,
		})
	require.Equal(t, http.StatusOK, updatedResponse.Code, updatedResponse.Body.String())
	workflow = decodeTaskRouteBody[workflowView](t, updatedResponse)
	assert.True(t, workflow.AutomaticTransitionsEnabled)

	taskResponse := taskRouteRequest(t, srv, http.MethodPost, "/api/v1/tasks",
		map[string]any{"project": "demo", "title": "Trace session"})
	require.Equal(t, http.StatusCreated, taskResponse.Code, taskResponse.Body.String())
	task := decodeTaskRouteBody[taskView](t, taskResponse)

	linkResponse := taskRouteRequest(t, srv, http.MethodPost,
		"/api/v1/tasks/"+task.ID+"/session-links", map[string]any{
			"session_id": "session-1", "harness": "claude", "method": "inferred", "confidence": 0.82,
		})
	require.Equal(t, http.StatusCreated, linkResponse.Code, linkResponse.Body.String())
	link := decodeTaskRouteBody[taskcontrol.SessionLink](t, linkResponse)
	assert.True(t, link.Active)
	assert.InDelta(t, 0.82, link.Confidence, 0.001)

	eventResponse := taskRouteRequest(t, srv, http.MethodPost,
		"/api/v1/tasks/"+task.ID+"/events", map[string]any{
			"type": "agent.progress", "source": "adapter", "payload": map[string]any{"phase": "Execute"},
		})
	require.Equal(t, http.StatusCreated, eventResponse.Code, eventResponse.Body.String())

	eventsResponse := taskRouteRequest(t, srv, http.MethodGet,
		"/api/v1/tasks/"+task.ID+"/events", nil)
	require.Equal(t, http.StatusOK, eventsResponse.Code, eventsResponse.Body.String())
	events := decodeTaskRouteBody[itemsBody[taskcontrol.TaskEvent]](t, eventsResponse)
	assert.Len(t, events.Items, 3)

	detailResponse := taskRouteRequest(t, srv, http.MethodGet,
		"/api/v1/tasks/"+task.ID, nil)
	require.Equal(t, http.StatusOK, detailResponse.Code, detailResponse.Body.String())
	detail := decodeTaskRouteBody[taskDetailView](t, detailResponse)
	assert.Len(t, detail.Events, 3)
	assert.Equal(t, []taskcontrol.SessionLink{link}, detail.SessionLinks)
	assert.True(t, detail.GateSummary.CompletionReady)
	assert.False(t, detail.EventsTruncated)
}

func TestTaskMetricsRouteFiltersAndEmptyData(t *testing.T) {
	srv, _ := taskRouteServer(t)

	emptyResponse := taskRouteRequest(t, srv, http.MethodGet, "/api/v1/task-metrics", nil)
	require.Equal(t, http.StatusOK, emptyResponse.Code, emptyResponse.Body.String())
	empty := decodeTaskRouteBody[taskcontrol.TaskMetrics](t, emptyResponse)
	assert.Zero(t, empty.TotalTasks)
	assert.NotNil(t, empty.Counts.ByProject)
	assert.NotNil(t, empty.Timing.PhaseTime)

	for _, task := range []map[string]any{
		{"id": "metric-a", "project": "alpha", "title": "A", "type": "feature"},
		{"id": "metric-b", "project": "beta", "title": "B", "type": "bug"},
	} {
		response := taskRouteRequest(t, srv, http.MethodPost, "/api/v1/tasks", task)
		require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
	}

	response := taskRouteRequest(t, srv, http.MethodGet,
		"/api/v1/task-metrics?project=alpha&type=feature&status=Backlog&phase=Understand", nil)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	metrics := decodeTaskRouteBody[taskcontrol.TaskMetrics](t, response)
	assert.Equal(t, 1, metrics.TotalTasks)
	assert.Equal(t, map[string]int{"alpha": 1}, metrics.Counts.ByProject)
	assert.Equal(t, map[string]int{"feature": 1}, metrics.Counts.ByType)
}

func TestTaskMetricsRouteRejectsBadTimeParameters(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{name: "bad from", path: "/api/v1/task-metrics?from=tomorrow"},
		{name: "bad to", path: "/api/v1/task-metrics?to=never"},
		{name: "reversed", path: "/api/v1/task-metrics?from=2026-01-02T00:00:00Z&to=2026-01-01T00:00:00Z"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			srv, _ := taskRouteServer(t)
			response := taskRouteRequest(t, srv, http.MethodGet, test.path, nil)
			assert.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
		})
	}
}
