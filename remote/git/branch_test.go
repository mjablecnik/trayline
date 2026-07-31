package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func gitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}

// repoWithCommit creates a repo with a single commit on branch "main".
func repoWithCommit(t *testing.T) string {
	t.Helper()
	dir := initRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	gitCmd(t, dir, "add", "README.md")
	gitCmd(t, dir, "commit", "-m", "initial commit")
	return dir
}

func TestIsRepo(t *testing.T) {
	r := NewRunner()
	repoDir := initRepo(t)
	if !r.IsRepo(repoDir) {
		t.Error("expected IsRepo true for initialized repo")
	}

	nonRepoDir := t.TempDir()
	if r.IsRepo(nonRepoDir) {
		t.Error("expected IsRepo false for plain directory")
	}
}

func TestCurrentBranch(t *testing.T) {
	r := NewRunner()
	dir := repoWithCommit(t)

	branch, err := r.CurrentBranch(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if branch != "main" {
		t.Errorf("expected branch %q, got %q", "main", branch)
	}
}

func TestBranches(t *testing.T) {
	r := NewRunner()
	dir := repoWithCommit(t)
	gitCmd(t, dir, "branch", "feature-x")

	branches, err := r.Branches(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[string]bool{"main": false, "feature-x": false}
	for _, b := range branches {
		if _, ok := want[b]; !ok {
			t.Errorf("unexpected branch %q", b)
		}
		want[b] = true
	}
	for name, found := range want {
		if !found {
			t.Errorf("expected branch %q in list, got %v", name, branches)
		}
	}
}

func TestRemoteURLNoOrigin(t *testing.T) {
	r := NewRunner()
	dir := repoWithCommit(t)

	url, err := r.RemoteURL(dir)
	if err != nil {
		t.Fatalf("expected no error when origin is unset, got: %v", err)
	}
	if url != "" {
		t.Errorf("expected empty remote URL, got %q", url)
	}
}

func TestRemoteURLWithOrigin(t *testing.T) {
	r := NewRunner()
	dir := repoWithCommit(t)
	gitCmd(t, dir, "remote", "add", "origin", "https://example.com/repo.git")

	url, err := r.RemoteURL(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "https://example.com/repo.git" {
		t.Errorf("expected remote URL, got %q", url)
	}
}
