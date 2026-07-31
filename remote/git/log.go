package git

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ErrInvalidRef indicates the requested ref does not resolve to a valid
// commit range.
var ErrInvalidRef = errors.New("git: invalid ref")

// LogResult is a paginated page of commits plus the total count reachable
// from the queried ref.
type LogResult struct {
	Commits []CommitEntry
	Total   int
}

// CommitEntry describes one commit in a log listing.
type CommitEntry struct {
	Hash      string
	ShortHash string
	Message   string
	Author    string
	Date      time.Time
}

// Log returns a page of commits reachable from ref (most recent first),
// along with the total number of commits reachable from ref.
func (r *Runner) Log(repoPath, ref string, limit, offset int) (*LogResult, error) {
	countOut, err := r.Run(repoPath, "rev-list", "--count", ref)
	if err != nil {
		return nil, refError(err)
	}
	total, err := strconv.Atoi(strings.TrimSpace(countOut))
	if err != nil {
		return nil, fmt.Errorf("git: unexpected rev-list output: %q", countOut)
	}

	format := "%H" + fieldSep + "%h" + fieldSep + "%s" + fieldSep + "%an" + fieldSep + "%aI"
	out, err := r.Run(repoPath, "log", "--format="+format, fmt.Sprintf("--skip=%d", offset), "-n", strconv.Itoa(limit), ref)
	if err != nil {
		return nil, refError(err)
	}

	commits, err := parseLogOutput(out)
	if err != nil {
		return nil, err
	}

	return &LogResult{Commits: commits, Total: total}, nil
}

// refError classifies a git command error as ErrInvalidRef when it failed
// due to a bad ref rather than a timeout.
func refError(err error) error {
	var gitErr *Error
	if errors.As(err, &gitErr) && !gitErr.Timeout {
		return ErrInvalidRef
	}
	return err
}

func parseLogOutput(out string) ([]CommitEntry, error) {
	trimmed := strings.TrimRight(out, "\n")
	if trimmed == "" {
		return []CommitEntry{}, nil
	}

	lines := strings.Split(trimmed, "\n")
	commits := make([]CommitEntry, 0, len(lines))
	for _, line := range lines {
		fields := strings.Split(line, fieldSep)
		if len(fields) != 5 {
			return nil, fmt.Errorf("git: unexpected log output format: %q", line)
		}
		date, err := time.Parse(time.RFC3339, fields[4])
		if err != nil {
			return nil, fmt.Errorf("git: unexpected commit date format: %q", fields[4])
		}
		commits = append(commits, CommitEntry{
			Hash:      fields[0],
			ShortHash: fields[1],
			Message:   fields[2],
			Author:    fields[3],
			Date:      date,
		})
	}
	return commits, nil
}
