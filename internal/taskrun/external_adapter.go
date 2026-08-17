package taskrun

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

type ExternalDefinition struct {
	AdapterID    string
	Executable   string
	Args         []string
	Environment  []string
	Capabilities Capabilities
}

type ExternalOperation string

const (
	ExternalLaunch  ExternalOperation = "launch"
	ExternalResume  ExternalOperation = "resume"
	ExternalCancel  ExternalOperation = "cancel"
	ExternalObserve ExternalOperation = "observe"
)

// ExternalRequest is one JSON line written to an external adapter's stdin.
type ExternalRequest struct {
	Protocol  string            `json:"protocol"`
	RequestID string            `json:"request_id"`
	Operation ExternalOperation `json:"operation"`
	Launch    *LaunchRequest    `json:"launch,omitempty"`
	Resume    *ResumeRequest    `json:"resume,omitempty"`
	RunID     string            `json:"run_id,omitempty"`
}

// ExternalResponse is one JSON line read from an external adapter's stdout.
// Streaming operations first emit kind=accepted, then kind=event responses.
// Cancel emits one kind=ack response and exits.
type ExternalResponse struct {
	Protocol  string `json:"protocol"`
	RequestID string `json:"request_id"`
	Kind      string `json:"kind"`
	RunID     string `json:"run_id,omitempty"`
	Event     *Event `json:"event,omitempty"`
	Error     string `json:"error,omitempty"`
}

type ExternalAdapter struct {
	definition ExternalDefinition

	mu   sync.Mutex
	runs map[string]*externalRun
}

type externalRun struct {
	cancel   context.CancelFunc
	events   chan Event
	done     chan struct{}
	observed bool
}

func NewExternalAdapter(definition ExternalDefinition) *ExternalAdapter {
	capabilities := make(Capabilities, len(definition.Capabilities))
	for capability, supported := range definition.Capabilities {
		capabilities[capability] = supported
	}
	definition.Capabilities = capabilities
	return &ExternalAdapter{definition: definition, runs: make(map[string]*externalRun)}
}

func (a *ExternalAdapter) ID() string { return a.definition.AdapterID }

func (a *ExternalAdapter) Capabilities() Capabilities {
	result := make(Capabilities, len(a.definition.Capabilities))
	for capability, supported := range a.definition.Capabilities {
		result[capability] = supported
	}
	return result
}

func (a *ExternalAdapter) Launch(ctx context.Context, request LaunchRequest) (RunRef, error) {
	if !a.definition.Capabilities.Supports(CapabilityLaunch) {
		return RunRef{}, ErrCapability
	}
	if err := request.Envelope.Validate(); err != nil {
		return RunRef{}, err
	}
	return a.startStream(ctx, ExternalRequest{Operation: ExternalLaunch, Launch: &request}, request.Worktree)
}

func (a *ExternalAdapter) Resume(ctx context.Context, request ResumeRequest) (RunRef, error) {
	if !a.definition.Capabilities.Supports(CapabilityResume) {
		return RunRef{}, ErrCapability
	}
	if err := request.Envelope.Validate(); err != nil {
		return RunRef{}, err
	}
	if request.SessionID == "" {
		return RunRef{}, errors.New("session ID is required to resume")
	}
	return a.startStream(ctx, ExternalRequest{Operation: ExternalResume, Resume: &request}, request.Worktree)
}

func (a *ExternalAdapter) Observe(ctx context.Context, runID string) (<-chan Event, error) {
	if !a.definition.Capabilities.Supports(CapabilityObserve) {
		return nil, ErrCapability
	}
	a.mu.Lock()
	run, ok := a.runs[runID]
	a.mu.Unlock()
	if !ok {
		ref, err := a.startStream(ctx, ExternalRequest{Operation: ExternalObserve, RunID: runID}, "")
		if err != nil {
			return nil, err
		}
		if ref.ID != runID {
			return nil, fmt.Errorf("external adapter observed run %q instead of %q", ref.ID, runID)
		}
		a.mu.Lock()
		run = a.runs[runID]
		a.mu.Unlock()
	}

	a.mu.Lock()
	if run.observed {
		a.mu.Unlock()
		return nil, errors.New("run is already being observed")
	}
	run.observed = true
	a.mu.Unlock()
	go func() {
		<-run.done
		a.mu.Lock()
		delete(a.runs, runID)
		a.mu.Unlock()
	}()
	return run.events, nil
}

func (a *ExternalAdapter) Cancel(ctx context.Context, runID string) error {
	if !a.definition.Capabilities.Supports(CapabilityCancel) {
		return ErrCapability
	}
	requestID, err := newRunID()
	if err != nil {
		return err
	}
	request := ExternalRequest{
		Protocol:  ExternalProtocolV1,
		RequestID: requestID,
		Operation: ExternalCancel,
		RunID:     runID,
	}
	if err := a.runOneShot(ctx, request, "ack"); err != nil {
		return err
	}
	a.mu.Lock()
	run, ok := a.runs[runID]
	a.mu.Unlock()
	if ok {
		run.cancel()
	}
	return nil
}

