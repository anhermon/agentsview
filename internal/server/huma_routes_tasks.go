package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.kenn.io/agentsview/internal/taskcontrol"
	"go.kenn.io/agentsview/internal/taskrun"
)

type itemsBody[T any] struct {
	Items []T `json:"items"`
}

type taskView struct {
	ID             string                    `json:"id"`
	Title          string                    `json:"title"`
	Description    string                    `json:"description,omitempty"`
	Project        string                    `json:"project"`
	Type           string                    `json:"type,omitempty"`
	Status         string                    `json:"status"`
	Phase          string                    `json:"phase"`
	Priority       int                       `json:"priority"`
	AssigneeID     string                    `json:"assignee_id,omitempty"`
	AssigneeName   string                    `json:"assignee_name,omitempty"`
	Harness        string                    `json:"harness,omitempty"`
	ActiveRunID    string                    `json:"active_run_id,omitempty"`
	LastActivityAt time.Time                 `json:"last_activity_at"`
	Evidence       []map[string]any          `json:"evidence"`
	Gates          []taskcontrol.Gate        `json:"gates"`
	SessionLinks   []taskcontrol.SessionLink `json:"session_links"`
	CreatedAt      time.Time                 `json:"created_at"`
	UpdatedAt      time.Time                 `json:"updated_at"`
}

type taskDetailView struct {
	taskView
	Events          []taskcontrol.TaskEvent `json:"events"`
	GateSummary     taskcontrol.GateSummary `json:"gate_summary"`
	Timing          taskcontrol.TaskTiming  `json:"timing"`
	EventsTruncated bool                    `json:"events_truncated"`
	AgentSession    *taskAgentSessionView   `json:"agent_session,omitempty"`
}

type taskAgentSessionView struct {
	AgentID        string                     `json:"agent_id,omitempty"`
	AgentName      string                     `json:"agent_name,omitempty"`
	Harness        string                     `json:"harness,omitempty"`
	RunID          string                     `json:"run_id,omitempty"`
	Active         bool                       `json:"active"`
	RunState       string                     `json:"run_state"`
	Phase          string                     `json:"phase"`
	LastActivityAt time.Time                  `json:"last_activity_at"`
	RecentActivity []taskcontrol.TaskEvent    `json:"recent_activity"`
	Links          []taskSessionReferenceView `json:"links"`
}

type taskSessionReferenceView struct {
	SessionID            string `json:"session_id"`
	Harness              string `json:"harness,omitempty"`
	Active               bool   `json:"active"`
	DetailAPIURL         string `json:"detail_api_url"`
	ActivityAPIURL       string `json:"activity_api_url"`
	RecentMessagesAPIURL string `json:"recent_messages_api_url"`
	FullSessionURL       string `json:"full_session_url"`
}

type taskAgentView struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Harness       string `json:"harness"`
	Mode          string `json:"mode"`
	Status        string `json:"status"`
	SessionID     string `json:"session_id,omitempty"`
	CurrentTaskID string `json:"current_task_id,omitempty"`
}

type workflowColumn struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Position int    `json:"position"`
	WIPLimit *int   `json:"wip_limit,omitempty"`
}

type workflowView struct {
	Project                     string           `json:"project"`
	Columns                     []workflowColumn `json:"columns"`
	Phases                      []string         `json:"phases"`
	AutomaticTransitionsEnabled bool             `json:"automatic_transitions_enabled"`
}

type taskListInput struct {
	Project string `query:"project" doc:"Project to filter by"`
}

type taskMetricsInput struct {
	Project  string `query:"project" doc:"Project to filter by"`
	Status   string `query:"status" doc:"Workflow status to filter by"`
	Phase    string `query:"phase" doc:"Universal phase to filter by"`
	Type     string `query:"type" doc:"Task type to filter by"`
	Assignee string `query:"assignee" doc:"Assignee ID to filter by"`
	From     string `query:"from" doc:"Inclusive task creation time in RFC3339 format"`
	To       string `query:"to" doc:"Exclusive task creation time in RFC3339 format"`
}

