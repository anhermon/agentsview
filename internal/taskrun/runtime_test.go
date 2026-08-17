package taskrun

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRuntimeDispatchesSupportedTriggers(t *testing.T) {
	t.Parallel()

	for _, triggerType := range []TriggerType{
		TriggerAssignment,
		TriggerDependencyCleared,
		TriggerMention,
		TriggerRetry,
	} {
		triggerType := triggerType
		t.Run(string(triggerType), func(t *testing.T) {
			t.Parallel()
			adapter := newFakeAdapter("fake")
			runtime, err := NewRuntime(t.TempDir(), adapter)
			require.NoError(t, err)

			run, err := runtime.Dispatch(context.Background(), Trigger{
				Type:      triggerType,
				AdapterID: adapter.ID(),
				Envelope:  testEnvelope("TASK-1"),
			})
			require.NoError(t, err)
			assert.Equal(t, triggerType, adapter.lastLaunch().Trigger)
			assert.Contains(t, run.Worktree, "task-1-")

			adapter.finish(run.ID, EventCompleted)
			events := collectEvents(t, run.Events)
			require.Len(t, events, 1)
			assert.Equal(t, EventCompleted, events[0].Type)
			assert.Equal(t, "TASK-1", events[0].TaskID)
			assert.Equal(t, adapter.ID(), events[0].AdapterID)
		})
	}
}

func TestRuntimeEnforcesSingleActiveRunPerTask(t *testing.T) {
	t.Parallel()

	adapter := newFakeAdapter("fake")
	runtime, err := NewRuntime(t.TempDir(), adapter)
	require.NoError(t, err)
	trigger := Trigger{Type: TriggerAssignment, AdapterID: adapter.ID(), Envelope: testEnvelope("TASK-2")}

	first, err := runtime.Dispatch(context.Background(), trigger)
	require.NoError(t, err)
	_, err = runtime.Dispatch(context.Background(), trigger)
	require.ErrorIs(t, err, ErrActiveRun)
	assert.Equal(t, 1, adapter.launchCount())

	adapter.finish(first.ID, EventCompleted)
	collectEvents(t, first.Events)
	second, err := runtime.Dispatch(context.Background(), trigger)
	require.NoError(t, err)
	adapter.finish(second.ID, EventCompleted)
	collectEvents(t, second.Events)
}

func TestRuntimeConcurrentDispatchStartsOnlyOneRun(t *testing.T) {
	t.Parallel()

	adapter := newFakeAdapter("fake")
	runtime, err := NewRuntime(t.TempDir(), adapter)
	require.NoError(t, err)
	trigger := Trigger{Type: TriggerAssignment, AdapterID: adapter.ID(), Envelope: testEnvelope("TASK-3")}

	const callers = 8
	results := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, dispatchErr := runtime.Dispatch(context.Background(), trigger)
			results <- dispatchErr
		}()
	}
	wg.Wait()
	close(results)

	successes := 0
	duplicates := 0
	for result := range results {
		switch {
		case result == nil:
			successes++
		case errors.Is(result, ErrActiveRun):
			duplicates++
		default:
			require.NoError(t, result)
		}
	}
	assert.Equal(t, 1, successes)
	assert.Equal(t, callers-1, duplicates)
}

func TestRuntimeCancelReleasesTask(t *testing.T) {
	t.Parallel()

	adapter := newFakeAdapter("fake")
	runtime, err := NewRuntime(t.TempDir(), adapter)
	require.NoError(t, err)
	trigger := Trigger{Type: TriggerAssignment, AdapterID: adapter.ID(), Envelope: testEnvelope("TASK-4")}
	run, err := runtime.Dispatch(context.Background(), trigger)
	require.NoError(t, err)

	require.NoError(t, runtime.CancelTask(context.Background(), "TASK-4"))
	events := collectEvents(t, run.Events)
	require.Len(t, events, 1)
	assert.Equal(t, EventCancelled, events[0].Type)
	_, active := runtime.ActiveRun("TASK-4")
	assert.False(t, active)
}

