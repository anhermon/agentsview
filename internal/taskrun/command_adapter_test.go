package taskrun

import (
	"context"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuiltInCommandDefinitionsCoverSupportedHarnesses(t *testing.T) {
	t.Parallel()

	definitions := BuiltInCommandDefinitions()
	byID := make(map[string]CommandDefinition, len(definitions))
	for _, definition := range definitions {
		byID[definition.AdapterID] = definition
	}
	for _, id := range []string{"claude", "codex", "agy", "antigravity", "pi", "hermes", "dsh"} {
		definition, ok := byID[id]
		assert.True(t, ok, id)
		assert.NotEmpty(t, definition.Executable, id)
		assert.NotNil(t, definition.ResumeArgs, id)
	}
}

func TestCommandAdapterStreamsOutputAndCompletion(t *testing.T) {
	t.Parallel()

	worktree := t.TempDir()
	adapter := commandTestAdapter("complete")
	ref, err := adapter.Launch(context.Background(), LaunchRequest{
		Envelope: testEnvelope("COMMAND-1"),
		Trigger:  TriggerAssignment,
		Worktree: worktree,
	})
	require.NoError(t, err)
	events, err := adapter.Observe(context.Background(), ref.ID)
	require.NoError(t, err)
	collected := collectEvents(t, events)

	assert.Contains(t, eventTypes(collected), EventStarted)
	assert.Contains(t, eventTypes(collected), EventOutput)
	assert.Equal(t, EventCompleted, collected[len(collected)-1].Type)
	assert.Contains(t, eventMessages(collected), "received task envelope")
}

func TestCommandAdapterCancellationEmitsTerminalEvent(t *testing.T) {
	t.Parallel()

	worktree := t.TempDir()
	adapter := commandTestAdapter("block")
	ref, err := adapter.Launch(context.Background(), LaunchRequest{
		Envelope: testEnvelope("COMMAND-2"),
		Trigger:  TriggerAssignment,
		Worktree: worktree,
	})
	require.NoError(t, err)
	events, err := adapter.Observe(context.Background(), ref.ID)
	require.NoError(t, err)

	ready := make(chan struct{})
	collected := make(chan []Event, 1)
	go func() {
		var values []Event
		for event := range events {
			values = append(values, event)
			if event.Message == "ready" {
				close(ready)
			}
		}
		collected <- values
	}()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "helper did not become ready")
	}
	require.NoError(t, adapter.Cancel(context.Background(), ref.ID))

	select {
	case values := <-collected:
		require.NotEmpty(t, values)
		assert.Equal(t, EventCancelled, values[len(values)-1].Type)
	case <-time.After(5 * time.Second):
		require.FailNow(t, "cancelled command did not exit")
	}
}

func TestCommandAdapterHelper(t *testing.T) {
	if os.Getenv("AGENTSVIEW_COMMAND_HELPER") != "1" {
		return
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
	switch os.Getenv("AGENTSVIEW_COMMAND_MODE") {
	case "complete":
		fmt.Println("received task envelope")
	case "block":
		fmt.Println("ready")
		for {
			time.Sleep(time.Hour)
		}
	default:
		os.Exit(2)
	}
	os.Exit(0)
}

func commandTestAdapter(mode string) *CommandAdapter {
	return NewCommandAdapter(CommandDefinition{
		AdapterID:  "test-command",
		Executable: os.Args[0],
		LaunchArgs: []string{"-test.run=TestCommandAdapterHelper"},
		Environment: []string{
			"AGENTSVIEW_COMMAND_HELPER=1",
			"AGENTSVIEW_COMMAND_MODE=" + mode,
		},
	})
}

func eventTypes(events []Event) []EventType {
	result := make([]EventType, 0, len(events))
	for _, event := range events {
		result = append(result, event.Type)
	}
	return result
}

func eventMessages(events []Event) []string {
	result := make([]string, 0, len(events))
	for _, event := range events {
		result = append(result, event.Message)
	}
	return result
}
