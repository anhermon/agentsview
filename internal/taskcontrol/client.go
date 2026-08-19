package taskcontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// TaskService is the compact agent-facing capability shared by the CLI and
// MCP adapters. It deliberately excludes database and session-archive details.
type TaskService interface {
	ListTasks(context.Context, string) ([]Task, error)
	GetTask(context.Context, string) (Task, error)
	CreateTask(context.Context, Task) (Task, error)
	UpdateTask(context.Context, string, TaskPatch) (Task, error)
	AppendTaskEvent(context.Context, TaskEvent) (TaskEvent, error)
	CompleteTask(context.Context, string, string, string) (Task, error)
	ListGates(context.Context, string) ([]Gate, error)
	CreateGate(context.Context, Gate) (Gate, error)
	EvaluateGate(ctx context.Context, taskID, gateID string, approved *bool, evidence map[string]any) (Gate, error)
}

type HTTPClient struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewHTTPClient(baseURL, token string) *HTTPClient {
	return &HTTPClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   strings.TrimSpace(token),
		client:  http.DefaultClient,
	}
}

type taskListResponse struct {
	Items []Task `json:"items"`
}

func (c *HTTPClient) ListTasks(ctx context.Context, project string) ([]Task, error) {
	path := "/api/v1/tasks"
	if project != "" {
		path += "?project=" + url.QueryEscape(project)
	}
	var response taskListResponse
	if err := c.do(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, err
	}
	if response.Items == nil {
		response.Items = []Task{}
	}
	return response.Items, nil
}

func (c *HTTPClient) GetTask(ctx context.Context, id string) (Task, error) {
	var task Task
	err := c.do(ctx, http.MethodGet, taskPath(id), nil, &task)
	return task, err
}

func (c *HTTPClient) CreateTask(ctx context.Context, task Task) (Task, error) {
	body := map[string]any{
		"id": task.ID, "project": task.Project, "title": task.Title,
		"description": task.Description, "type": task.TaskType,
		"status": task.Status, "phase": task.Phase, "priority": task.Priority,
		"assignee_id": task.AssigneeID, "active_run_id": task.ActiveRunID,
	}
	var created Task
	err := c.do(ctx, http.MethodPost, "/api/v1/tasks", body, &created)
	return created, err
}

func (c *HTTPClient) UpdateTask(
	ctx context.Context, id string, patch TaskPatch,
) (Task, error) {
	body := map[string]any{}
	putPointer(body, "title", patch.Title)
	putPointer(body, "description", patch.Description)
	putPointer(body, "type", patch.TaskType)
	putPointer(body, "status", patch.Status)
	putPointer(body, "phase", patch.Phase)
	putPointer(body, "priority", patch.Priority)
	putPointer(body, "assignee_id", patch.AssigneeID)
	putPointer(body, "active_run_id", patch.ActiveRunID)
	var updated Task
	err := c.do(ctx, http.MethodPatch, taskPath(id), body, &updated)
	return updated, err
}

func putPointer[T any](body map[string]any, key string, value *T) {
	if value != nil {
		body[key] = *value
	}
}

func (c *HTTPClient) AppendTaskEvent(
	ctx context.Context, event TaskEvent,
) (TaskEvent, error) {
	body := map[string]any{
		"type": event.Type, "source": event.Source, "payload": event.Payload,
	}
	var created TaskEvent
	err := c.do(ctx, http.MethodPost, taskPath(event.TaskID)+"/events", body, &created)
	return created, err
}

func (c *HTTPClient) CompleteTask(
	ctx context.Context, id, status, phase string,
) (Task, error) {
	if status == "" {
		status = "Done"
	}
	patch := TaskPatch{Status: &status}
	if phase != "" {
		patch.Phase = &phase
	}
	return c.UpdateTask(ctx, id, patch)
}

func taskPath(id string) string {
	return "/api/v1/tasks/" + url.PathEscape(strings.TrimSpace(id))
}

type gateListResponse struct {
	Items []Gate `json:"items"`
}

func (c *HTTPClient) ListGates(ctx context.Context, taskID string) ([]Gate, error) {
	var response gateListResponse
	if err := c.do(ctx, http.MethodGet, taskPath(taskID)+"/gates", nil, &response); err != nil {
		return nil, err
	}
	if response.Items == nil {
		response.Items = []Gate{}
	}
	return response.Items, nil
}

func (c *HTTPClient) CreateGate(ctx context.Context, gate Gate) (Gate, error) {
	body := map[string]any{
		"id": gate.ID, "name": gate.Name, "kind": gate.Kind, "rule": gate.Rule,
		"config": gate.Config, "required": gate.Required, "sort_order": gate.SortOrder,
	}
	var created Gate
	err := c.do(ctx, http.MethodPost, taskPath(gate.TaskID)+"/gates", body, &created)
	return created, err
}

func (c *HTTPClient) EvaluateGate(
	ctx context.Context, taskID, gateID string, approved *bool, evidence map[string]any,
) (Gate, error) {
	body := map[string]any{"evidence": evidence}
	if approved != nil {
		body["approved"] = *approved
	}
	var evaluated Gate
	err := c.do(ctx, http.MethodPost,
		taskPath(taskID)+"/gates/"+url.PathEscape(strings.TrimSpace(gateID))+"/evaluate", body, &evaluated)
	return evaluated, err
}

func (c *HTTPClient) do(
	ctx context.Context, method, path string, body, out any,
) error {
	if c == nil || c.baseURL == "" {
		return errors.New("task API URL is required")
	}
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode task request: %w", err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet && method != http.MethodHead {
		req.Header.Set("Origin", c.baseURL)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("task API request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		var apiErr struct {
			Message string `json:"error"`
		}
		_ = json.Unmarshal(data, &apiErr)
		if apiErr.Message == "" {
			apiErr.Message = strings.TrimSpace(string(data))
		}
		if apiErr.Message == "" {
			apiErr.Message = resp.Status
		}
		return fmt.Errorf("task API: %s", apiErr.Message)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode task response: %w", err)
	}
	return nil
}
