package taskrun

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type WorktreePreparer interface {
	Prepare(context.Context, string, string) error
}

type WorktreePreparerFunc func(context.Context, string, string) error

func (f WorktreePreparerFunc) Prepare(ctx context.Context, taskID, path string) error {
	return f(ctx, taskID, path)
}

// GitWorktreePreparer creates detached task worktrees from a configured local
// repository. It performs no network access and is only used when explicitly
// supplied to NewRuntimeWithPreparer.
type GitWorktreePreparer struct {
	Repository string
	Ref        string
}

func (p GitWorktreePreparer) Prepare(ctx context.Context, _ string, path string) error {
	repository, err := filepath.Abs(strings.TrimSpace(p.Repository))
	if err != nil {
		return fmt.Errorf("resolve repository: %w", err)
	}
	info, err := os.Stat(repository)
	if err != nil {
		return fmt.Errorf("inspect repository: %w", err)
	}
	if !info.IsDir() {
		return errors.New("repository is not a directory")
	}
	if _, err := os.Stat(path); err == nil {
		if _, gitErr := os.Stat(filepath.Join(path, ".git")); gitErr == nil {
			return nil
		}
		return errors.New("worktree path already exists but is not a git worktree")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect worktree: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create worktree root: %w", err)
	}
	ref := strings.TrimSpace(p.Ref)
	if ref == "" {
		ref = "HEAD"
	}
	output, err := exec.CommandContext(
		ctx, "git", "-C", repository, "worktree", "add", "--detach", path, ref,
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree add: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// ResolveWorktreePath returns a stable, task-specific path below root. The
// digest prevents collisions between task IDs that produce the same slug.
func ResolveWorktreePath(root, taskID string) (string, error) {
	if strings.TrimSpace(taskID) == "" {
		return "", errors.New("task ID is required")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve worktree root: %w", err)
	}
	root = filepath.Clean(root)
	if root == string(filepath.Separator) {
		return "", errors.New("filesystem root cannot be used as worktree root")
	}

	slug := taskSlug(taskID)
	digest := sha256.Sum256([]byte(taskID))
	path := filepath.Join(root, slug+"-"+hex.EncodeToString(digest[:5]))
	if err := ValidateWorktreePath(root, path); err != nil {
		return "", err
	}
	return path, nil
}

func taskSlug(taskID string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(taskID)) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			lastDash = false
		} else if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "task"
	}
	const maxSlugLength = 48
	if len(slug) > maxSlugLength {
		slug = strings.TrimRight(slug[:maxSlugLength], "-")
	}
	return slug
}

// ValidateWorktreePath rejects lexical traversal and symlink escapes through
// existing ancestors. The final task directory does not need to exist yet.
func ValidateWorktreePath(root, path string) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve worktree root: %w", err)
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve worktree path: %w", err)
	}
	rootAbs = filepath.Clean(rootAbs)
	pathAbs = filepath.Clean(pathAbs)
	if pathAbs == rootAbs || !pathWithin(rootAbs, pathAbs) {
		return errors.New("worktree path must be a child of worktree root")
	}

	resolvedRoot, err := evalExisting(rootAbs)
	if err != nil {
		return fmt.Errorf("resolve worktree root symlinks: %w", err)
	}
	ancestor, err := nearestExisting(pathAbs)
	if err != nil {
		return fmt.Errorf("inspect worktree path: %w", err)
	}
	resolvedAncestor, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return fmt.Errorf("resolve worktree path symlinks: %w", err)
	}
	if resolvedAncestor != resolvedRoot && !pathWithin(resolvedRoot, resolvedAncestor) {
		return errors.New("worktree path escapes root through a symlink")
	}
	return nil
}

func pathWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func evalExisting(path string) (string, error) {
	ancestor, err := nearestExisting(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(ancestor)
}

func nearestExisting(path string) (string, error) {
	current := filepath.Clean(path)
	for {
		_, err := os.Lstat(current)
		if err == nil {
			return current, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", os.ErrNotExist
		}
		current = parent
	}
}
