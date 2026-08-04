package git

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDiscardFileModified(t *testing.T) {
	r := NewRunner()
	dir := repoWithCommit(t)

	path := filepath.Join(dir, "README.md")
	if err := os.WriteFile(path, []byte("hacked\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if err := r.DiscardFile(dir, "README.md"); err != nil {
		t.Fatalf("DiscardFile: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(got) != "hello\n" {
		t.Errorf("expected file restored to HEAD content, got %q", got)
	}

	status, err := r.Status(dir)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !status.Clean {
		t.Errorf("expected clean working tree after discard, got %+v", status.Files)
	}
}

func TestDiscardFileStagedModified(t *testing.T) {
	r := NewRunner()
	dir := repoWithCommit(t)

	path := filepath.Join(dir, "README.md")
	if err := os.WriteFile(path, []byte("staged change\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	gitCmd(t, dir, "add", "README.md")
	// Further unstaged edit on top of the staged one, to confirm discard
	// reverts both the index and the working tree back to HEAD, not just
	// the working tree back to whatever's staged.
	if err := os.WriteFile(path, []byte("staged change\nplus more\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if err := r.DiscardFile(dir, "README.md"); err != nil {
		t.Fatalf("DiscardFile: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(got) != "hello\n" {
		t.Errorf("expected file restored to HEAD content, got %q", got)
	}

	status, err := r.Status(dir)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !status.Clean {
		t.Errorf("expected clean working tree after discard, got %+v", status.Files)
	}
}

func TestDiscardFileDeleted(t *testing.T) {
	r := NewRunner()
	dir := repoWithCommit(t)

	path := filepath.Join(dir, "README.md")
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove file: %v", err)
	}

	if err := r.DiscardFile(dir, "README.md"); err != nil {
		t.Fatalf("DiscardFile: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected deleted file restored, stat error: %v", err)
	}
}

func TestDiscardFileNewlyStaged(t *testing.T) {
	r := NewRunner()
	dir := repoWithCommit(t)

	path := filepath.Join(dir, "new.txt")
	if err := os.WriteFile(path, []byte("new\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	gitCmd(t, dir, "add", "new.txt")

	if err := r.DiscardFile(dir, "new.txt"); err != nil {
		t.Fatalf("DiscardFile: %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected newly staged file removed, stat error: %v", err)
	}

	status, err := r.Status(dir)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !status.Clean {
		t.Errorf("expected clean working tree after discard, got %+v", status.Files)
	}
}

func TestDiscardFileUntracked(t *testing.T) {
	r := NewRunner()
	dir := repoWithCommit(t)

	path := filepath.Join(dir, "scratch.txt")
	if err := os.WriteFile(path, []byte("scratch\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if err := r.DiscardFile(dir, "scratch.txt"); err != nil {
		t.Fatalf("DiscardFile: %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected untracked file removed, stat error: %v", err)
	}
}

func TestDiscardFileUntrackedDirectory(t *testing.T) {
	r := NewRunner()
	dir := repoWithCommit(t)

	subdir := filepath.Join(dir, "newdir")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subdir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if err := r.DiscardFile(dir, "newdir"); err != nil {
		t.Fatalf("DiscardFile: %v", err)
	}

	if _, err := os.Stat(subdir); !os.IsNotExist(err) {
		t.Errorf("expected untracked directory removed, stat error: %v", err)
	}
}

func TestDiscardFileDoesNotTouchOtherFiles(t *testing.T) {
	r := NewRunner()
	dir := repoWithCommit(t)

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "untouched.txt"), []byte("keep me\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if err := r.DiscardFile(dir, "README.md"); err != nil {
		t.Fatalf("DiscardFile: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "untouched.txt"))
	if err != nil {
		t.Fatalf("expected untracked sibling file to survive: %v", err)
	}
	if string(got) != "keep me\n" {
		t.Errorf("sibling file content changed unexpectedly: %q", got)
	}
}

func TestDiscardAll(t *testing.T) {
	r := NewRunner()
	dir := repoWithCommit(t)

	// Commit a second tracked file so there's something to delete below
	// alongside the modify/add/untracked cases.
	if err := os.WriteFile(filepath.Join(dir, "tracked2.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	gitCmd(t, dir, "add", "tracked2.txt")
	gitCmd(t, dir, "commit", "-m", "add tracked2")

	// Now dirty everything: modify one tracked file, delete another, add a
	// new staged file, and drop an untracked file (plus an untracked dir).
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "tracked2.txt")); err != nil {
		t.Fatalf("remove file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	gitCmd(t, dir, "add", "new.txt")
	if err := os.WriteFile(filepath.Join(dir, "scratch.txt"), []byte("scratch\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "newdir"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "newdir", "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	status, err := r.Status(dir)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.Clean || len(status.Files) == 0 {
		t.Fatalf("expected a dirty working tree before DiscardAll, got %+v", status)
	}

	if err := r.DiscardAll(dir); err != nil {
		t.Fatalf("DiscardAll: %v", err)
	}

	status, err = r.Status(dir)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !status.Clean {
		t.Errorf("expected clean working tree after DiscardAll, got %+v", status.Files)
	}

	for _, removed := range []string{"new.txt", "scratch.txt", "newdir"} {
		if _, err := os.Stat(filepath.Join(dir, removed)); !os.IsNotExist(err) {
			t.Errorf("expected %q removed after DiscardAll, stat error: %v", removed, err)
		}
	}
	for _, restored := range []string{"README.md", "tracked2.txt"} {
		if _, err := os.Stat(filepath.Join(dir, restored)); err != nil {
			t.Errorf("expected %q restored after DiscardAll: %v", restored, err)
		}
	}
}

// Regression: a stale (or genuinely held) .git/index.lock must surface as
// ErrIndexLocked, not a generic error - and, critically, must NOT be
// silently swallowed by DiscardFile's untracked-fallback path. Before this
// was fixed, a checkout failure for any reason (not just "path unknown to
// git") fell through to `git clean`, which no-ops on a tracked file and
// would report success without having discarded anything.
func TestDiscardFileModified_StaleIndexLock(t *testing.T) {
	r := NewRunner()
	dir := repoWithCommit(t)

	path := filepath.Join(dir, "README.md")
	if err := os.WriteFile(path, []byte("dirty\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	lockPath := filepath.Join(dir, ".git", "index.lock")
	if err := os.WriteFile(lockPath, nil, 0o644); err != nil {
		t.Fatalf("create stale lock: %v", err)
	}
	t.Cleanup(func() { os.Remove(lockPath) })

	err := r.DiscardFile(dir, "README.md")
	if err == nil {
		t.Fatal("expected an error while the index is locked, got nil")
	}
	if !errors.Is(err, ErrIndexLocked) {
		t.Errorf("expected errors.Is(err, ErrIndexLocked), got: %v", err)
	}

	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read file: %v", readErr)
	}
	if string(got) != "dirty\n" {
		t.Errorf("expected file left untouched (discard failed), got %q", got)
	}
}

func TestDiscardAll_StaleIndexLock(t *testing.T) {
	r := NewRunner()
	dir := repoWithCommit(t)

	lockPath := filepath.Join(dir, ".git", "index.lock")
	if err := os.WriteFile(lockPath, nil, 0o644); err != nil {
		t.Fatalf("create stale lock: %v", err)
	}
	t.Cleanup(func() { os.Remove(lockPath) })

	err := r.DiscardAll(dir)
	if err == nil {
		t.Fatal("expected an error while the index is locked, got nil")
	}
	if !errors.Is(err, ErrIndexLocked) {
		t.Errorf("expected errors.Is(err, ErrIndexLocked), got: %v", err)
	}
}
