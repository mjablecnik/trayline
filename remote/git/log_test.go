package git

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// repoWithCommits creates a repo with n sequential commits, each touching
// commit.txt, on branch "main".
func repoWithCommits(t *testing.T, n int) string {
	t.Helper()
	dir := initRepo(t)
	for i := 1; i <= n; i++ {
		content := []byte(strconv.Itoa(i) + "\n")
		if err := os.WriteFile(filepath.Join(dir, "commit.txt"), content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}
		gitCmd(t, dir, "add", "commit.txt")
		gitCmd(t, dir, "commit", "-m", "commit "+strconv.Itoa(i))
	}
	return dir
}

func TestLogReturnsCommitsMostRecentFirst(t *testing.T) {
	r := NewRunner()
	dir := repoWithCommits(t, 3)

	result, err := r.Log(dir, "main", 50, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Total != 3 {
		t.Fatalf("expected total 3, got %d", result.Total)
	}
	if len(result.Commits) != 3 {
		t.Fatalf("expected 3 commits, got %d", len(result.Commits))
	}
	if result.Commits[0].Message != "commit 3" {
		t.Errorf("expected most recent commit first, got %q", result.Commits[0].Message)
	}
	if result.Commits[2].Message != "commit 1" {
		t.Errorf("expected oldest commit last, got %q", result.Commits[2].Message)
	}
	if result.Commits[0].Hash == "" || result.Commits[0].ShortHash == "" {
		t.Error("expected non-empty hash and short hash")
	}
	if result.Commits[0].Author != "Test" {
		t.Errorf("expected author %q, got %q", "Test", result.Commits[0].Author)
	}
	if result.Commits[0].Date.IsZero() {
		t.Error("expected non-zero commit date")
	}
}

func TestLogPagination(t *testing.T) {
	r := NewRunner()
	dir := repoWithCommits(t, 5)

	page1, err := r.Log(dir, "main", 2, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(page1.Commits) != 2 || page1.Total != 5 {
		t.Fatalf("expected 2 commits and total 5, got %d commits total %d", len(page1.Commits), page1.Total)
	}
	if page1.Commits[0].Message != "commit 5" || page1.Commits[1].Message != "commit 4" {
		t.Errorf("unexpected page1 commits: %+v", page1.Commits)
	}

	page2, err := r.Log(dir, "main", 2, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(page2.Commits) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(page2.Commits))
	}
	if page2.Commits[0].Message != "commit 3" || page2.Commits[1].Message != "commit 2" {
		t.Errorf("unexpected page2 commits: %+v", page2.Commits)
	}
}

func TestLogInvalidRefReturnsErrInvalidRef(t *testing.T) {
	r := NewRunner()
	dir := repoWithCommits(t, 1)

	_, err := r.Log(dir, "no-such-ref", 50, 0)
	if err != ErrInvalidRef {
		t.Fatalf("expected ErrInvalidRef, got %v", err)
	}
}

func TestLogEmptyRepoReturnsErrInvalidRef(t *testing.T) {
	r := NewRunner()
	dir := initRepo(t)

	_, err := r.Log(dir, "main", 50, 0)
	if err != ErrInvalidRef {
		t.Fatalf("expected ErrInvalidRef for repo with no commits, got %v", err)
	}
}