type taskCreateInput struct {
	Body struct {
		ID          string `json:"id,omitempty"`
		Project     string `json:"project"`
		Title       string `json:"title"`
		Description string `json:"description,omitempty"`
		Type        string `json:"type,omitempty"`
		Status      string `json:"status,omitempty"`
		Phase       string `json:"phase,omitempty"`
		Priority    int    `json:"priority,omitempty"`
		AssigneeID  string `json:"assignee_id,omitempty"`
		ActiveRunID string `json:"active_run_id,omitempty"`
	}
}

type taskPathInput struct {
	ID string `path:"id" required:"true" doc:"Task ID"`
}

type taskPatchInput struct {
	ID   string `path:"id" required:"true" doc:"Task ID"`
	Body struct {
		Title       *string `json:"title,omitempty"`
		Description *string `json:"description,omitempty"`
		Type        *string `json:"type,omitempty"`
		Status      *string `json:"status,omitempty"`
		Phase       *string `json:"phase,omitempty"`
		Priority    *int    `json:"priority,omitempty"`
		AssigneeID  *string `json:"assignee_id,omitempty"`
		ActiveRunID *string `json:"active_run_id,omitempty"`
	}
}

type taskAgentCreateInput struct {
	Body struct {
		ID      string `json:"id,omitempty"`
		Name    string `json:"name"`
		Harness string `json:"harness"`
		Mode    string `json:"mode,omitempty"`
	}
}

type taskWorkflowInput struct {
	Project string `path:"project" required:"true" doc:"Project identifier"`
}

type taskWorkflowPutInput struct {
	Project string `path:"project" required:"true" doc:"Project identifier"`
	Body    struct {
		Columns                     []workflowColumn `json:"columns"`
		AutomaticTransitionsEnabled bool             `json:"automatic_transitions_enabled"`
	}
}

type taskGateCreateInput struct {
	ID   string `path:"id" required:"true" doc:"Task ID"`
	Body struct {
		ID        string               `json:"id,omitempty"`
		Name      string               `json:"name"`
		Kind      taskcontrol.GateKind `json:"kind"`
		Rule      string               `json:"rule,omitempty"`
		Config    map[string]any       `json:"config,omitempty"`
		Required  *bool                `json:"required,omitempty"`
		SortOrder int                  `json:"sort_order,omitempty"`
	}
}

type taskGateEvaluateInput struct {
	ID     string `path:"id" required:"true" doc:"Task ID"`
	GateID string `path:"gateId" required:"true" doc:"Gate ID"`
	Body   struct {
		Approved *bool          `json:"approved,omitempty"`
		Evidence map[string]any `json:"evidence,omitempty"`
	}
}

type taskEventCreateInput struct {
	ID   string `path:"id" required:"true" doc:"Task ID"`
	Body struct {
		Type    string         `json:"type"`
		Source  string         `json:"source"`
		Payload map[string]any `json:"payload,omitempty"`
	}
}

type taskSessionLinkCreateInput struct {
	ID   string `path:"id" required:"true" doc:"Task ID"`
	Body struct {
		ID         string   `json:"id,omitempty"`
		SessionID  string   `json:"session_id"`
		Harness    string   `json:"harness,omitempty"`
		Method     string   `json:"method"`
		Confidence *float64 `json:"confidence,omitempty"`
		Active     *bool    `json:"active,omitempty"`
	}
}

func (s *Server) registerTaskControlRoutes() {
	group := newRouteGroup(s.api, "/api/v1", "Tasks")
	get(s, group, "/tasks", "List tasks", s.humaListTasks)
	post(s, group, "/tasks", "Create task", s.humaCreateTask)
	get(s, group, "/task-metrics", "Get aggregate task metrics", s.humaTaskMetrics)
	get(s, group, "/tasks/{id}", "Get task", s.humaGetTask)
	patch(s, group, "/tasks/{id}", "Update task", s.humaPatchTask)
	get(s, group, "/task-agents", "List task agents", s.humaListTaskAgents)
	post(s, group, "/task-agents", "Create task agent", s.humaCreateTaskAgent)
	get(s, group, "/task-workflows/{project}", "Get project task workflow", s.humaGetTaskWorkflow)
	put(s, group, "/task-workflows/{project}", "Update project task workflow", s.humaPutTaskWorkflow)
	get(s, group, "/tasks/{id}/gates", "List task completion gates", s.humaListTaskGates)
	post(s, group, "/tasks/{id}/gates", "Create task completion gate", s.humaCreateTaskGate)
	post(s, group, "/tasks/{id}/gates/{gateId}/evaluate", "Evaluate task completion gate", s.humaEvaluateTaskGate)
	get(s, group, "/tasks/{id}/events", "List task events", s.humaListTaskEvents)
	post(s, group, "/tasks/{id}/events", "Append task event", s.humaAppendTaskEvent)
	get(s, group, "/tasks/{id}/session-links", "List task session links", s.humaListTaskSessionLinks)
	post(s, group, "/tasks/{id}/session-links", "Create task session link", s.humaCreateTaskSessionLink)
}

