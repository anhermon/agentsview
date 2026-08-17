package taskrun

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveWorktreePathIsStableAndTaskSpecific(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	first, err := ResolveWorktreePath(root, "Feature / Parse Sessions")
	require.NoError(t, err)
	again, err := ResolveWorktreePath(root, "Feature / Parse Sessions")
	require.NoError(t, err)
	other, err := ResolveWorktreePath(root, "Feature: Parse Sessions")
	require.NoError(t, err)

	assert.Equal(t, first, again)
	assert.NotEqual(t, first, other)
	assert.Equal(t, filepath.Clean(root), filepath.Dir(first))
	assert.Contains(t, filepath.Base(first), "feature-parse-sessions-")
}

func TestValidateWorktreePathRejectsTraversal(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.Error(t, ValidateWorktreePath(root, filepath.Join(root, "..", "outside")))
	require.Error(t, ValidateWorktreePath(root, root))
}

func TestValidateWorktreePathRejectsSymlinkEscape(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := t.TempDir()
	escape := filepath.Join(root, "escape")
	if err := os.Symlink(outside, escape); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	err := ValidateWorktreePath(root, filepath.Join(escape, "task"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "symlink")
}

func TestGitWorktreePreparerCreatesIdempotentDetachedWorktree(t *testing.T) {
	t.Parallel()

	repository := t.TempDir()
	runWorktreeGit(t, repository, "init")
	require.NoError(t, os.WriteFile(filepath.Join(repository, "README.md"), []byte("fixture\n"), 0o644))
	runWorktreeGit(t, repository, "add", "README.md")
	runWorktreeGit(t, repository, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "fixture")
	root := t.TempDir()
	path, err := ResolveWorktreePath(root, "TASK-GIT")
	require.NoError(t, err)
	preparer := GitWorktreePreparer{Repository: repository}

	require.NoError(t, preparer.Prepare(context.Background(), "TASK-GIT", path))
	info, err := os.Stat(filepath.Join(path, ".git"))
	require.NoError(t, err)
	assert.False(t, info.IsDir(), "linked worktree uses a .git file")
	require.NoError(t, preparer.Prepare(context.Background(), "TASK-GIT", path))
}

func runWorktreeGit(t *testing.T, repository string, args ...string) {
	t.Helper()
	commandArgs := append([]string{"-C", repository}, args...)
	output, err := exec.Command("git", commandArgs...).CombinedOutput()
	require.NoError(t, err, string(output))
}
