package taskrun

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
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
	return a.start(ctx, request, a.definition.LaunchArgs, "")
}

func (a *CommandAdapter) Resume(ctx context.Context, request ResumeRequest) (RunRef, error) {
	if a.definition.ResumeArgs == nil {
		return RunRef{}, ErrCapability
	}
	if strings.TrimSpace(request.SessionID) == "" {
		return RunRef{}, errors.New("session ID is required to resume")
	}
	return a.start(ctx, request.LaunchRequest, a.definition.ResumeArgs(request.SessionID), request.SessionID)
}

func (a *CommandAdapter) start(ctx context.Context, request LaunchRequest, args []string, sessionID string) (RunRef, error) {
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
	run.events <- Event{Type: EventStarted, RunID: runID, SessionID: sessionID, Time: time.Now().UTC()}
	go a.collect(runID, runCtx, cmd, stdout, stderr, run)
	return RunRef{ID: runID, SessionID: sessionID}, nil
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
		line := scanner.Text()
		now := time.Now().UTC()
		if activity, ok := normalizeCommandActivity(line); ok {
			activity.RunID = runID
			activity.Time = now
			activity.Stream = stream
			boundedEventSend(events, activity, false)
		}
		boundedEventSend(events, Event{Type: EventOutput, RunID: runID, Time: now, Stream: stream, Message: line}, false)
	}
	if err := scanner.Err(); err != nil {
		boundedEventSend(events, Event{Type: EventOutput, RunID: runID, Time: time.Now().UTC(), Stream: stream, Message: "read output: " + err.Error()}, false)
	}
}

const maxActivityMessageBytes = 240

// normalizeCommandActivity extracts only stable session identifiers and a
// compact activity label from structured harness output. The original line is
// kept on the in-memory output stream and is never copied into this event.
func normalizeCommandActivity(line string) (Event, bool) {
	var value map[string]any
	if err := json.Unmarshal([]byte(line), &value); err != nil {
		return Event{}, false
	}
	sessionID := firstTopLevelString(value, "session_id", "sessionId", "thread_id", "threadId", "conversation_id", "conversationId")
	parts := compactActivityParts(value)
	if len(parts) == 0 && sessionID == "" {
		return Event{}, false
	}
	message := strings.Join(parts, ": ")
	if len(message) > maxActivityMessageBytes {
		message = message[:maxActivityMessageBytes]
	}
	return Event{Type: EventActivity, SessionID: sessionID, Message: message}, true
}

func compactActivityParts(value map[string]any) []string {
	parts := make([]string, 0, 3)
	for _, key := range []string{"type", "subtype"} {
		if text, ok := value[key].(string); ok && strings.TrimSpace(text) != "" {
			parts = append(parts, strings.TrimSpace(text))
		}
	}
	if nested, ok := value["item"].(map[string]any); ok {
		if text, ok := nested["type"].(string); ok && strings.TrimSpace(text) != "" {
			parts = append(parts, strings.TrimSpace(text))
		}
	}
	if len(parts) < 3 {
		if name := firstToolName(value); name != "" {
			parts = append(parts, name)
		}
	}
	return parts
}

func firstToolName(value any) string {
	switch typed := value.(type) {
	case map[string]any:
		if eventType, _ := typed["type"].(string); eventType == "tool_use" {
			if name, _ := typed["name"].(string); strings.TrimSpace(name) != "" {
				return strings.TrimSpace(name)
			}
		}
		for _, child := range typed {
			if name := firstToolName(child); name != "" {
				return name
			}
		}
	case []any:
		for _, child := range typed {
			if name := firstToolName(child); name != "" {
				return name
			}
		}
	}
	return ""
}

func firstTopLevelString(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if text, ok := value[key].(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
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
