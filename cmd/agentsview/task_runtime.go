package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.kenn.io/agentsview/internal/config"
	"go.kenn.io/agentsview/internal/server"
	"go.kenn.io/agentsview/internal/taskrun"
)

const defaultTaskWorktreeDirectory = "task-worktrees"

type managedTaskRuntimeConfig struct {
	repository   string
	worktreeRoot string
	ref          string
}

// managedTaskRuntimeOption is deliberately inert when disabled. In
// particular, it does not inspect Git, create directories, or launch a harness.
func managedTaskRuntimeOption(cfg config.Config) (server.Option, error) {
	resolved, enabled, err := resolveManagedTaskRuntimeConfig(cfg)
	if err != nil || !enabled {
		return nil, err
	}
	runtime, err := taskrun.NewRuntimeWithPreparer(
		resolved.worktreeRoot,
		taskrun.GitWorktreePreparer{Repository: resolved.repository, Ref: resolved.ref},
		taskrun.BuiltInCommandAdapters()...,
	)
	if err != nil {
		return nil, fmt.Errorf("create managed task runtime: %w", err)
	}
	return server.WithTaskRuntime(runtime), nil
}

// taskGateEvaluatorOption is deliberately inert (returns nil, nil) when no
// task_runtime.repository is configured, so an unconfigured daemon keeps
// today's caller-asserted gate behavior.
func taskGateEvaluatorOption(cfg config.Config) (server.Option, error) {
	resolved, ok, err := resolveGateRuleRepository(cfg)
	if err != nil || !ok {
		return nil, err
	}
	return server.WithTaskGateEvaluator(
		server.NewRuleGateEvaluator(resolved.repository, resolved.worktreeRoot),
	), nil
}

func resolveManagedTaskRuntimeConfig(
	cfg config.Config,
) (managedTaskRuntimeConfig, bool, error) {
	if !cfg.TaskRuntime.Enabled {
		return managedTaskRuntimeConfig{}, false, nil
	}
	resolved, err := validateTaskRuntimeConfig(cfg, "task runtime repository is required when enabled")
	if err != nil {
		return managedTaskRuntimeConfig{}, false, err
	}
	return resolved, true, nil
}

// resolveGateRuleRepository resolves the repository/worktree pair used to run
// deterministic gate rules server-side. Unlike the managed runtime, gate
// evaluation does not require task_runtime.enabled: gates get evaluated
// through the CLI/API whether or not the daemon spawns agents. It is inert
// (ok=false, err=nil) when no repository is configured, so an unconfigured
// daemon keeps today's caller-asserted gate behavior.
func resolveGateRuleRepository(cfg config.Config) (managedTaskRuntimeConfig, bool, error) {
	if strings.TrimSpace(cfg.TaskRuntime.Repository) == "" {
		return managedTaskRuntimeConfig{}, false, nil
	}
	resolved, err := validateTaskRuntimeConfig(cfg, "")
	if err != nil {
		return managedTaskRuntimeConfig{}, false, err
	}
	return resolved, true, nil
}

func validateTaskRuntimeConfig(cfg config.Config, emptyRepositoryMessage string) (managedTaskRuntimeConfig, error) {
	repository := strings.TrimSpace(cfg.TaskRuntime.Repository)
	if repository == "" {
		if emptyRepositoryMessage == "" {
			emptyRepositoryMessage = "task runtime repository is required"
		}
		return managedTaskRuntimeConfig{}, errors.New(emptyRepositoryMessage)
	}
	if !filepath.IsAbs(repository) {
		return managedTaskRuntimeConfig{}, errors.New("task runtime repository must be an absolute path")
	}
	repository, err := filepath.EvalSymlinks(filepath.Clean(repository))
	if err != nil {
		return managedTaskRuntimeConfig{}, fmt.Errorf("resolve task runtime repository: %w", err)
	}
	info, err := os.Stat(repository)
	if err != nil {
		return managedTaskRuntimeConfig{}, fmt.Errorf("inspect task runtime repository: %w", err)
	}
	if !info.IsDir() {
		return managedTaskRuntimeConfig{}, errors.New("task runtime repository is not a directory")
	}
	if _, err := os.Stat(filepath.Join(repository, ".git")); err != nil {
		return managedTaskRuntimeConfig{}, errors.New("task runtime repository is not a Git worktree")
	}

	worktreeRoot := strings.TrimSpace(cfg.TaskRuntime.WorktreeRoot)
	if worktreeRoot == "" {
		worktreeRoot = filepath.Join(cfg.DataDir, defaultTaskWorktreeDirectory)
	}
	if !filepath.IsAbs(worktreeRoot) {
		return managedTaskRuntimeConfig{}, errors.New("task runtime worktree root must be an absolute path")
	}
	worktreeRoot, err = resolveProspectivePath(filepath.Clean(worktreeRoot))
	if err != nil {
		return managedTaskRuntimeConfig{}, fmt.Errorf("resolve task runtime worktree root: %w", err)
	}
	if worktreeRoot == string(filepath.Separator) {
		return managedTaskRuntimeConfig{}, errors.New("filesystem root cannot be used as task runtime worktree root")
	}
	if pathsOverlap(repository, worktreeRoot) {
		return managedTaskRuntimeConfig{}, errors.New("task runtime repository and worktree root must not overlap")
	}
	if info, statErr := os.Stat(worktreeRoot); statErr == nil && !info.IsDir() {
		return managedTaskRuntimeConfig{}, errors.New("task runtime worktree root is not a directory")
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return managedTaskRuntimeConfig{}, fmt.Errorf("inspect task runtime worktree root: %w", statErr)
	}
	if _, err := taskrun.ResolveWorktreePath(worktreeRoot, "validation"); err != nil {
		return managedTaskRuntimeConfig{}, err
	}

	ref := strings.TrimSpace(cfg.TaskRuntime.Ref)
	if ref == "" {
		ref = "HEAD"
	}
	if strings.HasPrefix(ref, "-") {
		return managedTaskRuntimeConfig{}, errors.New("task runtime ref must not start with '-'")
	}
	return managedTaskRuntimeConfig{
		repository: repository, worktreeRoot: worktreeRoot, ref: ref,
	}, nil
}

func resolveProspectivePath(path string) (string, error) {
	ancestor := path
	for {
		_, err := os.Lstat(ancestor)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return "", os.ErrNotExist
		}
		ancestor = parent
	}
	resolvedAncestor, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(ancestor, path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(filepath.Join(resolvedAncestor, relative)), nil
}

func pathsOverlap(first, second string) bool {
	return pathContains(first, second) || pathContains(second, first)
}

func pathContains(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	return err == nil && relative != ".." &&
		!strings.HasPrefix(relative, ".."+string(filepath.Separator)) &&
		!filepath.IsAbs(relative)
}
