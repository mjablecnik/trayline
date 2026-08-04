package git

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
		return err
	}
	if _, err := r.Run(repoPath, "checkout", "HEAD", "--", path); err == nil {
		return nil
	}
	_, err := r.Run(repoPath, "clean", "-f", "-d", "--", path)
	return err
}

// DiscardAll reverts every tracked change back to HEAD and removes every
// untracked file and directory, leaving the working tree clean.
func (r *Runner) DiscardAll(repoPath string) error {
	if _, err := r.Run(repoPath, "reset", "--hard", "HEAD"); err != nil {
		return err
	}
	_, err := r.Run(repoPath, "clean", "-f", "-d")
	return err
}
