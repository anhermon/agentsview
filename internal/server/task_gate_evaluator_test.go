package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.kenn.io/agentsview/internal/taskcontrol"
	"go.kenn.io/agentsview/internal/taskrun"
)

func TestRuleGateEvaluatorExecutesRuleIgnoringCallerAssertion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		rule           string
		callerEvidence map[string]any
		wantStatus     taskcontrol.GateStatus
		wantExitCode   int
	}{
		{
			name:           "passing rule passes regardless of caller claim",
			rule:           "exit 0",
			callerEvidence: map[string]any{"passed": false},
			wantStatus:     taskcontrol.GateStatusPassed,
			wantExitCode:   0,
		},
		{
			name:           "failing rule fails even when caller asserts passed",
			rule:           "exit 1",
			callerEvidence: map[string]any{"passed": true},
			wantStatus:     taskcontrol.GateStatusFailed,
			wantExitCode:   1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			evaluator := NewRuleGateEvaluator(t.TempDir(), "")
			gate := taskcontrol.Gate{
				Kind: taskcontrol.GateKindDeterministic, Rule: test.rule,
			}
			input := taskcontrol.GateEvaluationContext{
				Task: taskcontrol.Task{ID: "task-1"}, Evidence: test.callerEvidence,
			}
			result, err := evaluator.Evaluate(context.Background(), gate, input)
			require.NoError(t, err)
			assert.Equal(t, test.wantStatus, result.Status)
			assert.Equal(t, test.wantExitCode, result.Evidence["exit_code"])
			assert.Equal(t, test.rule, result.Evidence["rule"])
		})
	}
}

func TestRuleGateEvaluatorCapturesOutput(t *testing.T) {
	t.Parallel()

	evaluator := NewRuleGateEvaluator(t.TempDir(), "")
	gate := taskcontrol.Gate{Kind: taskcontrol.GateKindDeterministic, Rule: "echo hello-from-rule"}
	result, err := evaluator.Evaluate(context.Background(), gate, taskcontrol.GateEvaluationContext{
		Task: taskcontrol.Task{ID: "task-1"},
	})
	require.NoError(t, err)
	assert.Equal(t, taskcontrol.GateStatusPassed, result.Status)
	assert.Contains(t, result.Evidence["output"], "hello-from-rule")
}

func TestRuleGateEvaluatorPrefersTaskWorktreeWhenPrepared(t *testing.T) {
	t.Parallel()

	repository := t.TempDir()
	worktreeRoot := t.TempDir()
	worktree, err := taskrun.ResolveWorktreePath(worktreeRoot, "task-with-worktree")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(worktree, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(worktree, "marker.txt"), []byte("x"), 0o644))

	evaluator := NewRuleGateEvaluator(repository, worktreeRoot)
	gate := taskcontrol.Gate{Kind: taskcontrol.GateKindDeterministic, Rule: "test -f marker.txt"}

	withWorktree, err := evaluator.Evaluate(context.Background(), gate, taskcontrol.GateEvaluationContext{
		Task: taskcontrol.Task{ID: "task-with-worktree"},
	})
	require.NoError(t, err)
	assert.Equal(t, taskcontrol.GateStatusPassed, withWorktree.Status)
	assert.Equal(t, "worktree", withWorktree.Evidence["dir_source"])

	withoutWorktree, err := evaluator.Evaluate(context.Background(), gate, taskcontrol.GateEvaluationContext{
		Task: taskcontrol.Task{ID: "task-without-worktree"},
	})
	require.NoError(t, err)
	assert.Equal(t, taskcontrol.GateStatusFailed, withoutWorktree.Status)
	assert.Equal(t, "repository", withoutWorktree.Evidence["dir_source"])
}

func TestRuleGateEvaluatorTimesOut(t *testing.T) {
	t.Parallel()

	evaluator := NewRuleGateEvaluator(t.TempDir(), "")
	evaluator.timeout = 50 * time.Millisecond
	gate := taskcontrol.Gate{Kind: taskcontrol.GateKindDeterministic, Rule: "sleep 2"}
	result, err := evaluator.Evaluate(context.Background(), gate, taskcontrol.GateEvaluationContext{
		Task: taskcontrol.Task{ID: "task-1"},
	})
	require.NoError(t, err)
	assert.Equal(t, taskcontrol.GateStatusFailed, result.Status)
	assert.Equal(t, true, result.Evidence["timed_out"])
}

func TestRuleGateEvaluatorFallsBackForNonDeterministicOrRuleless(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		gate taskcontrol.Gate
	}{
		{name: "human gate", gate: taskcontrol.Gate{Kind: taskcontrol.GateKindHuman, Rule: "exit 1"}},
		{name: "llm gate", gate: taskcontrol.Gate{Kind: taskcontrol.GateKindLLM, Rule: "exit 1"}},
		{name: "deterministic gate without a rule", gate: taskcontrol.Gate{Kind: taskcontrol.GateKindDeterministic}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			evaluator := NewRuleGateEvaluator(t.TempDir(), "")
			approved := true
			result, err := evaluator.Evaluate(context.Background(), test.gate, taskcontrol.GateEvaluationContext{
				Task: taskcontrol.Task{ID: "task-1"}, Approved: &approved,
			})
			require.NoError(t, err)
			assert.Equal(t, taskcontrol.GateStatusPassed, result.Status)
			_, hasExitCode := result.Evidence["exit_code"]
			assert.False(t, hasExitCode, "fallback path must not claim it ran a rule")
		})
	}
}
