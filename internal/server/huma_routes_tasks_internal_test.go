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
}
