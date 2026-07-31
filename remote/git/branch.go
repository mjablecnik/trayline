package git

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// IsRepo reports whether path contains a .git entry (directory or file, to
// also cover worktrees/submodules).
func (r *Runner) IsRepo(path string) bool {
	_, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil
}

// CurrentBranch returns the name of the currently checked-out branch.
// For a detached HEAD, this returns "HEAD".
func (r *Runner) CurrentBranch(repoPath string) (string, error) {
	out, err := r.Run(repoPath, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// Branches returns the list of local branch names.
func (r *Runner) Branches(repoPath string) ([]string, error) {
	out, err := r.Run(repoPath, "branch", "--format=%(refname:short)")
	if err != nil {
		return nil, err
	}

	var branches []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			branches = append(branches, line)
		}
	}
	return branches, nil
}

// RemoteURL returns the URL of the "origin" remote, or "" if no such remote
// is configured.
func (r *Runner) RemoteURL(repoPath string) (string, error) {
	out, err := r.Run(repoPath, "remote", "get-url", "origin")
	if err != nil {
		var gitErr *Error
		if errors.As(err, &gitErr) && !gitErr.Timeout {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(out), nil
}
