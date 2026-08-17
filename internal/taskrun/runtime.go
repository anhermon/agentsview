package taskrun

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type Runtime struct {
	worktreeRoot string
	preparer     WorktreePreparer
	adapters     map[string]Adapter

	mu     sync.Mutex
	active map[string]activeRun
}

type activeRun struct {
	ref     RunRef
	adapter Adapter
}

func NewRuntime(worktreeRoot string, adapters ...Adapter) (*Runtime, error) {
	return NewRuntimeWithPreparer(worktreeRoot, nil, adapters...)
}

func NewRuntimeWithPreparer(
	worktreeRoot string, preparer WorktreePreparer, adapters ...Adapter,
) (*Runtime, error) {
	if worktreeRoot == "" {
		return nil, errors.New("worktree root is required")
	}
	runtime := &Runtime{
		worktreeRoot: worktreeRoot,
		preparer:     preparer,
		adapters:     make(map[string]Adapter, len(adapters)),
		active:       make(map[string]activeRun),
	}
	for _, adapter := range adapters {
		if adapter == nil || adapter.ID() == "" {
			return nil, errors.New("adapter ID is required")
		}
		if _, exists := runtime.adapters[adapter.ID()]; exists {
			return nil, fmt.Errorf("duplicate adapter %q", adapter.ID())
		}
		runtime.adapters[adapter.ID()] = adapter
	}
	return runtime, nil
}

// Dispatch handles one explicit event. It never retries, polls, or schedules
// work; another invocation is required for every subsequent trigger.
func (r *Runtime) Dispatch(ctx context.Context, trigger Trigger) (*Run, error) {
	if !trigger.Type.Valid() {
		return nil, fmt.Errorf("invalid trigger %q", trigger.Type)
	}
	if err := trigger.Envelope.Validate(); err != nil {
		return nil, err
	}
	adapter, ok := r.adapters[trigger.AdapterID]
	if !ok {
		return nil, fmt.Errorf("unknown adapter %q", trigger.AdapterID)
	}
	observer, ok := adapter.(Observer)
	if !ok || !adapter.Capabilities().Supports(CapabilityObserve) {
		return nil, fmt.Errorf("%w: %s does not observe runs", ErrCapability, adapter.ID())
	}
	worktree, err := ResolveWorktreePath(r.worktreeRoot, trigger.Envelope.TaskID)
	if err != nil {
		return nil, err
	}

	taskID := trigger.Envelope.TaskID
	r.mu.Lock()
	if active, exists := r.active[taskID]; exists {
		r.mu.Unlock()
		return nil, fmt.Errorf("%w: task %s run %s", ErrActiveRun, taskID, active.ref.ID)
	}
	// An empty reservation closes the race between concurrent launch calls.
	r.active[taskID] = activeRun{adapter: adapter}
	r.mu.Unlock()
	if r.preparer != nil {
		if err := r.preparer.Prepare(ctx, taskID, worktree); err != nil {
			r.release(taskID, "")
			return nil, fmt.Errorf("prepare task worktree: %w", err)
		}
	}

	request := LaunchRequest{Envelope: trigger.Envelope, Trigger: trigger.Type, Worktree: worktree}
	// A dispatch context controls admission, not the lifetime of an accepted
	// agent process. Runs are stopped explicitly through CancelTask.
	runCtx := context.WithoutCancel(ctx)
	var ref RunRef
	if trigger.SessionID == "" {
		launcher, supported := adapter.(Launcher)
		if !supported || !adapter.Capabilities().Supports(CapabilityLaunch) {
			r.release(taskID, "")
			return nil, fmt.Errorf("%w: %s does not launch runs", ErrCapability, adapter.ID())
		}
		ref, err = launcher.Launch(runCtx, request)
	} else {
		resumer, supported := adapter.(Resumer)
		if !supported || !adapter.Capabilities().Supports(CapabilityResume) {
			r.release(taskID, "")
			return nil, fmt.Errorf("%w: %s does not resume runs", ErrCapability, adapter.ID())
		}
		ref, err = resumer.Resume(runCtx, ResumeRequest{LaunchRequest: request, SessionID: trigger.SessionID})
	}
	if err != nil {
		r.release(taskID, "")
		return nil, err
	}
	if ref.ID == "" {
		r.release(taskID, "")
		return nil, errors.New("adapter returned an empty run ID")
	}

	r.mu.Lock()
	r.active[taskID] = activeRun{ref: ref, adapter: adapter}
	r.mu.Unlock()

	source, err := observer.Observe(runCtx, ref.ID)
	if err != nil {
		if canceler, supported := adapter.(Canceler); supported {
			_ = canceler.Cancel(context.WithoutCancel(ctx), ref.ID)
		}
		r.release(taskID, ref.ID)
		return nil, err
	}
	events := make(chan Event, 64)
	go r.forward(taskID, adapter.ID(), ref.ID, source, events)

	return &Run{
		ID:        ref.ID,
		TaskID:    taskID,
		AdapterID: adapter.ID(),
		Worktree:  worktree,
		Events:    events,
	}, nil
}

func (r *Runtime) CancelTask(ctx context.Context, taskID string) error {
	r.mu.Lock()
	active, ok := r.active[taskID]
	r.mu.Unlock()
	if !ok || active.ref.ID == "" {
		return ErrRunNotFound
	}
	canceler, ok := active.adapter.(Canceler)
	if !ok || !active.adapter.Capabilities().Supports(CapabilityCancel) {
		return fmt.Errorf("%w: %s does not cancel runs", ErrCapability, active.adapter.ID())
	}
	return canceler.Cancel(ctx, active.ref.ID)
}

func (r *Runtime) ActiveRun(taskID string) (RunRef, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	active, ok := r.active[taskID]
	return active.ref, ok && active.ref.ID != ""
}

func (r *Runtime) forward(taskID, adapterID, runID string, source <-chan Event, destination chan Event) {
	defer close(destination)
	terminal := false
	for event := range source {
		if event.RunID == "" {
			event.RunID = runID
		}
		if event.TaskID == "" {
			event.TaskID = taskID
		}
		if event.AdapterID == "" {
			event.AdapterID = adapterID
		}
		if event.Time.IsZero() {
			event.Time = time.Now().UTC()
		}
		if event.Type.Terminal() {
			terminal = true
			r.release(taskID, runID)
		}
		deliverEvent(destination, event, terminal)
		if terminal {
			return
		}
	}
	if !terminal {
		r.release(taskID, runID)
		deliverEvent(destination, Event{
			Type:      EventFailed,
			RunID:     runID,
			TaskID:    taskID,
			AdapterID: adapterID,
			Time:      time.Now().UTC(),
			Message:   "adapter event stream closed without a terminal event",
		}, true)
	}
}

// deliverEvent keeps observation bounded when a UI disconnects. Activity may
// be coalesced by dropping the oldest buffered item, but terminal state is
// always retained so callers can reconcile the run later.
func deliverEvent(destination chan Event, event Event, terminal bool) {
	select {
	case destination <- event:
		return
	default:
	}
	if !terminal {
		return
	}
	select {
	case <-destination:
	default:
	}
	destination <- event
}

func (r *Runtime) release(taskID, runID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	active, ok := r.active[taskID]
	if !ok {
		return
	}
	if runID == "" || active.ref.ID == runID {
		delete(r.active, taskID)
	}
}
