package git

import (
	"strconv"
	"strings"
)

// maxDiffSize is the per-file diff size limit; larger diffs are replaced
// with a placeholder message.
const maxDiffSize = 500 * 1024 // 500 KB

// StatusResult is the working tree's uncommitted changes plus a summary.
type StatusResult struct {
	Clean   bool
	Files   []FileChange
	Summary StatusSummary
}

// FileChange describes one changed file in the working tree.
type FileChange struct {
	Path       string
	Status     string // "modified", "added", "deleted", "untracked"
	Insertions int
	Deletions  int
	Diff       *string // nil for untracked files
}

// StatusSummary aggregates counts across all changed files.
type StatusSummary struct {
	FilesChanged int
	Insertions   int
	Deletions    int
}

// Status returns the working tree's uncommitted changes (staged, unstaged,
// and untracked), including a per-file diff for every tracked change.
func (r *Runner) Status(repoPath string) (*StatusResult, error) {
	porcelainOut, err := r.Run(repoPath, "status", "--porcelain=v1")
	if err != nil {
		return nil, err
	}

	paths, statuses := parsePorcelain(porcelainOut)
	if len(paths) == 0 {
		return &StatusResult{Clean: true, Files: []FileChange{}}, nil
	}

	numstatOut, err := r.Run(repoPath, "diff", "HEAD", "--numstat")
	if err != nil {
		return nil, err
	}
	counts := parseNumstat(numstatOut)

	files := make([]FileChange, 0, len(paths))
	var summary StatusSummary
	for _, path := range paths {
		fc := FileChange{Path: path, Status: statuses[path]}

		if c, ok := counts[path]; ok {
			fc.Insertions = c.insertions
			fc.Deletions = c.deletions
		}

		if fc.Status != "untracked" {
			diff, err := r.Run(repoPath, "diff", "HEAD", "--", path)
			if err != nil {
				return nil, err
			}
			if len(diff) > maxDiffSize {
				diff = "(diff too large)"
			}
			fc.Diff = &diff
		}

		files = append(files, fc)
		summary.FilesChanged++
		summary.Insertions += fc.Insertions
		summary.Deletions += fc.Deletions
	}

	return &StatusResult{Clean: false, Files: files, Summary: summary}, nil
}

// parsePorcelain parses `git status --porcelain=v1` output, returning the
// ordered list of changed paths and a path -> status map.
func parsePorcelain(out string) ([]string, map[string]string) {
	trimmed := strings.TrimRight(out, "\n")
	if trimmed == "" {
		return nil, nil
	}

	var paths []string
	statuses := make(map[string]string)
	for _, line := range strings.Split(trimmed, "\n") {
		if len(line) < 4 {
			continue
		}
		code := line[:2]
		rest := line[3:]
		path := rest
		if idx := strings.Index(rest, " -> "); idx != -1 {
			path = rest[idx+4:]
		}
		paths = append(paths, path)
		statuses[path] = classifyStatus(code)
	}
	return paths, statuses
}

// classifyStatus maps a porcelain XY status code to one of "modified",
// "added", "deleted", "untracked".
func classifyStatus(code string) string {
	if code == "??" {
		return "untracked"
	}
	x, y := code[0], code[1]
	switch {
	case x == 'A' || y == 'A':
		return "added"
	case x == 'D' || y == 'D':
		return "deleted"
	default:
		return "modified"
	}
}

type numstatCount struct {
	insertions int
	deletions  int
}

// parseNumstat parses `git diff --numstat` output into a path -> counts
// map. Binary files report "-" for both counts, which are treated as zero.
func parseNumstat(out string) map[string]numstatCount {
	counts := make(map[string]numstatCount)
	trimmed := strings.TrimRight(out, "\n")
	if trimmed == "" {
		return counts
	}
	for _, line := range strings.Split(trimmed, "\n") {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		ins, _ := strconv.Atoi(parts[0])
		del, _ := strconv.Atoi(parts[1])
		counts[parts[2]] = numstatCount{insertions: ins, deletions: del}
	}
	return counts
}
