package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.kenn.io/agentsview/internal/config"
)

func TestManagedTaskRuntimeDefaultIsInert(t *testing.T) {
	t.Parallel()

	option, err := managedTaskRuntimeOption(config.Config{
		TaskRuntime: config.TaskRuntimeConfig{
			Repository: "not-validated-while-disabled",
		},
	})
	require.NoError(t, err)
	assert.Nil(t, option)
}

func TestManagedTaskRuntimeEnabledBuildsLazyOption(t *testing.T) {
	t.Parallel()

	repository := initTaskRuntimeRepository(t)
	dataDir := t.TempDir()
	worktreeRoot := filepath.Join(dataDir, defaultTaskWorktreeDirectory)
	option, err := managedTaskRuntimeOption(config.Config{
		DataDir: dataDir,
		TaskRuntime: config.TaskRuntimeConfig{
			Enabled: true, Repository: repository,
		},
	})
	require.NoError(t, err)
	assert.NotNil(t, option)
	assert.NoDirExists(t, worktreeRoot, "idle runtime must not prepare a worktree")
}

func TestResolveManagedTaskRuntimeConfig(t *testing.T) {
	t.Parallel()

	repository := initTaskRuntimeRepository(t)
	tests := []struct {
		name      string
		configure func(*config.Config)
		message   string
	}{
		{
			name: "missing repository",
			configure: func(cfg *config.Config) {
				cfg.TaskRuntime.Repository = ""
			},
			message: "repository is required",
		},
		{
			name: "relative repository",
			configure: func(cfg *config.Config) {
				cfg.TaskRuntime.Repository = "relative/repo"
			},
			message: "repository must be an absolute path",
		},
		{
			name: "overlapping worktree root",
			configure: func(cfg *config.Config) {
				cfg.TaskRuntime.WorktreeRoot = filepath.Join(repository, "tasks")
			},
			message: "must not overlap",
		},
		{
			name: "option shaped ref",
			configure: func(cfg *config.Config) {
				cfg.TaskRuntime.Ref = "--orphan"
			},
			message: "ref must not start",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.Config{
				DataDir: t.TempDir(),
				TaskRuntime: config.TaskRuntimeConfig{
					Enabled: true, Repository: repository,
				},
			}
			test.configure(&cfg)
			_, _, err := resolveManagedTaskRuntimeConfig(cfg)
			require.ErrorContains(t, err, test.message)
		})
	}
}

func TestResolveManagedTaskRuntimeConfigDefaults(t *testing.T) {
	t.Parallel()

	repository := initTaskRuntimeRepository(t)
	dataDir := t.TempDir()
	resolved, enabled, err := resolveManagedTaskRuntimeConfig(config.Config{
		DataDir: dataDir,
		TaskRuntime: config.TaskRuntimeConfig{
			Enabled: true, Repository: repository,
		},
	})
	require.NoError(t, err)
	assert.True(t, enabled)
	assert.Equal(t, repository, resolved.repository)
	canonicalDataDir, err := filepath.EvalSymlinks(dataDir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(canonicalDataDir, defaultTaskWorktreeDirectory), resolved.worktreeRoot)
	assert.Equal(t, "HEAD", resolved.ref)
}

func initTaskRuntimeRepository(t *testing.T) string {
	t.Helper()
	repository := t.TempDir()
	output, err := exec.Command("git", "-C", repository, "init", "--quiet").CombinedOutput()
	require.NoError(t, err, string(output))
	repository, err = filepath.EvalSymlinks(repository)
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(repository, ".git"))
	require.NoError(t, err)
	return repository
}