func (s *Server) taskControlStore() (*taskcontrol.Store, error) {
	s.taskStoreOnce.Do(func() {
		if s.taskStore != nil {
			return
		}
		dataDir := strings.TrimSpace(s.dataDir)
		if dataDir == "" {
			dataDir = strings.TrimSpace(s.cfg.DataDir)
		}
		if dataDir == "" {
			s.taskStoreErr = errors.New("task control requires a data directory")
			return
		}
		s.taskStore, s.taskStoreErr = taskcontrol.Open(filepath.Join(dataDir, "tasks.db"))
		s.ownsTaskStore = s.taskStoreErr == nil
	})
	return s.taskStore, s.taskStoreErr
}

func (s *Server) taskRunCoordinator(store *taskcontrol.Store) *taskcontrol.RunCoordinator {
	if s.taskRuntime == nil {
		return nil
	}
	s.taskRunnerOnce.Do(func() {
		runner := taskcontrol.NewRunCoordinator(s.baseCtx, store, s.taskRuntime, s.emitTasks)
		s.mu.Lock()
		s.taskRunner = runner
		s.mu.Unlock()
	})
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.taskRunner
}

func (s *Server) triggerTaskRun(
	store *taskcontrol.Store, taskID string, triggerType taskrun.TriggerType,
) {
	runner := s.taskRunCoordinator(store)
	if runner == nil {
		return
	}
	if err := runner.Trigger(taskID, triggerType); err != nil {
		log.Printf("task %s trigger %s did not start a run: %v", taskID, triggerType, err)
	}
}

func taskAPIError(err error) error {
	switch {
	case errors.Is(err, taskcontrol.ErrNotFound):
		return apiError(http.StatusNotFound, err.Error())
	case errors.Is(err, taskcontrol.ErrConflict), errors.Is(err, taskcontrol.ErrCompletionBlocked):
		return apiError(http.StatusConflict, err.Error())
	default:
		return apiError(http.StatusBadRequest, err.Error())
	}
}

func (s *Server) emitTasks() {
	if s.broadcaster != nil {
		s.broadcaster.Emit("tasks")
	}
}

func (s *Server) taskViews(ctx context.Context, tasks []taskcontrol.Task) ([]taskView, error) {
	store, err := s.taskControlStore()
	if err != nil {
		return nil, err
	}
	agents, err := store.ListAgents(ctx)
	if err != nil {
		return nil, err
	}
	agentByID := make(map[string]taskcontrol.Agent, len(agents))
	for _, agent := range agents {
		agentByID[agent.ID] = agent
	}
	views := make([]taskView, 0, len(tasks))
	for _, task := range tasks {
		gates, err := store.ListGates(ctx, task.ID)
		if err != nil {
			return nil, err
		}
		links, err := store.ListSessionLinks(ctx, task.ID)
		if err != nil {
			return nil, err
		}
		evidence := []map[string]any{}
		for _, gate := range gates {
			if len(gate.Evidence) > 0 {
				evidence = append(evidence, gate.Evidence)
			}
		}
		view := taskView{ID: task.ID, Title: task.Title, Description: task.Description,
			Project: task.Project, Type: task.TaskType, Status: task.Status, Phase: task.Phase,
			Priority: task.Priority, AssigneeID: task.AssigneeID, ActiveRunID: task.ActiveRunID,
			LastActivityAt: task.UpdatedAt, Evidence: evidence, Gates: gates,
			SessionLinks: links, CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt}
		if agent, ok := agentByID[task.AssigneeID]; ok {
			view.AssigneeName, view.Harness = agent.Name, agent.Harness
		}
		views = append(views, view)
	}
	return views, nil
}