func TestRuntimeDoesNotTieAcceptedRunToDispatchContext(t *testing.T) {
	t.Parallel()

	adapter := newFakeAdapter("fake")
	runtime, err := NewRuntime(t.TempDir(), adapter)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	run, err := runtime.Dispatch(ctx, Trigger{
		Type: TriggerAssignment, AdapterID: adapter.ID(), Envelope: testEnvelope("TASK-CONTEXT"),
	})
	require.NoError(t, err)
	cancel()

	select {
	case <-adapter.lastLaunchContext().Done():
		require.FailNow(t, "accepted run inherited dispatch cancellation")
	case <-time.After(20 * time.Millisecond):
	}
	adapter.finish(run.ID, EventCompleted)
	collectEvents(t, run.Events)
}

func TestRuntimeTerminalStateIsNotBlockedBySlowConsumer(t *testing.T) {
	t.Parallel()

	adapter := newFakeAdapter("fake")
	runtime, err := NewRuntime(t.TempDir(), adapter)
	require.NoError(t, err)
	run, err := runtime.Dispatch(context.Background(), Trigger{
		Type: TriggerAssignment, AdapterID: adapter.ID(), Envelope: testEnvelope("TASK-SLOW-UI"),
	})
	require.NoError(t, err)

	go adapter.floodAndFinish(run.ID, 200)
	require.Eventually(t, func() bool {
		_, active := runtime.ActiveRun("TASK-SLOW-UI")
		return !active
	}, 5*time.Second, 10*time.Millisecond)
	events := collectEvents(t, run.Events)
	require.NotEmpty(t, events)
	assert.Equal(t, EventCompleted, events[len(events)-1].Type)
}

func TestRuntimeResumesWhenSessionIDIsPresent(t *testing.T) {
	t.Parallel()

	adapter := newFakeAdapter("fake")
	runtime, err := NewRuntime(t.TempDir(), adapter)
	require.NoError(t, err)
	run, err := runtime.Dispatch(context.Background(), Trigger{
		Type:      TriggerRetry,
		AdapterID: adapter.ID(),
		Envelope:  testEnvelope("TASK-5"),
		SessionID: "session-123",
	})
	require.NoError(t, err)
	assert.Equal(t, "session-123", adapter.lastResume().SessionID)
	adapter.finish(run.ID, EventCompleted)
	collectEvents(t, run.Events)
}

func TestRuntimeRejectsInvalidTriggerAndMissingCapability(t *testing.T) {
	t.Parallel()

	adapter := newFakeAdapter("fake")
	adapter.capabilities[CapabilityResume] = false
	runtime, err := NewRuntime(t.TempDir(), adapter)
	require.NoError(t, err)

	_, err = runtime.Dispatch(context.Background(), Trigger{
		Type:      "timer",
		AdapterID: adapter.ID(),
		Envelope:  testEnvelope("TASK-6"),
	})
	require.ErrorContains(t, err, "invalid trigger")

	_, err = runtime.Dispatch(context.Background(), Trigger{
		Type:      TriggerRetry,
		AdapterID: adapter.ID(),
		Envelope:  testEnvelope("TASK-6"),
		SessionID: "session-123",
	})
	require.ErrorIs(t, err, ErrCapability)
	_, active := runtime.ActiveRun("TASK-6")
	assert.False(t, active)
}

func TestRuntimePreparesWorktreeBeforeLaunch(t *testing.T) {
	t.Parallel()

	adapter := newFakeAdapter("fake")
	var preparedPath string
	preparer := WorktreePreparerFunc(func(_ context.Context, taskID, path string) error {
		assert.Equal(t, "TASK-PREPARE", taskID)
		preparedPath = path
		return nil
	})
	runtime, err := NewRuntimeWithPreparer(t.TempDir(), preparer, adapter)
	require.NoError(t, err)
	run, err := runtime.Dispatch(context.Background(), Trigger{
		Type: TriggerAssignment, AdapterID: adapter.ID(), Envelope: testEnvelope("TASK-PREPARE"),
	})
	require.NoError(t, err)
	assert.Equal(t, run.Worktree, preparedPath)
	assert.Equal(t, preparedPath, adapter.lastLaunch().Worktree)
	adapter.finish(run.ID, EventCompleted)
	collectEvents(t, run.Events)
}

