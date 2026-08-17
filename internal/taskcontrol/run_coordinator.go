package taskcontrol

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"

	"go.kenn.io/agentsview/internal/taskrun"
)

var ErrTaskHasNoAssignee = errors.New("task has no assigned agent")

// RunCoordinator is the persistence bridge between explicit task events and
// the process runtime. It contains no scheduler, polling loop, or heartbeat.
type RunCoordinator struct {
	ctx     context.Context
	cancel  context.CancelFunc
	store   *Store
	runtime *taskrun.Runtime
	changed func()

	mu     sync.Mutex
	active map[string]string
	wg     sync.WaitGroup
}

func NewRunCoordinator(
	ctx context.Context, store *Store, runtime *taskrun.Runtime, changed func(),
) *RunCoordinator {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	return &RunCoordinator{
		ctx: ctx, cancel: cancel, store: store, runtime: runtime, changed: changed,
		active: make(map[string]string),
	}
}

func TriggerTypeForEvent(eventType string) (taskrun.TriggerType, bool) {
	switch strings.ToLower(strings.TrimSpace(eventType)) {
	case "task.assigned":
		return taskrun.TriggerAssignment, true
	case "task.dependency-cleared":
		return taskrun.TriggerDependencyCleared, true
	case "task.mentioned", "task.mention":
		return taskrun.TriggerMention, true
	case "task.retry":
		return taskrun.TriggerRetry, true
	default:
		return "", false
	}
}

func (c *RunCoordinator) Trigger(taskID string, triggerType taskrun.TriggerType) error {
	if c == nil || c.runtime == nil {
		return nil
	}
	if err := c.ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	if runID, exists := c.active[taskID]; exists {
		c.mu.Unlock()
		return fmt.Errorf("%w: task %s run %s", taskrun.ErrActiveRun, taskID, runID)
	}
	c.active[taskID] = "starting"
	c.mu.Unlock()
	release := true
	defer func() {
		if release {
			c.release(taskID)
		}
	}()

	task, err := c.store.Task(c.ctx, taskID)
	if err != nil {
		return err
	}
	if task.AssigneeID == "" {
		return ErrTaskHasNoAssignee
	}
	agent, err := c.store.Agent(c.ctx, task.AssigneeID)
	if err != nil {
		return fmt.Errorf("load assigned agent: %w", err)
	}
	gates, err := c.store.ListGates(c.ctx, taskID)
	if err != nil {
		return fmt.Errorf("list task criteria: %w", err)
	}
	criteria := make([]taskrun.Criterion, 0, len(gates))
	for _, gate := range gates {
		summary := gate.Name
		if gate.Rule != "" {
			summary += ": " + gate.Rule
		}
		criteria = append(criteria, taskrun.Criterion{ID: gate.ID, Summary: summary})
	}

	run, err := c.runtime.Dispatch(c.ctx, taskrun.Trigger{
		Type:      triggerType,
		AdapterID: agent.Harness,
		Envelope: taskrun.TaskEnvelope{
			TaskID:     task.ID,
			Summary:    compactTaskSummary(task),
			Criteria:   criteria,
			DetailsRef: "agentsview://tasks/" + url.PathEscape(task.ID),
		},
	})
	if err != nil {
		c.appendFailure(task.ID, triggerType, err)
		return err
	}
	c.mu.Lock()
	c.active[taskID] = run.ID
	c.mu.Unlock()

	workflow, err := c.store.Workflow(c.ctx, task.Project)
	if err != nil {
		_ = c.runtime.CancelTask(context.Background(), taskID)
		return err
	}
	runningStatus := workflowStatus(workflow, "In Progress", task.Status, 1)
	runID := run.ID
	phase := UniversalPhases[0]
	if _, err := c.store.PatchTask(c.ctx, task.ID, TaskPatch{
		Status: &runningStatus, Phase: &phase, ActiveRunID: &runID,
	}); err != nil {
		_ = c.runtime.CancelTask(context.Background(), taskID)
		return fmt.Errorf("persist active run: %w", err)
	}
	if _, err := c.store.AppendEvent(c.ctx, TaskEvent{
		TaskID: task.ID, Type: "agent.run.dispatched", Source: agent.Harness,
		Payload: map[string]any{"run_id": run.ID, "trigger": triggerType},
	}); err != nil {
		_ = c.runtime.CancelTask(context.Background(), taskID)
		emptyRunID := ""
		_, _ = c.store.PatchTask(c.ctx, task.ID, TaskPatch{ActiveRunID: &emptyRunID})
		return fmt.Errorf("persist run dispatch: %w", err)
	}
	c.notifyChanged()

	release = false
	c.wg.Add(1)
	go c.observe(task, workflow, run)
	return nil
}

