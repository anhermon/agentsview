package taskrun

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExternalAdapterStreamsNormalizedEvents(t *testing.T) {
	t.Parallel()

	adapter := externalTestAdapter("complete")
	ref, err := adapter.Launch(context.Background(), LaunchRequest{
		Envelope: testEnvelope("EXTERNAL-1"),
		Trigger:  TriggerAssignment,
		Worktree: t.TempDir(),
	})
	require.NoError(t, err)
	assert.Equal(t, "external-run", ref.ID)
	assert.Equal(t, "external-session", ref.SessionID)
	events, err := adapter.Observe(context.Background(), ref.ID)
	require.NoError(t, err)
	collected := collectEvents(t, events)
	require.Len(t, collected, 2)
	assert.Equal(t, EventStarted, collected[0].Type)
	assert.Equal(t, EventCompleted, collected[1].Type)
}

func TestExternalAdapterRejectsMalformedAcceptedResponse(t *testing.T) {
	t.Parallel()

	adapter := externalTestAdapter("malformed-first")
	_, err := adapter.Launch(context.Background(), LaunchRequest{
		Envelope: testEnvelope("EXTERNAL-2"),
		Trigger:  TriggerAssignment,
		Worktree: t.TempDir(),
	})
	require.ErrorIs(t, err, ErrMalformedOutput)
}

func TestExternalAdapterTurnsMalformedStreamIntoFailure(t *testing.T) {
	t.Parallel()

	adapter := externalTestAdapter("malformed-stream")
	ref, err := adapter.Launch(context.Background(), LaunchRequest{
		Envelope: testEnvelope("EXTERNAL-3"),
		Trigger:  TriggerAssignment,
		Worktree: t.TempDir(),
	})
	require.NoError(t, err)
	events, err := adapter.Observe(context.Background(), ref.ID)
	require.NoError(t, err)
	collected := collectEvents(t, events)
	require.NotEmpty(t, collected)
	assert.Equal(t, EventFailed, collected[len(collected)-1].Type)
	assert.Contains(t, collected[len(collected)-1].Message, ErrMalformedOutput.Error())
}

func TestExternalAdapterCancellationUsesProtocolAndStopsStream(t *testing.T) {
	t.Parallel()

	adapter := externalTestAdapter("block")
	ref, err := adapter.Launch(context.Background(), LaunchRequest{
		Envelope: testEnvelope("EXTERNAL-4"),
		Trigger:  TriggerAssignment,
		Worktree: t.TempDir(),
	})
	require.NoError(t, err)
	events, err := adapter.Observe(context.Background(), ref.ID)
	require.NoError(t, err)

	first := <-events
	assert.Equal(t, EventStarted, first.Type)
	cancelCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, adapter.Cancel(cancelCtx, ref.ID))
	rest := collectEvents(t, events)
	require.NotEmpty(t, rest)
	assert.Equal(t, EventCancelled, rest[len(rest)-1].Type)
}

func TestExternalAdapterCapabilitiesAreDefensivelyCopied(t *testing.T) {
	t.Parallel()

	capabilities := allExternalCapabilities()
	adapter := NewExternalAdapter(ExternalDefinition{AdapterID: "external", Capabilities: capabilities})
	capabilities[CapabilityLaunch] = false
	returned := adapter.Capabilities()
	returned[CapabilityCancel] = false

	assert.True(t, adapter.Capabilities().Supports(CapabilityLaunch))
	assert.True(t, adapter.Capabilities().Supports(CapabilityCancel))
}

func TestExternalAdapterHelper(t *testing.T) {
	if os.Getenv("AGENTSVIEW_EXTERNAL_HELPER") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		os.Exit(2)
	}
	var request ExternalRequest
	if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
		os.Exit(3)
	}
	mode := os.Getenv("AGENTSVIEW_EXTERNAL_MODE")
	if mode == "malformed-first" {
		fmt.Println("not-json")
		os.Exit(0)
	}
	if request.Operation == ExternalCancel {
		writeExternalResponse(ExternalResponse{
			Protocol:  ExternalProtocolV1,
			RequestID: request.RequestID,
			Kind:      "ack",
		})
		os.Exit(0)
	}
	writeExternalResponse(ExternalResponse{
		Protocol:  ExternalProtocolV1,
		RequestID: request.RequestID,
		Kind:      "accepted",
		RunID:     "external-run",
		SessionID: "external-session",
	})
	if mode == "malformed-stream" {
		fmt.Println("not-json")
		os.Exit(0)
	}
	writeExternalResponse(ExternalResponse{
		Protocol:  ExternalProtocolV1,
		RequestID: request.RequestID,
		Kind:      "event",
		Event:     &Event{Type: EventStarted},
	})
	if mode == "block" {
		for {
			time.Sleep(time.Hour)
		}
	}
	writeExternalResponse(ExternalResponse{
		Protocol:  ExternalProtocolV1,
		RequestID: request.RequestID,
		Kind:      "event",
		Event:     &Event{Type: EventCompleted},
	})
	os.Exit(0)
}

func writeExternalResponse(response ExternalResponse) {
	if err := json.NewEncoder(os.Stdout).Encode(response); err != nil {
		os.Exit(4)
	}
}

func externalTestAdapter(mode string) *ExternalAdapter {
	return NewExternalAdapter(ExternalDefinition{
		AdapterID:  "external-test",
		Executable: os.Args[0],
		Args:       []string{"-test.run=TestExternalAdapterHelper"},
		Environment: []string{
			"AGENTSVIEW_EXTERNAL_HELPER=1",
			"AGENTSVIEW_EXTERNAL_MODE=" + mode,
		},
		Capabilities: allExternalCapabilities(),
	})
}

func allExternalCapabilities() Capabilities {
	return Capabilities{
		CapabilityLaunch:  true,
		CapabilityResume:  true,
		CapabilityCancel:  true,
		CapabilityObserve: true,
	}
}