func (s *Server) humaListTasks(ctx context.Context, in *taskListInput) (*jsonOutput[itemsBody[taskView]], error) {
	store, err := s.taskControlStore()
	if err != nil {
		return nil, internalError("open task store", err)
	}
	tasks, err := store.ListTasks(ctx, in.Project)
	if err != nil {
		return nil, internalError("list tasks", err)
	}
	views, err := s.taskViews(ctx, tasks)
	if err != nil {
		return nil, internalError("expand tasks", err)
	}
	return &jsonOutput[itemsBody[taskView]]{Body: itemsBody[taskView]{Items: views}}, nil
}

func (s *Server) humaCreateTask(ctx context.Context, in *taskCreateInput) (*createdOutput[taskView], error) {
	store, err := s.taskControlStore()
	if err != nil {
		return nil, internalError("open task store", err)
	}
	task, err := store.CreateTask(ctx, taskcontrol.Task{ID: in.Body.ID, Project: in.Body.Project,
		Title: in.Body.Title, Description: in.Body.Description, TaskType: in.Body.Type,
		Status: in.Body.Status, Phase: in.Body.Phase, Priority: in.Body.Priority,
		AssigneeID: in.Body.AssigneeID, ActiveRunID: in.Body.ActiveRunID})
	if err != nil {
		return nil, taskAPIError(err)
	}
	if task.AssigneeID != "" {
		s.triggerTaskRun(store, task.ID, taskrun.TriggerAssignment)
		task, _ = store.Task(ctx, task.ID)
	}
	views, err := s.taskViews(ctx, []taskcontrol.Task{task})
	if err != nil {
		return nil, internalError("expand task", err)
	}
	s.emitTasks()
	return &createdOutput[taskView]{Status: http.StatusCreated, Body: views[0]}, nil
}

func (s *Server) humaGetTask(ctx context.Context, in *taskPathInput) (*jsonOutput[taskDetailView], error) {
	store, err := s.taskControlStore()
	if err != nil {
		return nil, internalError("open task store", err)
	}
	detail, err := store.TaskDetail(ctx, in.ID)
	if err != nil {
		return nil, taskAPIError(err)
	}
	views, err := s.taskViews(ctx, []taskcontrol.Task{detail.Task})
	if err != nil {
		return nil, internalError("expand task", err)
	}
	view := views[0]
	view.Gates = detail.Gates
	view.SessionLinks = detail.SessionLinks
	if len(detail.Events) > 0 {
		lastEvent := detail.Events[len(detail.Events)-1]
		if lastEvent.CreatedAt.After(view.LastActivityAt) {
			view.LastActivityAt = lastEvent.CreatedAt
		}
	}
	return &jsonOutput[taskDetailView]{Body: taskDetailView{
		taskView:        view,
		Events:          detail.Events,
		GateSummary:     detail.GateSummary,
		Timing:          detail.Timing,
		EventsTruncated: detail.EventsTruncated,
		AgentSession:    buildTaskAgentSessionView(view, detail),
	}}, nil
}