func (a *ExternalAdapter) startStream(ctx context.Context, request ExternalRequest, worktree string) (RunRef, error) {
	requestID, err := newRunID()
	if err != nil {
		return RunRef{}, err
	}
	request.Protocol = ExternalProtocolV1
	request.RequestID = requestID
	runCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(runCtx, a.definition.Executable, a.definition.Args...)
	cmd.Dir = worktree
	cmd.Env = append(os.Environ(), a.definition.Environment...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return RunRef{}, fmt.Errorf("open external adapter stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return RunRef{}, fmt.Errorf("open external adapter stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return RunRef{}, fmt.Errorf("open external adapter stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return RunRef{}, fmt.Errorf("start external adapter: %w", err)
	}
	go func() { _, _ = io.Copy(io.Discard, stderr) }()
	if err := json.NewEncoder(stdin).Encode(request); err != nil {
		cancel()
		_ = cmd.Wait()
		return RunRef{}, fmt.Errorf("write external adapter request: %w", err)
	}
	if err := stdin.Close(); err != nil {
		cancel()
		_ = cmd.Wait()
		return RunRef{}, fmt.Errorf("close external adapter request: %w", err)
	}

	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() {
		cancel()
		waitErr := cmd.Wait()
		if scanner.Err() != nil {
			return RunRef{}, fmt.Errorf("%w: %v", ErrMalformedOutput, scanner.Err())
		}
		return RunRef{}, fmt.Errorf("%w: missing accepted response: %v", ErrMalformedOutput, waitErr)
	}
	response, err := decodeExternalResponse(scanner.Bytes(), requestID)
	if err != nil || response.Kind != "accepted" || response.RunID == "" {
		cancel()
		_ = cmd.Wait()
		if err != nil {
			return RunRef{}, err
		}
		return RunRef{}, fmt.Errorf("%w: expected accepted response with run_id", ErrMalformedOutput)
	}

	run := &externalRun{cancel: cancel, events: make(chan Event, 128), done: make(chan struct{})}
	a.mu.Lock()
	if _, exists := a.runs[response.RunID]; exists {
		a.mu.Unlock()
		cancel()
		_ = cmd.Wait()
		return RunRef{}, fmt.Errorf("external adapter reused active run ID %q", response.RunID)
	}
	a.runs[response.RunID] = run
	a.mu.Unlock()
	go a.collectStream(runCtx, cmd, scanner, requestID, response.RunID, run)
	return RunRef{ID: response.RunID}, nil
}

func (a *ExternalAdapter) collectStream(ctx context.Context, cmd *exec.Cmd, scanner *bufio.Scanner, requestID, runID string, run *externalRun) {
	defer run.cancel()
	terminal := false
	malformed := error(nil)
	for scanner.Scan() {
		response, err := decodeExternalResponse(scanner.Bytes(), requestID)
		if err != nil {
			malformed = err
			break
		}
		if response.Kind != "event" || response.Event == nil || !validEventType(response.Event.Type) {
			malformed = fmt.Errorf("%w: expected normalized event response", ErrMalformedOutput)
			break
		}
		event := *response.Event
		if event.RunID == "" {
			event.RunID = runID
		}
		boundedEventSend(run.events, event, event.Type.Terminal())
		if event.Type.Terminal() {
			terminal = true
			break
		}
	}
	if err := scanner.Err(); err != nil && malformed == nil {
		malformed = fmt.Errorf("%w: %v", ErrMalformedOutput, err)
	}
	if malformed != nil {
		run.cancel()
	}
	if terminal {
		run.cancel()
	}
	waitErr := cmd.Wait()
	if !terminal {
		event := Event{RunID: runID, Time: time.Now().UTC()}
		switch {
		case malformed != nil:
			event.Type = EventFailed
			event.Message = malformed.Error()
		case errors.Is(ctx.Err(), context.Canceled):
			event.Type = EventCancelled
			event.Message = "run cancelled"
		case waitErr != nil:
			event.Type = EventFailed
			event.Message = waitErr.Error()
		default:
			event.Type = EventFailed
			event.Message = "external adapter exited without a terminal event"
		}
		boundedEventSend(run.events, event, true)
	}
	close(run.events)
	close(run.done)
}

func (a *ExternalAdapter) runOneShot(ctx context.Context, request ExternalRequest, expectedKind string) error {
	opCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(opCtx, a.definition.Executable, a.definition.Args...)
	cmd.Env = append(os.Environ(), a.definition.Environment...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	if err := json.NewEncoder(stdin).Encode(request); err != nil {
		_ = stdin.Close()
		_ = cmd.Wait()
		return err
	}
	_ = stdin.Close()
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() {
		cancel()
		_ = cmd.Wait()
		return fmt.Errorf("%w: missing %s response", ErrMalformedOutput, expectedKind)
	}
	response, err := decodeExternalResponse(scanner.Bytes(), request.RequestID)
	if err != nil {
		cancel()
		_ = cmd.Wait()
		return err
	}
	if response.Kind != expectedKind {
		cancel()
		_ = cmd.Wait()
		return fmt.Errorf("%w: expected %s response", ErrMalformedOutput, expectedKind)
	}
	if response.Error != "" {
		cancel()
		_ = cmd.Wait()
		return errors.New(response.Error)
	}
	cancel()
	_ = cmd.Wait()
	return nil
}

func decodeExternalResponse(line []byte, requestID string) (ExternalResponse, error) {
	var response ExternalResponse
	if err := json.Unmarshal(line, &response); err != nil {
		return response, fmt.Errorf("%w: %v", ErrMalformedOutput, err)
	}
	if response.Protocol != ExternalProtocolV1 {
		return response, fmt.Errorf("%w: unsupported protocol %q", ErrMalformedOutput, response.Protocol)
	}
	if response.RequestID != requestID {
		return response, fmt.Errorf("%w: response request_id mismatch", ErrMalformedOutput)
	}
	return response, nil
}

func validEventType(eventType EventType) bool {
	switch eventType {
	case EventStarted, EventOutput, EventPhaseChanged, EventProgress, EventBlocked, EventCompleted, EventFailed, EventCancelled:
		return true
	default:
		return false
	}
}
