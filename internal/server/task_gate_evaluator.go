package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"go.kenn.io/agentsview/internal/taskcontrol"
	"go.kenn.io/agentsview/internal/taskrun"
)

const ruleGateEvaluationTimeout = 5 * time.Minute

// ruleGateEvaluator runs a deterministic gate's stored Rule as a shell
// command and gates on its exit code, instead of trusting a caller-supplied
// evidence.passed/approved boolean. It prefers the task's managed-runtime
// worktree when one has been prepared, and falls back to the configured
// repository root otherwise (the common case today, since gates are mostly
// evaluated against the integrated branch, not an isolated agent worktree).
// Gates with kind human/llm, or a deterministic gate with no rule, fall back
// to defaultTaskGateEvaluator: the daemon cannot execute human or model
// judgment.
type ruleGateEvaluator struct {
	repository   string
	worktreeRoot string
	timeout      time.Duration
}

// NewRuleGateEvaluator constructs a taskcontrol.GateEvaluator that executes
// deterministic gates' Rule server-side. repository must be an absolute,
// existing Git repository; worktreeRoot may be empty to always evaluate
// against repository.
func NewRuleGateEvaluator(repository, worktreeRoot string) *ruleGateEvaluator {
	return &ruleGateEvaluator{
		repository: repository, worktreeRoot: worktreeRoot,
		timeout: ruleGateEvaluationTimeout,
	}
}

func (e *ruleGateEvaluator) Evaluate(
	ctx context.Context, gate taskcontrol.Gate, input taskcontrol.GateEvaluationContext,
) (taskcontrol.GateEvaluation, error) {
	rule := strings.TrimSpace(gate.Rule)
	if gate.Kind != taskcontrol.GateKindDeterministic || rule == "" {
		return defaultTaskGateEvaluator(ctx, gate, input)
	}

	dir, dirSource := e.repository, "repository"
	if e.worktreeRoot != "" {
		if worktree, err := taskrun.ResolveWorktreePath(e.worktreeRoot, input.Task.ID); err == nil {
			if info, statErr := os.Stat(worktree); statErr == nil && info.IsDir() {
				dir, dirSource = worktree, "worktree"
			}
		}
	}

	timeout := e.timeout
	if timeout <= 0 {
		timeout = ruleGateEvaluationTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "sh", "-c", rule)
	cmd.Dir = dir
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	started := time.Now()
	runErr := cmd.Run()
	elapsed := time.Since(started)

	status := taskcontrol.GateStatusPassed
	exitCode := 0
	if runErr != nil {
		status = taskcontrol.GateStatusFailed
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	evidence := map[string]any{
		"rule": rule, "exit_code": exitCode, "duration_ms": elapsed.Milliseconds(),
		"dir_source": dirSource, "output": truncateStr(output.String(), 4000),
	}
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		status = taskcontrol.GateStatusFailed
		evidence["timed_out"] = true
		evidence["error"] = fmt.Sprintf("rule did not finish within %s", timeout)
	}
	return taskcontrol.GateEvaluation{Status: status, Evidence: evidence}, nil
}
