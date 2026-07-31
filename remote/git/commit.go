package git

import (
	"fmt"
	"strings"
)

// fieldSep is git's unit separator; safe to split on since it cannot appear
// in a commit subject, author name, or ISO date.
const fieldSep = "\x1f"

// Commit describes a single git commit.
type Commit struct {
	Hash    string `json:"hash"`
	Message string `json:"message"`
	Author  string `json:"author"`
	Date    string `json:"date"` // ISO 8601
}

// LastCommit returns the most recent commit reachable from HEAD.
func (r *Runner) LastCommit(repoPath string) (*Commit, error) {
	format := "%H" + fieldSep + "%s" + fieldSep + "%an" + fieldSep + "%aI"
	out, err := r.Run(repoPath, "log", "-1", "--format="+format)
	if err != nil {
		return nil, err
	}

	fields := strings.Split(strings.TrimRight(out, "\n"), fieldSep)
	if len(fields) != 4 {
		return nil, fmt.Errorf("git: unexpected log output format: %q", out)
	}

	return &Commit{
		Hash:    fields[0],
		Message: fields[1],
		Author:  fields[2],
		Date:    fields[3],
	}, nil
}

// HasUncommittedChanges reports whether the working tree has any
// uncommitted changes (staged, unstaged, or untracked files).
func (r *Runner) HasUncommittedChanges(repoPath string) (bool, error) {
	out, err := r.Run(repoPath, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}
