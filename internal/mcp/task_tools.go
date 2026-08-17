package mcp

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"go.kenn.io/agentsview/internal/taskcontrol"
)

type listTasksIn struct {
	Project string `json:"project,omitempty" jsonschema:"Restrict tasks to one project."`
}

type listTasksOut struct {
	Tasks []taskcontrol.Task `json:"tasks"`
}

func (t *toolset) listTasks(
	ctx context.Context, _ *mcp.CallToolRequest, in listTasksIn,
) (*mcp.CallToolResult, listTasksOut, error) {
	if t.taskSvc == nil {
		return nil, listTasksOut{}, errors.New("task service is not available")
	}
	tasks, err := t.taskSvc.ListTasks(ctx, in.Project)
	if err != nil {
		return nil, listTasksOut{}, err
	}
	return nil, listTasksOut{Tasks: tasks}, nil
}

type taskIDIn struct {
	ID string `json:"id" jsonschema:"Task ID."`
}

type taskOut struct {
	Task taskcontrol.Task `json:"task"`
}

func (t *toolset) getTask(
	ctx context.Context, _ *mcp.CallToolRequest, in taskIDIn,
) (*mcp.CallToolResult, taskOut, error) {
	if t.taskSvc == nil {
		return nil, taskOut{}, errors.New("task service is not available")
	}
	task, err := t.taskSvc.GetTask(ctx, in.ID)
	if err != nil {
		return nil, taskOut{}, err
	}
	return nil, taskOut{Task: task}, nil
}

type updateTaskIn struct {
	ID          string  `json:"id" jsonschema:"Task ID."`
	Title       *string `json:"title,omitempty"`
	Description *string `json:"description,omitempty"`
	Type        *string `json:"type,omitempty"`
	Status      *string `json:"status,omitempty"`
	Phase       *string `json:"phase,omitempty"`
	Priority    *int    `json:"priority,omitempty"`
	AssigneeID  *string `json:"assignee_id,omitempty"`
	ActiveRunID *string `json:"active_run_id,omitempty"`
}

func (t *toolset) updateTask(
	ctx context.Context, _ *mcp.CallToolRequest, in updateTaskIn,
) (*mcp.CallToolResult, taskOut, error) {
	if t.taskSvc == nil {
		return nil, taskOut{}, errors.New("task service is not available")
	}
	if in.Title == nil && in.Description == nil && in.Type == nil &&
		in.Status == nil && in.Phase == nil && in.Priority == nil &&
		in.AssigneeID == nil && in.ActiveRunID == nil {
		return nil, taskOut{}, errors.New("at least one task field is required")
	}
	task, err := t.taskSvc.UpdateTask(ctx, in.ID, taskcontrol.TaskPatch{
		Title: in.Title, Description: in.Description, TaskType: in.Type,
		Status: in.Status, Phase: in.Phase, Priority: in.Priority,
		AssigneeID: in.AssigneeID, ActiveRunID: in.ActiveRunID,
	})
	if err != nil {
		return nil, taskOut{}, err
	}
	return nil, taskOut{Task: task}, nil
}

type recordTaskEventIn struct {
	ID      string         `json:"id" jsonschema:"Task ID."`
	Type    string         `json:"type" jsonschema:"Event type, for example agent.progress."`
	Source  string         `json:"source,omitempty" jsonschema:"Event producer; defaults to agent."`
	Payload map[string]any `json:"payload,omitempty" jsonschema:"Small structured event payload."`
}

type taskEventOut struct {
	Event taskcontrol.TaskEvent `json:"event"`
}

func (t *toolset) recordTaskEvent(
	ctx context.Context, _ *mcp.CallToolRequest, in recordTaskEventIn,
) (*mcp.CallToolResult, taskEventOut, error) {
	if t.taskSvc == nil {
		return nil, taskEventOut{}, errors.New("task service is not available")
	}
	if in.Source == "" {
		in.Source = "agent"
	}
	event, err := t.taskSvc.AppendTaskEvent(ctx, taskcontrol.TaskEvent{
		TaskID: in.ID, Type: in.Type, Source: in.Source, Payload: in.Payload,
	})
	if err != nil {
		return nil, taskEventOut{}, err
	}
	return nil, taskEventOut{Event: event}, nil
}

type completeTaskIn struct {
	ID     string `json:"id" jsonschema:"Task ID."`
	Status string `json:"status,omitempty" jsonschema:"Terminal workflow state; defaults to Done."`
	Phase  string `json:"phase,omitempty" jsonschema:"Completion phase; defaults to Deliver."`
}

func (t *toolset) completeTask(
	ctx context.Context, _ *mcp.CallToolRequest, in completeTaskIn,
) (*mcp.CallToolResult, taskOut, error) {
	if t.taskSvc == nil {
		return nil, taskOut{}, errors.New("task service is not available")
	}
	if in.Phase == "" {
		in.Phase = "Deliver"
	}
	task, err := t.taskSvc.CompleteTask(ctx, in.ID, in.Status, in.Phase)
	if err != nil {
		return nil, taskOut{}, err
	}
	return nil, taskOut{Task: task}, nil
}