func buildTaskAgentSessionView(view taskView, detail taskcontrol.TaskDetail) *taskAgentSessionView {
	if view.AssigneeID == "" && view.ActiveRunID == "" && len(detail.SessionLinks) == 0 {
		return nil
	}
	result := &taskAgentSessionView{
		AgentID: view.AssigneeID, AgentName: view.AssigneeName, Harness: view.Harness,
		RunID: view.ActiveRunID, Active: view.ActiveRunID != "", RunState: "idle",
		Phase: view.Phase, LastActivityAt: view.LastActivityAt,
		RecentActivity: []taskcontrol.TaskEvent{}, Links: []taskSessionReferenceView{},
	}
	if result.Active {
		result.RunState = "dispatched"
	}
	for index := len(detail.Events) - 1; index >= 0; index-- {
		event := detail.Events[index]
		if !isAgentActivity(event.Type) {
			continue
		}
		eventRunID, _ := event.Payload["run_id"].(string)
		if result.RunID == "" && eventRunID != "" {
			result.RunID = eventRunID
		}
		if eventRunID != "" && result.RunID != "" && eventRunID != result.RunID {
			continue
		}
		if len(result.RecentActivity) < 20 {
			result.RecentActivity = append(result.RecentActivity, event)
		}
		if event.CreatedAt.After(result.LastActivityAt) {
			result.LastActivityAt = event.CreatedAt
		}
		if phase, ok := event.Payload["phase"].(string); ok && phase != "" && len(result.RecentActivity) == 1 {
			result.Phase = phase
		}
		if state := taskRunState(event.Type); state != "" && result.RunState == "idle" {
			result.RunState = state
		} else if state != "" && len(result.RecentActivity) == 1 {
			result.RunState = state
		}
	}
	for left, right := 0, len(result.RecentActivity)-1; left < right; left, right = left+1, right-1 {
		result.RecentActivity[left], result.RecentActivity[right] =
			result.RecentActivity[right], result.RecentActivity[left]
	}
	result.Active = result.Active && result.RunState != "completed" &&
		result.RunState != "failed" && result.RunState != "cancelled"
	for _, link := range detail.SessionLinks {
		escaped := url.PathEscape(link.SessionID)
		base := "/api/v1/sessions/" + escaped
		result.Links = append(result.Links, taskSessionReferenceView{
			SessionID: link.SessionID, Harness: link.Harness, Active: link.Active,
			DetailAPIURL: base, ActivityAPIURL: base + "/activity",
			RecentMessagesAPIURL: base + "/messages?limit=20&direction=desc",
			FullSessionURL:       "/sessions/" + escaped,
		})
	}
	return result
}

func isAgentActivity(eventType string) bool {
	return strings.HasPrefix(eventType, "agent.run.") || eventType == "agent.progress" ||
		eventType == "agent.completion.blocked"
}

func taskRunState(eventType string) string {
	switch eventType {
	case "agent.run.dispatched":
		return "dispatched"
	case "agent.run.started", "agent.run.progress", "agent.run.phase-changed", "agent.run.activity", "agent.progress":
		return "running"
	case "agent.run.blocked", "agent.completion.blocked":
		return "blocked"
	case "agent.run.completed":
		return "completed"
	case "agent.run.failed":
		return "failed"
	case "agent.run.cancelled":
		return "cancelled"
	default:
		return ""
	}
}

func (s *Server) humaTaskMetrics(
	ctx context.Context, in *taskMetricsInput,
) (*jsonOutput[taskcontrol.TaskMetrics], error) {
	from, err := parseTaskMetricsTime("from", in.From)
	if err != nil {
		return nil, apiError(http.StatusBadRequest, err.Error())
	}
	to, err := parseTaskMetricsTime("to", in.To)
	if err != nil {
		return nil, apiError(http.StatusBadRequest, err.Error())
	}
	if from != nil && to != nil && from.After(*to) {
		return nil, apiError(http.StatusBadRequest, "from must not be after to")
	}
	store, err := s.taskControlStore()
	if err != nil {
		return nil, internalError("open task store", err)
	}
	metrics, err := store.TaskMetrics(ctx, taskcontrol.TaskFilter{
		Project: strings.TrimSpace(in.Project), AssigneeID: strings.TrimSpace(in.Assignee),
		Status: strings.TrimSpace(in.Status), Phase: strings.TrimSpace(in.Phase),
		TaskType: strings.TrimSpace(in.Type), From: from, To: to,
	})
	if err != nil {
		return nil, taskAPIError(err)
	}
	return &jsonOutput[taskcontrol.TaskMetrics]{Body: metrics}, nil
}

func parseTaskMetricsTime(name, value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, fmt.Errorf("%s must be an RFC3339 timestamp", name)
	}
	parsed = parsed.UTC()
	return &parsed, nil
}

