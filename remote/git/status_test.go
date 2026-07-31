package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatusCleanRepo(t *testing.T) {
	r := NewRunner()
	dir := repoWithCommit(t)

	result, err := r.Status(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Clean {
		t.Error("expected clean working tree")
	}
	if len(result.Files) != 0 {
		t.Errorf("expected no files, got %+v", result.Files)
	}
	if result.Summary != (StatusSummary{}) {
		t.Errorf("expected zero summary, got %+v", result.Summary)
	}
}

func TestStatusModifiedFile(t *testing.T) {
	r := NewRunner()
	dir := repoWithCommit(t)

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	result, err := r.Status(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Clean {
		t.Fatal("expected dirty working tree")
	}
	if len(result.Files) != 1 {
		t.Fatalf("expected 1 changed file, got %+v", result.Files)
	}
	fc := result.Files[0]
	if fc.Path != "README.md" {
		t.Errorf("expected path %q, got %q", "README.md", fc.Path)
	}
	if fc.Status != "modified" {
		t.Errorf("expected status %q, got %q", "modified", fc.Status)
	}
	if fc.Insertions != 1 || fc.Deletions != 0 {
		t.Errorf("expected 1 insertion 0 deletions, got %d/%d", fc.Insertions, fc.Deletions)
	}
	if fc.Diff == nil || !strings.Contains(*fc.Diff, "+world") {
		t.Errorf("expected diff to contain added line, got %v", fc.Diff)
	}
	if result.Summary.FilesChanged != 1 || result.Summary.Insertions != 1 || result.Summary.Deletions != 0 {
		t.Errorf("unexpected summary: %+v", result.Summary)
	}
}

func TestStatusAddedFile(t *testing.T) {
	r := NewRunner()
	dir := repoWithCommit(t)

	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new content\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	gitCmd(t, dir, "add", "new.txt")

	result, err := r.Status(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Files) != 1 {
		t.Fatalf("expected 1 changed file, got %+v", result.Files)
	}
	fc := result.Files[0]
	if fc.Path != "new.txt" {
		t.Errorf("expected path %q, got %q", "new.txt", fc.Path)
	}
	if fc.Status != "added" {
		t.Errorf("expected status %q, got %q", "added", fc.Status)
	}
	if fc.Diff == nil || !strings.Contains(*fc.Diff, "+new content") {
		t.Errorf("expected diff to contain new content, got %v", fc.Diff)
	}
}

func TestStatusDeletedFile(t *testing.T) {
	r := NewRunner()
	dir := repoWithCommit(t)

	if err := os.Remove(filepath.Join(dir, "README.md")); err != nil {
		t.Fatalf("remove file: %v", err)
	}

	result, err := r.Status(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Files) != 1 {
		t.Fatalf("expected 1 changed file, got %+v", result.Files)
	}
	fc := result.Files[0]
	if fc.Status != "deleted" {
		t.Errorf("expected status %q, got %q", "deleted", fc.Status)
	}
	if fc.Deletions != 1 {
		t.Errorf("expected 1 deletion, got %d", fc.Deletions)
	}
	if fc.Diff == nil || !strings.Contains(*fc.Diff, "-hello") {
		t.Errorf("expected diff to show removed line, got %v", fc.Diff)
	}
}

func TestStatusUntrackedFile(t *testing.T) {
	r := NewRunner()
	dir := repoWithCommit(t)

	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("stuff\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	result, err := r.Status(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Files) != 1 {
		t.Fatalf("expected 1 changed file, got %+v", result.Files)
	}
	fc := result.Files[0]
	if fc.Path != "untracked.txt" {
		t.Errorf("expected path %q, got %q", "untracked.txt", fc.Path)
	}
	if fc.Status != "untracked" {
		t.Errorf("expected status %q, got %q", "untracked", fc.Status)
	}
	if fc.Diff != nil {
		t.Errorf("expected nil diff for untracked file, got %v", *fc.Diff)
	}
	if fc.Insertions != 0 || fc.Deletions != 0 {
		t.Errorf("expected zero counts for untracked file, got %d/%d", fc.Insertions, fc.Deletions)
	}
}

func TestStatusTruncatesLargeDiff(t *testing.T) {
	r := NewRunner()
	dir := repoWithCommit(t)

	var b strings.Builder
	line := strings.Repeat("x", 100) + "\n"
	for b.Len() < maxDiffSize+1024 {
		b.WriteString(line)
	}
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	gitCmd(t, dir, "add", "big.txt")

	result, err := r.Status(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Files) != 1 {
		t.Fatalf("expected 1 changed file, got %+v", result.Files)
	}
	fc := result.Files[0]
	if fc.Diff == nil || *fc.Diff != "(diff too large)" {
		t.Errorf("expected truncated diff placeholder, got %v", fc.Diff)
	}
}
