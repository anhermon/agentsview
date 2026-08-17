package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.kenn.io/agentsview/internal/taskcontrol"
)

type fakeTaskService struct {
	tasks      []taskcontrol.Task
	created    taskcontrol.Task
	updatedID  string
	patch      taskcontrol.TaskPatch
	event      taskcontrol.TaskEvent
	completeID string
	status     string
	phase      string
}

func (f *fakeTaskService) ListTasks(_ context.Context, project string) ([]taskcontrol.Task, error) {
	if project == "" {
		return append([]taskcontrol.Task(nil), f.tasks...), nil
	}
	result := []taskcontrol.Task{}
	for _, task := range f.tasks {
		if task.Project == project {
			result = append(result, task)
		}
	}
	return result, nil
}
func (f *fakeTaskService) GetTask(_ context.Context, id string) (taskcontrol.Task, error) {
	for _, task := range f.tasks {
		if task.ID == id {
			return task, nil
		}
	}
	return taskcontrol.Task{ID: id}, nil
}
func (f *fakeTaskService) CreateTask(_ context.Context, task taskcontrol.Task) (taskcontrol.Task, error) {
	f.created = task
	task.ID = "task-new"
	return task, nil
}
func (f *fakeTaskService) UpdateTask(_ context.Context, id string, patch taskcontrol.TaskPatch) (taskcontrol.Task, error) {
	f.updatedID, f.patch = id, patch
	task := taskcontrol.Task{ID: id}
	if patch.Status != nil {
		task.Status = *patch.Status
	}
	if patch.Phase != nil {
		task.Phase = *patch.Phase
	}
	if patch.AssigneeID != nil {
		task.AssigneeID = *patch.AssigneeID
	}
	return task, nil
}
func (f *fakeTaskService) AppendTaskEvent(_ context.Context, event taskcontrol.TaskEvent) (taskcontrol.TaskEvent, error) {
	f.event = event
	event.ID = 1
	return event, nil
}
func (f *fakeTaskService) CompleteTask(_ context.Context, id, status, phase string) (taskcontrol.Task, error) {
	f.completeID, f.status, f.phase = id, status, phase
	return taskcontrol.Task{ID: id, Status: status, Phase: phase}, nil
}

func testTaskCommand(svc taskcontrol.TaskService) *cobra.Command {
	cmd := newTaskCommandWithDeps(taskCommandDeps{resolve: func(*cobra.Command) (taskcontrol.TaskService, error) { return svc, nil }})
	cmd.GroupID = ""
	return cmd
}

func TestTaskCommandListCreateAndShowJSON(t *testing.T) {
	fake := &fakeTaskService{tasks: []taskcontrol.Task{{ID: "task-1", Project: "demo", Title: "Board"}}}

	out, err := executeCommand(testTaskCommand(fake), "list", "--project", "demo", "--json")
	require.NoError(t, err)
	var list struct {
		Items []taskcontrol.Task `json:"items"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &list))
	require.Len(t, list.Items, 1)

	out, err = executeCommand(testTaskCommand(fake), "create", "--project", "demo", "--title", "Ship", "--type", "feature", "--json")
	require.NoError(t, err)
	assert.Equal(t, "demo", fake.created.Project)
	assert.Equal(t, "feature", fake.created.TaskType)
	var created taskcontrol.Task
	require.NoError(t, json.Unmarshal([]byte(out), &created))
	assert.Equal(t, "task-new", created.ID)

	_, err = executeCommand(testTaskCommand(fake), "show", "task-1", "--json")
	require.NoError(t, err)
}

func TestTaskCommandAssignmentTransitionEventAndCompletion(t *testing.T) {
	fake := &fakeTaskService{}

	_, err := executeCommand(testTaskCommand(fake), "update", "task-1", "--title", "Updated", "--priority", "0", "--json")
	require.NoError(t, err)
	assert.Equal(t, "Updated", *fake.patch.Title)
	require.NotNil(t, fake.patch.Priority)
	assert.Zero(t, *fake.patch.Priority)

	_, err = executeCommand(testTaskCommand(fake), "assign", "task-1", "agent-1", "--run", "session-1", "--json")
	require.NoError(t, err)
	assert.Equal(t, "agent-1", *fake.patch.AssigneeID)
	assert.Equal(t, "session-1", *fake.patch.ActiveRunID)

	_, err = executeCommand(testTaskCommand(fake), "transition", "task-1", "Review", "--phase", "Verify", "--json")
	require.NoError(t, err)
	assert.Equal(t, "Review", *fake.patch.Status)
	assert.Equal(t, "Verify", *fake.patch.Phase)

	out, err := executeCommand(testTaskCommand(fake), "event", "task-1", "--type", "agent.progress", "--source", "codex", "--payload", `{"phase":"Execute"}`)
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":1,"task_id":"task-1","type":"agent.progress","source":"codex","payload":{"phase":"Execute"},"created_at":"0001-01-01T00:00:00Z"}`, out)

	_, err = executeCommand(testTaskCommand(fake), "complete", "task-1", "--json")
	require.NoError(t, err)
	assert.Equal(t, "task-1", fake.completeID)
	assert.Equal(t, "Done", fake.status)
	assert.Equal(t, "Deliver", fake.phase)
}

func TestRootRegistersTaskCommand(t *testing.T) {
	root := newRootCommand()
	for _, command := range root.Commands() {
		if command.Name() == "task" {
			return
		}
	}
	t.Fatal("root command does not register task")
}
