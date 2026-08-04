package git

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// showTimeout extends the default timeout for Show, since full commit diffs
// can be large and slow to generate.
const showTimeout = 10 * time.Second

// CommitDetail is the full metadata, stats, and unified diff for a single
// commit.
type CommitDetail struct {
	Hash         string
	ShortHash    string
	Message      string
	Author       string
	Date         time.Time
	FilesChanged int
	Insertions   int
	Deletions    int
	Diff         string
}

// statSummaryRe matches the trailing summary line of `git show --stat`
// output, e.g. "2 files changed, 8 insertions(+), 5 deletions(-)".
var statSummaryRe = regexp.MustCompile(`^\s*(\d+) files? changed(?:, (\d+) insertions?\(\+\))?(?:, (\d+) deletions?\(-\))?\s*$`)

// Show returns full metadata, change stats, and the unified diff for hash.
// It returns ErrNotFound if hash does not exist in the repo.
func (r *Runner) Show(repoPath, hash string) (*CommitDetail, error) {
	showRunner := &Runner{Timeout: showTimeout}

	format := "%H" + fieldSep + "%h" + fieldSep + "%s" + fieldSep + "%an" + fieldSep + "%aI"
	statOut, err := showRunner.Run(repoPath, "show", "--stat", "--format="+format, hash)
	if err != nil {
		return nil, notFoundError(err)
	}

	detail, err := parseShowStat(statOut)
	if err != nil {
		return nil, err
	}

	// --root ensures the initial commit's diff is shown against the empty
	// tree instead of being silently empty (diff-tree's default behavior
	// for commits with no parent).
	diff, err := showRunner.Run(repoPath, "diff-tree", "-p", "--no-commit-id", "--root", hash)
	if err != nil {
		return nil, notFoundError(err)
	}
	detail.Diff = diff

	return detail, nil
}

// notFoundError classifies a git command error as ErrNotFound when it
// failed due to a bad object rather than a timeout.
func notFoundError(err error) error {
	var gitErr *Error
	if errors.As(err, &gitErr) && !gitErr.Timeout {
		return ErrNotFound
	}
	return err
}

func parseShowStat(out string) (*CommitDetail, error) {
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return nil, fmt.Errorf("git: unexpected show output: %q", out)
	}

	fields := strings.Split(lines[0], fieldSep)
	if len(fields) != 5 {
		return nil, fmt.Errorf("git: unexpected show format line: %q", lines[0])
	}
	date, err := time.Parse(time.RFC3339, fields[4])
	if err != nil {
		return nil, fmt.Errorf("git: unexpected commit date format: %q", fields[4])
	}

	detail := &CommitDetail{
		Hash:      fields[0],
		ShortHash: fields[1],
		Message:   fields[2],
		Author:    fields[3],
		Date:      date,
	}

	for _, line := range lines[1:] {
		m := statSummaryRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		detail.FilesChanged, _ = strconv.Atoi(m[1])
		if m[2] != "" {
			detail.Insertions, _ = strconv.Atoi(m[2])
		}
		if m[3] != "" {
			detail.Deletions, _ = strconv.Atoi(m[3])
		}
		break
	}

	return detail, nil
}
