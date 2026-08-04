package git

import (
	"errors"
	"strings"
)

// ErrIndexLocked indicates a discard operation failed because
// repoPath/.git/index.lock already exists - either another git process is
// genuinely running against the repo right now, or a previous one crashed
// and left a stale lock file behind. Either way this is an environmental
// condition outside the repo's working-tree state, not something safe to
// resolve automatically: removing a lock a live process still holds can
// corrupt the repo.
var ErrIndexLocked = errors.New("git: index is locked by another process")

// classifyLockError wraps err as ErrIndexLocked if it's a *Error whose
// stderr shows git refused to acquire repoPath/.git/index.lock.
func classifyLockError(err error) error {
	var gitErr *Error
	if errors.As(err, &gitErr) && strings.Contains(gitErr.Stderr, "index.lock") {
		return errors.Join(ErrIndexLocked, err)
	}
	return err
}

// isPathspecMismatch reports whether err is git's "pathspec ... did not
// match any file(s) known to git" - the expected, harmless way `checkout
// HEAD -- path` fails for a path HEAD has never seen. Any other error
// (lock contention, permissions, ...) must NOT be treated as that case:
// falling back to `git clean` for a different failure could silently no-op
// on a path clean doesn't consider untracked, reporting success without
// having discarded anything.
func isPathspecMismatch(err error) bool {
	var gitErr *Error
	return errors.As(err, &gitErr) && strings.Contains(gitErr.Stderr, "did not match any file")
}

// DiscardFile reverts path back to its HEAD state, discarding both staged
// and unstaged changes to it. If HEAD has never seen the path - it's a
// plain untracked file, or was staged as new but never committed - checkout
// can't restore it from HEAD, so it's removed instead. Either way, the net
// effect matches whatever Status reported as a "change" for that path.
func (r *Runner) DiscardFile(repoPath, path string) error {
	// Unstage first: a newly `git add`ed file stays in the index (and thus
	// invisible to `git clean`) even after checkout fails to restore it from
	// HEAD below, unless it's dropped back to plain-untracked first. Safe
	// no-op for paths that were never staged.
	if _, err := r.Run(repoPath, "reset", "--", path); err != nil {
		return classifyLockError(err)
	}
	_, checkoutErr := r.Run(repoPath, "checkout", "HEAD", "--", path)
	if checkoutErr == nil {
		return nil
	}
	if !isPathspecMismatch(checkoutErr) {
		return classifyLockError(checkoutErr)
	}
	_, err := r.Run(repoPath, "clean", "-f", "-d", "--", path)
	return classifyLockError(err)
}

// DiscardAll reverts every tracked change back to HEAD and removes every
// untracked file and directory, leaving the working tree clean.
func (r *Runner) DiscardAll(repoPath string) error {
	if _, err := r.Run(repoPath, "reset", "--hard", "HEAD"); err != nil {
		return classifyLockError(err)
	}
	_, err := r.Run(repoPath, "clean", "-f", "-d")
	return classifyLockError(err)
}
