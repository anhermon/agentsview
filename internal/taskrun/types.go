// Package taskrun provides an event-driven runtime boundary for assigning tasks
// to agent harnesses. It deliberately has no persistence or scheduling loop.
package taskrun

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const ExternalProtocolV1 = "agentsview.taskrun/v1"

var (
	ErrActiveRun       = errors.New("task already has an active run")
	ErrCapability      = errors.New("adapter capability is not supported")
	ErrMalformedOutput = errors.New("malformed adapter output")
	ErrRunNotFound     = errors.New("run not found")
)

type Capability string

const (
	CapabilityLaunch  Capability = "launch"
	CapabilityResume  Capability = "resume"
	CapabilityCancel  Capability = "cancel"
	CapabilityObserve Capability = "observe"
)

type Capabilities map[Capability]bool

func (c Capabilities) Supports(capability Capability) bool {
	return c[capability]
}

type Adapter interface {
	ID() string
	Capabilities() Capabilities
}

type Launcher interface {
	Adapter
	Launch(context.Context, LaunchRequest) (RunRef, error)
}

type Resumer interface {
	Adapter
	Resume(context.Context, ResumeRequest) (RunRef, error)
}

type Canceler interface {
	Adapter
	Cancel(context.Context, string) error
}

type Observer interface {
	Adapter
	Observe(context.Context, string) (<-chan Event, error)
}

type TriggerType string

const (
	TriggerAssignment        TriggerType = "assignment"
	TriggerDependencyCleared TriggerType = "dependency-cleared"
	TriggerMention           TriggerType = "mention"
	TriggerRetry             TriggerType = "retry"
)

func (t TriggerType) Valid() bool {
	switch t {
	case TriggerAssignment, TriggerDependencyCleared, TriggerMention, TriggerRetry:
		return true
	default:
		return false
	}
}

type Criterion struct {
	ID      string `json:"id"`
	Summary string `json:"summary"`
}

type Reference struct {
	Label string `json:"label,omitempty"`
	Kind  string `json:"kind"`
	URI   string `json:"uri"`
}

// TaskEnvelope is intentionally compact. Full task history and large artifacts
// stay behind DetailsRef and can be fetched by the agent only when needed.
type TaskEnvelope struct {
	TaskID       string      `json:"task_id"`
	Summary      string      `json:"summary"`
	Criteria     []Criterion `json:"criteria,omitempty"`
	References   []Reference `json:"references,omitempty"`
	Instructions []string    `json:"instructions,omitempty"`
	DetailsRef   string      `json:"details_ref,omitempty"`
}

func (e TaskEnvelope) Validate() error {
	if strings.TrimSpace(e.TaskID) == "" {
		return errors.New("task envelope requires task_id")
	}
	if strings.TrimSpace(e.Summary) == "" {
		return errors.New("task envelope requires summary")
	}
	for i, criterion := range e.Criteria {
		if strings.TrimSpace(criterion.Summary) == "" {
			return fmt.Errorf("criterion %d requires summary", i)
		}
	}
	for i, reference := range e.References {
		if strings.TrimSpace(reference.Kind) == "" || strings.TrimSpace(reference.URI) == "" {
			return fmt.Errorf("reference %d requires kind and uri", i)
		}
	}
	for i, instruction := range e.Instructions {
		if strings.TrimSpace(instruction) == "" {
			return fmt.Errorf("instruction %d must not be empty", i)
		}
	}
	return nil
}

// Prompt renders only the bounded task envelope, not task history.
func (e TaskEnvelope) Prompt() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Task %s: %s\n", e.TaskID, e.Summary)
	if len(e.Criteria) > 0 {
		b.WriteString("Completion criteria:\n")
		for _, criterion := range e.Criteria {
			if criterion.ID != "" {
				fmt.Fprintf(&b, "- [%s] %s\n", criterion.ID, criterion.Summary)
			} else {
				fmt.Fprintf(&b, "- %s\n", criterion.Summary)
			}
		}
	}
	if len(e.References) > 0 {
		b.WriteString("References:\n")
		for _, reference := range e.References {
			label := reference.Label
			if label == "" {
				label = reference.Kind
			}
			fmt.Fprintf(&b, "- %s: %s\n", label, reference.URI)
		}
	}
	if len(e.Instructions) > 0 {
		b.WriteString("Lifecycle instructions:\n")
		for _, instruction := range e.Instructions {
			fmt.Fprintf(&b, "- %s\n", instruction)
		}
	}
	if e.DetailsRef != "" {
		fmt.Fprintf(&b, "Fetch additional details on demand from: %s\n", e.DetailsRef)
	}
	return b.String()
}

type LaunchRequest struct {
	Envelope TaskEnvelope `json:"envelope"`
	Trigger  TriggerType  `json:"trigger"`
	Worktree string       `json:"worktree"`
}

type ResumeRequest struct {
	LaunchRequest
	SessionID string `json:"session_id"`
}

type RunRef struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id,omitempty"`
}

type EventType string

const (
	EventStarted      EventType = "started"
	EventOutput       EventType = "output"
	EventActivity     EventType = "activity"
	EventPhaseChanged EventType = "phase-changed"
	EventProgress     EventType = "progress"
	EventBlocked      EventType = "blocked"
	EventCompleted    EventType = "completed"
	EventFailed       EventType = "failed"
	EventCancelled    EventType = "cancelled"
)

func (t EventType) Terminal() bool {
	return t == EventCompleted || t == EventFailed || t == EventCancelled
}

type Event struct {
	Type      EventType      `json:"type"`
	RunID     string         `json:"run_id,omitempty"`
	TaskID    string         `json:"task_id,omitempty"`
	AdapterID string         `json:"adapter_id,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	Time      time.Time      `json:"time,omitempty"`
	Phase     string         `json:"phase,omitempty"`
	Message   string         `json:"message,omitempty"`
	Stream    string         `json:"stream,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
}

type Trigger struct {
	Type      TriggerType
	AdapterID string
	Envelope  TaskEnvelope
	SessionID string
}

type Run struct {
	ID        string
	TaskID    string
	AdapterID string
	SessionID string
	Worktree  string
	Events    <-chan Event
}
