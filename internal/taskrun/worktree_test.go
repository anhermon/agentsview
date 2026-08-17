package taskrun

import (
	"os"
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
