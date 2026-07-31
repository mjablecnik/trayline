package git

import (
	"os"
	"path/filepath"
	"testing"
)

// repoWithTree creates a repo with a nested directory structure and a
// commit, returning the repo dir.
func repoWithTree(t *testing.T) string {
	t.Helper()
	dir := initRepo(t)

	mustWrite := func(rel, content string) {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}

	mustWrite("README.md", "hello\n")
	mustWrite("main.go", "package main\n")
	mustWrite("src/util.go", "package src\n")
	mustWrite("src/nested/deep.go", "package nested\n")

	gitCmd(t, dir, "add", ".")
	gitCmd(t, dir, "commit", "-m", "add tree")
	return dir
}

func TestTreeRootListsDirsFirstThenFilesAlphabetically(t *testing.T) {
	r := NewRunner()
	dir := repoWithTree(t)

	entries, err := r.Tree(dir, "main", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d: %+v", len(entries), entries)
	}
	if entries[0].Name != "src" || entries[0].Type != "directory" {
		t.Errorf("expected first entry to be directory %q, got %+v", "src", entries[0])
	}
	if entries[1].Name != "README.md" || entries[1].Type != "file" {
		t.Errorf("expected second entry %q, got %+v", "README.md", entries[1])
	}
	if entries[2].Name != "main.go" || entries[2].Type != "file" {
		t.Errorf("expected third entry %q, got %+v", "main.go", entries[2])
	}
	if entries[1].Size <= 0 {
		t.Errorf("expected non-zero file size for README.md, got %d", entries[1].Size)
	}
}

func TestTreeSubdirectory(t *testing.T) {
	r := NewRunner()
	dir := repoWithTree(t)

	entries, err := r.Tree(dir, "main", "src")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(entries), entries)
	}
	if entries[0].Name != "nested" || entries[0].Type != "directory" {
		t.Errorf("expected first entry %q dir, got %+v", "nested", entries[0])
	}
	if entries[1].Name != "util.go" {
		t.Errorf("expected second entry %q, got %+v", "util.go", entries[1])
	}
}

func TestTreeNonExistentPathReturnsErrNotFound(t *testing.T) {
	r := NewRunner()
	dir := repoWithTree(t)

	_, err := r.Tree(dir, "main", "does/not/exist")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestTreeNonExistentRefReturnsErrNotFound(t *testing.T) {
	r := NewRunner()
	dir := repoWithTree(t)

	_, err := r.Tree(dir, "no-such-ref", "")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestBlobReturnsFileContent(t *testing.T) {
	r := NewRunner()
	dir := repoWithTree(t)

	content, err := r.Blob(dir, "main", "README.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(content) != "hello\n" {
		t.Errorf("expected %q, got %q", "hello\n", string(content))
	}
}

func TestBlobNestedPath(t *testing.T) {
	r := NewRunner()
	dir := repoWithTree(t)

	content, err := r.Blob(dir, "main", "src/nested/deep.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(content) != "package nested\n" {
		t.Errorf("expected %q, got %q", "package nested\n", string(content))
	}
}

func TestBlobNonExistentPathReturnsErrNotFound(t *testing.T) {
	r := NewRunner()
	dir := repoWithTree(t)

	_, err := r.Blob(dir, "main", "does-not-exist.txt")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestBlobOnDirectoryPathReturnsErrNotFound(t *testing.T) {
	r := NewRunner()
	dir := repoWithTree(t)

	_, err := r.Blob(dir, "main", "src")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound for directory path, got %v", err)
	}
}