func TestRuntimePreparerFailureReleasesReservation(t *testing.T) {
	t.Parallel()

	adapter := newFakeAdapter("fake")
	preparer := WorktreePreparerFunc(func(context.Context, string, string) error {
		return errors.New("cannot prepare")
	})
	runtime, err := NewRuntimeWithPreparer(t.TempDir(), preparer, adapter)
	require.NoError(t, err)
	trigger := Trigger{
		Type: TriggerAssignment, AdapterID: adapter.ID(), Envelope: testEnvelope("TASK-PREPARE-FAIL"),
	}
	_, err = runtime.Dispatch(context.Background(), trigger)
	require.ErrorContains(t, err, "cannot prepare")
	_, active := runtime.ActiveRun("TASK-PREPARE-FAIL")
	assert.False(t, active)
	assert.Equal(t, 0, adapter.launchCount())
	_, err = runtime.Dispatch(context.Background(), trigger)
	require.ErrorContains(t, err, "cannot prepare")
}

func testEnvelope(taskID string) TaskEnvelope {
	return TaskEnvelope{
		TaskID:  taskID,
		Summary: "Implement the bounded change",
		Criteria: []Criterion{
			{ID: "tests", Summary: "focused tests pass"},
		},
		DetailsRef: "agentsview://tasks/" + taskID,
	}
}

func collectEvents(t *testing.T, source <-chan Event) []Event {
	t.Helper()
	var events []Event
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case event, ok := <-source:
			if !ok {
				return events
			}
			events = append(events, event)
		case <-timer.C:
			require.FailNow(t, "timed out waiting for event stream")
		}
	}
}

type fakeAdapter struct {
	id           string
	capabilities Capabilities

	mu       sync.Mutex
	sequence int
	launches []LaunchRequest
	contexts []context.Context
	resumes  []ResumeRequest
	runs     map[string]chan Event
}

func newFakeAdapter(id string) *fakeAdapter {
	return &fakeAdapter{
		id: id,
		capabilities: Capabilities{
			CapabilityLaunch:  true,
			CapabilityResume:  true,
			CapabilityCancel:  true,
			CapabilityObserve: true,
		},
		runs: make(map[string]chan Event),
	}
}

func (a *fakeAdapter) ID() string { return a.id }

func (a *fakeAdapter) Capabilities() Capabilities { return a.capabilities }

func (a *fakeAdapter) Launch(ctx context.Context, request LaunchRequest) (RunRef, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.launches = append(a.launches, request)
	a.contexts = append(a.contexts, ctx)
	return a.newRun(), nil
}

func (a *fakeAdapter) Resume(_ context.Context, request ResumeRequest) (RunRef, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.resumes = append(a.resumes, request)
	return a.newRun(), nil
}

func (a *fakeAdapter) newRun() RunRef {
	a.sequence++
	runID := fmt.Sprintf("run-%d", a.sequence)
	a.runs[runID] = make(chan Event, 2)
	return RunRef{ID: runID}
}

func (a *fakeAdapter) Observe(_ context.Context, runID string) (<-chan Event, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	events, ok := a.runs[runID]
	if !ok {
		return nil, ErrRunNotFound
	}
	return events, nil
}

func (a *fakeAdapter) Cancel(_ context.Context, runID string) error {
	a.finish(runID, EventCancelled)
	return nil
}

func (a *fakeAdapter) finish(runID string, eventType EventType) {
	a.mu.Lock()
	defer a.mu.Unlock()
	events := a.runs[runID]
	events <- Event{Type: eventType}
	close(events)
	delete(a.runs, runID)
}

func (a *fakeAdapter) floodAndFinish(runID string, count int) {
	a.mu.Lock()
	events := a.runs[runID]
	a.mu.Unlock()
	for i := range count {
		events <- Event{Type: EventOutput, Message: fmt.Sprintf("event-%d", i)}
	}
	events <- Event{Type: EventCompleted}
	close(events)
	a.mu.Lock()
	delete(a.runs, runID)
	a.mu.Unlock()
}

func (a *fakeAdapter) lastLaunch() LaunchRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.launches[len(a.launches)-1]
}

func (a *fakeAdapter) lastResume() ResumeRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.resumes[len(a.resumes)-1]
}

func (a *fakeAdapter) lastLaunchContext() context.Context {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.contexts[len(a.contexts)-1]
}

func (a *fakeAdapter) launchCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.launches)
}
