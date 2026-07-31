package git

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLastCommit(t *testing.T) {
	r := NewRunner()
	dir := repoWithCommit(t)

	commit, err := r.LastCommit(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if commit.Message != "initial commit" {
		t.Errorf("expected message %q, got %q", "initial commit", commit.Message)
	}
	if commit.Author != "Test" {
		t.Errorf("expected author %q, got %q", "Test", commit.Author)
	}
	if commit.Hash == "" {
		t.Error("expected non-empty hash")
	}
	if commit.Date == "" {
		t.Error("expected non-empty date")
	}
}

func TestLastCommitEmptyRepoErrors(t *testing.T) {
	r := NewRunner()
	dir := initRepo(t)

	if _, err := r.LastCommit(dir); err == nil {
		t.Fatal("expected error for repo with no commits")
	}
}

func TestHasUncommittedChangesFalse(t *testing.T) {
	r := NewRunner()
	dir := repoWithCommit(t)

	dirty, err := r.HasUncommittedChanges(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dirty {
		t.Error("expected clean working tree")
	}
}

func TestHasUncommittedChangesTrue(t *testing.T) {
	r := NewRunner()
	dir := repoWithCommit(t)

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	dirty, err := r.HasUncommittedChanges(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !dirty {
		t.Error("expected dirty working tree after modification")
	}
}

func TestHasUncommittedChangesUntrackedFile(t *testing.T) {
	r := NewRunner()
	dir := repoWithCommit(t)

	if err := os.WriteFile(filepath.Join(dir, "new-file.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	dirty, err := r.HasUncommittedChanges(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !dirty {
		t.Error("expected dirty working tree with untracked file")
	}
}
