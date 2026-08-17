package taskrun

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type CommandDefinition struct {
	AdapterID   string
	Executable  string
	LaunchArgs  []string
	ResumeArgs  func(sessionID string) []string
	Environment []string
}

// BuiltInCommandDefinitions are thin process-launch definitions. The task
// envelope is sent on stdin so it does not appear in process listings.
func BuiltInCommandDefinitions() []CommandDefinition {
	return []CommandDefinition{
		{AdapterID: "claude", Executable: "claude", LaunchArgs: []string{"--print", "--output-format", "stream-json", "--verbose"}, ResumeArgs: appendSession("--resume")},
		{AdapterID: "codex", Executable: "codex", LaunchArgs: []string{"exec", "--json", "-"}, ResumeArgs: codexResumeArgs},
		{AdapterID: "agy", Executable: "agy", LaunchArgs: []string{"run", "--json"}, ResumeArgs: appendSession("resume")},
		{AdapterID: "antigravity", Executable: "agy", LaunchArgs: []string{"run", "--json"}, ResumeArgs: appendSession("resume")},
		{AdapterID: "pi", Executable: "pi", LaunchArgs: []string{"--print", "--json"}, ResumeArgs: appendSession("--resume")},
		{AdapterID: "hermes", Executable: "hermes", LaunchArgs: []string{"chat", "--json"}, ResumeArgs: appendSession("--resume")},
		{AdapterID: "dsh", Executable: "dsh", LaunchArgs: []string{"run", "--json"}, ResumeArgs: appendSession("resume")},
	}
}

func BuiltInCommandAdapters() []Adapter {
	definitions := BuiltInCommandDefinitions()
	adapters := make([]Adapter, 0, len(definitions))
	for _, definition := range definitions {
		adapters = append(adapters, NewCommandAdapter(definition))
	}
	return adapters
}

func appendSession(flag string) func(string) []string {
	return func(sessionID string) []string { return []string{flag, sessionID} }
}

func codexResumeArgs(sessionID string) []string {
	return []string{"exec", "resume", sessionID, "--json", "-"}
}

type CommandAdapter struct {
	definition CommandDefinition

	mu   sync.Mutex
	runs map[string]*commandRun
}

type commandRun struct {
	cancel   context.CancelFunc
	events   chan Event
	done     chan struct{}
	observed bool
}

func NewCommandAdapter(definition CommandDefinition) *CommandAdapter {
	return &CommandAdapter{definition: definition, runs: make(map[string]*commandRun)}
}

func (a *CommandAdapter) ID() string { return a.definition.AdapterID }

func (a *CommandAdapter) Capabilities() Capabilities {
	return Capabilities{
		CapabilityLaunch:  true,
		CapabilityResume:  a.definition.ResumeArgs != nil,
		CapabilityCancel:  true,
		CapabilityObserve: true,
	}
}

func (a *CommandAdapter) Launch(ctx context.Context, request LaunchRequest) (RunRef, error) {
	return a.start(ctx, request, a.definition.LaunchArgs)
}

func (a *CommandAdapter) Resume(ctx context.Context, request ResumeRequest) (RunRef, error) {
	if a.definition.ResumeArgs == nil {
		return RunRef{}, ErrCapability
	}
	if strings.TrimSpace(request.SessionID) == "" {
		return RunRef{}, errors.New("session ID is required to resume")
	}
	return a.start(ctx, request.LaunchRequest, a.definition.ResumeArgs(request.SessionID))
}

func (a *CommandAdapter) start(ctx context.Context, request LaunchRequest, args []string) (RunRef, error) {
	if err := request.Envelope.Validate(); err != nil {
		return RunRef{}, err
	}
	info, err := os.Stat(request.Worktree)
	if err != nil {
		return RunRef{}, fmt.Errorf("inspect worktree: %w", err)
	}
	if !info.IsDir() {
		return RunRef{}, errors.New("worktree is not a directory")
	}
	runID, err := newRunID()
	if err != nil {
		return RunRef{}, err
	}
	runCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(runCtx, a.definition.Executable, args...)
	cmd.Dir = request.Worktree
	cmd.Env = append(os.Environ(), a.definition.Environment...)
	cmd.Stdin = strings.NewReader(request.Envelope.Prompt())
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return RunRef{}, fmt.Errorf("capture adapter stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return RunRef{}, fmt.Errorf("capture adapter stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return RunRef{}, fmt.Errorf("start %s adapter: %w", a.ID(), err)
	}

	run := &commandRun{cancel: cancel, events: make(chan Event, 128), done: make(chan struct{})}
	a.mu.Lock()
	a.runs[runID] = run
	a.mu.Unlock()
	run.events <- Event{Type: EventStarted, RunID: runID, Time: time.Now().UTC()}
	go a.collect(runID, runCtx, cmd, stdout, stderr, run)
	return RunRef{ID: runID}, nil
}

func (a *CommandAdapter) collect(runID string, ctx context.Context, cmd *exec.Cmd, stdout, stderr io.Reader, run *commandRun) {
	defer run.cancel()
	var scanners sync.WaitGroup
	scanners.Add(2)
	go scanCommandOutput(&scanners, run.events, runID, "stdout", stdout)
	go scanCommandOutput(&scanners, run.events, runID, "stderr", stderr)
	scanners.Wait()
	err := cmd.Wait()

	event := Event{RunID: runID, Time: time.Now().UTC()}
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		event.Type = EventCancelled
		event.Message = "run cancelled"
	case err != nil:
		event.Type = EventFailed
		event.Message = err.Error()
	default:
		event.Type = EventCompleted
	}
	boundedEventSend(run.events, event, true)
	close(run.events)
	close(run.done)
}

func scanCommandOutput(wg *sync.WaitGroup, events chan Event, runID, stream string, reader io.Reader) {
	defer wg.Done()
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		boundedEventSend(events, Event{Type: EventOutput, RunID: runID, Time: time.Now().UTC(), Stream: stream, Message: scanner.Text()}, false)
	}
	if err := scanner.Err(); err != nil {
		boundedEventSend(events, Event{Type: EventOutput, RunID: runID, Time: time.Now().UTC(), Stream: stream, Message: "read output: " + err.Error()}, false)
	}
}

func boundedEventSend(events chan Event, event Event, terminal bool) {
	select {
	case events <- event:
		return
	default:
	}
	if !terminal {
		return
	}
	select {
	case <-events:
	default:
	}
	events <- event
}

func (a *CommandAdapter) Observe(_ context.Context, runID string) (<-chan Event, error) {
	a.mu.Lock()
	run, ok := a.runs[runID]
	if !ok {
		a.mu.Unlock()
		return nil, ErrRunNotFound
	}
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

func (a *CommandAdapter) Cancel(_ context.Context, runID string) error {
	a.mu.Lock()
	run, ok := a.runs[runID]
	a.mu.Unlock()
	if !ok {
		return ErrRunNotFound
	}
	run.cancel()
	return nil
}

func newRunID() (string, error) {
	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("create run ID: %w", err)
	}
	return hex.EncodeToString(value[:]), nil
}