func (s *Server) humaPatchTask(ctx context.Context, in *taskPatchInput) (*jsonOutput[taskView], error) {
	store, err := s.taskControlStore()
	if err != nil {
		return nil, internalError("open task store", err)
	}
	assignmentChanged := false
	if in.Body.AssigneeID != nil && strings.TrimSpace(*in.Body.AssigneeID) != "" {
		before, taskErr := store.Task(ctx, in.ID)
		if taskErr != nil {
			return nil, taskAPIError(taskErr)
		}
		assignmentChanged = before.AssigneeID != strings.TrimSpace(*in.Body.AssigneeID)
	}
	task, err := store.PatchTask(ctx, in.ID, taskcontrol.TaskPatch{Title: in.Body.Title,
		Description: in.Body.Description, TaskType: in.Body.Type, Status: in.Body.Status,
		Phase: in.Body.Phase, Priority: in.Body.Priority, AssigneeID: in.Body.AssigneeID,
		ActiveRunID: in.Body.ActiveRunID})
	if err != nil {
		return nil, taskAPIError(err)
	}
	if assignmentChanged {
		s.triggerTaskRun(store, task.ID, taskrun.TriggerAssignment)
		task, _ = store.Task(ctx, task.ID)
	}
	views, err := s.taskViews(ctx, []taskcontrol.Task{task})
	if err != nil {
		return nil, internalError("expand task", err)
	}
	s.emitTasks()
	return &jsonOutput[taskView]{Body: views[0]}, nil
}

func (s *Server) humaListTaskAgents(ctx context.Context, _ *emptyInput) (*jsonOutput[itemsBody[taskAgentView]], error) {
	store, err := s.taskControlStore()
	if err != nil {
		return nil, internalError("open task store", err)
	}
	agents, err := store.ListAgents(ctx)
	if err != nil {
		return nil, internalError("list task agents", err)
	}
	tasks, err := store.ListTasks(ctx, "")
	if err != nil {
		return nil, internalError("list assigned tasks", err)
	}
	assigned := map[string]taskcontrol.Task{}
	for _, task := range tasks {
		if task.AssigneeID != "" {
			assigned[task.AssigneeID] = task
		}
	}
	items := make([]taskAgentView, 0, len(agents))
	for _, agent := range agents {
		item := taskAgentView{ID: agent.ID, Name: agent.Name, Harness: agent.Harness, Mode: agent.Mode, Status: "idle"}
		if task, ok := assigned[agent.ID]; ok {
			item.CurrentTaskID, item.SessionID = task.ID, task.ActiveRunID
			item.Status = "assigned"
			if task.ActiveRunID != "" {
				item.Status = "working"
			}
		}
		items = append(items, item)
	}
	return &jsonOutput[itemsBody[taskAgentView]]{Body: itemsBody[taskAgentView]{Items: items}}, nil
}

func (s *Server) humaCreateTaskAgent(ctx context.Context, in *taskAgentCreateInput) (*createdOutput[taskAgentView], error) {
	store, err := s.taskControlStore()
	if err != nil {
		return nil, internalError("open task store", err)
	}
	agent, err := store.CreateAgent(ctx, taskcontrol.Agent{ID: in.Body.ID, Name: in.Body.Name, Harness: in.Body.Harness, Mode: in.Body.Mode})
	if err != nil {
		return nil, taskAPIError(err)
	}
	s.emitTasks()
	return &createdOutput[taskAgentView]{Status: http.StatusCreated, Body: taskAgentView{ID: agent.ID, Name: agent.Name, Harness: agent.Harness, Mode: agent.Mode, Status: "idle"}}, nil
}

func workflowResponse(workflow taskcontrol.Workflow) workflowView {
	columns := make([]workflowColumn, len(workflow.Statuses))
	for i, status := range workflow.Statuses {
		columns[i] = workflowColumn{ID: status, Label: status, Position: i}
	}
	return workflowView{Project: workflow.Project, Columns: columns, Phases: workflow.Phases,
		AutomaticTransitionsEnabled: workflow.InferredTransitionsEnabled}
}

