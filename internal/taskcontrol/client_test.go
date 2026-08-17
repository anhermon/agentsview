package taskcontrol

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPClientTaskLifecycle(t *testing.T) {
	t.Helper()
	var patches []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer secret", r.Header.Get("Authorization"))
		if r.Method != http.MethodGet {
			assert.NotEmpty(t, r.Header.Get("Origin"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/tasks":
			assert.Equal(t, "demo", r.URL.Query().Get("project"))
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"items": []Task{{ID: "task-1", Project: "demo", Title: "Build board"}},
			}))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/tasks/task-1":
			require.NoError(t, json.NewEncoder(w).Encode(Task{ID: "task-1", Title: "Build board"}))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/tasks/task-1":
			var patch map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&patch))
			patches = append(patches, patch)
			task := Task{ID: "task-1", Title: "Build board"}
			if value, ok := patch["status"].(string); ok {
				task.Status = value
			}
			if value, ok := patch["phase"].(string); ok {
				task.Phase = value
			}
			require.NoError(t, json.NewEncoder(w).Encode(task))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/tasks/task-1/events":
			var event TaskEvent
			require.NoError(t, json.NewDecoder(r.Body).Decode(&event))
			event.ID, event.TaskID = 7, "task-1"
			require.NoError(t, json.NewEncoder(w).Encode(event))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	client := NewHTTPClient(server.URL, "secret")
	tasks, err := client.ListTasks(context.Background(), "demo")
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	assert.Equal(t, "task-1", tasks[0].ID)

	task, err := client.GetTask(context.Background(), "task-1")
	require.NoError(t, err)
	assert.Equal(t, "Build board", task.Title)

	assignee := "agent-1"
	_, err = client.UpdateTask(context.Background(), "task-1", TaskPatch{AssigneeID: &assignee})
	require.NoError(t, err)
	completed, err := client.CompleteTask(context.Background(), "task-1", "", "Deliver")
	require.NoError(t, err)
	assert.Equal(t, "Done", completed.Status)
	assert.Equal(t, "Deliver", completed.Phase)
	require.Len(t, patches, 2)
	assert.Equal(t, "agent-1", patches[0]["assignee_id"])
	assert.Equal(t, "Done", patches[1]["status"])

	event, err := client.AppendTaskEvent(context.Background(), TaskEvent{
		TaskID: "task-1", Type: "agent.progress", Source: "codex",
		Payload: map[string]any{"phase": "Execute"},
	})
	require.NoError(t, err)
	assert.Equal(t, int64(7), event.ID)
}

func TestHTTPClientSurfacesAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"gate tests is pending"}`))
	}))
	t.Cleanup(server.Close)

	_, err := NewHTTPClient(server.URL, "").CompleteTask(
		context.Background(), "task-1", "Done", "Deliver",
	)
	require.EqualError(t, err, "task API: gate tests is pending")
}
