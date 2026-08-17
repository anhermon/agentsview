package config

import (
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskRuntimeConfigDefaultsDisabled(t *testing.T) {
	t.Parallel()

	cfg, err := Default()
	require.NoError(t, err)
	assert.False(t, cfg.TaskRuntime.Enabled)
	assert.Empty(t, cfg.TaskRuntime.Repository)
	assert.Empty(t, cfg.TaskRuntime.WorktreeRoot)
	assert.Empty(t, cfg.TaskRuntime.Ref)
}

func TestTaskRuntimeConfigLoadsFromTOML(t *testing.T) {
	t.Parallel()

	cfg, err := Default()
	require.NoError(t, err)
	require.NoError(t, cfg.applyConfigTOML(`
[task_runtime]
enabled = true
repository = "/srv/project"
worktree_root = "/srv/worktrees"
ref = "main"
`))
	assert.Equal(t, TaskRuntimeConfig{
		Enabled: true, Repository: "/srv/project", WorktreeRoot: "/srv/worktrees", Ref: "main",
	}, cfg.TaskRuntime)
}

func TestTaskRuntimeServeFlagsOverrideConfig(t *testing.T) {
	t.Parallel()

	cfg, err := Default()
	require.NoError(t, err)
	flags := pflag.NewFlagSet("serve", pflag.ContinueOnError)
	RegisterServePFlags(flags)
	require.NoError(t, flags.Parse([]string{
		"--task-runtime",
		"--task-runtime-repository=/srv/project",
		"--task-runtime-worktree-root=/srv/worktrees",
		"--task-runtime-ref=release",
	}))
	applyPFlags(&cfg, flags)

	assert.Equal(t, TaskRuntimeConfig{
		Enabled: true, Repository: "/srv/project", WorktreeRoot: "/srv/worktrees", Ref: "release",
	}, cfg.TaskRuntime)
}