func (s *Server) humaGetTaskWorkflow(ctx context.Context, in *taskWorkflowInput) (*jsonOutput[workflowView], error) {
	store, err := s.taskControlStore()
	if err != nil {
		return nil, internalError("open task store", err)
	}
	workflow, err := store.Workflow(ctx, in.Project)
	if err != nil {
		return nil, taskAPIError(err)
	}
	return &jsonOutput[workflowView]{Body: workflowResponse(workflow)}, nil
}

func (s *Server) humaPutTaskWorkflow(ctx context.Context, in *taskWorkflowPutInput) (*jsonOutput[workflowView], error) {
	store, err := s.taskControlStore()
	if err != nil {
		return nil, internalError("open task store", err)
	}
	columns := append([]workflowColumn(nil), in.Body.Columns...)
	sort.SliceStable(columns, func(i, j int) bool { return columns[i].Position < columns[j].Position })
	statuses := make([]string, 0, len(columns))
	for _, column := range columns {
		id := strings.TrimSpace(column.ID)
		if id == "" {
			id = strings.TrimSpace(column.Label)
		}
		statuses = append(statuses, id)
	}
	workflow, err := store.PutWorkflow(ctx, in.Project, statuses, in.Body.AutomaticTransitionsEnabled)
	if err != nil {
		return nil, taskAPIError(err)
	}
	s.emitTasks()
	return &jsonOutput[workflowView]{Body: workflowResponse(workflow)}, nil
}

func (s *Server) humaListTaskGates(ctx context.Context, in *taskPathInput) (*jsonOutput[itemsBody[taskcontrol.Gate]], error) {
	store, err := s.taskControlStore()
	if err != nil {
		return nil, internalError("open task store", err)
	}
	if _, err := store.Task(ctx, in.ID); err != nil {
		return nil, taskAPIError(err)
	}
	items, err := store.ListGates(ctx, in.ID)
	if err != nil {
		return nil, internalError("list task gates", err)
	}
	return &jsonOutput[itemsBody[taskcontrol.Gate]]{Body: itemsBody[taskcontrol.Gate]{Items: items}}, nil
}

func (s *Server) humaCreateTaskGate(ctx context.Context, in *taskGateCreateInput) (*createdOutput[taskcontrol.Gate], error) {
	store, err := s.taskControlStore()
	if err != nil {
		return nil, internalError("open task store", err)
	}
	required := true
	if in.Body.Required != nil {
		required = *in.Body.Required
	}
	gate, err := store.CreateGate(ctx, taskcontrol.Gate{ID: in.Body.ID, TaskID: in.ID,
		Name: in.Body.Name, Kind: in.Body.Kind, Rule: in.Body.Rule, Config: in.Body.Config,
		Required: required, SortOrder: in.Body.SortOrder})
	if err != nil {
		return nil, taskAPIError(err)
	}
	_, _ = store.AppendEvent(ctx, taskcontrol.TaskEvent{TaskID: in.ID,
		Type: "gate.created", Source: "api", Payload: map[string]any{
			"gate_id": gate.ID, "kind": gate.Kind,
		}})
	s.emitTasks()
	return &createdOutput[taskcontrol.Gate]{Status: http.StatusCreated, Body: gate}, nil
}

func defaultTaskGateEvaluator(_ context.Context, gate taskcontrol.Gate, input taskcontrol.GateEvaluationContext) (taskcontrol.GateEvaluation, error) {
	passed, ok := input.Evidence["passed"].(bool)
	if input.Approved != nil {
		passed, ok = *input.Approved, true
	}
	if !ok {
		return taskcontrol.GateEvaluation{}, fmt.Errorf("%s gate evaluation requires approved or evidence.passed", gate.Kind)
	}
	status := taskcontrol.GateStatusFailed
	if passed {
		status = taskcontrol.GateStatusPassed
	}
	return taskcontrol.GateEvaluation{Status: status, Evidence: input.Evidence}, nil
}