func (c *RunCoordinator) observe(task Task, workflow Workflow, run *taskrun.Run) {
	defer c.wg.Done()
	defer c.release(task.ID)
	for event := range run.Events {
		if event.Type == taskrun.EventOutput {
			continue
		}
		c.persistEvent(task, workflow, run, event)
		if event.Type.Terminal() {
			return
		}
	}
}

func (c *RunCoordinator) persistEvent(
	task Task, workflow Workflow, run *taskrun.Run, event taskrun.Event,
) {
	payload := map[string]any{"run_id": run.ID}
	if event.Phase != "" {
		payload["phase"] = event.Phase
	}
	if event.Message != "" {
		payload["message"] = compactText(event.Message, 2000)
	}
	if len(event.Data) > 0 {
		payload["data"] = event.Data
	}
	_, _ = c.store.AppendEvent(c.ctx, TaskEvent{
		TaskID: task.ID, Type: "agent.run." + string(event.Type),
		Source: run.AdapterID, Payload: payload,
	})

	emptyRunID := ""
	switch event.Type {
	case taskrun.EventPhaseChanged:
		if slices.Contains(UniversalPhases, event.Phase) {
			_, _ = c.store.PatchTask(c.ctx, task.ID, TaskPatch{Phase: &event.Phase})
		}
	case taskrun.EventBlocked:
		blocked := workflowStatus(workflow, "Blocked", task.Status, -1)
		_, _ = c.store.PatchTask(c.ctx, task.ID, TaskPatch{Status: &blocked})
	case taskrun.EventCompleted:
		_, _ = c.store.PatchTask(c.ctx, task.ID, TaskPatch{ActiveRunID: &emptyRunID})
		done := workflow.Statuses[len(workflow.Statuses)-1]
		deliver := UniversalPhases[len(UniversalPhases)-1]
		if _, err := c.store.PatchTask(c.ctx, task.ID, TaskPatch{Status: &done, Phase: &deliver}); err != nil {
			_, _ = c.store.AppendEvent(c.ctx, TaskEvent{
				TaskID: task.ID, Type: "agent.completion.blocked", Source: run.AdapterID,
				Payload: map[string]any{"run_id": run.ID, "error": compactText(err.Error(), 2000)},
			})
		}
	case taskrun.EventFailed:
		blocked := workflowStatus(workflow, "Blocked", task.Status, -1)
		_, _ = c.store.PatchTask(c.ctx, task.ID, TaskPatch{Status: &blocked, ActiveRunID: &emptyRunID})
	case taskrun.EventCancelled:
		_, _ = c.store.PatchTask(c.ctx, task.ID, TaskPatch{ActiveRunID: &emptyRunID})
	}
	c.notifyChanged()
}

func (c *RunCoordinator) appendFailure(taskID string, trigger taskrun.TriggerType, err error) {
	_, _ = c.store.AppendEvent(c.ctx, TaskEvent{
		TaskID: taskID, Type: "agent.run.failed", Source: "runtime",
		Payload: map[string]any{"trigger": trigger, "error": compactText(err.Error(), 2000)},
	})
	c.notifyChanged()
}

func (c *RunCoordinator) Close(ctx context.Context) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	taskIDs := make([]string, 0, len(c.active))
	for taskID := range c.active {
		taskIDs = append(taskIDs, taskID)
	}
	c.mu.Unlock()
	for _, taskID := range taskIDs {
		_ = c.runtime.CancelTask(context.Background(), taskID)
	}
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		c.cancel()
		return nil
	case <-ctx.Done():
		c.cancel()
		return ctx.Err()
	}
}

func (c *RunCoordinator) ActiveRun(taskID string) (string, bool) {
	if c == nil {
		return "", false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	runID, ok := c.active[taskID]
	return runID, ok
}

func (c *RunCoordinator) release(taskID string) {
	c.mu.Lock()
	delete(c.active, taskID)
	c.mu.Unlock()
}

func (c *RunCoordinator) notifyChanged() {
	if c.changed != nil {
		c.changed()
	}
}

func workflowStatus(workflow Workflow, preferred, fallback string, fallbackIndex int) string {
	if slices.Contains(workflow.Statuses, preferred) {
		return preferred
	}
	if fallbackIndex >= 0 && fallbackIndex < len(workflow.Statuses) {
		return workflow.Statuses[fallbackIndex]
	}
	if slices.Contains(workflow.Statuses, fallback) {
		return fallback
	}
	return workflow.Statuses[0]
}

func compactTaskSummary(task Task) string {
	summary := strings.TrimSpace(task.Title)
	if description := strings.TrimSpace(task.Description); description != "" {
		summary += "\n\n" + description
	}
	return compactText(summary, 2000)
}

func compactText(value string, maxRunes int) string {
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes]) + "…"
}
