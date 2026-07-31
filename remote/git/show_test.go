package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShowRootCommit(t *testing.T) {
	r := NewRunner()
	dir := repoWithCommit(t)

	commit, err := r.LastCommit(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	detail, err := r.Show(dir, commit.Hash)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if detail.Hash != commit.Hash {
		t.Errorf("expected hash %q, got %q", commit.Hash, detail.Hash)
	}
	if detail.ShortHash == "" {
		t.Error("expected non-empty short hash")
	}
	if detail.Message != "initial commit" {
		t.Errorf("expected message %q, got %q", "initial commit", detail.Message)
	}
	if detail.Author != "Test" {
		t.Errorf("expected author %q, got %q", "Test", detail.Author)
	}
	if detail.Date.IsZero() {
		t.Error("expected non-zero date")
	}
	if detail.FilesChanged != 1 {
		t.Errorf("expected 1 file changed for root commit, got %d", detail.FilesChanged)
	}
	if detail.Insertions != 1 {
		t.Errorf("expected 1 insertion for root commit, got %d", detail.Insertions)
	}
	if !strings.Contains(detail.Diff, "README.md") {
		t.Errorf("expected root commit diff to include README.md, got: %s", detail.Diff)
	}
	if !strings.Contains(detail.Diff, "+hello") {
		t.Errorf("expected root commit diff to show added content, got: %s", detail.Diff)
	}
}

func TestShowNonRootCommitStatsAndDiff(t *testing.T) {
	r := NewRunner()
	dir := repoWithCommit(t)

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	gitCmd(t, dir, "add", "README.md")
	gitCmd(t, dir, "commit", "-m", "add a line")

	commit, err := r.LastCommit(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	detail, err := r.Show(dir, commit.Hash)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if detail.Message != "add a line" {
		t.Errorf("expected message %q, got %q", "add a line", detail.Message)
	}
	if detail.FilesChanged != 1 {
		t.Errorf("expected 1 file changed, got %d", detail.FilesChanged)
	}
	if detail.Insertions != 1 {
		t.Errorf("expected 1 insertion, got %d", detail.Insertions)
	}
	if detail.Deletions != 0 {
		t.Errorf("expected 0 deletions, got %d", detail.Deletions)
	}
	if !strings.Contains(detail.Diff, "+world") {
		t.Errorf("expected diff to contain added line, got: %s", detail.Diff)
	}
}

func TestShowNonExistentHashReturnsErrNotFound(t *testing.T) {
	r := NewRunner()
	dir := repoWithCommit(t)

	_, err := r.Show(dir, "0000000000000000000000000000000000dead")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
