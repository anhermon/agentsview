package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.kenn.io/agentsview/internal/dbtest"
	"go.kenn.io/agentsview/internal/service"
	"go.kenn.io/agentsview/internal/taskcontrol"
)

type fakeMCPTaskService struct {
	patch taskcontrol.TaskPatch
	event taskcontrol.TaskEvent
}

func (f *fakeMCPTaskService) ListTasks(_ context.Context, project string) ([]taskcontrol.Task, error) {
	return []taskcontrol.Task{{ID: "task-1", Project: project, Title: "Board"}}, nil
}
func (f *fakeMCPTaskService) GetTask(_ context.Context, id string) (taskcontrol.Task, error) {
	return taskcontrol.Task{ID: id, Title: "Board"}, nil
}
func (f *fakeMCPTaskService) CreateTask(_ context.Context, task taskcontrol.Task) (taskcontrol.Task, error) {
	return task, nil
}
func (f *fakeMCPTaskService) UpdateTask(_ context.Context, id string, patch taskcontrol.TaskPatch) (taskcontrol.Task, error) {
	f.patch = patch
	return taskcontrol.Task{ID: id, Status: valueOrEmpty(patch.Status)}, nil
}
func (f *fakeMCPTaskService) AppendTaskEvent(_ context.Context, event taskcontrol.TaskEvent) (taskcontrol.TaskEvent, error) {
	f.event = event
	event.ID = 2
	return event, nil
}
func (f *fakeMCPTaskService) CompleteTask(_ context.Context, id, status, phase string) (taskcontrol.Task, error) {
	if status == "" {
		status = "Done"
	}
	return taskcontrol.Task{ID: id, Status: status, Phase: phase}, nil
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func TestNewServerRegistersAndRunsOptionalTaskTools(t *testing.T) {
	database := dbtest.OpenTestDB(t)
	tasks := &fakeMCPTaskService{}
	srv := newServer(ServeOptions{Service: service.NewDirectBackend(database, nil), TaskService: tasks})
	st, ct := newInMemoryPair(t, srv)

	tools, err := ct.ListTools(context.Background(), nil)
	require.NoError(t, err)
	require.Len(t, tools.Tools, 12)

	list, err := ct.CallTool(context.Background(), callParams(ToolListTasks, map[string]any{"project": "demo"}))
	require.NoError(t, err)
	var listOut listTasksOut
	raw, err := json.Marshal(list.StructuredContent)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &listOut))
	require.Len(t, listOut.Tasks, 1)
	assert.Equal(t, "demo", listOut.Tasks[0].Project)

	_, err = ct.CallTool(context.Background(), callParams(ToolUpdateTask, map[string]any{"id": "task-1", "status": "Review", "phase": "Verify"}))
	require.NoError(t, err)
	assert.Equal(t, "Review", *tasks.patch.Status)

	_, err = ct.CallTool(context.Background(), callParams(ToolRecordTaskEvent, map[string]any{"id": "task-1", "type": "agent.progress", "payload": map[string]any{"phase": "Execute"}}))
	require.NoError(t, err)
	assert.Equal(t, "agent", tasks.event.Source)

	completed, err := ct.CallTool(context.Background(), callParams(ToolCompleteTask, map[string]any{"id": "task-1"}))
	require.NoError(t, err)
	var completedOut taskOut
	raw, err = json.Marshal(completed.StructuredContent)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &completedOut))
	assert.Equal(t, "Done", completedOut.Task.Status)
	assert.Equal(t, "Deliver", completedOut.Task.Phase)

	require.NoError(t, ct.Close())
	require.NoError(t, st.Wait())
}