func (s *Server) humaEvaluateTaskGate(ctx context.Context, in *taskGateEvaluateInput) (*jsonOutput[taskcontrol.Gate], error) {
	store, err := s.taskControlStore()
	if err != nil {
		return nil, internalError("open task store", err)
	}
	evaluator := s.taskGateEvaluator
	if evaluator == nil {
		evaluator = taskcontrol.GateEvaluatorFunc(defaultTaskGateEvaluator)
	}
	gate, err := store.EvaluateGate(ctx, in.ID, in.GateID, evaluator, taskcontrol.GateEvaluationContext{Evidence: in.Body.Evidence, Approved: in.Body.Approved})
	if err != nil {
		return nil, taskAPIError(err)
	}
	_, _ = store.AppendEvent(ctx, taskcontrol.TaskEvent{TaskID: in.ID, Type: "gate.evaluated", Source: "api", Payload: map[string]any{"gate_id": gate.ID, "status": gate.Status}})
	s.emitTasks()
	return &jsonOutput[taskcontrol.Gate]{Body: gate}, nil
}

func (s *Server) humaListTaskEvents(ctx context.Context, in *taskPathInput) (*jsonOutput[itemsBody[taskcontrol.TaskEvent]], error) {
	store, err := s.taskControlStore()
	if err != nil {
		return nil, internalError("open task store", err)
	}
	if _, err := store.Task(ctx, in.ID); err != nil {
		return nil, taskAPIError(err)
	}
	items, err := store.ListEvents(ctx, in.ID)
	if err != nil {
		return nil, internalError("list task events", err)
	}
	return &jsonOutput[itemsBody[taskcontrol.TaskEvent]]{Body: itemsBody[taskcontrol.TaskEvent]{Items: items}}, nil
}

func (s *Server) humaAppendTaskEvent(ctx context.Context, in *taskEventCreateInput) (*createdOutput[taskcontrol.TaskEvent], error) {
	store, err := s.taskControlStore()
	if err != nil {
		return nil, internalError("open task store", err)
	}
	event, err := store.AppendEvent(ctx, taskcontrol.TaskEvent{TaskID: in.ID, Type: in.Body.Type, Source: in.Body.Source, Payload: in.Body.Payload})
	if err != nil {
		return nil, taskAPIError(err)
	}
	if triggerType, ok := taskcontrol.TriggerTypeForEvent(event.Type); ok {
		s.triggerTaskRun(store, event.TaskID, triggerType)
	}
	s.emitTasks()
	return &createdOutput[taskcontrol.TaskEvent]{Status: http.StatusCreated, Body: event}, nil
}

func (s *Server) humaListTaskSessionLinks(ctx context.Context, in *taskPathInput) (*jsonOutput[itemsBody[taskcontrol.SessionLink]], error) {
	store, err := s.taskControlStore()
	if err != nil {
		return nil, internalError("open task store", err)
	}
	if _, err := store.Task(ctx, in.ID); err != nil {
		return nil, taskAPIError(err)
	}
	items, err := store.ListSessionLinks(ctx, in.ID)
	if err != nil {
		return nil, internalError("list task session links", err)
	}
	return &jsonOutput[itemsBody[taskcontrol.SessionLink]]{Body: itemsBody[taskcontrol.SessionLink]{Items: items}}, nil
}

func (s *Server) humaCreateTaskSessionLink(ctx context.Context, in *taskSessionLinkCreateInput) (*createdOutput[taskcontrol.SessionLink], error) {
	store, err := s.taskControlStore()
	if err != nil {
		return nil, internalError("open task store", err)
	}
	confidence := 1.0
	if in.Body.Confidence != nil {
		confidence = *in.Body.Confidence
	}
	active := true
	if in.Body.Active != nil {
		active = *in.Body.Active
	}
	link, err := store.CreateSessionLink(ctx, taskcontrol.SessionLink{ID: in.Body.ID, TaskID: in.ID,
		SessionID: in.Body.SessionID, Harness: in.Body.Harness, Method: in.Body.Method,
		Confidence: confidence, Active: active})
	if err != nil {
		return nil, taskAPIError(err)
	}
	_, _ = store.AppendEvent(ctx, taskcontrol.TaskEvent{TaskID: in.ID, Type: "session.linked", Source: "api", Payload: map[string]any{"session_id": link.SessionID, "confidence": link.Confidence}})
	s.emitTasks()
	return &createdOutput[taskcontrol.SessionLink]{Status: http.StatusCreated, Body: link}, nil
}
